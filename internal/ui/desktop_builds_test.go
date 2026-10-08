//go:build !headless

package ui

import (
	"net/http"
	"os"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// listFixture runs a signed-in free-game lookup against a checked-in API
// upload listing and applies it to a fresh picker. The fixtures are written
// by hand in the shape of the API v2 answer, with the filenames the device
// showed for these games; they are not captured responses.
func listFixture(t *testing.T, fixture, selection string) (*CatDownloadFlow, *appui.DownloadSelectModel) {
	t.Helper()
	body, err := os.ReadFile("../../testdata/api/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	site := newFreeGameSite(t, func(w http.ResponseWriter, _ *http.Request) { w.Write(body) })
	flow, model := newCatDownloadFlowForTest(t)
	flow.client = itchio.NewClientWithBase(site.srv.URL)
	flow.cfg.APIKey, flow.cfg.ROMSelection = sessionTestKey, selection
	flow.game.IsFree = true
	flow.detail = &itchio.GameDetail{GameID: "42"}
	flow.updates = make(chan catDownloadUpdate, 1)
	flow.discover()
	waitFor(t, func() bool { return flow.Sync(model) })
	if model.State == appui.DownloadSelectError {
		t.Fatalf("listing failed: %s", model.Message)
	}
	return flow, model
}

// R20-5: Yume Nikki's PS1 port lists Windows and Linux builds before the
// disc image. Automatic mode picks the disc image without asking.
func TestAutomaticModeSkipsDesktopBuilds(t *testing.T) {
	flow, model := listFixture(t, "uploads_yume_nikki_ps1.json", "auto")
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanArchive || plan.Uploads[0].Filename != "ps1.zip" {
		t.Fatalf("plan = %#v, model = %#v; want ps1.zip without asking", plan, model)
	}
}

// R20-5: Power Bee offers its Game Boy ROM and a web build. Automatic mode
// picks the ROM without asking.
func TestAutomaticModeSkipsWebBuild(t *testing.T) {
	flow, model := listFixture(t, "uploads_power_bee.json", "auto")
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || plan.Uploads[0].Filename != "power_bee_v3.gb" {
		t.Fatalf("plan = %#v, model = %#v; want power_bee_v3.gb without asking", plan, model)
	}
}

// R20-5: when you choose, desktop and web builds wait behind "Show all
// files", which lists them last, so every upload stays reachable.
func TestPickerListsDesktopBuildsBehindShowAllFiles(t *testing.T) {
	flow, model := listFixture(t, "uploads_yume_nikki_ps1.json", "ask")
	if flow.TakePlan() != nil || model.State != appui.DownloadSelectChoices {
		t.Fatalf("model = %#v, want a picker", model)
	}
	if titles := choiceTitles(model); len(titles) != 2 || titles[0] != "ps1.zip" || titles[1] != "Show all files" {
		t.Fatalf("choices = %q", titles)
	}
	model.Cursor = 1
	flow.Choose(model)
	if plan := flow.TakePlan(); plan != nil {
		t.Fatalf("Show all files started a download: %#v", plan)
	}
	if titles := choiceTitles(model); len(titles) != 3 || titles[0] != "ps1.zip" || titles[1] != "win32.zip" || titles[2] != "linux.zip" {
		t.Fatalf("all choices = %q", titles)
	}
	if model.Cursor != 1 {
		t.Fatalf("cursor = %d, want the first file that was hidden", model.Cursor)
	}
	flow.Choose(model)
	if plan := flow.TakePlan(); plan == nil || plan.Uploads[0].Filename != "win32.zip" {
		t.Fatalf("plan = %#v, want win32.zip", plan)
	}
}

// When every candidate is a desktop or web build, automatic mode asks
// rather than picking one.
func TestAutomaticModeAsksWhenOnlyDesktopBuildsRemain(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{
		{Filename: "win32.zip", DesktopOrWeb: true}, {Filename: "web.zip", DesktopOrWeb: true},
	})
	if plan := flow.TakePlan(); plan != nil || len(model.Choices) != 2 {
		t.Fatalf("plan = %#v, choices = %q", plan, choiceTitles(model))
	}
	flow.setUploads(model, []roms.Upload{{Filename: "win32.zip", DesktopOrWeb: true}})
	if plan := flow.TakePlan(); plan != nil || len(model.Choices) != 1 {
		t.Fatalf("a lone desktop build was picked automatically: %#v", plan)
	}
}

// A file with a ROM extension is never set aside, even when its author
// tagged it with a platform.
func TestTaggedROMIsNotSetAside(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{
		{Filename: "game.gb", DesktopOrWeb: true}, {Filename: "win32.zip", DesktopOrWeb: true},
	})
	if plan := flow.TakePlan(); plan == nil || plan.Uploads[0].Filename != "game.gb" {
		t.Fatalf("plan = %#v, choices = %q; want game.gb", plan, choiceTitles(model))
	}
}

func choiceTitles(model *appui.DownloadSelectModel) []string {
	titles := make([]string, 0, len(model.Choices))
	for _, choice := range model.Choices {
		titles = append(titles, choice.Title)
	}
	return titles
}
