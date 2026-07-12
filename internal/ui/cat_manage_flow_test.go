//go:build !headless

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func configureManageFixture(t *testing.T, sources leaf.SourceList, catalog *leaf.Catalog) {
	t.Helper()
	configs := make([]roms.SourcePathConfig, 0, len(sources))
	for _, source := range sources {
		dirs := make(map[string]string)
		images := make(map[string]string)
		for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"} {
			dir, err := catalog.ROMDir(source, id)
			if err != nil {
				t.Fatal(err)
			}
			dirs[id] = dir
			imageDir, err := catalog.ImageDir(source, id)
			if err != nil {
				t.Fatal(err)
			}
			images[id] = imageDir
		}
		configs = append(configs, roms.SourcePathConfig{
			SourceID: source.ID, Root: source.Root, MusicRoot: source.MusicPath,
			StatesRoot: source.StatesPath, SystemDirs: dirs, ImageDirs: images,
		})
	}
	if err := roms.ConfigurePaths(roms.PathConfig{
		SystemDirs: configs[0].SystemDirs, ImageDirs: configs[0].ImageDirs, SourceID: sources[0].ID, PrimaryRoot: sources[0].Root,
		MusicRoot: sources[0].MusicPath, StatesRoot: sources[0].StatesPath, Sources: configs,
	}); err != nil {
		t.Fatal(err)
	}
}

func addManagedROM(t *testing.T, inv *inventory.Inventory, gameURL, title, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv.Add(gameURL, inventory.Entry{Title: title, Author: "UMRK"}, inventory.DownloadedFile{
		Filename: filepath.Base(path), DestPath: path,
	})
}

func TestCatManageDeletesSourceOwnedFile(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/game"
	romPath := filepath.Join(sources[0].RomsPath, "GBC", "Game.gbc")
	addManagedROM(t, inv, gameURL, "Game", romPath)
	imageDir, err := catalog.ImageDir(sources[0], "GBC")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userArt := filepath.Join(imageDir, "Game.png")
	if err := os.WriteFile(userArt, []byte("user art"), 0o644); err != nil {
		t.Fatal(err)
	}
	flow, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if model.Items[0].Badge != "ROM" || !model.Items[0].Enabled {
		t.Fatalf("managed row = %#v", model.Items[0])
	}
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if model.State != appui.ManageConfirm {
		t.Fatalf("state = %v, want confirm", model.State)
	}
	allGone, err := flow.Confirm(model)
	if err != nil || !allGone {
		t.Fatalf("delete = %v, %v", allGone, err)
	}
	if _, err := os.Stat(romPath); !os.IsNotExist(err) {
		t.Fatalf("ROM still present: %v", err)
	}
	if _, ok := inv.Lookup(gameURL); ok {
		t.Fatal("empty inventory entry remains")
	}
	if _, err := os.Stat(userArt); err != nil {
		t.Fatalf("user-owned artwork was deleted: %v", err)
	}
	if !flow.TakeLibraryScanRequest() {
		t.Fatal("committed ROM deletion did not request a library rescan")
	}
	if flow.TakeLibraryScanRequest() {
		t.Fatal("one deletion batch requested more than one library rescan")
	}
}

