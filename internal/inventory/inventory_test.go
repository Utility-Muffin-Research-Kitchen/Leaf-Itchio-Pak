package inventory_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func TestMain(m *testing.M) {
	err := roms.ConfigurePaths(roms.PathConfig{
		SystemDirs: map[string]string{
			"GB": "/leaf/Roms/GB", "GBC": "/leaf/Roms/GBC", "GBA": "/leaf/Roms/GBA",
			"FC": "/leaf/Roms/NES", "MD": "/leaf/Roms/GENESIS", "PICO8": "/leaf/Roms/PICO8", "PS": "/leaf/Roms/PSX",
		},
		ImageDirs: map[string]string{
			"GB": "/leaf/Images/GB", "GBC": "/leaf/Images/GBC", "GBA": "/leaf/Images/GBA",
			"FC": "/leaf/Images/NES", "MD": "/leaf/Images/GENESIS", "PICO8": "/leaf/Images/PICO8", "PS": "/leaf/Images/PSX",
		},
		SourceID: "primary", PrimaryRoot: "/leaf", MusicRoot: "/leaf/Music", StatesRoot: "/leaf/States",
		Sources: []roms.SourcePathConfig{
			{
				SourceID: "primary", Root: "/leaf", MusicRoot: "/leaf/Music", StatesRoot: "/leaf/States",
				SystemDirs: map[string]string{
					"GB": "/leaf/Roms/GB", "GBC": "/leaf/Roms/GBC", "GBA": "/leaf/Roms/GBA",
					"FC": "/leaf/Roms/NES", "MD": "/leaf/Roms/GENESIS", "PICO8": "/leaf/Roms/PICO8", "PS": "/leaf/Roms/PSX",
				},
				ImageDirs: map[string]string{
					"GB": "/leaf/Images/GB", "GBC": "/leaf/Images/GBC", "GBA": "/leaf/Images/GBA",
					"FC": "/leaf/Images/NES", "MD": "/leaf/Images/GENESIS", "PICO8": "/leaf/Images/PICO8", "PS": "/leaf/Images/PSX",
				},
			},
			{
				SourceID: "secondary_sd", Root: "/secondary", MusicRoot: "/secondary/Music", StatesRoot: "/secondary/States",
				SystemDirs: map[string]string{
					"GB": "/secondary/Roms/GB", "GBC": "/secondary/Roms/GBC", "GBA": "/secondary/Roms/GBA",
					"FC": "/secondary/Roms/NES", "MD": "/secondary/Roms/GENESIS", "PICO8": "/secondary/Roms/PICO8", "PS": "/secondary/Roms/PSX",
				},
				ImageDirs: map[string]string{
					"GB": "/secondary/Images/GB", "GBC": "/secondary/Images/GBC", "GBA": "/secondary/Images/GBA",
					"FC": "/secondary/Images/NES", "MD": "/secondary/Images/GENESIS", "PICO8": "/secondary/Images/PICO8", "PS": "/secondary/Images/PSX",
				},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestLoad_MissingFile_ReturnsEmpty(t *testing.T) {
	inv, err := inventory.Load("/nonexistent/path/inventory.json")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(inv.Entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(inv.Entries))
	}
}

func TestLoad_CorruptFile_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	inv, err := inventory.Load(path)
	if !errors.Is(err, inventory.ErrUnsupportedSchema) {
		t.Fatalf("Load error = %v, want ErrUnsupportedSchema", err)
	}
	if len(inv.Entries) != 0 {
		t.Errorf("expected empty entries on corrupt file, got %d", len(inv.Entries))
	}
	if _, err := os.Stat(path + ".corrupt.bak"); err != nil {
		t.Fatalf("corrupt backup: %v", err)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")

	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{
		GameURL:  "https://dev.itch.io/game",
		Title:    "My Game",
		Author:   "dev",
		CoverURL: "https://img.itch.zone/cover.png",
	}, inventory.DownloadedFile{
		Filename:     "my-game.gb",
		DestPath:     "/mnt/SDCARD/Roms/Game Boy (GB)/my-game.gb",
		DownloadedAt: time.Now().Truncate(time.Second),
	})

	if err := inv.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := inventory.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e, ok := loaded.Lookup("https://dev.itch.io/game")
	if !ok {
		t.Fatal("expected entry, not found")
	}
	if e.Title != "My Game" {
		t.Errorf("Title = %q, want %q", e.Title, "My Game")
	}
	if len(e.Files) != 1 {
		t.Fatalf("Files len = %d, want 1", len(e.Files))
	}
	if e.Files[0].Filename != "my-game.gb" {
		t.Errorf("Filename = %q, want %q", e.Files[0].Filename, "my-game.gb")
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")

	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/a", inventory.Entry{Title: "Old"}, inventory.DownloadedFile{Filename: "a.gb", DestPath: "/a.gb"})
	_ = inv.Save(path)

	inv.Add("https://dev.itch.io/b", inventory.Entry{Title: "New"}, inventory.DownloadedFile{Filename: "b.gb", DestPath: "/b.gb"})
	if err := inv.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("tmp file should not linger after successful save")
	}
}

func TestAdd_NewEntry(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{
		Title: "Game", Author: "dev",
	}, inventory.DownloadedFile{Filename: "game.gb", DestPath: "/roms/game.gb"})

	e, ok := inv.Lookup("https://dev.itch.io/game")
	if !ok {
		t.Fatal("entry not found")
	}
	if e.Title != "Game" {
		t.Errorf("Title = %q, want %q", e.Title, "Game")
	}
	if len(e.Files) != 1 {
		t.Errorf("Files len = %d, want 1", len(e.Files))
	}
}

func TestAdd_PopulatesLeafPathIdentity(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{GameID: "42", Title: "Game"}, inventory.DownloadedFile{
		Filename: "game.gb", DestPath: "/leaf/Roms/GB/game.gb", DownloadedAt: time.Now(),
	})
	entry, ok := inv.Lookup("https://dev.itch.io/game")
	if !ok || len(entry.Files) != 1 {
		t.Fatal("expected one inventory file")
	}
	file := entry.Files[0]
	if entry.GameID != "42" || file.SourceID != "primary" || file.RelativePath != "Roms/GB/game.gb" || file.CanonicalSystem != "GB" {
		t.Fatalf("unexpected Leaf identity: entry=%+v file=%+v", entry, file)
	}
	if file.ContentKind != inventory.FileTypeROM || file.OriginalUpload != "game.gb" || file.InstalledName != "game.gb" {
		t.Fatalf("missing normalized inventory fields: %+v", file)
	}
}

func TestAdd_PreservesSecondaryLeafPathIdentity(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{GameID: "42", Title: "Game"}, inventory.DownloadedFile{
		Filename: "game.gbc", DestPath: "/secondary/Roms/GBC/RPG/game.gbc", DownloadedAt: time.Now(),
	})
	entry, ok := inv.Lookup("https://dev.itch.io/game")
	if !ok || len(entry.Files) != 1 {
		t.Fatal("expected one inventory file")
	}
	file := entry.Files[0]
	if file.SourceID != "secondary_sd" || file.RelativePath != "Roms/GBC/RPG/game.gbc" || file.CanonicalSystem != "GBC" {
		t.Fatalf("unexpected secondary identity: %+v", file)
	}
}

