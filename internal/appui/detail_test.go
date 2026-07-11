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
