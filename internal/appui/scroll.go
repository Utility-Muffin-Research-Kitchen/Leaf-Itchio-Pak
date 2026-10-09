package appui

// BodyScroll is how far a screen's body text is scrolled, for text that may
// not fit the screen. ScrollLine is the first body line shown. The screen
// sets ScrollMax and ScrollRows (how many body lines show at once) from what
// it draws, so scrolling stays within the text and a page is a screenful.
type BodyScroll struct {
	ScrollLine, ScrollMax, ScrollRows int
}

// SetScrollBounds sets the last line the body can start at and keeps
// ScrollLine within it.
func (s *BodyScroll) SetScrollBounds(maximum int) {
	s.ScrollMax = max(maximum, 0)
	s.clampScroll()
}

// SetScrollRows sets how many body lines show at once, which Left and Right
// page by.
func (s *BodyScroll) SetScrollRows(rows int) { s.ScrollRows = max(rows, 0) }

// ResetScroll starts new body text at its first line.
func (s *BodyScroll) ResetScroll() { s.ScrollLine, s.ScrollMax = 0, 0 }

// HandleScroll moves the body one line for Up or Down and one screen for
// Left or Right, and reports whether button was one of them. A screen is the
// lines that show at once less one, so the last line read stays in view.
func (s *BodyScroll) HandleScroll(button Button) bool {
	switch button {
	case ButtonUp:
		s.ScrollLine--
	case ButtonDown:
		s.ScrollLine++
	case ButtonLeft:
		s.ScrollLine -= s.page()
	case ButtonRight:
		s.ScrollLine += s.page()
	default:
		return false
	}
	s.clampScroll()
	return true
}

func (s *BodyScroll) page() int { return max(s.ScrollRows-1, 1) }

func (s *BodyScroll) clampScroll() {
	s.ScrollLine = min(max(s.ScrollLine, 0), s.ScrollMax)
}
