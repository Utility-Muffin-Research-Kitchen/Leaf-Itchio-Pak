//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bodgit/sevenzip"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

var (
	ownerGame = itchio.Game{Title: "Moss Garden", URL: "https://dev.itch.io/moss-garden", IsFree: true}
	otherGame = itchio.Game{Title: "Other Game", URL: "https://dev.itch.io/other-game", IsFree: true}
)

// plantOwnedFile writes path with data and records it in inv as a download
// of game from upload, the way an earlier install leaves it.
func plantOwnedFile(t *testing.T, inv *inventory.Inventory, game itchio.Game, upload, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	inv.Add(game.URL, inventory.Entry{Title: game.Title, IsFree: true}, inventory.DownloadedFile{
		Filename: upload, DestPath: path, DownloadedAt: time.Now(), FileType: inventory.FileTypeROM,
	})
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", filepath.Base(path), err)
	}
	return string(data)
}

func entryPaths(t *testing.T, inv *inventory.Inventory, gameURL string) []string {
	t.Helper()
	entry, ok := inv.Lookup(gameURL)
	if !ok {
		t.Fatalf("no inventory entry for %s", gameURL)
	}
	paths := make([]string, 0, len(entry.Files))
	for _, file := range entry.Files {
		paths = append(paths, file.DestPath)
	}
	return paths
}

func runDirect(t *testing.T, cfg *settings.Config, game itchio.Game, inv *inventory.Inventory, invPath string,
	srv interface{ URL() string }, upload roms.Upload, dest string) *DirectDownloadWorker {
	t.Helper()
	worker := NewDirectDownloadWorker(itchio.NewClientWithBase(srv.URL()), cfg, game, &itchio.GameDetail{}, upload, dest, inv, invPath)
	waitForWorker(t, func() bool { return worker.loadState() != dlDownloading })
	if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
		t.Fatalf("direct download = %+v", snapshot)
	}
	return worker
}

type serverURL string

func (s serverURL) URL() string { return string(s) }

// A download that keeps its upload name must not replace another game's file
// of the same name (review finding R18-1).
func TestDirectDownloadKeepsAnotherGamesFileWithTheSameName(t *testing.T) {
	for _, unified := range []bool{false, true} {
		primary, _ := transactionPaths(t)
		gbDir := filepath.Join(primary, "Roms", "GB")
		inv, invPath := collisionInventory(t)
		mine := filepath.Join(gbDir, "game.gb")
		plantOwnedFile(t, inv, ownerGame, "game.gb", mine, "OWNER-GAME")

		srv := freeFileServer(t, map[string][]byte{"1": gbROM("OTHER")})
		worker := runDirect(t, &settings.Config{UnifiedNaming: unified}, otherGame, inv, invPath,
			serverURL(srv.URL), freeUpload(srv, "1", "game.gb"), mine)

		if got := readFile(t, mine); got != "OWNER-GAME" {
			t.Fatalf("unified=%v: the other game's download replaced game.gb", unified)
		}
		if sameFAT32(worker.dest, mine) {
			t.Fatalf("unified=%v: the other game recorded %s", unified, worker.dest)
		}
		want := "Other Game - game.gb"
		if unified {
			want = "Other Game.gb"
		}
		if filepath.Base(worker.dest) != want {
			t.Fatalf("unified=%v: saved as %q, want %q", unified, filepath.Base(worker.dest), want)
		}
		if paths := entryPaths(t, inv, ownerGame.URL); len(paths) != 1 || !sameFAT32(paths[0], mine) {
			t.Fatalf("unified=%v: owner records %v", unified, paths)
		}

		// Downloading it again replaces the other game's own copy in place.
		again := runDirect(t, &settings.Config{UnifiedNaming: unified}, otherGame, inv, invPath,
			serverURL(srv.URL), freeUpload(srv, "1", "game.gb"), inv.ExistingDestPath(otherGame.URL, "game.gb"))
		if again.dest != worker.dest {
			t.Fatalf("unified=%v: reinstall saved %q, want %q", unified, filepath.Base(again.dest), filepath.Base(worker.dest))
		}
		if got := filesIn(t, gbDir); len(got) != 2 {
			t.Fatalf("unified=%v: GB folder after reinstall = %v", unified, keys(got))
		}
	}
}

