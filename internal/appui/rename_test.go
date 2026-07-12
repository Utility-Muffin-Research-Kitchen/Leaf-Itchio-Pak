package appui

import "testing"

func TestRenamePromptsUseBackThenSkip(t *testing.T) {
	model := NewRenameModel("Rename")
	model.SetPrompt(RenameConfirmROM, "ROM", "Rename?", nil)
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != RenameIntentBack {
		t.Fatalf("ROM B = %v, want back", got)
	}
	model.SetPrompt(RenameConfirmSaves, "Saves", "Rename?", nil)
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != RenameIntentSkip {
		t.Fatalf("save B = %v, want skip", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != RenameIntentConfirm {
		t.Fatalf("save A = %v, want confirm", got)
	}
}

func TestRenameDoneKeepsLibraryStatusSeparate(t *testing.T) {
	model := NewRenameModel("Rename")
	model.SetDone("ROM renamed.")
	model.SetLibraryStatus("Files changed · automatic rescan failed; use Rescan in Leaf.")
	if model.State != RenameDone || model.Message != "ROM renamed." || model.LibraryStatus == "" {
		t.Fatalf("rename result = %#v", model)
	}
}
