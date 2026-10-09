package roms_test

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// F31: a file name that shows a system the app cannot install names it. The
// extension at the end of the name decides first.
func TestUnsupportedSystemFromTheExtension(t *testing.T) {
	for _, tc := range []struct{ filename, want string }{
		{"Hidden_palace.nds", "Nintendo DS"},
		{"HIDDEN.NDS", "Nintendo DS"},
		{"game.3ds", "Nintendo 3DS"},
		{"game.n64", "Nintendo 64"},
		{"game.z64", "Nintendo 64"},
		{"game.V64", "Nintendo 64"},
		{"game.sfc", "Super Nintendo"},
		{"game.smc", "Super Nintendo"},
		{"game.pocket", "Analogue Pocket"},
		{"setup.exe", "Windows"},
		{"game.apk", "Android"},
		{"game.dmg", "macOS"},
		// Systems the app installs, and extensions that fit more than one
		// system, are left alone.
		{"game.gbc", ""},
		{"game.gb", ""},
		{"game.nes", ""},
		{"disc.iso", ""},
		{"disc.chd", ""},
		{"disc.bin", ""},
		{"game.zip", ""},
		{"game.p8.png", ""},
		{"mystery", ""},
		{"Glory Hunters 2.0", ""},
		{"notes.txt", ""},
	} {
		if got := roms.UnsupportedSystem(tc.filename); got != tc.want {
			t.Errorf("UnsupportedSystem(%q) = %q, want %q", tc.filename, got, tc.want)
		}
	}
}

// F31: itch.io upload names often put a version after the extension, as in
// "Hidden_palace.nds v0.1 (Post-jam bug fix)", so the extension is inside the
// name. It counts only as a whole word after a dot.
func TestUnsupportedSystemInsideTheName(t *testing.T) {
	for _, tc := range []struct{ filename, want string }{
		{"Hidden_palace.nds v0.1 (Post-jam bug fix)", "Nintendo DS"},
		{"Hidden_palace.nds v0.0 (Jam version)", "Nintendo DS"},
		{"Hidden_palace.NDS", "Nintendo DS"},
		{"game.nds.rom", "Nintendo DS"},
		{"game.nds_v2", "Nintendo DS"},
		{"Game.z64 (EU)", "Nintendo 64"},
		{"Game.exe build 4", "Windows"},
		// Not a whole word, or not after a dot.
		{"Glory Hunters v1.3dsomething", ""},
		{"Glory Hunters 1.3ds", ""},
		{"ndsfoo", ""},
		{"nds v2", ""},
		{"game.ndsx v2", ""},
		{"game.gbc v1.2", ""},
		{"Glory Hunters 2.0", ""},
		{"mystery", ""},
	} {
		if got := roms.UnsupportedSystemInName(tc.filename); got != tc.want {
			t.Errorf("UnsupportedSystemInName(%q) = %q, want %q", tc.filename, got, tc.want)
		}
	}
}
