package appui

import "testing"

func TestRefreshControls(t *testing.T) {
	model := NewRefreshModel("Refresh")
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != RefreshIntentCancel {
		t.Fatalf("loading B = %v", got)
	}
	model.State = RefreshDone
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != RefreshIntentBack {
		t.Fatalf("done A = %v", got)
	}
}