// The reported case: two PlayStation games whose discs share upload names.
func TestMultiDownloadKeepsAnotherGamesDiscs(t *testing.T) {
	primary, _ := transactionPaths(t)
	psDir := filepath.Join(primary, "Roms", "PS")
	inv, invPath := collisionInventory(t)
	plantOwnedFile(t, inv, ownerGame, "disc1.chd", filepath.Join(psDir, "disc1.chd"), "OWNER-1")
	plantOwnedFile(t, inv, ownerGame, "disc2.chd", filepath.Join(psDir, "disc2.chd"), "OWNER-2")

	srv := freeFileServer(t, map[string][]byte{"1": []byte("OTHER-1"), "2": []byte("OTHER-2")})
	downloads := []romDownload{
		{Upload: freeUpload(srv, "1", "disc1.chd"), DestPath: filepath.Join(psDir, "disc1.chd")},
		{Upload: freeUpload(srv, "2", "disc2.chd"), DestPath: filepath.Join(psDir, "disc2.chd")},
	}
	worker := NewMultiDownloadWorker(itchio.NewClientWithBase(srv.URL), &settings.Config{UnifiedNaming: true},
		otherGame, &itchio.GameDetail{}, downloads, inv, invPath)
	waitForWorker(t, func() bool { return worker.loadState() != multiDLDownloading })
	if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
		t.Fatalf("multi download = %+v", snapshot)
	}

	got := filesIn(t, psDir)
	want := map[string]string{
		"disc1.chd": "OWNER-1", "disc2.chd": "OWNER-2",
		"Other Game - disc1.chd": "OTHER-1", "Other Game - disc2.chd": "OTHER-2",
	}
	if len(got) != len(want) {
		t.Fatalf("PS folder = %v, want %v", keys(got), want)
	}
	for name, data := range want {
		if got[name] != data {
			t.Fatalf("PS folder %s = %q, want %q (folder %v)", name, got[name], data, keys(got))
		}
	}
	for _, path := range entryPaths(t, inv, otherGame.URL) {
		for _, owned := range entryPaths(t, inv, ownerGame.URL) {
			if sameFAT32(path, owned) {
				t.Fatalf("both games record %s", path)
			}
		}
	}
}

func runArchiveFor(t *testing.T, primary string, game itchio.Game, inv *inventory.Inventory, invPath,
	filename string, data []byte, cfg *settings.Config) *ArchiveDownloadWorker {
	t.Helper()
	srv := freeFileServer(t, map[string][]byte{"9": data})
	var manifest roms.ZIPManifest
	if strings.HasSuffix(filename, ".7z") {
		reader, err := sevenzip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		manifest = manifestFrom7z(reader.File)
	} else {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		manifest = manifestFromZIP(reader.File)
	}
	plan := ZIPPlan{
		Upload: freeUpload(srv, "9", filename), CDNURL: srv.URL + "/cdn/9",
		Manifest: manifest, DownloadROMs: true,
	}
	worker := NewArchiveDownloadWorker(itchio.NewClientWithBase(srv.URL), cfg, game, &itchio.GameDetail{}, plan, inv, invPath)
	waitForWorker(t, func() bool { state := worker.loadState(); return state == zipDLDone || state == zipDLError })
	if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
		t.Fatalf("%s: archive = %+v", filename, snapshot)
	}
	return worker
}

