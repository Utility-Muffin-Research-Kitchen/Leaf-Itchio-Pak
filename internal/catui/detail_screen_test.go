package catui

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

func footerButtons(hints []FooterHint) map[Button]FooterHint {
	buttons := make(map[Button]FooterHint, len(hints))
	for _, hint := range hints {
		buttons[hint.Button] = hint
	}
	return buttons
}

// F6: a downloaded game keeps B Back. Settings and the image hint are the
// ones the composer may drop, Settings first.
func TestDetailFooterKeepsBackForDownloadedGames(t *testing.T) {
	model := appui.NewDetailModel(appui.DetailGame{Title: "Glory Hunters", IsFree: true, CanDownload: true, Downloaded: true})
	model.SetReady("", nil, []string{"one", "two"}, false, nil)
	hints := detailFooter(model)
	if len(hints) == 0 || hints[0].Button != ButtonB || hints[0].Label != "Back" || hints[0].DropRank != 0 {
		t.Fatalf("first hint = %+v, want a required B Back", hints)
	}
	buttons := footerButtons(hints)
	for _, button := range []Button{ButtonA, ButtonX} {
		if hint, ok := buttons[button]; !ok || hint.DropRank != 0 {
			t.Fatalf("hint for button %d = %+v (present %v), want required", button, hint, ok)
		}
	}
	if got := buttons[ButtonStart].DropRank; got != 1 {
		t.Fatalf("Settings drop rank = %d, want 1", got)
	}
	if got := buttons[ButtonL1].DropRank; got != 2 {
		t.Fatalf("image hint drop rank = %d, want 2", got)
	}
	if buttons[ButtonA].IsConfirm {
		t.Fatal("A moved to the right group; keep it with the left group")
	}

	model.SetError("Go back and reopen this game to try again.")
	buttons = footerButtons(detailFooter(model))
	if _, ok := buttons[ButtonB]; !ok {
		t.Fatal("unavailable page lost B Back")
	}
	if _, ok := buttons[ButtonX]; !ok {
		t.Fatal("unavailable page lost X Manage for a downloaded game")
	}
}

// F33: Start opens Content Moderation while the content warning shows, and
// Settings everywhere else, so the hint is labelled for where it goes. The
// warning's Start is the only way forward, so the composer may shorten its
// label but never drop it.
func TestDetailFooterLabelsStartForWhereItGoes(t *testing.T) {
	game := appui.DetailGame{Title: "Lava Boy", IsFree: true, CanDownload: true}

	warning := appui.NewDetailModel(game)
	warning.SetReady("", nil, nil, false, []string{"Heavy Themes"})
	if warning.State != appui.DetailWarning {
		t.Fatalf("state = %v, want the content warning", warning.State)
	}
	if got := warning.Handle(appui.InputEvent{Button: appui.ButtonStart, Pressed: true}); got != appui.DetailIntentSettings {
		t.Fatalf("Start on the warning = %v, want the settings intent that main routes to Content Moderation", got)
	}
	buttons := footerButtons(detailFooter(warning))
	start, ok := buttons[ButtonStart]
	if !ok {
		t.Fatal("the content warning has no Start hint")
	}
	if start.Label != "Content Moderation" || start.NarrowLabel != "Moderation" {
		t.Errorf("warning Start hint = %q (narrow %q), want %q (narrow %q)",
			start.Label, start.NarrowLabel, "Content Moderation", "Moderation")
	}
	if start.DropRank != 0 {
		t.Errorf("warning Start drop rank = %d, want 0: it is the only way to change the filter", start.DropRank)
	}
	if back, ok := buttons[ButtonB]; !ok || back.Label != "Back" {
		t.Errorf("warning B hint = %+v (present %v), want Back", back, ok)
	}
	if len(buttons) != 2 {
		t.Errorf("warning footer = %+v, want only Back and Start", buttons)
	}

	loading := appui.NewDetailModel(game)
	ready := appui.NewDetailModel(game)
	ready.SetReady("", nil, []string{"one"}, false, nil)
	failed := appui.NewDetailModel(game)
	failed.SetError("Go back and reopen this game to try again.")
	for name, model := range map[string]*appui.DetailModel{"loading": loading, "ready": ready, "error": failed} {
		start, ok := footerButtons(detailFooter(model))[ButtonStart]
		if !ok || start.Label != "Settings" || start.NarrowLabel != "Set" || start.DropRank != 1 {
			t.Errorf("%s Start hint = %+v (present %v), want Settings", name, start, ok)
		}
	}
}

func TestUnavailableTextOffersManageOnlyForDownloadedGames(t *testing.T) {
	const removed = "This game was removed from itch.io."
	if got, want := unavailableText(removed, true), "This game was removed from itch.io. You can still manage your files."; got != want {
		t.Fatalf("downloaded = %q, want %q", got, want)
	}
	if got := unavailableText(removed, false); got != removed {
		t.Fatalf("not downloaded = %q, want %q", got, removed)
	}
}
