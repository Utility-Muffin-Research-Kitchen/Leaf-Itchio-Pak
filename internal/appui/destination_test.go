package appui

import "testing"

func TestDestinationSkipsUnavailableSource(t *testing.T) {
	model := NewDestinationModel("Destination")
	model.SetSources([]DestinationItem{
		{Kind: DestinationItemSource, Label: "Primary", Enabled: true},
		{Kind: DestinationItemSource, Label: "Secondary", Enabled: false},
		{Kind: DestinationItemSource, Label: "Third", Enabled: true},
	})
	model.Handle(InputEvent{Button: ButtonDown, Pressed: true})
	if model.Cursor != 2 {
		t.Fatalf("cursor = %d, want 2", model.Cursor)
	}
}

func TestDestinationFolderControls(t *testing.T) {
	model := NewDestinationModel("Destination")
	model.SetFolders("Choose folder", "Primary / GBC", []DestinationItem{
		{Kind: DestinationItemSave, Label: "Save here", Enabled: true},
		{Kind: DestinationItemFolder, Label: "RPG", Enabled: true},
	})
	if got := model.Handle(InputEvent{Button: ButtonA, Pressed: true}); got != DestinationIntentActivate {
		t.Fatalf("A intent = %v, want activate", got)
	}
	if got := model.Handle(InputEvent{Button: ButtonB, Pressed: true}); got != DestinationIntentBack {
		t.Fatalf("B intent = %v, want back", got)
	}
}