// Archive entries keep their names unless unified naming renames them; the
// extraction must not land on another game's file either way.
func TestArchiveKeepsAnotherGamesROMWithTheSameName(t *testing.T) {
	for _, unified := range []bool{false, true} {
		for name, data := range map[string][]byte{
			"other.zip": zipOf(t, map[string][]byte{"leafbound.gb": gbROM("OTHER")}),
			// romsCollision7z holds extra.dat and leafbound.gb.
			"other.7z": decode7z(t, romsCollision7z),
		} {
			primary, _ := transactionPaths(t)
			gbDir := filepath.Join(primary, "Roms", "GB")
			inv, invPath := collisionInventory(t)
			mine := filepath.Join(gbDir, "leafbound.gb")
			plantOwnedFile(t, inv, ownerGame, "leafbound.gb", mine, "OWNER-GAME")

			worker := runArchiveFor(t, primary, otherGame, inv, invPath, name, data, &settings.Config{UnifiedNaming: unified})
			if got := readFile(t, mine); got != "OWNER-GAME" {
				t.Fatalf("%s unified=%v: extraction replaced the owner's leafbound.gb", name, unified)
			}
			for _, path := range worker.extracted {
				if sameFAT32(path, mine) {
					t.Fatalf("%s unified=%v: the other game recorded %s", name, unified, path)
				}
			}
			if len(worker.extracted) == 0 {
				t.Fatalf("%s unified=%v: nothing extracted (skipped %v)", name, unified, worker.skipped)
			}
		}
	}
}

// Defence in depth: a file two inventory entries reference (left by the
// earlier bug) is not deleted from under the other game.
func TestCatManageKeepsAFileAnotherGameReferences(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	shared := filepath.Join(sources[0].RomsPath, "PSX", "disc1.chd")
	addManagedROM(t, inv, ownerGame.URL, ownerGame.Title, shared)
	inv.Add(otherGame.URL, inventory.Entry{Title: otherGame.Title}, inventory.DownloadedFile{
		Filename: "disc1.chd", DestPath: shared,
	})

	flow, model, err := NewCatManageFlow(inv, cfgPath, ownerGame.URL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	gone, err := flow.Confirm(model)
	if err != nil || !gone {
		t.Fatalf("delete = %v, %v", gone, err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatalf("the file the other game uses was deleted: %v", err)
	}
	if _, ok := inv.Lookup(ownerGame.URL); ok {
		t.Fatal("the deleting game kept its record")
	}
	if paths := entryPaths(t, inv, otherGame.URL); len(paths) != 1 || paths[0] != shared {
		t.Fatalf("other game records %v", paths)
	}
}

func sameFAT32(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// A second build for the same system keeps the first one: both stay, under
// distinct names (review finding R18-2, decision D2).
func TestDirectDownloadKeepsBothBuildsOfOneSystem(t *testing.T) {
	primary, _ := transactionPaths(t)
	gbaDir := filepath.Join(primary, "Roms", "GBA")
	inv, invPath := collisionInventory(t)
	game := itchio.Game{Title: "Glory Hunters", URL: "https://dev.itch.io/glory-hunters", IsFree: true}
	srv := freeFileServer(t, map[string][]byte{"1": []byte("PLAIN"), "2": []byte("EZ-IV")})
	cfg := &settings.Config{UnifiedNaming: true}

	plain := runDirect(t, cfg, game, inv, invPath, serverURL(srv.URL),
		freeUpload(srv, "1", "glory.gba"), filepath.Join(gbaDir, "glory.gba"))
	ez := runDirect(t, cfg, game, inv, invPath, serverURL(srv.URL),
		freeUpload(srv, "2", "glory_ez4.gba"), filepath.Join(gbaDir, "glory_ez4.gba"))

	if filepath.Base(plain.dest) != "Glory Hunters.gba" || filepath.Base(ez.dest) != "Glory Hunters (glory_ez4).gba" {
		t.Fatalf("saved %q and %q", filepath.Base(plain.dest), filepath.Base(ez.dest))
	}
	got := filesIn(t, gbaDir)
	if len(got) != 2 || got["Glory Hunters.gba"] != "PLAIN" || got["Glory Hunters (glory_ez4).gba"] != "EZ-IV" {
		t.Fatalf("GBA folder = %v", keys(got))
	}
	if paths := entryPaths(t, inv, game.URL); len(paths) != 2 {
		t.Fatalf("inventory rows = %v, want one per build", paths)
	}

	again := runDirect(t, cfg, game, inv, invPath, serverURL(srv.URL),
		freeUpload(srv, "2", "glory_ez4.gba"), inv.ExistingDestPath(game.URL, "glory_ez4.gba"))
	if again.dest != ez.dest || len(filesIn(t, gbaDir)) != 2 {
		t.Fatalf("reinstalling the second build saved %q", filepath.Base(again.dest))
	}
}
