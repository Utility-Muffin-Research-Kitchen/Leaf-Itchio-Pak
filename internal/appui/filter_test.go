package appui

import "testing"

func press(button Button) InputEvent { return InputEvent{Button: button, Pressed: true} }

func TestFilterModelStagesApplyAndCancel(t *testing.T) {
	m := NewFilterModel("GB", "az", "leaf")
	m.Handle(press(ButtonDown))
	m.Handle(press(ButtonRight))
	if m.Platform != "GBC" {
		t.Fatalf("platform = %q, want GBC", m.Platform)
	}
	if got := m.Handle(press(ButtonB)); got != FilterIntentCancel {
		t.Fatalf("cancel intent = %v", got)
	}
	if got := m.Handle(press(ButtonSelect)); got != FilterIntentApply {
		t.Fatalf("apply intent = %v", got)
	}
}

func TestFilterModelClear(t *testing.T) {
	m := NewFilterModel("GBA", "paid", "game")
	m.Handle(press(ButtonY))
	if m.Platform != "" || m.Sort != "" || m.Query != "" {
		t.Fatalf("clear left values: %#v", m)
	}
}

func TestFilterIncludesPlayStation(t *testing.T) {
	m := NewFilterModel("PSX", "", "")
	if m.Platform != "PSX" || m.PlatformLabel() != "PlayStation" {
		t.Fatalf("PlayStation filter = %q/%q", m.Platform, m.PlatformLabel())
	}
}
