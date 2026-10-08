//go:build !headless

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
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
		filepath.Join(primary, "Images"), filepath.Join(secondary, "Images"),
		filepath.Join(primary, "Music"), filepath.Join(secondary, "Music", "Albums"),
		filepath.Join(primary, "Saves"), filepath.Join(secondary, "Saves"),
		filepath.Join(primary, "States"), filepath.Join(secondary, "States"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sources := leaf.SourceList{
		{ID: "primary", Root: primary, Primary: true, RomsPath: filepath.Join(primary, "Roms"), ImagesPath: filepath.Join(primary, "Images"), MusicPath: filepath.Join(primary, "Music"), SavesPath: filepath.Join(primary, "Saves"), StatesPath: filepath.Join(primary, "States")},
		{ID: "secondary_sd", Root: secondary, RomsPath: filepath.Join(secondary, "Roms"), ImagesPath: filepath.Join(secondary, "Images"), MusicPath: filepath.Join(secondary, "Music"), SavesPath: filepath.Join(secondary, "Saves"), StatesPath: filepath.Join(secondary, "States")},
	}
	platform := filepath.Join(root, "platform")
	if err := os.MkdirAll(filepath.Join(platform, "defaults"), 0o755); err != nil {
		t.Fatal(err)
	}
	ids := []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"}
	json := `{"version":1,"platform":"mlp1","systems":[`
	for index, id := range ids {
		if index > 0 {
			json += ","
		}
		folder := id
		if id == "PS" {
			folder = "PSX"
		}
		json += fmt.Sprintf(`{"id":%q,"name":%q,"rom_root":%q,"image_root":%q}`,
			id, id, "Roms/"+folder, "Images/"+folder)
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
		nil, itchio.Game{Title: "Game"}, []roms.Upload{{Filename: "game.gbc"}})
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
	if err != nil || complete || model.Phase != appui.DestinationConfirm {
		t.Fatalf("summary = %v, state %v, %v", complete, model.Phase, err)
	}
	if len(model.SummaryLines) < 1 || model.SummaryLines[0] != "Roms/GBC/RPG/game.gbc" {
		t.Fatalf("destination summary = %v", model.SummaryLines)
	}
	complete, err = flow.Activate(model)
	if err != nil || !complete {
		t.Fatalf("confirm = %v, %v", complete, err)
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
		nil, itchio.Game{Title: "Game"}, []roms.Upload{{Filename: "game.gbc"}})
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
	if err != nil || complete || model.Phase != appui.DestinationConfirm {
		t.Fatalf("music summary = %v, state %v, %v", complete, model.Phase, err)
	}
	complete, err = flow.Activate(model)
	if err != nil || !complete {
		t.Fatalf("music confirm = %v, %v", complete, err)
	}
	if cfg.MusicDestination == nil || cfg.MusicDestination.SourceID != "secondary_sd" || cfg.MusicDestination.RelativePath != "Albums" {
		t.Fatalf("music preference = %#v", cfg.MusicDestination)
	}
}

func TestCatDestinationStopsWhenSelectedCardIsRemoved(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath,
		nil, itchio.Game{Title: "Game"}, []roms.Upload{{Filename: "game.gbc"}})
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

func TestCatDestinationRechecksCardAfterSummary(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath,
		nil, itchio.Game{Title: "Game"}, []roms.Upload{{Filename: "game.gbc"}})
	if err != nil {
		t.Fatal(err)
	}
	model.Cursor = 1
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	model.Cursor = 0
	if complete, err := flow.Activate(model); err != nil || complete || model.Phase != appui.DestinationConfirm {
		t.Fatalf("summary = %v, state %v, %v", complete, model.Phase, err)
	}
	if err := os.RemoveAll(sources[1].Root); err != nil {
		t.Fatal(err)
	}
	if complete, err := flow.Activate(model); err == nil || complete {
		t.Fatalf("confirm after removal = %v, %v; want blocked", complete, err)
	}
	if len(cfg.ROMDestinations) != 0 {
		t.Fatalf("removed card persisted destination preferences: %v", cfg.ROMDestinations)
	}
}

