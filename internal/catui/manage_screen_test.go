package catui

import (
	"testing"
	"unicode/utf8"
)

// A long archive member under a Manage row ends in "..." and never splits a
// character, so a Japanese file name stays legible up to the cut.
func TestFitNoteTextCutsBetweenCharacters(t *testing.T) {
	measure := func(text string) int { return utf8.RuneCountInString(text) }
	for _, tc := range []struct {
		text  string
		width int
		want  string
	}{
		{"From Glory Hunters 1.3.gba", 40, "From Glory Hunters 1.3.gba"},
		{"From Glory Hunters 1.3 EZ IV Patched.gba", 20, "From Glory Hunter..."},
		{"From 葉/リーフバウンド.gbc", 12, "From 葉/リー..."},
		{"From Glory", 8, "From..."},
		{"From Glory", 2, "..."},
	} {
		if got := fitNoteText(tc.text, tc.width, measure); got != tc.want {
			t.Errorf("fitNoteText(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
		}
	}
}
