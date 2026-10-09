package catui

import (
	"strings"
	"testing"
)

// D9: Up and Down scroll About and never close it; A, B and Start still do.
func TestAboutScrollsWithoutClosing(t *testing.T) {
	screen := &AboutScreen{}
	screen.scroll.SetScrollBounds(2)
	if screen.HandleInput(InputEvent{Button: ButtonDown, Pressed: true}) {
		t.Fatal("Down closed About")
	}
	if screen.scroll.ScrollLine != 1 {
		t.Fatalf("Down scrolled to line %d, want 1", screen.scroll.ScrollLine)
	}
	if screen.HandleInput(InputEvent{Button: ButtonUp, Pressed: true}) {
		t.Fatal("Up closed About")
	}
	if screen.scroll.ScrollLine != 0 {
		t.Fatalf("Up scrolled to line %d, want 0", screen.scroll.ScrollLine)
	}
	for _, button := range []Button{ButtonA, ButtonB, ButtonStart} {
		if !screen.HandleInput(InputEvent{Button: button, Pressed: true}) {
			t.Fatalf("button %d no longer closes About", button)
		}
	}
	if screen.HandleInput(InputEvent{Button: ButtonB, Wake: true, Pressed: true}) {
		t.Fatal("a wake closed About")
	}
}

// D9: the repository is a caption under the QR code, not a body line, so
// the body fits at the device's font size.
func TestAboutLeavesTheRepositoryToTheQRCaption(t *testing.T) {
	for _, paragraph := range aboutParagraphs("v0.13.0") {
		if strings.Contains(paragraph, "Repository") || strings.Contains(paragraph, "QR") {
			t.Fatalf("body still mentions the repository: %q", paragraph)
		}
	}
	// The Leaf version closes the credits block without a blank line
	// before it, which leaves a spare line for a wrapped upstream name.
	if got := aboutParagraphs("v0.13.0"); len(got) != 2 || !strings.HasSuffix(got[1], "Kitchen\nLeaf v0.13.0") {
		t.Fatalf("body = %#v, want the description, then the credits ending in the Leaf version", got)
	}
	if aboutQRCaption != "Scan for the source code" {
		t.Fatalf("caption = %q", aboutQRCaption)
	}
}

// The QR code and its caption are centered as one in their column; a short
// column shrinks the code so the caption still fits under it.
func TestAboutQRLeavesRoomForItsCaption(t *testing.T) {
	if got := captionedQRRect(Rect{X: 10, Y: 20, W: 300, H: 500}, 30); got != (Rect{X: 10, Y: 105, W: 300, H: 300}) {
		t.Fatalf("tall column QR = %+v", got)
	}
	if got := captionedQRRect(Rect{X: 10, Y: 20, W: 300, H: 250}, 30); got != (Rect{X: 50, Y: 20, W: 220, H: 220}) {
		t.Fatalf("short column QR = %+v", got)
	}
}