func TestAddPreservesArtworkOwnershipWhenRedownloadHasNoNewArt(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	const gameURL = "https://dev.itch.io/game"
	first := inventory.DownloadedFile{
		Filename: "game.gb", DestPath: "/leaf/Roms/GB/game.gb",
		ArtworkPath: "/leaf/Images/GB/game.png", ArtworkHash: "abc123", ArtworkCreated: true,
	}
	inv.Add(gameURL, inventory.Entry{Title: "Game"}, first)
	inv.Add(gameURL, inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename: "game.gb", DestPath: "/leaf/Roms/GB/game.gb",
	})
	entry, ok := inv.Lookup(gameURL)
	if !ok || len(entry.Files) != 1 {
		t.Fatal("redownloaded inventory row is missing")
	}
	file := entry.Files[0]
	if file.ArtworkPath != first.ArtworkPath || file.ArtworkHash != first.ArtworkHash || !file.ArtworkCreated {
		t.Fatalf("redownload lost artwork metadata: %+v", file)
	}
}

func TestArtworkPathForPrefersRecordedPath(t *testing.T) {
	file := inventory.DownloadedFile{
		DestPath:    "/leaf/Roms/PICO8/Game/cart.p8",
		ArtworkPath: "/leaf/Images/PICO8/Game.png",
	}
	if got := inventory.ArtworkPathFor("cover", file); got != file.ArtworkPath {
		t.Fatalf("ArtworkPathFor = %q, want %q", got, file.ArtworkPath)
	}
}

func TestArtworkReferencedOutsideExcludesPendingDeletion(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	artPath := "/leaf/Images/GB/Game.png"
	first := inventory.DownloadedFile{Filename: "Game.gb", DestPath: "/leaf/Roms/GB/Game.gb", ArtworkCreated: true}
	second := inventory.DownloadedFile{Filename: "Game.zip", DestPath: "/leaf/Roms/GB/Game.zip", ArtworkCreated: true}
	inv.Add("one", inventory.Entry{CoverURL: "cover"}, first)
	inv.Add("two", inventory.Entry{CoverURL: "cover"}, second)
	if !inv.ArtworkReferencedOutside(artPath, []inventory.DownloadedFile{first}) {
		t.Fatal("remaining artwork owner was not detected")
	}
	if inv.ArtworkReferencedOutside(artPath, []inventory.DownloadedFile{first, second}) {
		t.Fatal("pending deletion set still counted as an artwork owner")
	}
}

