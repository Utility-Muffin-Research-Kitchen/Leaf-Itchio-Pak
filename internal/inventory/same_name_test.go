package inventory_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// F23: a record stands for one file on the card. Two files that share only
// their name, such as main.p8 in two folders of one archive, are two records.

const sameNameGame = "https://dev.itch.io/moss-garden"

// member is a record of an archive member the app extracted to path.
func member(path, archive, memberPath string, at time.Time, fingerprint string) inventory.DownloadedFile {
	file := p8(path, archive)
	file.SourceMember = memberPath
	file.DownloadedAt = at
	file.UploadFingerprint = fingerprint
	return file
}

// installSet records a Pico-8 game's files the way the archive worker does,
// then acknowledges the upload as a whole.
func installSet(inv *inventory.Inventory, at time.Time, fingerprint string) []string {
	const game = "/leaf/Roms/PICO8/Moss Garden/"
	files := []inventory.DownloadedFile{
		member(game+"main.p8", "moss.zip", "game/main.p8", at, fingerprint),
		member(game+"world2/main.p8", "moss.zip", "game/world2/main.p8", at, fingerprint),
		member(game+"lib.lua", "moss.zip", "game/lib.lua", at, fingerprint),
	}
	written := make([]string, 0, len(files))
	for _, file := range files {
		inv.Add(sameNameGame, inventory.Entry{Title: "Moss Garden"}, file)
		written = append(written, file.DestPath)
	}
	inv.CommitUploadInstall(sameNameGame, inventory.UploadInstall{
		Filename: "moss.zip", Fingerprint: fingerprint, Written: written,
	})
	return written
}

func recordedPaths(t *testing.T, inv *inventory.Inventory) []string {
	t.Helper()
	entry, ok := inv.Lookup(sameNameGame)
	if !ok {
		t.Fatal("no entry for the game")
	}
	paths := make([]string, 0, len(entry.Files))
	for _, file := range entry.Files {
		paths = append(paths, file.DestPath)
	}
	return paths
}

func TestAddKeepsSameNamedFilesInDifferentFolders(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	written := installSet(inv, time.Now(), "upd:1/10")

	entry, _ := inv.Lookup(sameNameGame)
	if len(entry.Files) != len(written) {
		t.Fatalf("records = %v, want one for each of %v", recordedPaths(t, inv), written)
	}
	members := map[string]string{}
	for _, file := range entry.Files {
		members[file.DestPath] = file.SourceMember
	}
	for path, want := range map[string]string{
		"/leaf/Roms/PICO8/Moss Garden/main.p8":        "game/main.p8",
		"/leaf/Roms/PICO8/Moss Garden/world2/main.p8": "game/world2/main.p8",
	} {
		if members[path] != want {
			t.Fatalf("%s records member %q, want %q (records: %v)", path, members[path], want, members)
		}
	}
}

func TestReinstallingAnArchiveKeepsOneRecordPerFile(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	first := time.Now().Add(-time.Hour)
	installSet(inv, first, "upd:1/10")
	second := first.Add(time.Hour)
	installSet(inv, second, "upd:1/10")

	entry, _ := inv.Lookup(sameNameGame)
	if len(entry.Files) != 3 {
		t.Fatalf("records after a reinstall = %v, want 3", recordedPaths(t, inv))
	}
	for _, file := range entry.Files {
		if !file.DownloadedAt.Equal(second) {
			t.Fatalf("%s kept the first install's record", file.DestPath)
		}
		if file.LeftOver {
			t.Fatalf("%s is flagged left over after the reinstall wrote it", file.DestPath)
		}
	}
}

func TestAnUpdateThatWritesTheSamePathsReplacesTheirRecords(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	first := time.Now().Add(-time.Hour)
	installSet(inv, first, "upd:1/10")
	installSet(inv, first.Add(time.Hour), "upd:2/12")

	entry, _ := inv.Lookup(sameNameGame)
	if len(entry.Files) != 3 {
		t.Fatalf("records after an update = %v, want 3", recordedPaths(t, inv))
	}
	for _, file := range entry.Files {
		if file.UploadFingerprint != "upd:2/12" {
			t.Fatalf("%s records fingerprint %q, want the update's", file.DestPath, file.UploadFingerprint)
		}
	}
}

