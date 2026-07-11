package appui

import "testing"

func TestDownloadSelectionNavigationAndFormatCycling(t *testing.T) {
	model := NewDownloadSelectModel("Game")
	model.SetChoices("Files", []DownloadChoice{
		{Title: "first"},
		{Title: "mystery", Badge: "AUTO", FormatOptions: []string{"AUTO", "GB", "GBC"}},
	})
	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	model.Handle(InputEvent{Button: ButtonLeft, Pressed: true})
	if model.Cursor != 1 || model.Choices[1].Badge != "GBC" {
		t.Fatalf("cursor/badge = %d/%q, want 1/GBC", model.Cursor, model.Choices[1].Badge)
	}
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != DownloadSelectIntentChoose {
		t.Fatalf("A intent = %v, want choose", got)
	}
}

func TestDownloadProgressCancelsButDoesNotNavigateWhileRunning(t *testing.T) {
	model := DownloadProgressModel{State: DownloadProgressRunning}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != DownloadProgressIntentCancel {
		t.Fatalf("running B intent = %v, want cancel", got)
	}
	model.State = DownloadProgressInhibitBlocked
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != DownloadProgressIntentContinue {
		t.Fatalf("blocked A intent = %v, want continue", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != DownloadProgressIntentBack {
		t.Fatalf("blocked B intent = %v, want back", got)
	}
}

func TestDownloadProgressLockedTransactionIgnoresCancel(t *testing.T) {
	model := DownloadProgressModel{State: DownloadProgressRunning, Locked: true}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != DownloadProgressIntentNone {
		t.Fatalf("locked B intent = %v", got)
	}
}
