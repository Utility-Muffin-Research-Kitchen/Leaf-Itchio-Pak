//go:build !headless

package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func newCatDownloadFlowForTest(t *testing.T) (*CatDownloadFlow, *appui.DownloadSelectModel) {
	t.Helper()
	root := t.TempDir()
	systems := map[string]string{}
	for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"} {
		systems[id] = filepath.Join(root, "Roms", id)
	}
	if err := roms.ConfigurePaths(roms.PathConfig{
		SystemDirs: systems, SourceID: "primary", PrimaryRoot: root,
		MusicRoot: filepath.Join(root, "Music"), StatesRoot: filepath.Join(root, "States"),
	}); err != nil {
		t.Fatal(err)
	}
	inv, err := inventory.Load(filepath.Join(root, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	flow := &CatDownloadFlow{
		cfg:  &settings.Config{ROMLocation: "auto", Pico8Core: "fake08"},
		game: itchio.Game{Title: "Fixture", URL: "https://example.invalid/game"}, inv: inv,
	}
	return flow, appui.NewDownloadSelectModel("Fixture")
}

func TestCatDownloadFlowClassifiesDirectAndMulti(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{{Filename: "game.gbc"}})
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || len(plan.DestPaths) != 1 {
		t.Fatalf("direct plan = %#v", plan)
	}

	flow.setUploads(model, []roms.Upload{{Filename: "game.chd"}})
	plan = flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || filepath.Base(plan.DestPaths[0]) != "game.chd" {
		t.Fatalf("PSX CHD plan = %#v", plan)
	}

	flow.setUploads(model, []roms.Upload{{Filename: "disc.cue"}, {Filename: "disc.bin"}})
	plan = flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanMulti || len(plan.DestPaths) != 2 {
		t.Fatalf("PSX cue/bin plan = %#v", plan)
	}

	flow.setUploads(model, []roms.Upload{{Filename: "orphan.bin"}})
	if plan = flow.TakePlan(); plan != nil || model.State != appui.DownloadSelectError {
		t.Fatalf("orphan BIN was accepted: plan=%#v model=%#v", plan, model)
	}

	flow.setUploads(model, []roms.Upload{{Filename: "one.gb"}, {Filename: "two.gbc"}})
	plan = flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanMulti || len(plan.DestPaths) != 2 {
		t.Fatalf("multi plan = %#v", plan)
	}
}

func TestCatDownloadFlowRequiresLaterSafeRoutes(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{{Filename: "bundle.zip"}})
	if plan := flow.TakePlan(); plan == nil || plan.Kind != CatDownloadPlanArchive {
		t.Fatalf("archive plan = %#v", plan)
	}

	flow.cfg.ROMLocation = "ask"
	flow.setUploads(model, []roms.Upload{{Filename: "game.gbc"}})
	if plan := flow.TakePlan(); plan == nil || plan.Kind != CatDownloadPlanDestination {
		t.Fatalf("destination plan = %#v", plan)
	}
}

func TestCatDownloadFlowManualUnknownFormat(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{{Filename: "mystery", NeedsFormat: true}})
	if model.State != appui.DownloadSelectChoices || model.Choices[0].Badge != "AUTO" {
		t.Fatalf("format choices = %#v", model.Choices)
	}
	model.Handle(appui.InputEvent{Button: appui.ButtonRight, Pressed: true})
	flow.Choose(model)
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || plan.Uploads[0].Filename != "mystery.p8.png" {
		t.Fatalf("manual format plan = %#v", plan)
	}
}

func TestCatDownloadFlowOffersPlayStationManualFormats(t *testing.T) {
	labels := allFormatLabels()
	for _, want := range []string{"CHD", "PBP", "CUE", "ISO", "IMG", "MDF", "TOC", "CBN", "M3U"} {
		found := false
		for _, label := range labels {
			found = found || label == want
		}
		if !found || formatExtension(want) != "."+strings.ToLower(want) {
			t.Errorf("manual PSX format %s missing or incorrectly mapped", want)
		}
	}
}

func TestCatDownloadBackendsRequestRescanOnlyForROMInstalls(t *testing.T) {
	if !(&DownloadScreen{}).CatNeedsLibraryScan() {
		t.Fatal("direct ROM download did not request a library rescan")
	}
	if !(&MultiROMDownloadScreen{}).CatNeedsLibraryScan() {
		t.Fatal("multi-ROM download did not request a library rescan")
	}
	if (&ZIPDownloadScreen{plan: ZIPPlan{DownloadMusic: true}}).CatNeedsLibraryScan() {
		t.Fatal("music-only archive requested a game-library rescan")
	}
	if !(&ZIPDownloadScreen{plan: ZIPPlan{DownloadROMs: true, DownloadMusic: true}}).CatNeedsLibraryScan() {
		t.Fatal("ROM archive did not request a library rescan")
	}
}
