package roms_test

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// ROMExt reports the compound ".p8.png" in lower case whatever the case of
// the name, so cutting a stem with strings.TrimSuffix(name, ROMExt(name))
// leaves "GAME.P8.PNG" whole. TrimROMExt cuts by length.
func TestTrimROMExtIgnoresLetterCase(t *testing.T) {
	tests := map[string]string{
		"game.p8.png":        "game",
		"GAME.P8.PNG":        "GAME",
		"Game.P8.png":        "Game",
		"Game.p8.PNG":        "Game",
		"my.game.P8.PNG":     "my.game",
		"game.p8":            "game",
		"GAME.P8":            "GAME",
		"Super Game.Gb":      "Super Game",
		"Super Game.GB":      "Super Game",
		"disc.cue":           "disc",
		"archive.tar.gz":     "archive.tar",
		"noextension":        "noextension",
		".p8.png":            "",
		"cart.p8.png.p8.png": "cart.p8.png",
		"photo.PNG":          "photo",
	}
	for name, want := range tests {
		if got := roms.TrimROMExt(name); got != want {
			t.Errorf("TrimROMExt(%q) = %q, want %q", name, got, want)
		}
	}
}
