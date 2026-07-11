//go:build !headless

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func destinationFixture(t *testing.T) (leaf.SourceList, *leaf.Catalog, string) {
	t.Helper()
	root := t.TempDir()
	primary, secondary := filepath.Join(root, "primary"), filepath.Join(root, "secondary")
	for _, path := range []string{
		primary, secondary, filepath.Join(primary, "Roms"), filepath.Join(secondary, "Roms", "GBC", "RPG"),
		filepath.Join(primary, "Music"), filepath.Join(secondary, "Music", "Albums"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources := leaf.SourceList{
		{ID: "primary", Root: primary, Primary: true, RomsPath: filepath.Join(primary, "Roms"), MusicPath: filepath.Join(primary, "Music")},
		{ID: "secondary_sd", Root: secondary, RomsPath: filepath.Join(secondary, "Roms"), MusicPath: filepath.Join(secondary, "Music")},
	}
	platform := filepath.Join(root, "platform")
	if err := os.MkdirAll(filepath.Join(platform, "defaults"), 0o755); err != nil {
		t.Fatal(err)
	}
	ids := []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8"}
	json := `{"version":1,"platform":"mlp1","systems":[`
	for index, id := range ids {
		if index > 0 {
			json += ","
		}
		json += fmt.Sprintf(`{"id":%q,"name":%q,"rom_root":%q,"image_root":%q}`,
			id, id, "Roms/"+id, "Images/"+id)
	}
	json += `]}`
	if err := os.WriteFile(filepath.Join(platform, "defaults", "systems.json"), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := leaf.LoadCatalog(leaf.Environment{Platform: "mlp1", PlatformPath: platform})
	if err != nil {
		t.Fatal(err)
	}
	return sources, catalog, filepath.Join(root, "config.json")
}

func TestCatROMDestinationSecondarySubfolder(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath,
		"Game", []roms.Upload{{Filename: "game.gbc"}})
	if err != nil {
		t.Fatal(err)
	}
	model.Cursor = 1
	if complete, err := flow.Activate(model); err != nil || complete {
		t.Fatalf("choose secondary = %v, %v", complete, err)
	}
	if model.Phase != appui.DestinationFolders || model.Path != "Secondary SD" {
		t.Fatalf("folder model = %#v", model)
	}
	model.Cursor = 1 // RPG; root has no Up row.
	if complete, err := flow.Activate(model); err != nil || complete {
		t.Fatalf("enter RPG = %v, %v", complete, err)
	}
	if model.Path != "Secondary SD / RPG" {
		t.Fatalf("path = %q", model.Path)
	}
	model.Cursor = 0
	complete, err := flow.Activate(model)
	if err != nil || !complete {
		t.Fatalf("save = %v, %v", complete, err)
	}
	want := filepath.Join(sources[1].RomsPath, "GBC", "RPG")
	if got := flow.DestPaths()[0]; got != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
	pref := cfg.ROMDestinations["GBC"]
	if pref.SourceID != "secondary_sd" || pref.RelativePath != "RPG" {
		t.Fatalf("preference = %#v", pref)
	}
}

func TestCatDestinationHidesSymlinkEscape(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	outside := t.TempDir()
	root := filepath.Join(sources[0].RomsPath, "GBC")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cfg := &settings.Config{ROMDestinations: map[string]settings.RememberedDestination{
		"GBC": {SourceID: "primary", RelativePath: "escape"},
	}}
	flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath,
		"Game", []roms.Upload{{Filename: "game.gbc"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if model.Path != "Primary SD" {
		t.Fatalf("unsafe remembered path used: %q", model.Path)
	}
	for _, item := range model.Items {
		if item.Value == "escape" {
			t.Fatal("symlink escape was exposed as a folder")
		}
	}
}

func TestCatMusicDestinationRemembersSourceRelativePath(t *testing.T) {
	sources, _, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatMusicDestinationFlow(sources, cfg, cfgPath, "Soundtrack")
	if err != nil {
		t.Fatal(err)
	}
	model.Cursor = 1
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	model.Cursor = 1 // Albums
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	model.Cursor = 0
	complete, err := flow.Activate(model)
	if err != nil || !complete {
		t.Fatalf("music save = %v, %v", complete, err)
	}
	if cfg.MusicDestination == nil || cfg.MusicDestination.SourceID != "secondary_sd" || cfg.MusicDestination.RelativePath != "Albums" {
		t.Fatalf("music preference = %#v", cfg.MusicDestination)
	}
}

func TestCatDestinationStopsWhenSelectedCardIsRemoved(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath,
		"Game", []roms.Upload{{Filename: "game.gbc"}})
	if err != nil {
		t.Fatal(err)
	}
	model.Cursor = 1
	if complete, err := flow.Activate(model); err != nil || complete {
		t.Fatalf("choose secondary = %v, %v", complete, err)
	}
	if err := os.RemoveAll(sources[1].Root); err != nil {
		t.Fatal(err)
	}
	model.Cursor = 0
	complete, err := flow.Activate(model)
	if err == nil || complete {
		t.Fatalf("save after removal = %v, %v; want stopped", complete, err)
	}
	if len(flow.DestPaths()) != 0 || len(cfg.ROMDestinations) != 0 {
		t.Fatalf("removed card mutated destination state: paths=%v prefs=%v", flow.DestPaths(), cfg.ROMDestinations)
	}
}

func TestCatDestinationVisitsEachCanonicalSystemOnce(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath,
		"Collection", []roms.Upload{
			{Filename: "one.gb"},
			{Filename: "two.gbc"},
			{Filename: "three.gb"},
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Activate(model); err != nil { // Primary SD.
		t.Fatal(err)
	}
	if model.Subtitle != "Choose GB folder (1/2)" {
		t.Fatalf("first target = %q", model.Subtitle)
	}
	model.Cursor = 0
	if complete, err := flow.Activate(model); err != nil || complete {
		t.Fatalf("save GB = %v, %v", complete, err)
	}
	if model.Subtitle != "Choose GBC folder (2/2)" {
		t.Fatalf("second target = %q", model.Subtitle)
	}
	model.Cursor = 0
	complete, err := flow.Activate(model)
	if err != nil || !complete {
		t.Fatalf("save GBC = %v, %v", complete, err)
	}
	paths := flow.DestPaths()
	if len(paths) != 3 || paths[0] != paths[2] || paths[0] == paths[1] {
		t.Fatalf("upload-aligned destinations = %v", paths)
	}
}
