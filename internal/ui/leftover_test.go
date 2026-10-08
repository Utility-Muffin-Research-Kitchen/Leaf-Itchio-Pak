//go:build !headless

package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func seedMusicRow(t *testing.T, inv *inventory.Inventory, path, sourceArchive string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv.Add(collisionGame.URL, inventory.Entry{Title: collisionGame.Title, IsFree: true}, inventory.DownloadedFile{
		Filename: filepath.Base(path), DestPath: path, DownloadedAt: time.Now(),
		FileType: inventory.FileTypeMusic, SourceArchive: sourceArchive,
	})
}

func leftOverPaths(t *testing.T, inv *inventory.Inventory) map[string]bool {
	t.Helper()
	entry, ok := inv.Lookup(collisionGame.URL)
	if !ok {
		t.Fatal("no inventory entry")
	}
	out := map[string]bool{}
	for _, file := range entry.Files {
		if file.LeftOver {
			out[filepath.Base(file.DestPath)] = true
		}
	}
	return out
}

// Reinstalling a soundtrack installed by an older version leaves the old flat
// tracks next to the new ones. They are listed as left over, never deleted
// (review finding R19-3, decision D4).
func TestArchiveMusicReinstallListsTracksFromAnOlderVersion(t *testing.T) {
	primary, _ := transactionPaths(t)
	inv, invPath := collisionInventory(t)
	music := filepath.Join(primary, "Music", "Leafbound")
	seedMusicRow(t, inv, filepath.Join(music, "01 Theme.ogg"), "")
	seedMusicRow(t, inv, filepath.Join(music, "02 Battle.ogg"), "")
	seedMusicRow(t, inv, filepath.Join(music, "99 Credits.ogg"), "")

	runArchiveFor(t, primary, collisionGame, inv, invPath, "leafbound.zip", fourTrackSoundtrack(t), &settings.Config{}, musicOnly)

	if got := leftOverPaths(t, inv); len(got) != 2 || !got["01 Theme.ogg"] || !got["02 Battle.ogg"] {
		t.Fatalf("left over = %v, want the two old flat tracks", got)
	}
	for _, name := range []string{"01 Theme.ogg", "02 Battle.ogg"} {
		if _, err := os.Stat(filepath.Join(music, name)); err != nil {
			t.Fatalf("%s was deleted: %v", name, err)
		}
	}
	if got := readFile(t, filepath.Join(music, "99 Credits.ogg")); got != "CREDITS" {
		t.Fatalf("rewritten track = %q", got)
	}
}

// Only the reinstalled upload's own files of the kinds it installed are
// listed: another upload's tracks and this upload's ROMs are not.
func TestArchiveReinstallListsOnlyThatUploadsFiles(t *testing.T) {
	primary, _ := transactionPaths(t)
	inv, invPath := collisionInventory(t)
	music := filepath.Join(primary, "Music", "Leafbound")
	seedMusicRow(t, inv, filepath.Join(music, "dropped.ogg"), "leafbound.zip")
	seedMusicRow(t, inv, filepath.Join(music, "other.ogg"), "leafbound-ost.zip")
	plantOwnedFile(t, inv, collisionGame, "Leafbound.gb", filepath.Join(primary, "Roms", "GB", "Leafbound.gb"), "ROM")
	if !inv.UpdateFile(collisionGame.URL, filepath.Join(primary, "Roms", "GB", "Leafbound.gb"), inventory.DownloadedFile{
		Filename: "Leafbound.gb", DestPath: filepath.Join(primary, "Roms", "GB", "Leafbound.gb"),
		FileType: inventory.FileTypeROM, ContentKind: inventory.ContentKindROM, SourceArchive: "leafbound.zip",
	}) {
		t.Fatal("seed ROM row")
	}

	data := zipOf(t, map[string][]byte{"Soundtrack/kept.ogg": []byte("KEPT")})
	runArchiveFor(t, primary, collisionGame, inv, invPath, "leafbound.zip", data, &settings.Config{}, musicOnly)

	if got := leftOverPaths(t, inv); len(got) != 1 || !got["dropped.ogg"] {
		t.Fatalf("left over = %v, want only dropped.ogg", got)
	}
}

// Manage lists left-over files and deletes only them (decision D4: offered,
// never deleted automatically).
func TestCatManageOffersLeftOverFiles(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	album := filepath.Join(sources[0].MusicPath, "Leafbound")
	current, old := filepath.Join(album, "cd1", "01 Theme.ogg"), filepath.Join(album, "01 Theme.ogg")
	for _, path := range []string{current, old} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("track"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv.Add(collisionGame.URL, inventory.Entry{Title: collisionGame.Title}, inventory.DownloadedFile{
		Filename: "cd1/01 Theme.ogg", DestPath: current, FileType: inventory.FileTypeMusic, SourceArchive: "leafbound.zip",
	})
	inv.Add(collisionGame.URL, inventory.Entry{Title: collisionGame.Title}, inventory.DownloadedFile{
		Filename: "01 Theme.ogg", DestPath: old, FileType: inventory.FileTypeMusic, SourceArchive: "leafbound.zip",
	})
	inv.MarkLeftOver(collisionGame.URL, "leafbound.zip", inventory.ContentKindMusic, []string{current})

	flow, model, err := NewCatManageFlow(inv, cfgPath, collisionGame.URL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := inv.Lookup(collisionGame.URL)
	action := -1
	for index, item := range model.Items {
		if item.Kind == appui.ManageItemFile && (item.Badge == "OLD") != (entry.Files[item.FileIndex].DestPath == old) {
			t.Fatalf("row %+v: only the left-over track has an OLD badge", item)
		}
		if item.Kind == appui.ManageItemDeleteLeftOver {
			action = index
		}
	}
	if action < 0 || model.Items[action].Badge != "1 OLD" {
		t.Fatalf("items = %+v, want a left-over action", model.Items)
	}
	model.Cursor = action
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if model.State != appui.ManageConfirm || len(model.PromptLines) == 0 || model.PromptLines[0] != "Left over from an older version" {
		t.Fatalf("confirm = %q %q", model.PromptTitle, model.PromptLines)
	}
	if _, err := flow.Confirm(model); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("left-over track still present: %v", err)
	}
	if _, err := os.Stat(current); err != nil {
		t.Fatalf("current track was deleted: %v", err)
	}
}
