package appui

import "testing"

func TestManageNavigationSkipsUnavailableFiles(t *testing.T) {
	model := NewManageModel("Manage")
	model.SetItems("2 files", []ManageItem{
		{Kind: ManageItemFile, Label: "missing.gb", Enabled: false},
		{Kind: ManageItemFile, Label: "ready.gb", Enabled: true},
		{Kind: ManageItemDeleteAll, Label: "Delete all", Enabled: true},
	})
	if model.Cursor != 1 {
		t.Fatalf("cursor = %d, want first enabled row", model.Cursor)
	}
	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	if model.Cursor != 2 {
		t.Fatalf("cursor = %d, want delete row", model.Cursor)
	}
}

func TestManageConfirmationControls(t *testing.T) {
	model := NewManageModel("Manage")
	model.SetConfirm("Delete?", []string{"game.gb"})
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != ManageIntentConfirm {
		t.Fatalf("A = %v, want confirm", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != ManageIntentCancel {
		t.Fatalf("B = %v, want cancel", got)
	}
}
