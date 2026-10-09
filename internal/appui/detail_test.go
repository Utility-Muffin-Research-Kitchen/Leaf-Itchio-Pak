package appui

import (
	"reflect"
	"testing"
)

func TestDetailNavigationAndWarningGate(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Leaf 葉"})
	model.SetReady("<p>Hello</p>", nil, []string{"cover", "shot"}, false, nil)
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
	model.SetReady("", nil, nil, false, nil)
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
	model.SetReady("", nil, nil, false, nil)
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

// F30: the content warning names the categories that matched and says where
// to change them.
func TestDetailWarningNamesTheMatchedCategories(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Lava Boy"})
	model.SetReady("<p>Hello</p>", []string{"nsfw"}, nil, false, []string{"Adult Content"})
	if model.State != DetailWarning || !reflect.DeepEqual(model.WarningCategories, []string{"Adult Content"}) {
		t.Fatalf("state %v, categories %q; want a warning naming Adult Content", model.State, model.WarningCategories)
	}
	model.SetReady("<p>Hello</p>", nil, nil, false, nil)
	if model.State != DetailReady || model.WarningCategories != nil {
		t.Fatalf("state %v, categories %q; want a ready page with no warning", model.State, model.WarningCategories)
	}

	for _, tc := range []struct {
		categories []string
		want       string
	}{
		{[]string{"Adult Content"},
			"This game matches your Adult Content filter.\n\nPress Start to change it under Content Moderation, then open the game again. Press B to go back."},
		{[]string{"Adult Content", "Heavy Themes"},
			"This game matches your Adult Content and Heavy Themes filters.\n\nPress Start to change them under Content Moderation, then open the game again. Press B to go back."},
		{[]string{"Adult Content", "Queer Content", "Substance Use"},
			"This game matches your Adult Content, Queer Content, and Substance Use filters.\n\nPress Start to change them under Content Moderation, then open the game again. Press B to go back."},
		{nil,
			"This game matches your content filters.\n\nPress Start to change them under Content Moderation, then open the game again. Press B to go back."},
	} {
		if got := WarningText(tc.categories); got != tc.want {
			t.Errorf("WarningText(%q) =\n%q\nwant\n%q", tc.categories, got, tc.want)
		}
	}
}
