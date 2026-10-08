package appui

import (
	"reflect"
	"testing"
)

func TestDetailNavigationAndWarningGate(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Leaf 葉"})
	model.SetReady("<p>Hello</p>", nil, []string{"cover", "shot"}, false, false)
	model.Handle(InputEvent{Button: ButtonLeft, Pressed: true})
	if model.ImageIndex != 1 {
		t.Fatalf("wrapped image index = %d, want 1", model.ImageIndex)
	}
	model.SetScrollBounds(2)
	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	if model.ScrollLine != 1 {
		t.Fatalf("scroll = %d, want 1", model.ScrollLine)
	}
	model.State = DetailWarning
	if got := model.Handle(InputEvent{Button: ButtonStart, Pressed: true}); got != DetailIntentSettings {
		t.Fatalf("warning Start intent = %v, want settings", got)
	}
	model.Handle(InputEvent{Button: ButtonRight, Pressed: true})
	if model.ImageIndex != 1 {
		t.Fatal("warning screen allowed gallery navigation")
	}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != DetailIntentBack {
		t.Fatalf("B intent = %v, want back", got)
	}
}

func TestDescriptionParagraphs(t *testing.T) {
	want := []string{"Hello 葉 world", "• First", "• Second"}
	got := DescriptionParagraphs(`<p>Hello <b>葉</b> world</p><ul><li>First</li><li>Second</li></ul>`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paragraphs = %#v, want %#v", got, want)
	}
}

func TestDetailDownloadIntentHonorsCapabilityAndBrowserOnly(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Game", CanDownload: true})
	model.SetReady("", nil, nil, false, false)
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != DetailIntentDownload {
		t.Fatalf("A intent = %v, want download", got)
	}
	model.BrowserOnly = true
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != DetailIntentNone {
		t.Fatalf("browser-only A intent = %v, want none", got)
	}
}

func TestDetailManageIntentRequiresDownloadedGame(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Game", Downloaded: true})
	model.SetReady("", nil, nil, false, false)
	if got := model.Handle(InputEvent{Button: ButtonX, Pressed: true}); got != DetailIntentManage {
		t.Fatalf("X intent = %v, want manage", got)
	}
	model.Game.Downloaded = false
	if got := model.Handle(InputEvent{Button: ButtonX, Pressed: true}); got != DetailIntentNone {
		t.Fatalf("not-downloaded X intent = %v, want none", got)
	}
}

func TestUnavailableDetailAllowsOnlyLocalManagement(t *testing.T) {
	for _, downloaded := range []bool{false, true} {
		model := NewDetailModel(DetailGame{Downloaded: downloaded, CanDownload: true})
		model.SetError("Game page unavailable")
		model.SetScrollBounds(2)
		model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
		if model.ScrollLine != 1 {
			t.Fatal("unavailable detail text cannot scroll")
		}
		if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != DetailIntentNone {
			t.Fatalf("unverified download intent = %v", got)
		}
		want := DetailIntentNone
		if downloaded {
			want = DetailIntentManage
		}
		if got := model.Handle(InputEvent{Button: ButtonX, Pressed: true}); got != want {
			t.Fatalf("downloaded=%v: X intent = %v, want %v", downloaded, got, want)
		}
		model.State = DetailWarning
		if got := model.Handle(InputEvent{Button: ButtonX, Pressed: true}); got != DetailIntentNone {
			t.Fatalf("warning bypassed by X: %v", got)
		}
	}
}

func TestDetailPriceTextFollowsOwnership(t *testing.T) {
	game := DetailGame{PriceLabel: "$5.00", Owned: true}
	if got := game.PriceText(); got != "Owned" {
		t.Fatalf("owned paid game = %q, want Owned", got)
	}
	// Signing out clears Owned at draw time; the price returns.
	game.Owned = false
	if got := game.PriceText(); got != "$5.00" {
		t.Fatalf("after sign-out = %q, want the price", got)
	}
	free := DetailGame{PriceLabel: "Free / name your price", Owned: true, IsFree: true}
	if got := free.PriceText(); got != "Free / name your price" {
		t.Fatalf("owned free game = %q, want its free label", got)
	}
}
