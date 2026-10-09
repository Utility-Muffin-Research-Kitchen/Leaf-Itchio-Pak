package appui

// BodyScroll is how far a screen's body text is scrolled, for text that may
// not fit the screen. ScrollLine is the first body line shown. The screen
// sets ScrollMax from what it draws, so Up and Down stay within the text.
type BodyScroll struct {
	ScrollLine, ScrollMax int
}

// SetScrollBounds sets the last line the body can start at and keeps
// ScrollLine within it.
func (s *BodyScroll) SetScrollBounds(maximum int) {
	s.ScrollMax = max(maximum, 0)
	s.clampScroll()
}

// ResetScroll starts new body text at its first line.
func (s *BodyScroll) ResetScroll() { s.ScrollLine, s.ScrollMax = 0, 0 }

// HandleScroll moves the body one line for Up or Down and reports whether
// button was one of them.
func (s *BodyScroll) HandleScroll(button Button) bool {
	switch button {
	case ButtonUp:
		s.ScrollLine--
	case ButtonDown:
		s.ScrollLine++
	default:
		return false
	}
	s.clampScroll()
	return true
}

func (s *BodyScroll) clampScroll() {
	s.ScrollLine = min(max(s.ScrollLine, 0), s.ScrollMax)
}
