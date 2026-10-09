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

// The list header names the platform and sort the way the filter screen
// does, so a PSX filter reads "PlayStation" in both.
func TestHeaderLabelsMatchTheFilterScreen(t *testing.T) {
	for _, code := range FilterPlatforms {
		if got, want := PlatformLabel(code), NewFilterModel(code, "", "").PlatformLabel(); got != want {
			t.Errorf("PlatformLabel(%q) = %q, want the filter's %q", code, got, want)
		}
	}
	for _, value := range FilterSortValues {
		if got, want := SortLabel(value), NewFilterModel("", value, "").SortLabel(); got != want {
			t.Errorf("SortLabel(%q) = %q, want the filter's %q", value, got, want)
		}
	}
	if got := PlatformLabel("PSX"); got != "PlayStation" {
		t.Errorf("PlatformLabel(PSX) = %q, want PlayStation", got)
	}
	// A code the filter does not list keeps its own name rather than
	// claiming "All platforms".
	if got := PlatformLabel("SNES"); got != "SNES" {
		t.Errorf("PlatformLabel(SNES) = %q, want SNES", got)
	}
	if got := SortLabel("bogus"); got != "Popular" {
		t.Errorf("SortLabel(bogus) = %q, want Popular like the sort cycle", got)
	}
	// The default sort is itch.io's browse order, which it calls Popular.
	// The stored value stays "", so saved filters keep working.
	if got := SortLabel(""); got != "Popular" {
		t.Errorf("SortLabel(\"\") = %q, want Popular", got)
	}
}