func TestVerifyAndCleanKeepsFileOnUnavailableSource(t *testing.T) {
	root := t.TempDir()
	missingSecondary := filepath.Join(root, "removed-secondary")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename: "game.gbc", DestPath: filepath.Join(missingSecondary, "Roms", "GBC", "game.gbc"),
		SourceID: "secondary_sd", RelativePath: "Roms/GBC/game.gbc", CanonicalSystem: "GBC",
	})
	sources := leaf.SourceList{
		{ID: "primary", Root: root, Primary: true},
		{ID: "secondary_sd", Root: missingSecondary},
	}
	if removed := inv.VerifyAndCleanWithSources(filepath.Join(root, "inventory.json"), sources); removed != 0 {
		t.Fatalf("removed %d unavailable-source files", removed)
	}
	if entry, ok := inv.Lookup("game"); !ok || len(entry.Files) != 1 {
		t.Fatal("unavailable-source inventory row was discarded")
	}
}

func TestVerifyAndCleanRemovesMissingFileOnAvailableSource(t *testing.T) {
	root := t.TempDir()
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename: "game.gbc", DestPath: filepath.Join(root, "Roms", "GBC", "game.gbc"),
		SourceID: "primary", RelativePath: "Roms/GBC/game.gbc", CanonicalSystem: "GBC",
	})
	if removed := inv.VerifyAndCleanWithSources(filepath.Join(root, "inventory.json"),
		leaf.SourceList{{ID: "primary", Root: root, Primary: true}}); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := inv.Lookup("game"); ok {
		t.Fatal("missing file on mounted source remained in inventory")
	}
}

func TestAdd_ExistingEntry_AppendsFile(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "v1.gb", DestPath: "/v1.gb"})
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "v2.gbc", DestPath: "/v2.gbc"})

	e, _ := inv.Lookup("https://dev.itch.io/game")
	if len(e.Files) != 2 {
		t.Errorf("Files len = %d, want 2", len(e.Files))
	}
}

func TestAdd_DeduplicatesByDestPath(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "game.gb", DestPath: "/roms/game.gb"})
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "game.gb", DestPath: "/roms/game.gb"})

	e, _ := inv.Lookup("https://dev.itch.io/game")
	if len(e.Files) != 1 {
		t.Errorf("Files len = %d, want 1 (dedup)", len(e.Files))
	}
}

func TestRemove(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "game.gb", DestPath: "/roms/game.gb"})
	inv.Remove("https://dev.itch.io/game")

	if _, ok := inv.Lookup("https://dev.itch.io/game"); ok {
		t.Error("entry should have been removed")
	}
}

func TestLookup_Missing(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	_, ok := inv.Lookup("https://nobody.itch.io/nothing")
	if ok {
		t.Error("Lookup of missing key should return false")
	}
}

func TestIsPresent_NoEntry(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	if inv.IsPresent("https://dev.itch.io/game") {
		t.Error("IsPresent should be false when no entry exists")
	}
}

func TestIsPresent_EntryWithFiles(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	if !inv.IsPresent("https://dev.itch.io/game") {
		t.Error("IsPresent should be true when entry has files")
	}
}

func TestVerifyAndClean_SetsVerifiedAtWhenAllFilesExist(t *testing.T) {
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.gb")
	if err := os.WriteFile(romPath, []byte("rom"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "inventory.json")
	before := time.Now()

	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		"https://dev.itch.io/game": {
			GameURL: "https://dev.itch.io/game",
			Title:   "Game",
			Files:   []inventory.DownloadedFile{{Filename: "game.gb", DestPath: romPath}},
		},
	}}
	inv.VerifyAndClean(path)

	e, ok := inv.Lookup("https://dev.itch.io/game")
	if !ok {
		t.Fatal("entry should still exist")
	}
	if !e.VerifiedAt.After(before) {
		t.Errorf("VerifiedAt = %v, want a time after %v", e.VerifiedAt, before)
	}
}

func TestVerifyAndClean_KeepsExistingFiles(t *testing.T) {
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.gb")
	if err := os.WriteFile(romPath, []byte("rom"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "inventory.json")

	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		"https://dev.itch.io/game": {
			GameURL: "https://dev.itch.io/game",
			Title:   "Game",
			Files:   []inventory.DownloadedFile{{Filename: "game.gb", DestPath: romPath}},
		},
	}}
	removed := inv.VerifyAndClean(path)
	if removed != 0 {
		t.Errorf("removed = %d, want 0", removed)
	}
	if _, ok := inv.Lookup("https://dev.itch.io/game"); !ok {
		t.Error("entry should still exist")
	}
}

