//go:build !headless

package ui

import (
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// A .bin installs from an archive only when a .cue it installs references
// it, so a BIOS image shipped next to the game stays out of the PlayStation
// folder (review finding R23-7, seen with Yume Nikki's openbios.bin).
func TestArchiveInstallsOnlyBINsACueReferences(t *testing.T) {
	cue := []byte("FILE \"Game (Track 1).bin\" BINARY\r\n  TRACK 01 MODE2/2352\r\n    INDEX 01 00:00:00\r\n")
	data := zipOf(t, map[string][]byte{
		"Yume/game.cue": cue, "Yume/Game (Track 1).bin": []byte("TRACK"), "Yume/openbios.bin": []byte("BIOS"),
	})
	worker, gbDir := runArchive(t, "yume.zip", data, &settings.Config{}, false)
	got := filesIn(t, filepath.Join(filepath.Dir(gbDir), "PS"))
	if len(got) != 2 || got["game.cue"] == "" || got["Game (Track 1).bin"] != "TRACK" {
		t.Fatalf("PS folder = %v (extracted %v)", keys(got), worker.extracted)
	}
}

// Without a .cue in the archive a .bin installs as before.
func TestArchiveWithoutACueInstallsItsBIN(t *testing.T) {
	data := zipOf(t, map[string][]byte{"track.bin": []byte("TRACK")})
	worker, gbDir := runArchive(t, "track.zip", data, &settings.Config{}, false)
	if got := filesIn(t, filepath.Join(filepath.Dir(gbDir), "PS")); len(got) != 1 || got["track.bin"] != "TRACK" {
		t.Fatalf("PS folder = %v (extracted %v)", keys(got), worker.extracted)
	}
}
