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
		cfg:  &settings.Config{ROMLocation: "auto"},
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
	if plan = flow.TakePlan(); plan != nil || model.State != appui.DownloadSelectChoices ||
		len(model.Choices) != 1 || model.Choices[0].Badge != "AUTO" {
		t.Fatalf("standalone BIN did not require detection: plan=%#v model=%#v", plan, model)
	}

	flow.setUploads(model, []roms.Upload{{Filename: "one.gb"}, {Filename: "two.gbc"}})
	plan = flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanMulti || len(plan.DestPaths) != 2 {
		t.Fatalf("multi plan = %#v", plan)
	}
}

func TestCatDownloadFlowRoutesDetectedGenesisBINToMegaDrive(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.updates = make(chan catDownloadUpdate, 1)
	flow.updates <- catDownloadUpdate{
		kind: catDownloadUpdateDetected,
		upload: roms.Upload{
			Filename: "Black Jewel Reborn DEMO 2.11.bin",
			URL:      "https://example.invalid/download",
		},
		ext: ".md",
	}
	if !flow.Sync(model) {
		t.Fatal("detected BIN update was not consumed")
	}
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || plan.Uploads[0].Filename != "Black Jewel Reborn DEMO 2.11.md" {
		t.Fatalf("detected Genesis plan = %#v", plan)
	}
	if got := filepath.Clean(plan.DestPaths[0]); got != filepath.Join(roms.SystemDir("MD"), "Black Jewel Reborn DEMO 2.11.md") {
		t.Fatalf("Genesis destination = %q", got)
	}
}

func TestCatDownloadFlowRejectsUndetectedStandaloneBIN(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.updates = make(chan catDownloadUpdate, 1)
	flow.updates <- catDownloadUpdate{
		kind: catDownloadUpdateDetected, upload: roms.Upload{Filename: "track.bin"},
	}
	if !flow.Sync(model) || model.State != appui.DownloadSelectError || !strings.Contains(model.Message, "matching CUE") {
		t.Fatalf("undetected BIN model = %#v", model)
	}
}

func TestFilenameWithFormatReplacesAmbiguousBIN(t *testing.T) {
	if got := filenameWithFormat("game.bin", ".md"); got != "game.md" {
		t.Fatalf("BIN format = %q", got)
	}
	if got := filenameWithFormat("mystery", ".gbc"); got != "mystery.gbc" {
		t.Fatalf("extensionless format = %q", got)
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

func TestCatDownloadFlowROMSelectionAskChoosesOneIndependentUpload(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.cfg.ROMSelection = "ask"
	flow.setUploads(model, []roms.Upload{{Filename: "game.gb"}, {Filename: "game.gbc"}})
	if plan := flow.TakePlan(); plan != nil || model.State != appui.DownloadSelectChoices || len(model.Choices) != 2 {
		t.Fatalf("ask choices = plan %#v model %#v", plan, model)
	}
	model.Cursor = 1
	flow.Choose(model)
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || plan.Uploads[0].Filename != "game.gbc" {
		t.Fatalf("ask plan = %#v", plan)
	}
}

func TestCatDownloadFlowROMSelectionAskShowsSingleUpload(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.cfg.ROMSelection = "ask"
	flow.setUploads(model, []roms.Upload{{Filename: "game.gbc"}})
	if plan := flow.TakePlan(); plan != nil || model.State != appui.DownloadSelectChoices || len(model.Choices) != 1 {
		t.Fatalf("single ask choice = plan %#v model %#v", plan, model)
	}
	flow.Choose(model)
	if plan := flow.TakePlan(); plan == nil || plan.Kind != CatDownloadPlanDirect {
		t.Fatalf("single ask plan = %#v", plan)
	}
}

func TestCatDownloadFlowROMSelectionAskKeepsCUEBINPaired(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.cfg.ROMSelection = "ask"
	flow.setUploads(model, []roms.Upload{{Filename: "disc.cue"}, {Filename: "track.bin"}})
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanMulti || len(plan.Uploads) != 2 {
		t.Fatalf("paired PSX plan = %#v model %#v", plan, model)
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
	if !(&DirectDownloadWorker{}).CatNeedsLibraryScan() {
		t.Fatal("direct ROM download did not request a library rescan")
	}
	if !(&MultiDownloadWorker{}).CatNeedsLibraryScan() {
		t.Fatal("multi-ROM download did not request a library rescan")
	}
	if (&ArchiveDownloadWorker{plan: ZIPPlan{DownloadMusic: true}}).CatNeedsLibraryScan() {
		t.Fatal("music-only archive requested a game-library rescan")
	}
	if !(&ArchiveDownloadWorker{plan: ZIPPlan{DownloadROMs: true, DownloadMusic: true}}).CatNeedsLibraryScan() {
		t.Fatal("ROM archive did not request a library rescan")
	}
}
