package appui

import "testing"

func TestMainListNavigationAndIntents(t *testing.T) {
	model := NewMainListModel([]ListItem{{Title: "One"}, {Title: "Two"}, {Title: "Three"}, {Title: "Four"}})
	model.VisibleRows = 2

	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	if model.Cursor != 1 {
		t.Fatalf("down cursor = %d, want 1", model.Cursor)
	}
	model.Handle(InputEvent{Button: ButtonRight, Pressed: true})
	if model.Cursor != 3 {
		t.Fatalf("page-right cursor = %d, want 3", model.Cursor)
	}
	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	if model.Cursor != 3 {
		t.Fatalf("clamped cursor = %d, want 3", model.Cursor)
	}
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != ListIntentOpen {
		t.Fatalf("A intent = %v, want open", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonSelect, Pressed: true}); got != ListIntentFilter {
		t.Fatalf("SELECT intent = %v, want filter", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonStart, Pressed: true}); got != ListIntentSettings {
		t.Fatalf("START intent = %v, want settings", got)
	}
}

func TestMainListLoadingAndErrorGrammar(t *testing.T) {
	model := NewMainListModel(nil)
	model.SetLoading()
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != ListIntentNone {
		t.Fatalf("loading A intent = %v, want none", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != ListIntentExit {
		t.Fatalf("loading B intent = %v, want exit", got)
	}

	model.SetError("offline")
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != ListIntentRetry {
		t.Fatalf("error A intent = %v, want retry", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != ListIntentExit {
		t.Fatalf("error B intent = %v, want exit", got)
	}
}