func TestVerifyAndClean_RemovesStaleFile(t *testing.T) {
	dir := t.TempDir()
	romPath := filepath.Join(dir, "real.gb")
	if err := os.WriteFile(romPath, []byte("rom"), 0644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "inventory.json")

	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		"https://dev.itch.io/game": {
			GameURL: "https://dev.itch.io/game",
			Title:   "Game",
			Files: []inventory.DownloadedFile{
				{Filename: "real.gb", DestPath: romPath},
				{Filename: "gone.gb", DestPath: "/nonexistent/gone.gb"},
			},
		},
	}}
	removed := inv.VerifyAndClean(path)
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	e, ok := inv.Lookup("https://dev.itch.io/game")
	if !ok {
		t.Fatal("entry should still exist after partial file removal")
	}
	if len(e.Files) != 1 {
		t.Errorf("Files len = %d, want 1", len(e.Files))
	}
	if e.Files[0].Filename != "real.gb" {
		t.Errorf("kept wrong file: %q", e.Files[0].Filename)
	}
}

func TestVerifyAndClean_RemovesEmptyEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")

	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		"https://dev.itch.io/gone": {
			GameURL: "https://dev.itch.io/gone",
			Title:   "Gone",
			Files:   []inventory.DownloadedFile{{Filename: "gone.gb", DestPath: "/nonexistent/gone.gb"}},
		},
	}}
	removed := inv.VerifyAndClean(path)
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	if _, ok := inv.Lookup("https://dev.itch.io/gone"); ok {
		t.Error("entry should have been removed")
	}
}

func TestVerifyAndClean_DeduplicatesSameFilename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")

	// Three on-disk files for the same upload — simulates the pre-fix duplicate bug.
	older := filepath.Join(dir, "Capybara Village (2).gb")
	older2 := filepath.Join(dir, "Capybara Village (3).gb")
	newest := filepath.Join(dir, "Capybara Village.gb")
	for _, p := range []string{older, older2, newest} {
		os.WriteFile(p, []byte("ROM"), 0644)
	}

	t1 := time.Now().Add(-2 * time.Hour)
	t2 := time.Now().Add(-time.Hour)
	t3 := time.Now()

	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		"https://dev.itch.io/capybara": {
			GameURL: "https://dev.itch.io/capybara",
			Title:   "Capybara Village",
			Files: []inventory.DownloadedFile{
				{Filename: "Capybara-Village-Update1.gb", DestPath: older, DownloadedAt: t1},
				{Filename: "Capybara-Village-Update1.gb", DestPath: older2, DownloadedAt: t2},
				{Filename: "Capybara-Village-Update1.gb", DestPath: newest, DownloadedAt: t3},
			},
		},
	}}
	removed := inv.VerifyAndClean(path)
	if removed != 2 {
		t.Errorf("removed = %d, want 2 (duplicates)", removed)
	}
	e, ok := inv.Lookup("https://dev.itch.io/capybara")
	if !ok {
		t.Fatal("entry should still exist")
	}
	if len(e.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(e.Files))
	}
	if e.Files[0].DestPath != newest {
		t.Errorf("kept wrong file: got %s, want %s", e.Files[0].DestPath, newest)
	}
}

func TestRemoveFile_PartialRemoval(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "v1.gb", DestPath: "/v1.gb"})
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "v2.gbc", DestPath: "/v2.gbc"})

	allGone := inv.RemoveFile("https://dev.itch.io/game", "/v1.gb")
	if allGone {
		t.Error("allGone should be false when one file remains")
	}
	e, ok := inv.Lookup("https://dev.itch.io/game")
	if !ok {
		t.Fatal("entry should still exist")
	}
	if len(e.Files) != 1 {
		t.Fatalf("Files len = %d, want 1", len(e.Files))
	}
	if e.Files[0].Filename != "v2.gbc" {
		t.Errorf("wrong file kept: %q", e.Files[0].Filename)
	}
}

func TestRemoveFile_LastFileRemovesEntry(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{Filename: "game.gb", DestPath: "/game.gb"})

	allGone := inv.RemoveFile("https://dev.itch.io/game", "/game.gb")
	if !allGone {
		t.Error("allGone should be true when last file is removed")
	}
	if _, ok := inv.Lookup("https://dev.itch.io/game"); ok {
		t.Error("entry should have been removed from inventory")
	}
}

func TestRemoveFile_UnknownURL(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}

	allGone := inv.RemoveFile("https://nobody.itch.io/nothing", "/some.gb")
	if !allGone {
		t.Error("allGone should be true for unknown game URL")
	}
}

func TestCoverArtPath_WithPNGCover(t *testing.T) {
	got := inventory.CoverArtPath(
		"https://img.itch.zone/abc/cover.png",
		"/leaf/Roms/GB/my-game.gb",
	)
	// Cover art is always stored as .png regardless of source format.
	want := "/leaf/Images/GB/my-game.png"
	if got != want {
		t.Errorf("CoverArtPath = %q, want %q", got, want)
	}
}