func TestCatManageMusicDeletionDoesNotRequestGameLibraryScan(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/music"
	musicPath := filepath.Join(sources[0].MusicPath, "Album", "track.ogg")
	if err := os.MkdirAll(filepath.Dir(musicPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(musicPath, []byte("music"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv.Add(gameURL, inventory.Entry{Title: "Album"}, inventory.DownloadedFile{
		Filename: "track.ogg", DestPath: musicPath, FileType: inventory.FileTypeMusic,
	})
	flow, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Confirm(model); err != nil {
		t.Fatal(err)
	}
	if flow.TakeLibraryScanRequest() {
		t.Fatal("music-only deletion requested a game-library rescan")
	}
}

func TestCatManageRechecksCardBeforeDeletion(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/game"
	romPath := filepath.Join(sources[1].RomsPath, "GBC", "Game.gbc")
	addManagedROM(t, inv, gameURL, "Game", romPath)
	flow, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sources[1].Root); err != nil {
		t.Fatal(err)
	}
	if allGone, err := flow.Confirm(model); err == nil || allGone {
		t.Fatalf("delete after removal = %v, %v; want blocked", allGone, err)
	}
	entry, ok := inv.Lookup(gameURL)
	if !ok || len(entry.Files) != 1 {
		t.Fatal("blocked deletion changed the inventory")
	}
}

func TestCatManageDeletesOnlyAppOwnedArtwork(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/owned-art"
	romPath := filepath.Join(sources[0].RomsPath, "GBC", "Game.gbc")
	artPath := inventory.CanonicalArtworkPath(romPath)
	for path, data := range map[string]string{romPath: "rom", artPath: "app art"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inv.Add(gameURL, inventory.Entry{Title: "Game", CoverURL: "cover"}, inventory.DownloadedFile{
		Filename: "Game.gbc", DestPath: romPath, ArtworkPath: artPath,
		ArtworkHash: "owned-hash", ArtworkCreated: true,
	})
	flow, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Confirm(model); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artPath); !os.IsNotExist(err) {
		t.Fatalf("app-owned artwork remains after ROM deletion: %v", err)
	}
}

func TestCatManageDoesNotOfferUnsafePSXDescriptorRename(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/psx"
	addManagedROM(t, inv, gameURL, "PSX Game", filepath.Join(sources[0].RomsPath, "PSX", "disc.cue"))
	addManagedROM(t, inv, gameURL, "PSX Game", filepath.Join(sources[0].RomsPath, "PSX", "disc.bin"))
	_, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range model.Items {
		if item.Kind == appui.ManageItemRename {
			t.Fatalf("unsafe PSX rename row exposed: %#v", item)
		}
	}
}

func TestCatManageDisablesFilesOnRemovedSource(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/game"
	romPath := filepath.Join(sources[1].RomsPath, "GBC", "Game.gbc")
	addManagedROM(t, inv, gameURL, "Game", romPath)
	if err := os.RemoveAll(sources[1].Root); err != nil {
		t.Fatal(err)
	}
	flow, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if model.Items[0].Enabled || model.Items[0].Badge != "UNAVAILABLE" {
		t.Fatalf("removed-source row = %#v", model.Items[0])
	}
	for _, item := range model.Items {
		if item.Kind == appui.ManageItemDeleteAll && item.Enabled {
			t.Fatal("delete-all enabled with an unavailable source")
		}
	}
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if model.State != appui.ManageError || model.Message != "Secondary SD is not mounted" {
		t.Fatalf("unavailable explanation = state %v message %q", model.State, model.Message)
	}
}

func TestCatRenameKeepsSaveAndStatesOnROMSource(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/game"
	romPath := filepath.Join(sources[0].RomsPath, "GBC", "Original.gbc")
	addManagedROM(t, inv, gameURL, "Leaf Title", romPath)
	saveDir := filepath.Join(sources[0].SavesPath, "GBC")
	stateDir := filepath.Join(sources[0].StatesPath, "GBC-gambatte")
	for path, data := range map[string]string{
		filepath.Join(saveDir, "Original.srm"):         "save",
		filepath.Join(stateDir, "Original.state1"):     "state",
		filepath.Join(stateDir, "Original.state1.png"): "thumb",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	flow, model, err := NewCatRenameFlow(inv, cfgPath, gameURL, 0, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := flow.Confirm(model); err != nil || model.State != appui.RenameConfirmSaves {
		t.Fatalf("ROM confirm = state %v, %v", model.State, err)
	}
	if err := flow.Confirm(model); err != nil || model.State != appui.RenameConfirmStates {
		t.Fatalf("save confirm = state %v, %v", model.State, err)
	}
	if err := flow.Confirm(model); err != nil || model.State != appui.RenameDone {
		t.Fatalf("state confirm = state %v, %v", model.State, err)
	}
	for _, path := range []string{
		filepath.Join(sources[0].RomsPath, "GBC", "Leaf Title.gbc"),
		filepath.Join(saveDir, "Leaf Title.srm"),
		filepath.Join(stateDir, "Leaf Title.state1"),
		filepath.Join(stateDir, "Leaf Title.state1.png"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("renamed path %s: %v", path, err)
		}
	}
	entry, _ := inv.Lookup(gameURL)
	if entry.Files[0].SourceID != "primary" || entry.Files[0].RelativePath != "Roms/GBC/Leaf Title.gbc" {
		t.Fatalf("renamed identity = %+v", entry.Files[0])
	}
	if !flow.TakeLibraryScanRequest() {
		t.Fatal("committed ROM rename did not request a library rescan")
	}
	if flow.TakeLibraryScanRequest() {
		t.Fatal("one rename transaction requested more than one library rescan")
	}
}

func TestCatRenameRechecksCardBeforeMutation(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/game"
	romPath := filepath.Join(sources[1].RomsPath, "GBC", "Original.gbc")
	addManagedROM(t, inv, gameURL, "Leaf Title", romPath)
	flow, model, err := NewCatRenameFlow(inv, cfgPath, gameURL, 0, sources)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sources[1].Root); err != nil {
		t.Fatal(err)
	}
	if err := flow.Confirm(model); err == nil {
		t.Fatal("rename continued after selected card removal")
	}
	entry, ok := inv.Lookup(gameURL)
	if !ok || entry.Files[0].DestPath != romPath {
		t.Fatal("blocked rename changed the inventory")
	}
}

func TestCatRenameMovesOnlyAppOwnedArtwork(t *testing.T) {
	for _, created := range []bool{false, true} {
		t.Run(fmt.Sprintf("created_%v", created), func(t *testing.T) {
			sources, catalog, cfgPath := destinationFixture(t)
			configureManageFixture(t, sources, catalog)
			inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
			gameURL := "https://example.invalid/rename-art"
			romPath := filepath.Join(sources[0].RomsPath, "GBC", "Original.gbc")
			oldArt := inventory.CanonicalArtworkPath(romPath)
			for path, data := range map[string]string{romPath: "rom", oldArt: "art"} {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			inv.Add(gameURL, inventory.Entry{Title: "Leaf Title", CoverURL: "cover"}, inventory.DownloadedFile{
				Filename: "Original.gbc", DestPath: romPath, ArtworkPath: oldArt,
				ArtworkHash: "same-hash", ArtworkCreated: created,
			})
			flow, model, err := NewCatRenameFlow(inv, cfgPath, gameURL, 0, sources)
			if err != nil {
				t.Fatal(err)
			}
			if err := flow.Confirm(model); err != nil || model.State != appui.RenameDone {
				t.Fatalf("rename = state %v, %v", model.State, err)
			}
			entry, _ := inv.Lookup(gameURL)
			file := entry.Files[0]
			newArt := inventory.CanonicalArtworkPath(file.DestPath)
			if created {
				if file.ArtworkPath != newArt || file.ArtworkHash != "same-hash" || !file.ArtworkCreated {
					t.Fatalf("app-owned artwork metadata = %+v", file)
				}
				if _, err := os.Stat(newArt); err != nil {
					t.Fatalf("app-owned artwork was not renamed: %v", err)
				}
				if _, err := os.Stat(oldArt); !os.IsNotExist(err) {
					t.Fatalf("old app-owned artwork remains: %v", err)
				}
			} else {
				if file.ArtworkPath != oldArt || file.ArtworkHash != "same-hash" || file.ArtworkCreated {
					t.Fatalf("user-owned artwork metadata = %+v", file)
				}
				if _, err := os.Stat(oldArt); err != nil {
					t.Fatalf("user-owned artwork moved: %v", err)
				}
				if _, err := os.Stat(newArt); !os.IsNotExist(err) {
					t.Fatalf("new art unexpectedly created for user-owned file: %v", err)
				}
			}
		})
	}
}

func TestDiscoverRenamePairsRejectsConflict(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"Old.srm", "New.srm"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := discoverRenamePairs(root, "Old.gbc", "New.gbc", false); err == nil {
		t.Fatal("existing related-file target was not rejected")
	}
}
