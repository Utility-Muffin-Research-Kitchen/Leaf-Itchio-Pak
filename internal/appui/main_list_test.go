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
	if model.Cursor != 0 {
		t.Fatalf("wrap-down cursor = %d, want 0", model.Cursor)
	}
	model.Handle(InputEvent{Button: ButtonUp, Pressed: true})
	if model.Cursor != 3 {
		t.Fatalf("wrap-up cursor = %d, want 3", model.Cursor)
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
	if got := model.Handle(InputEvent{Button: ButtonL2, Pressed: true}); got != ListIntentPreviousPlatform {
		t.Fatalf("L2 intent = %v, want previous platform", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonR2, Pressed: true}); got != ListIntentNextPlatform {
		t.Fatalf("R2 intent = %v, want next platform", got)
	}
}

func TestMainListAlphaJump(t *testing.T) {
	model := NewMainListModel([]ListItem{
		{Title: "Alwa's Awakening"}, {Title: "Arkade Boy"},
		{Title: "Balloon Trip"}, {Title: "Byte Defender"}, {Title: "Cave Crawler"},
	})
	model.Sort = "A-Z"
	model.Handle(InputEvent{Button: ButtonRight, Pressed: true})
	if model.Cursor != 2 {
		t.Fatalf("alpha jump right cursor = %d, want 2", model.Cursor)
	}
	model.Handle(InputEvent{Button: ButtonLeft, Pressed: true})
	if model.Cursor != 1 {
		t.Fatalf("alpha jump left cursor = %d, want 1", model.Cursor)
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