func TestCoverArtPath_EmptyCoverURL(t *testing.T) {
	got := inventory.CoverArtPath("", "/roms/game.gb")
	if got != "" {
		t.Errorf("empty coverURL should return empty string, got %q", got)
	}
}

func TestCoverArtPath_NoExtensionInURL(t *testing.T) {
	got := inventory.CoverArtPath(
		"https://img.itch.zone/abc/coverimage",
		"/leaf/Roms/GB/game.gb",
	)
	// Always .png regardless of whether source URL has an extension.
	want := "/leaf/Images/GB/game.png"
	if got != want {
		t.Errorf("CoverArtPath = %q, want %q", got, want)
	}
}

func TestCoverArtPath_FullStemPreserved(t *testing.T) {
	// Jawaka looks up cover art by the full ROM stem (including [v1.2]).
	got := inventory.CoverArtPath(
		"https://img.itch.zone/abc/cover.png",
		"/leaf/Roms/GBC/Kero Kero Cowboy [v1.2].gbc",
	)
	want := "/leaf/Images/GBC/Kero Kero Cowboy [v1.2].png"
	if got != want {
		t.Errorf("CoverArtPath = %q, want %q", got, want)
	}
}

// ── HasPendingUpdates ────────────────────────────────────────────────────────

func TestHasPendingUpdates_NoEntry(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	if inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want false for missing entry")
	}
}

func TestHasPendingUpdates_NoUpstreamFiles(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	if inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want false when no upstream files recorded")
	}
}

func TestHasPendingUpdates_AllDownloaded(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	inv.SetUpstreamFiles("https://dev.itch.io/game",
		[]inventory.UpstreamFile{{Filename: "g.gb", UploadID: "1", SeenAt: time.Now()}})
	if inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want false when all upstream files are downloaded")
	}
}

func TestHasPendingUpdates_NewFile(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	// First check: only g.gb is available (no pending update yet).
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "g.gb", UploadID: "1", SeenAt: time.Now()},
	})
	// Second check: developer published g-v2.gb — genuinely new.
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "g.gb", UploadID: "1", SeenAt: time.Now()},
		{Filename: "g-v2.gb", UploadID: "2", SeenAt: time.Now()},
	})
	if !inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want true when upstream has genuinely new file not in downloaded set")
	}
}

func TestHasPendingUpdates_DismissedFile(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	seenAt := time.Now().Add(-time.Hour)
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "g.gb", UploadID: "1", SeenAt: seenAt},
		{Filename: "g-v2.gb", UploadID: "2", SeenAt: seenAt},
	})
	inv.DismissUpdate("https://dev.itch.io/game")
	if inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want false after dismiss")
	}
}

func TestHasPendingUpdates_NewFileAfterDismiss(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	oldSeenAt := time.Now().Add(-time.Hour)
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "g-v2.gb", UploadID: "2", SeenAt: oldSeenAt},
	})
	inv.DismissUpdate("https://dev.itch.io/game")

	newSeenAt := time.Now().Add(time.Hour)
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "g-v2.gb", UploadID: "2", SeenAt: oldSeenAt},
		{Filename: "g-v3.gb", UploadID: "3", SeenAt: newSeenAt},
	})
	if !inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want true when new file appears after dismiss")
	}
}

func TestHasPendingUpdates_FormatPickerExtensionMismatch(t *testing.T) {
	// Format-picker appends ".gbc" to an upload that has no extension.
	// Stored Filename = "Game Boy ROM.gbc", upstream Filename = "Game Boy ROM".
	// The downloaded file must not be treated as a new upstream entry.
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "Game Boy ROM.gbc", DestPath: "/roms/Game Boy ROM.gbc"})
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "Game Boy ROM", UploadID: "1", SeenAt: time.Now()},
	})
	if inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want false — 'Game Boy ROM' is the already-downloaded file, extension just appended by format-picker")
	}
}

func TestHasPendingUpdates_FormatPickerWithGenuineNewFile(t *testing.T) {
	// Same format-picker scenario, but there is also a genuinely new upload.
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "Game Boy ROM.gbc", DestPath: "/roms/Game Boy ROM.gbc"})
	// First check: only the original upload exists.
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "Game Boy ROM", UploadID: "1", SeenAt: time.Now()},
	})
	// Second check: developer added an Analogue Pocket ROM — genuinely new.
	inv.SetUpstreamFiles("https://dev.itch.io/game", []inventory.UpstreamFile{
		{Filename: "Game Boy ROM", UploadID: "1", SeenAt: time.Now()},
		{Filename: "Analogue Pocket ROM", UploadID: "2", SeenAt: time.Now()},
	})
	if !inv.HasPendingUpdates("https://dev.itch.io/game") {
		t.Error("HasPendingUpdates: want true — 'Analogue Pocket ROM' is a genuinely new upload")
	}
}

// ── IsRemoved ────────────────────────────────────────────────────────────────