func TestCatDestinationVisitsEachCanonicalSystemOnce(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath,
		nil, itchio.Game{Title: "Collection"}, []roms.Upload{
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
	if err != nil || complete || model.Phase != appui.DestinationConfirm {
		t.Fatalf("summary GBC = %v, state %v, %v", complete, model.Phase, err)
	}
	complete, err = flow.Activate(model)
	if err != nil || !complete {
		t.Fatalf("confirm GBC = %v, %v", complete, err)
	}
	paths := flow.DestPaths()
	if len(paths) != 3 || paths[0] != paths[2] || paths[0] == paths[1] {
		t.Fatalf("upload-aligned destinations = %v", paths)
	}
}

func TestCatArchiveDestinationReturnsInnerExtensionMap(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	cfg := &settings.Config{}
	flow, model, err := NewCatArchiveROMDestinationFlow(sources, catalog, cfg, cfgPath,
		"Archive", []string{".gb", ".gbc", ".gb"})
	if err != nil {
		t.Fatal(err)
	}
	model.Cursor = 1 // Secondary SD.
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	model.Cursor = 0
	if complete, err := flow.Activate(model); err != nil || complete {
		t.Fatalf("save GB = %v, %v", complete, err)
	}
	model.Cursor = 0
	if complete, err := flow.Activate(model); err != nil || complete || model.Phase != appui.DestinationConfirm {
		t.Fatalf("summary GBC = %v, state %v, %v", complete, model.Phase, err)
	}
	if complete, err := flow.Activate(model); err != nil || !complete {
		t.Fatalf("confirm GBC = %v, %v", complete, err)
	}
	dirs := flow.ArchiveROMDirs()
	if dirs[".gb"] == "" || dirs[".gbc"] == "" || dirs[".gb"] == dirs[".gbc"] {
		t.Fatalf("archive dirs = %#v", dirs)
	}
}

func TestCatPSXArchiveDestinationKeepsCueAndBinOnSecondary(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	flow, model, err := NewCatArchiveROMDestinationFlow(sources, catalog, &settings.Config{}, cfgPath,
		"PSX Archive", []string{".cue", ".bin"})
	if err != nil {
		t.Fatal(err)
	}
	model.Cursor = 1
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	model.Cursor = 0
	if complete, err := flow.Activate(model); err != nil || complete || model.Phase != appui.DestinationConfirm {
		t.Fatalf("PSX summary = %v, state %v, %v", complete, model.Phase, err)
	}
	if complete, err := flow.Activate(model); err != nil || !complete {
		t.Fatalf("confirm PSX destination = %v, %v", complete, err)
	}
	want := filepath.Join(sources[1].RomsPath, "PSX")
	dirs := flow.ArchiveROMDirs()
	if dirs[".cue"] != want || dirs[".bin"] != want {
		t.Fatalf("PSX archive dirs = %#v, want %q", dirs, want)
	}
}

// The confirm screen names each file the way the install will write it, not
// by its upload name. On the device it said "Roms/GB/power_bee_v3.gb" while
// the reinstall wrote the game's existing "Power Bee (GB) (2).gb".
func TestCatROMDestinationConfirmShowsTheInstalledName(t *testing.T) {
	game := itchio.Game{Title: "Power Bee (GB)", URL: "https://dev.itch.io/power-bee"}
	upload := roms.Upload{Filename: "power_bee_v3.gb"}
	for _, tc := range []struct {
		name      string
		unified   bool
		unknown   bool   // a file the app does not know holds the title's name
		installed string // where an earlier install of the upload is
		secondary bool   // the earlier install is on the secondary card
		want      string
	}{
		{name: "new install keeps the upload name", want: "Roms/GB/power_bee_v3.gb"},
		{name: "new install with unified naming", unified: true, want: "Roms/GB/Power Bee (GB).gb"},
		{name: "unified name taken by an unknown file", unified: true, unknown: true,
			want: "Roms/GB/Power Bee (GB) (2).gb"},
		{name: "reinstall keeps the existing name", unified: true, unknown: true,
			installed: "Power Bee (GB) (2).gb", want: "Roms/GB/Power Bee (GB) (2).gb"},
		{name: "reinstall in another folder", installed: "Arcade/power_bee_v3.gb",
			want: "Roms/GB/Arcade/power_bee_v3.gb"},
		{name: "reinstall on the other card", installed: "power_bee_v3.gb", secondary: true,
			want: "Secondary SD / Roms/GB/power_bee_v3.gb"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sources, catalog, cfgPath := destinationFixture(t)
			configureManageFixture(t, sources, catalog)
			gbDir := filepath.Join(sources[0].RomsPath, "GB")
			if err := os.MkdirAll(gbDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.unknown {
				if err := os.WriteFile(filepath.Join(gbDir, "Power Bee (GB).gb"), []byte("unknown"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
			if tc.installed != "" {
				dir := gbDir
				if tc.secondary {
					dir = filepath.Join(sources[1].RomsPath, "GB")
				}
				plantOwnedFile(t, inv, game, upload.Filename, filepath.Join(dir, tc.installed), "earlier")
			}
			cfg := &settings.Config{UnifiedNaming: tc.unified}
			flow, model, err := NewCatROMDestinationFlow(sources, catalog, cfg, cfgPath, inv, game, []roms.Upload{upload})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := flow.Activate(model); err != nil { // Primary SD.
				t.Fatal(err)
			}
			model.Cursor = 0 // Save here, in Roms/GB.
			if complete, err := flow.Activate(model); err != nil || complete || model.Phase != appui.DestinationConfirm {
				t.Fatalf("summary = %v, state %v, %v", complete, model.Phase, err)
			}
			if len(model.SummaryLines) == 0 || model.SummaryLines[0] != tc.want {
				t.Fatalf("confirm lines = %q, want %q first", model.SummaryLines, tc.want)
			}
			if complete, err := flow.Activate(model); err != nil || !complete {
				t.Fatalf("confirm = %v, %v", complete, err)
			}

			// The download starts from these paths, and the install names
			// them the way the confirm screen did.
			dests := flow.UploadDestPaths()
			targets, err := planInstallTargets(inv, cfg, game, []romDownload{{Upload: upload, DestPath: dests[0]}})
			if err != nil {
				t.Fatal(err)
			}
			if got := flow.displayPath(targets[0].final); got != tc.want {
				t.Fatalf("install writes %q, confirm said %q", got, tc.want)
			}
		})
	}
}
