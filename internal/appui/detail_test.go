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