func TestIsRemoved_NoEntry(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	if inv.IsRemoved("https://dev.itch.io/game") {
		t.Error("IsRemoved: want false for missing entry")
	}
}

func TestIsRemoved_NotRemoved(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	if inv.IsRemoved("https://dev.itch.io/game") {
		t.Error("IsRemoved: want false when GameRemovedAt is zero")
	}
}

func TestIsRemoved_Removed(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	inv.MarkRemoved("https://dev.itch.io/game")
	if !inv.IsRemoved("https://dev.itch.io/game") {
		t.Error("IsRemoved: want true after MarkRemoved")
	}
}

func TestIsRemoved_DismissedRemoval(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	inv.MarkRemoved("https://dev.itch.io/game")
	inv.DismissRemoval("https://dev.itch.io/game")
	if inv.IsRemoved("https://dev.itch.io/game") {
		t.Error("IsRemoved: want false after DismissRemoval")
	}
}

func TestIsRemoved_MarkRemovedIdempotent(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	inv.MarkRemoved("https://dev.itch.io/game")
	inv.DismissRemoval("https://dev.itch.io/game")
	inv.MarkRemoved("https://dev.itch.io/game")
	if inv.IsRemoved("https://dev.itch.io/game") {
		t.Error("IsRemoved: MarkRemoved must be idempotent; badge must stay suppressed after dismiss")
	}
}

func TestMarkRemovedThenReachable_ClearsBoth(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	inv.MarkRemoved("https://dev.itch.io/game")
	inv.MarkReachable("https://dev.itch.io/game")
	if inv.IsRemoved("https://dev.itch.io/game") {
		t.Error("IsRemoved: want false after MarkReachable")
	}
	inv.MarkRemoved("https://dev.itch.io/game")
	if !inv.IsRemoved("https://dev.itch.io/game") {
		t.Error("IsRemoved: want true after fresh MarkRemoved post-MarkReachable")
	}
}

// ── IsFree persistence ───────────────────────────────────────────────────────

func TestAdd_PreservesIsFree(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game",
		inventory.Entry{Title: "G", IsFree: true},
		inventory.DownloadedFile{Filename: "g.gb", DestPath: "/g.gb"})
	e, ok := inv.Lookup("https://dev.itch.io/game")
	if !ok {
		t.Fatal("entry not found")
	}
	if !e.IsFree {
		t.Error("IsFree should be true after Add with IsFree:true")
	}
}

func TestDownloadedFile_UnifiedName_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")

	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"}, inventory.DownloadedFile{
		Filename:    "Game Boy ROM.gb",
		DestPath:    "/roms/Doomslinger Dungeon.gb",
		UnifiedName: true,
	})
	if err := inv.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, _ := inventory.Load(path)
	e, _ := loaded.Lookup("https://dev.itch.io/game")
	if !e.Files[0].UnifiedName {
		t.Error("UnifiedName should survive round-trip")
	}
}

func TestEntry_UnifiedNamingDisabled_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")

	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"}, inventory.DownloadedFile{
		Filename: "g.gb", DestPath: "/g.gb",
	})
	inv.SetUnifiedNamingDisabled("https://dev.itch.io/game", true)
	if err := inv.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, _ := inventory.Load(path)
	e, _ := loaded.Lookup("https://dev.itch.io/game")
	if !e.UnifiedNamingDisabled {
		t.Error("UnifiedNamingDisabled should survive round-trip")
	}
}

func TestUpdateFile_UpdatesDestPathAndUnifiedName(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"}, inventory.DownloadedFile{
		Filename: "Game Boy ROM.gb", DestPath: "/roms/Game Boy ROM.gb",
	})

	ok := inv.UpdateFile("https://dev.itch.io/game", "/roms/Game Boy ROM.gb", inventory.DownloadedFile{
		Filename:    "Game Boy ROM.gb",
		DestPath:    "/roms/Doomslinger Dungeon.gb",
		UnifiedName: true,
	})
	if !ok {
		t.Fatal("UpdateFile returned false")
	}
	e, _ := inv.Lookup("https://dev.itch.io/game")
	if e.Files[0].DestPath != "/roms/Doomslinger Dungeon.gb" {
		t.Errorf("DestPath = %q", e.Files[0].DestPath)
	}
	if !e.Files[0].UnifiedName {
		t.Error("UnifiedName should be true")
	}
}

func TestUpdateFile_UnknownDestPath_ReturnsFalse(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/game", inventory.Entry{Title: "G"}, inventory.DownloadedFile{
		Filename: "g.gb", DestPath: "/g.gb",
	})
	ok := inv.UpdateFile("https://dev.itch.io/game", "/nonexistent.gb", inventory.DownloadedFile{})
	if ok {
		t.Error("UpdateFile should return false for unknown dest path")
	}
}

