//go:build !headless

package ui

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// F31: the message names every system once, and an installable file next to
// an unsupported one is offered alone.
func TestUnsupportedUploadsNameEachSystemOnce(t *testing.T) {
	flow, model := newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{
		{Filename: "game.nds", UnsupportedSystem: "Nintendo DS"},
		{Filename: "game.exe", UnsupportedSystem: "Windows"},
		{Filename: "game2.nds", UnsupportedSystem: "Nintendo DS"},
	})
	if want := "This game has no files the app can install. Its files are for Nintendo DS and Windows."; model.Message != want {
		t.Fatalf("message = %q, want %q", model.Message, want)
	}
	flow.setUploads(model, []roms.Upload{
		{Filename: "a.nds", UnsupportedSystem: "Nintendo DS"}, {Filename: "b.exe", UnsupportedSystem: "Windows"},
		{Filename: "c.apk", UnsupportedSystem: "Android"},
	})
	if want := "This game has no files the app can install. Its files are for Nintendo DS, Windows, and Android."; model.Message != want {
		t.Fatalf("message = %q, want %q", model.Message, want)
	}

	flow, model = newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{
		{Filename: "Hidden_palace.nds v0.1", UnsupportedSystem: "Nintendo DS"}, {Filename: "game.gb"},
	})
	plan := flow.TakePlan()
	if plan == nil || plan.Kind != CatDownloadPlanDirect || len(plan.Uploads) != 1 || plan.Uploads[0].Filename != "game.gb" {
		t.Fatalf("plan = %#v, model = %#v; want game.gb alone", plan, model)
	}
	flow, model = newCatDownloadFlowForTest(t)
	flow.cfg.ROMSelection = "ask"
	flow.setUploads(model, []roms.Upload{
		{Filename: "Hidden_palace.nds v0.1", UnsupportedSystem: "Nintendo DS"}, {Filename: "game.gb"}, {Filename: "game.gbc"},
	})
	if titles := choiceTitles(model); len(titles) != 2 || titles[0] != "game.gb" || titles[1] != "game.gbc" {
		t.Fatalf("choices = %q, want the two installable files only", titles)
	}
	flow, model = newCatDownloadFlowForTest(t)
	flow.setUploads(model, []roms.Upload{
		{Filename: "Hidden_palace.nds v0.1", UnsupportedSystem: "Nintendo DS"}, {Filename: "mystery", NeedsFormat: true},
	})
	if titles := choiceTitles(model); len(titles) != 1 || titles[0] != "mystery" {
		t.Fatalf("choices = %q, want the unknown file only", titles)
	}
}
