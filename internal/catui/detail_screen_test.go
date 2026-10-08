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
	model.SetReady("", nil, []string{"one", "two"}, false, false)
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

func TestUnavailableTextOffersManageOnlyForDownloadedGames(t *testing.T) {
	const removed = "This game was removed from itch.io."
	if got, want := unavailableText(removed, true), "This game was removed from itch.io. You can still manage your files."; got != want {
		t.Fatalf("downloaded = %q, want %q", got, want)
	}
	if got := unavailableText(removed, false); got != removed {
		t.Fatalf("not downloaded = %q, want %q", got, removed)
	}
}
