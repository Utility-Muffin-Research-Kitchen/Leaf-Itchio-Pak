//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
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

// The one way an archive reached that rename: the inspection reads a member
// that has no Pico-8 extension by its bytes, so a PNG 128 pixels wide counts as
// a cart, while the extractors choose members by name. The inspection then
// counts two .p8.png carts where one is installed, and the archive is routed to
// the multi-file extractor with a single .p8.png. A set's files keep the names
// the game refers to them by, so that cart is not renamed to the title.
func TestMultiFileSetKeepsTheNamesOfItsCarts(t *testing.T) {
	phantom := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x80"), make([]byte, 32)...)
	data := zipOf(t, map[string][]byte{
		"game/cart.p8.png": []byte("compiled cart"),
		"game/level2.p8":   []byte("pico-8 cartridge // LEVEL2\n"),
		"game/data":        phantom,
	})
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifestFromZIP(reader.File)
	if err != nil {
		t.Fatal(err)
	}
	flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "auto", UnifiedNaming: true}, manifest)
	flow.prepareInitialAction()
	if flow.plan.Pico8GameDir == "" {
		t.Fatalf("manifest %+v is not routed to the multi-file extractor; this test needs another way to reach it", manifest.Entries)
	}

	worker, dir := runArchive(t, "leafbound.zip", data, &settings.Config{UnifiedNaming: true}, true)
	got := treeOf(t, dir)
	names := make([]string, 0, len(got))
	for name := range got {
		if !isPlaylist(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if fmt.Sprint(names) != "[cart.p8.png level2.p8]" {
		t.Fatalf("game folder = %v (skipped %v), want the carts under their own names", names, worker.skipped)
	}
}