func TestDownloadedFileFileType_RoundTrip(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	// Add a music file
	inv.Add("http://example.com/game", inventory.Entry{
		GameURL: "http://example.com/game", Title: "Game",
	}, inventory.DownloadedFile{
		Filename: "ost.zip",
		DestPath: "/mnt/SDCARD/Music/Game/track.mp3",
		FileType: inventory.FileTypeMusic,
	})
	// Add a ROM file (different filename to avoid dedup)
	inv.Add("http://example.com/game", inventory.Entry{
		GameURL: "http://example.com/game", Title: "Game",
	}, inventory.DownloadedFile{
		Filename: "game.gbc",
		DestPath: "/mnt/SDCARD/Roms/Game Boy Color (GBC)/Game.gbc",
		FileType: inventory.FileTypeROM,
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "inv.json")
	if err := inv.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := inventory.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := loaded.Lookup("http://example.com/game")
	if !ok {
		t.Fatal("entry not found after load")
	}
	types := map[string]string{}
	for _, f := range entry.Files {
		types[f.DestPath] = f.FileType
	}
	if types["/mnt/SDCARD/Music/Game/track.mp3"] != inventory.FileTypeMusic {
		t.Errorf("music file type = %q, want %q", types["/mnt/SDCARD/Music/Game/track.mp3"], inventory.FileTypeMusic)
	}
	if types["/mnt/SDCARD/Roms/Game Boy Color (GBC)/Game.gbc"] != inventory.FileTypeROM {
		t.Errorf("ROM file type = %q, want %q", types["/mnt/SDCARD/Roms/Game Boy Color (GBC)/Game.gbc"], inventory.FileTypeROM)
	}
}

func TestLoad_OldSchema_BacksUpWithoutPartialImport(t *testing.T) {
	// Versionless NextUI inventory is deliberately not interpreted as Leaf data.
	raw := `{"entries":{"http://example.com/game":{"game_url":"http://example.com/game","title":"Game","author":"","cover_url":"","files":[{"filename":"game.gbc","dest_path":"/mnt/SDCARD/Roms/Game Boy Color (GBC)/Game.gbc","downloaded_at":"2024-01-01T00:00:00Z"}]}}}`
	dir := t.TempDir()
	path := filepath.Join(dir, "inv.json")
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	inv, err := inventory.Load(path)
	if !errors.Is(err, inventory.ErrUnsupportedSchema) {
		t.Fatalf("Load error = %v, want ErrUnsupportedSchema", err)
	}
	if len(inv.Entries) != 0 {
		t.Fatalf("old inventory was partially imported: %d entries", len(inv.Entries))
	}
	if _, err := os.Stat(path + ".schema-0.bak"); err != nil {
		t.Fatalf("schema backup: %v", err)
	}
}

func TestVerifyAndClean_MixedFileTypes(t *testing.T) {
	dir := t.TempDir()

	romPath := filepath.Join(dir, "game.gbc")
	musicPath := filepath.Join(dir, "track.mp3")
	os.WriteFile(romPath, []byte("rom"), 0644)
	os.WriteFile(musicPath, []byte("music"), 0644)

	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("http://g", inventory.Entry{GameURL: "http://g", Title: "G"}, inventory.DownloadedFile{
		Filename: "game.gbc", DestPath: romPath, FileType: inventory.FileTypeROM,
	})
	inv.Add("http://g", inventory.Entry{GameURL: "http://g", Title: "G"}, inventory.DownloadedFile{
		Filename: "ost.zip", DestPath: musicPath, FileType: inventory.FileTypeMusic,
	})

	invPath := filepath.Join(dir, "inv.json")
	inv.Save(invPath)

	// Delete the music file
	os.Remove(musicPath)
	inv.VerifyAndClean(invPath)

	entry, ok := inv.Lookup("http://g")
	if !ok {
		t.Fatal("game entry should still exist (ROM is present)")
	}
	for _, f := range entry.Files {
		if f.FileType == inventory.FileTypeMusic {
			t.Error("music entry should have been pruned")
		}
	}
	if len(entry.Files) != 1 || entry.Files[0].FileType != inventory.FileTypeROM {
		t.Errorf("only ROM entry should remain, got %+v", entry.Files)
	}
}

// ── Bug 1: Re-downloading creates duplicate files ─────────────────────────────

func TestAdd_UpdatesExistingOnSameFilename(t *testing.T) {
	inv, _ := inventory.Load("/nonexistent")
	now := time.Now().Add(-time.Hour)
	inv.Add("https://example.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename:     "game-v1.gb",
		DestPath:     "/roms/Game.gb",
		DownloadedAt: now,
	})
	// Re-download: same upload filename, same dest path — overwrite record (no duplicate)
	later := time.Now()
	inv.Add("https://example.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename:     "game-v1.gb",
		DestPath:     "/roms/Game.gb",
		DownloadedAt: later,
	})
	e, ok := inv.Lookup("https://example.itch.io/game")
	if !ok {
		t.Fatal("entry missing")
	}
	if len(e.Files) != 1 {
		t.Errorf("expected 1 file, got %d (duplicate created)", len(e.Files))
	}
	if !e.Files[0].DownloadedAt.Equal(later) {
		t.Error("DownloadedAt not updated on re-download")
	}
}