// FAT32 ignores letter case, so a path that differs only in case is the file
// already recorded.
func TestAddReplacesTheRecordOfTheSameFileInAnotherLetterCase(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	const game = "/leaf/Roms/PICO8/Moss Garden/"
	inv.Add(sameNameGame, inventory.Entry{Title: "Moss Garden"}, p8(game+"Main.p8", "moss.zip"))
	inv.Add(sameNameGame, inventory.Entry{Title: "Moss Garden"}, p8(game+"main.p8", "moss.zip"))

	entry, _ := inv.Lookup(sameNameGame)
	if len(entry.Files) != 1 || entry.Files[0].DestPath != game+"main.p8" {
		t.Fatalf("records = %v, want the one file under its latest spelling", recordedPaths(t, inv))
	}
}

// The same upload on the other card is another file: Manage must be able to
// show and delete both.
func TestAddKeepsTheSameNamedFileOnAnotherCard(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add(sameNameGame, inventory.Entry{Title: "Moss Garden"}, p8("/leaf/Roms/PICO8/Moss Garden/main.p8", "moss.zip"))
	inv.Add(sameNameGame, inventory.Entry{Title: "Moss Garden"}, p8("/secondary/Roms/PICO8/Moss Garden/main.p8", "moss.zip"))

	if paths := recordedPaths(t, inv); len(paths) != 2 {
		t.Fatalf("records = %v, want one for each card", paths)
	}
}

// VerifyAndClean runs at every launch. It must not fold the records back into
// one, which would undo the fix at the next start.
func TestVerifyAndCleanKeepsSameNamedFilesInDifferentFolders(t *testing.T) {
	dir := t.TempDir()
	game := filepath.Join(dir, "Moss Garden")
	first, second := filepath.Join(game, "main.p8"), filepath.Join(game, "world2", "main.p8")
	for _, path := range []string{first, second} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("pico-8 cartridge\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		sameNameGame: {GameURL: sameNameGame, Title: "Moss Garden", Files: []inventory.DownloadedFile{
			member(first, "moss.zip", "game/main.p8", now, ""),
			member(second, "moss.zip", "game/world2/main.p8", now, ""),
		}},
	}}
	if removed := inv.VerifyAndClean(filepath.Join(dir, "inventory.json")); removed != 0 {
		t.Fatalf("removed %d record(s), want none", removed)
	}
	entry, _ := inv.Lookup(sameNameGame)
	if len(entry.Files) != 2 || entry.Files[0].DestPath != first || entry.Files[1].DestPath != second {
		t.Fatalf("records = %v, want both files in their recorded order", recordedPaths(t, inv))
	}
}

// A reinstall is planned from the path of the earlier install. With copies in
// two places, the one installed last is the one a reinstall replaces.
func TestExistingDestPathFollowsTheLatestInstall(t *testing.T) {
	now := time.Now()
	latest := p8("/secondary/Roms/PICO8/Moss Garden/main.p8", "")
	latest.DownloadedAt = now
	older := p8("/leaf/Roms/PICO8/Moss Garden/main.p8", "")
	older.DownloadedAt = now.Add(-time.Hour)
	for name, order := range map[string][]inventory.DownloadedFile{
		"latest first": {latest, older}, "latest last": {older, latest},
	} {
		inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
		for _, file := range order {
			inv.Add(sameNameGame, inventory.Entry{Title: "Moss Garden"}, file)
		}
		if got := inv.ExistingDestPath(sameNameGame, "main.p8"); got != latest.DestPath {
			t.Fatalf("%s: ExistingDestPath = %q, want %q", name, got, latest.DestPath)
		}
	}
}
