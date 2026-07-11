//go:build !headless

package ui

import (
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func archiveFlowFixture(t *testing.T, cfg *settings.Config, manifest roms.ZIPManifest) *CatArchiveFlow {
	t.Helper()
	root := t.TempDir()
	systems := make(map[string]string)
	for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8"} {
		systems[id] = filepath.Join(root, "Roms", id) + string(filepath.Separator)
	}
	if err := roms.ConfigurePaths(roms.PathConfig{SystemDirs: systems, SourceID: "primary",
		PrimaryRoot: root, MusicRoot: filepath.Join(root, "Music") + string(filepath.Separator),
		StatesRoot: filepath.Join(root, "States")}); err != nil {
		t.Fatal(err)
	}
	inv, err := inventory.Load(filepath.Join(root, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &CatArchiveFlow{cfg: cfg, game: itchio.Game{Title: "Leafbound", URL: "https://example.invalid/game"},
		upload: roms.Upload{Filename: "bundle.zip"}, inv: inv,
		plan: ZIPPlan{Upload: roms.Upload{Filename: "bundle.zip"}, CDNURL: "https://cdn.invalid/bundle.zip", Manifest: manifest}}
}

func TestCatArchiveSingleROMZIPUsesInspectedExtension(t *testing.T) {
	manifest := roms.ZIPManifest{Entries: []roms.ZIPEntry{{Name: "game.gba", Kind: roms.KindROM}}}
	flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "auto"}, manifest)
	flow.prepareInitialAction()
	if action := flow.TakeAction(); action != CatArchiveStartDirect {
		t.Fatalf("action = %v", action)
	}
	plan := flow.TakeDirectPlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || len(plan.DestPaths) != 1 || filepath.Base(plan.DestPaths[0]) != "bundle.zip" {
		t.Fatalf("direct plan = %#v", plan)
	}

	flow = archiveFlowFixture(t, &settings.Config{ROMLocation: "ask"}, manifest)
	flow.prepareInitialAction()
	plan = flow.TakeDirectPlan()
	if plan == nil || plan.Kind != CatDownloadPlanDestination || len(plan.LogicalExts) != 1 || plan.LogicalExts[0] != ".gba" {
		t.Fatalf("logical destination plan = %#v", plan)
	}
}

func TestCatArchiveWalksDuplicateAndMusicChoices(t *testing.T) {
	manifest := roms.ZIPManifest{Entries: []roms.ZIPEntry{
		{Name: "v1/game.gbc", Kind: roms.KindROM},
		{Name: "v2/game.gbc", Kind: roms.KindROM},
		{Name: "soundtrack/theme.ogg", Kind: roms.KindMusic},
	}}
	flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "ask", MusicDownload: "ask", MusicLocation: "ask"}, manifest)
	flow.prepareInitialAction()
	if action := flow.TakeAction(); action != CatArchiveChooseContents {
		t.Fatalf("initial action = %v", action)
	}
	model := appui.NewDownloadSelectModel("Leafbound")
	flow.PrepareChoices(model)
	model.Cursor = 1
	flow.Choose(model)
	if got := flow.plan.SelectedROMs[".gbc"]; got != "v2/game.gbc" {
		t.Fatalf("selected ROM = %q", got)
	}
	if len(model.Choices) != 2 || model.Choices[0].Badge != "YES" {
		t.Fatalf("music choices = %#v", model.Choices)
	}
	model.Cursor = 0
	flow.Choose(model)
	if action := flow.TakeAction(); action != CatArchiveChooseROMDestination {
		t.Fatalf("post-choice action = %v", action)
	}
	flow.SetROMDirs(map[string]string{".gbc": "/card2/Roms/GBC"})
	if action := flow.TakeAction(); action != CatArchiveChooseMusicDestination {
		t.Fatalf("post-ROM action = %v", action)
	}
	flow.SetMusicDir("/card2/Music/Leafbound")
	if action := flow.TakeAction(); action != CatArchiveStartExtraction {
		t.Fatalf("final action = %v", action)
	}
}

func TestCatArchivePico8SpecialCaseStillAsksForMusic(t *testing.T) {
	manifest := roms.ZIPManifest{Entries: []roms.ZIPEntry{
		{Name: "src/game.p8", Kind: roms.KindROM},
		{Name: "release/game.p8.png", Kind: roms.KindROM},
		{Name: "music/theme.ogg", Kind: roms.KindMusic},
	}}
	flow := archiveFlowFixture(t, &settings.Config{MusicDownload: "ask", MusicLocation: "auto"}, manifest)
	flow.prepareInitialAction()
	if action := flow.TakeAction(); action != CatArchiveChooseContents {
		t.Fatalf("initial action = %v", action)
	}
	model := appui.NewDownloadSelectModel("Leafbound")
	flow.PrepareChoices(model)
	if len(model.Choices) != 2 || model.Choices[0].Badge != "YES" {
		t.Fatalf("music choices = %#v", model.Choices)
	}
	model.Cursor = 0
	flow.Choose(model)
	if action := flow.TakeAction(); action != CatArchiveStartExtraction {
		t.Fatalf("final action = %v", action)
	}
	if flow.plan.SelectedROMs[".p8"] != "" || flow.plan.SelectedROMs[".p8.png"] != "release/game.p8.png" {
		t.Fatalf("special selection = %#v", flow.plan.SelectedROMs)
	}
}

func TestZIPSelectionDistinguishesNestedEntries(t *testing.T) {
	screen := &ZIPDownloadScreen{plan: ZIPPlan{DownloadROMs: true,
		SelectedROMs: map[string]string{".gbc": "v2/game.gbc"}}}
	if screen.shouldExtractROM("v1/game.gbc") {
		t.Fatal("unselected nested ROM was accepted")
	}
	if !screen.shouldExtractROM("v2/game.gbc") {
		t.Fatal("selected nested ROM was rejected")
	}
}
