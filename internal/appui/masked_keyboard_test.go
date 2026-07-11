package appui

import (
	"strings"
	"testing"
)

func TestMaskedKeyboardNeverExposesValue(t *testing.T) {
	model := NewMaskedKeyboardModel("API", "secret42")
	if strings.Contains(model.Masked(), "secret") || model.Masked() == model.Value {
		t.Fatalf("masked field exposed value: %q", model.Masked())
	}
	model.Handle(InputEvent{Button: ButtonB, Pressed: true})
	if model.Value != "secret4" {
		t.Fatalf("backspace value = %q", model.Value)
	}
}

func TestMaskedKeyboardAcceptAndCancel(t *testing.T) {
	model := NewMaskedKeyboardModel("API", "a")
	if got := model.Handle(InputEvent{Button: ButtonStart, Pressed: true}); got != MaskedKeyboardIntentAccept {
		t.Fatalf("Start = %v", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonY, Pressed: true}); got != MaskedKeyboardIntentCancel {
		t.Fatalf("Y = %v", got)
	}
}

func TestMaskedKeyboardInsertsAtRuneCursor(t *testing.T) {
	model := NewMaskedKeyboardModel("API", "a葉c")
	model.Cursor = 2
	model.Selected = 1 // b
	model.Handle(InputEvent{Button: ButtonA, Pressed: true})
	if model.Value != "a葉bc" || model.Cursor != 3 {
		t.Fatalf("insert = %q cursor=%d", model.Value, model.Cursor)
	}
	if got := model.Masked(); got != "•••│•" {
		t.Fatalf("masked cursor = %q", got)
	}
}