func TestAdd_ReplacesDestPathOnSameFilename(t *testing.T) {
	inv, _ := inventory.Load("/nonexistent")
	inv.Add("https://example.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename: "game-v1.gb",
		DestPath: "/roms/Game (raw).gb",
	})
	// Same filename, different dest (unified rename happened)
	inv.Add("https://example.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename: "game-v1.gb",
		DestPath: "/roms/Game.gb",
	})
	e, _ := inv.Lookup("https://example.itch.io/game")
	if len(e.Files) != 1 {
		t.Fatalf("expected 1 file after upsert, got %d", len(e.Files))
	}
	if e.Files[0].DestPath != "/roms/Game.gb" {
		t.Errorf("DestPath not updated: %s", e.Files[0].DestPath)
	}
}

func TestExistingDestPath(t *testing.T) {
	inv, _ := inventory.Load("/nonexistent")
	inv.Add("https://example.itch.io/game", inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
		Filename: "Capybara-Village-Update1.gb",
		DestPath: "/roms/Capybara Village.gb",
	})
	got := inv.ExistingDestPath("https://example.itch.io/game", "Capybara-Village-Update1.gb")
	if got != "/roms/Capybara Village.gb" {
		t.Errorf("ExistingDestPath = %q, want %q", got, "/roms/Capybara Village.gb")
	}
	if inv.ExistingDestPath("https://example.itch.io/game", "other.gb") != "" {
		t.Error("ExistingDestPath should return empty for unknown filename")
	}
	if inv.ExistingDestPath("https://other.itch.io/other", "Capybara-Village-Update1.gb") != "" {
		t.Error("ExistingDestPath should return empty for unknown game")
	}
}

// ── Bug 2: False update badge for files present since first check ─────────────

func TestHasPendingUpdates_FalseForFirstCheckFiles(t *testing.T) {
	inv, _ := inventory.Load("/nonexistent")
	inv.Add("https://example.itch.io/game", inventory.Entry{Title: "Game", IsFree: true},
		inventory.DownloadedFile{Filename: "game-v1.gb", DestPath: "/roms/Game.gb"})

	// First update check discovers two files (game-v1.gb downloaded, old-jam.gb not)
	inv.SetUpstreamFiles("https://example.itch.io/game", []inventory.UpstreamFile{
		{Filename: "game-v1.gb", UploadID: "1"},
		{Filename: "old-jam.gb", UploadID: "2"},
	})

	// old-jam.gb was present from the very first check — should NOT trigger update badge
	if inv.HasPendingUpdates("https://example.itch.io/game") {
		t.Error("HasPendingUpdates = true for file seen on first check, want false")
	}
}

func TestAllURLs(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://a.itch.io/game-a", inventory.Entry{Title: "A"}, inventory.DownloadedFile{Filename: "a.p8", DestPath: "/roms/a.p8"})
	inv.Add("https://b.itch.io/game-b", inventory.Entry{Title: "B"}, inventory.DownloadedFile{Filename: "b.p8", DestPath: "/roms/b.p8"})

	urls := inv.AllURLs()
	if len(urls) != 2 {
		t.Fatalf("AllURLs() len = %d, want 2", len(urls))
	}
	urlSet := map[string]bool{}
	for _, u := range urls {
		urlSet[u] = true
	}
	if !urlSet["https://a.itch.io/game-a"] {
		t.Error("expected https://a.itch.io/game-a in AllURLs()")
	}
	if !urlSet["https://b.itch.io/game-b"] {
		t.Error("expected https://b.itch.io/game-b in AllURLs()")
	}
}

func TestHasPendingUpdates_TrueForSubsequentNewFile(t *testing.T) {
	inv, _ := inventory.Load("/nonexistent")
	inv.Add("https://example.itch.io/game", inventory.Entry{Title: "Game", IsFree: true},
		inventory.DownloadedFile{Filename: "game-v1.gb", DestPath: "/roms/Game.gb"})

	// First check: only v1 available
	inv.SetUpstreamFiles("https://example.itch.io/game", []inventory.UpstreamFile{
		{Filename: "game-v1.gb", UploadID: "1"},
	})

	// Second check: developer published v2
	inv.SetUpstreamFiles("https://example.itch.io/game", []inventory.UpstreamFile{
		{Filename: "game-v1.gb", UploadID: "1"},
		{Filename: "game-v2.gb", UploadID: "3"},
	})

	// game-v2.gb is genuinely new — should trigger update badge
	if !inv.HasPendingUpdates("https://example.itch.io/game") {
		t.Error("HasPendingUpdates = false for genuinely new upload, want true")
	}
}
