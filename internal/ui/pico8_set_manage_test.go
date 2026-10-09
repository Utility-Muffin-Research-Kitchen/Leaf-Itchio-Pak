//go:build !headless

package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// F19: a Pico-8 game with several files keeps its names. Its playlist lists
// the carts by file name and a cart includes its Lua files by name, so a
// rename of any of them can stop the game from loading. Manage offers no
// rename for a file of such a game, and still offers every delete.
func TestManageOffersNoRenameForAMultiFilePico8Game(t *testing.T) {
	cases := []struct {
		name    string
		members map[string]string
		files   []string
	}{
		{"carts, Lua and a playlist", map[string]string{
			"game/main.p8": "pico-8 cartridge // MAIN\n", "game/level2.p8": "pico-8 cartridge // TWO\n", "game/lib.lua": "-- lib\n",
		}, []string{"Leafbound.m3u", "level2.p8", "lib.lua", "main.p8"}},
		{"one cart and Lua, no playlist", map[string]string{
			"game/main.p8": "pico-8 cartridge // MAIN\n", "game/lib.lua": "-- lib\n",
		}, []string{"lib.lua", "main.p8"}},
		{"two carts and a playlist", map[string]string{
			"a.p8": "pico-8 cartridge // A\n", "b.p8": "pico-8 cartridge // B\n",
		}, []string{"Leafbound.m3u", "a.p8", "b.p8"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources, catalog, _ := destinationFixture(t)
			configureManageFixture(t, sources, catalog)
			inv, invPath := collisionInventory(t)
			data := map[string][]byte{}
			for name, content := range tc.members {
				data[name] = []byte(content)
			}
			gameDir := filepath.Join(sources[0].RomsPath, "PICO8", "Leafbound")
			installArchiveInto(t, inv, invPath, "leafbound.zip", zipOf(t, data), &settings.Config{UnifiedNaming: true},
				func(plan *ZIPPlan) { plan.Pico8GameDir = gameDir + string(filepath.Separator) })
			if got := keys(filesIn(t, gameDir)); len(got) != len(tc.files) {
				t.Fatalf("game folder = %v, want %v", got, tc.files)
			}

			flow, model, err := NewCatManageFlow(inv, invPath, collisionGame.URL, sources, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if rows := renameRows(model); len(rows) != 0 {
				t.Fatalf("rename rows = %v, want none", rows)
			}
			fileRows := 0
			for _, item := range model.Items {
				if item.Kind == appui.ManageItemFile {
					fileRows++
				}
			}
			if fileRows != len(tc.files) {
				t.Fatalf("file rows = %d, want %d", fileRows, len(tc.files))
			}

			// Marking a file as renamed to the title does not offer the
			// way back either.
			entry, _ := inv.Lookup(collisionGame.URL)
			for _, file := range entry.Files {
				file.UnifiedName = true
				inv.UpdateFile(collisionGame.URL, file.DestPath, file)
			}
			if _, model, err = NewCatManageFlow(inv, invPath, collisionGame.URL, sources, catalog); err != nil {
				t.Fatal(err)
			}
			if rows := renameRows(model); len(rows) != 0 {
				t.Fatalf("rename rows for files marked as renamed = %v, want none", rows)
			}

			// Each file can still be deleted.
			model.Cursor = 0
			if _, _, err := flow.Activate(model); err != nil || model.State != appui.ManageConfirm {
				t.Fatalf("delete = state %v, %v", model.State, err)
			}
		})
	}
}

// F19: a single cart keeps its rename rows, whether it sits straight in the
// Pico-8 folder, in a folder you picked for it, or beside the single carts of
// the game's other uploads.
func TestManageKeepsRenameRowsForSinglePico8Carts(t *testing.T) {
	sources, catalog, invPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	pico8 := filepath.Join(sources[0].RomsPath, "PICO8")

	// Two uploads of one game, each an archive holding one cart, saved in
	// the Pico-8 folder, and a cart in a folder picked as the destination.
	fromArchiveNamed := func(archive string) func(*inventory.DownloadedFile) {
		return func(file *inventory.DownloadedFile) {
			file.SourceArchive, file.OriginalUpload, file.SourceMember = archive, archive, file.Filename
		}
	}
	recordROM(t, inv, gloryHuntersURL, "Glory Hunters", filepath.Join(pico8, "web_cart.p8.png"), fromArchiveNamed("web.zip"))
	recordROM(t, inv, gloryHuntersURL, "Glory Hunters", filepath.Join(pico8, "native_cart.p8"), fromArchiveNamed("native.zip"))
	recordROM(t, inv, gloryHuntersURL, "Glory Hunters", filepath.Join(pico8, "Platformers", "jump.p8"), downloaded("jump.p8", false))
	recordROM(t, inv, gloryHuntersURL, "Glory Hunters", filepath.Join(pico8, "Platformers", "run.p8.png"), downloaded("run.p8.png", false))

	flow, model, err := NewCatManageFlow(inv, invPath, gloryHuntersURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	rows := renameRows(model)
	for _, name := range []string{"web_cart.p8.png", "native_cart.p8", "jump.p8", "run.p8.png"} {
		index, ok := rows["Use title for "+name]
		if !ok {
			t.Fatalf("no rename row for %s; rows = %v", name, rows)
		}
		model.Cursor = index
		if rename, _, err := flow.Activate(model); err != nil || rename == nil {
			t.Fatalf("rename %s = %v, %q", name, rename, screentext.FromError(err))
		}
	}
}

// A single cart installed through the archive flow, next to the cart of an
// earlier install of the same game, is renamed after the title as before.
func TestSinglePico8CartFromAnArchiveStillRenamesToTheTitle(t *testing.T) {
	sources, catalog, _ := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv, invPath := collisionInventory(t)
	installArchiveInto(t, inv, invPath, "leafbound.zip",
		zipOf(t, map[string][]byte{"carts/Moss.P8.PNG": []byte("CART")}), &settings.Config{}, nil)
	_, model, err := NewCatManageFlow(inv, invPath, collisionGame.URL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if rows := renameRows(model); len(rows) != 1 {
		t.Fatalf("rename rows = %v, want the title row", rows)
	} else if _, ok := rows["Use title for Moss.p8.png"]; !ok {
		t.Fatalf("rename rows = %v", rows)
	}
}

// F19: the rename flow refuses a file of a multi-file Pico-8 game with a
// plain sentence, however it was reached, and moves nothing.
func TestRenameRefusesAFileOfAMultiFilePico8Game(t *testing.T) {
	sources, catalog, _ := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv, invPath := collisionInventory(t)
	gameDir := filepath.Join(sources[0].RomsPath, "PICO8", "Leafbound")
	members := map[string][]byte{
		"main.p8": []byte("pico-8 cartridge // MAIN\n"), "level2.p8": []byte("pico-8 cartridge // TWO\n"), "lib.lua": []byte("-- lib\n"),
	}
	installArchiveInto(t, inv, invPath, "leafbound.zip", zipOf(t, members), &settings.Config{},
		func(plan *ZIPPlan) { plan.Pico8GameDir = gameDir + string(filepath.Separator) })
	entry, _ := inv.Lookup(collisionGame.URL)
	before := keys(filesIn(t, gameDir))
	for index, file := range entry.Files {
		_, _, err := NewCatRenameFlow(inv, invPath, collisionGame.URL, index, sources)
		if err == nil {
			t.Fatalf("rename of %s was allowed", filepath.Base(file.DestPath))
		}
		assertScreenSentence(t, screentext.FromError(err),
			"Files of a multi-file Pico-8 game keep their original names, so the game still finds them.")
	}
	if after := keys(filesIn(t, gameDir)); len(after) != len(before) {
		t.Fatalf("game folder = %v, was %v", after, before)
	}
	for _, name := range []string{"main.p8", "level2.p8", "lib.lua"} {
		if _, err := os.Stat(filepath.Join(gameDir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
