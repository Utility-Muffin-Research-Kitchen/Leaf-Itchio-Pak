//go:build !headless

package ui

import (
	"fmt"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// pico8ManifestFor builds the inspected contents of an archive that holds
// pngs compiled carts, p8s text carts, luas Lua files and a soundtrack when
// music is set.
func pico8ManifestFor(pngs, p8s, luas int, music bool) roms.ZIPManifest {
	var manifest roms.ZIPManifest
	add := func(name string, kind roms.FileKind) {
		manifest.Entries = append(manifest.Entries, roms.ZIPEntry{Name: name, Kind: kind})
	}
	for i := 0; i < pngs; i++ {
		add(fmt.Sprintf("game/cart%d.p8.png", i), roms.KindROM)
	}
	for i := 0; i < p8s; i++ {
		add(fmt.Sprintf("game/level%d.p8", i), roms.KindROM)
	}
	for i := 0; i < luas; i++ {
		add(fmt.Sprintf("game/lib%d.lua", i), roms.KindOther)
	}
	if music {
		add("game/theme.ogg", roms.KindMusic)
	}
	return manifest
}

// The multi-file extractors once gave the only .p8.png of an archive the
// game's title. They never had one to rename: an archive with exactly one
// .p8.png goes to the multi-file extractor in no combination of carts, Lua
// files and music (CatArchiveFlow.prepareInitialAction sends it to a normal
// install first, or ZIPManifest.IsPico8MultiFileGame refuses it).
//
// The inspection could count a second .p8.png for such an archive: it read a
// member with no Pico-8 name by its bytes, and a PNG 128 pixels wide counted as
// a cart that the extractors then skipped. A PNG is no longer a cart by its
// bytes (roms.ClassifyArchiveMember), so that route is closed too; see
// TestAPNGWithoutACartNameIsNotACart.
func TestAnArchiveWithOneP8PNGNeverRoutesToTheMultiFileExtractor(t *testing.T) {
	multiFile := 0
	for pngs := 0; pngs <= 3; pngs++ {
		for p8s := 0; p8s <= 2; p8s++ {
			for luas := 0; luas <= 1; luas++ {
				for _, music := range []bool{false, true} {
					for _, ask := range []string{"ask", "auto"} {
						manifest := pico8ManifestFor(pngs, p8s, luas, music)
						flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "auto", MusicDownload: ask, MusicLocation: "auto"}, manifest)
						flow.prepareInitialAction()
						routed := flow.plan.Pico8GameDir != ""
						if routed {
							multiFile++
						}
						if routed && pngs == 1 {
							t.Fatalf("%d .p8.png, %d .p8, %d .lua, music %v (%s): routed to the multi-file extractor", pngs, p8s, luas, music, ask)
						}
					}
				}
			}
		}
	}
	if multiFile == 0 {
		t.Fatal("no combination reached the multi-file extractor; the routing this test covers has changed")
	}
}
