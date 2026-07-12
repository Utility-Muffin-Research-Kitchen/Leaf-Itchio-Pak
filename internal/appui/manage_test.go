package appui

import "testing"

func TestManageNavigationIncludesUnavailableFilesForInspection(t *testing.T) {
	model := NewManageModel("Manage")
	model.SetItems("2 files", []ManageItem{
		{Kind: ManageItemFile, Label: "missing.gb", Enabled: false},
		{Kind: ManageItemFile, Label: "ready.gb", Enabled: true},
		{Kind: ManageItemDeleteAll, Label: "Delete all", Enabled: true},
	})
	if model.Cursor != 0 {
		t.Fatalf("cursor = %d, want first row", model.Cursor)
	}
	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	if model.Cursor != 1 {
		t.Fatalf("cursor = %d, want unavailable row to be focusable", model.Cursor)
	}
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != ManageIntentActivate {
		t.Fatalf("unavailable A = %v, want controller explanation", got)
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

func TestManageResultKeepsLibraryStatusSeparate(t *testing.T) {
	model := NewManageModel("Manage")
	model.SetResult("Deleted 2 managed files.")
	model.SetLibraryStatus("Leaf library rescan queued.")
	if model.State != ManageResult || model.Message != "Deleted 2 managed files." ||
		model.LibraryStatus != "Leaf library rescan queued." {
		t.Fatalf("management result = %#v", model)
	}
}
