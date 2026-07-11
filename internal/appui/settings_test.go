package appui

import "testing"

func TestSettingsNavigationAndReadOnlyRows(t *testing.T) {
	model := NewSettingsModel("Settings")
	model.SetRows("Leaf", []SettingsRow{
		{Key: SettingsAPIKey, Label: "API Key", ActionEnabled: true},
		{Key: SettingsAppData, Label: "App Data", ActionEnabled: false},
	})
	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	if model.Cursor != 1 {
		t.Fatalf("cursor = %d", model.Cursor)
	}
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != SettingsIntentNone {
		t.Fatalf("read-only A = %v", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonStart, Pressed: true}); got != SettingsIntentNone {
		t.Fatalf("Start = %v, want no-op while already in Settings", got)
	}
}

func TestSettingsConfirmControls(t *testing.T) {
	model := NewSettingsModel("Settings")
	model.SetConfirm("Remove key?", []string{"Downloads stay installed."})
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != SettingsIntentConfirm {
		t.Fatalf("A = %v", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != SettingsIntentCancel {
		t.Fatalf("B = %v", got)
	}
}

func TestSettingsWorkingCanReturnWithoutWaitingForNetwork(t *testing.T) {
	model := NewSettingsModel("Settings")
	model.State = SettingsWorking
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != SettingsIntentBack {
		t.Fatalf("working B = %v", got)
	}
}
