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

func newCatDownloadFlowForTest(t *testing.T) (*CatDownloadFlow, *appui.DownloadSelectModel) {
	t.Helper()
	root := t.TempDir()
	systems := map[string]string{}
	for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8"} {
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
