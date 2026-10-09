package appui

import "testing"

func TestBodyScrollStaysWithinBounds(t *testing.T) {
	var scroll BodyScroll
	if scroll.HandleScroll(ButtonDown); scroll.ScrollLine != 0 {
		t.Fatalf("scrolled to %d before the body was drawn", scroll.ScrollLine)
	}
	scroll.SetScrollBounds(2)
	for range 3 {
		scroll.HandleScroll(ButtonDown)
	}
	if scroll.ScrollLine != 2 {
		t.Fatalf("Down x3 = line %d, want 2", scroll.ScrollLine)
	}
	scroll.SetScrollBounds(1)
	if scroll.ScrollLine != 1 {
		t.Fatalf("shrunk bounds left line %d, want 1", scroll.ScrollLine)
	}
	for range 3 {
		scroll.HandleScroll(ButtonUp)
	}
	if scroll.ScrollLine != 0 {
		t.Fatalf("Up x3 = line %d, want 0", scroll.ScrollLine)
	}
	if scroll.HandleScroll(ButtonA) {
		t.Fatal("A counted as a scroll button")
	}
	scroll.SetScrollBounds(-4)
	if scroll.ScrollMax != 0 {
		t.Fatalf("negative bound = %d, want 0", scroll.ScrollMax)
	}
	scroll.SetScrollBounds(3)
	scroll.HandleScroll(ButtonDown)
	scroll.ResetScroll()
	if scroll.ScrollLine != 0 {
		t.Fatalf("reset left line %d", scroll.ScrollLine)
	}
}

// scrollsOnce checks that Down moves the body one line and returns no intent,
// so scrolling never closes or confirms a screen.
func scrollsOnce[I comparable](t *testing.T, name string, scroll *BodyScroll, handle func(InputEvent) I) {
	t.Helper()
	var none I
	scroll.SetScrollBounds(3)
	if got := handle(press(ButtonDown)); got != none {
		t.Fatalf("%s: Down intent = %v, want none", name, got)
	}
	if scroll.ScrollLine != 1 {
		t.Fatalf("%s: Down scrolled to line %d, want 1", name, scroll.ScrollLine)
	}
	if got := handle(press(ButtonUp)); got != none {
		t.Fatalf("%s: Up intent = %v, want none", name, got)
	}
	if scroll.ScrollLine != 0 {
		t.Fatalf("%s: Up scrolled to line %d, want 0", name, scroll.ScrollLine)
	}
	scroll.HandleScroll(ButtonDown)
}

func TestDetailStartsANewBodyAtTheTop(t *testing.T) {
	model := NewDetailModel(DetailGame{Title: "Leafbound", Downloaded: true})
	model.SetReady("<p>One</p><p>Two</p>", nil, nil, false, false)
	model.SetScrollBounds(3)
	model.Handle(press(ButtonDown))
	model.SetError("Can't reach itch.io. Check the connection, then reopen this game.")
	if model.ScrollLine != 0 {
		t.Fatalf("unavailable page kept line %d", model.ScrollLine)
	}
	scrollsOnce(t, "unavailable", &model.BodyScroll, model.Handle)
}
