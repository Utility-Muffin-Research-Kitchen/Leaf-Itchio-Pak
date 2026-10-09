//go:build !headless

package ui

import (
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// F18: roms.ROMExt reports ".p8.png" in lower case whatever the member's
// case, so an install that cut the stem with a case-sensitive TrimSuffix
// kept "GAME.P8.PNG" whole and added the extension again.
//
// A member installs with its extension in lower case and the rest of its
// name as it was. Rename ROM Files names it after the title. Manage's
// "Use original name" gives the name the install without renaming gives.
func TestArchiveInstallsAMixedCaseExtensionOnce(t *testing.T) {
	cases := []struct {
		member, system, plain, titled string
	}{
		{"GAME.P8.PNG", "PICO8", "GAME.p8.png", "Leafbound.p8.png"},
		{"release/Cart.P8.Png", "PICO8", "Cart.p8.png", "Leafbound.p8.png"},
		{"Level 1.P8", "PICO8", "Level 1.p8", "Leafbound.p8"},
		{"Super Game.Gb", "GB", "Super Game.gb", "Leafbound.gb"},
	}
	for _, tc := range cases {
		t.Run(tc.member, func(t *testing.T) {
			data := zipOf(t, map[string][]byte{tc.member: gbROM("CART")})

			sources, catalog, _ := destinationFixture(t)
			configureManageFixture(t, sources, catalog)
			dir := filepath.Join(sources[0].RomsPath, tc.system)
			inv, invPath := collisionInventory(t)
			installArchiveInto(t, inv, invPath, "leafbound.zip", data, &settings.Config{}, nil)
			if got := keys(filesIn(t, dir)); len(got) != 1 || got[0] != tc.plain {
				t.Fatalf("install without renaming wrote %v, want only %s", got, tc.plain)
			}

			sources, catalog, _ = destinationFixture(t)
			configureManageFixture(t, sources, catalog)
			dir = filepath.Join(sources[0].RomsPath, tc.system)
			inv, invPath = collisionInventory(t)
			installArchiveInto(t, inv, invPath, "leafbound.zip", data, &settings.Config{UnifiedNaming: true}, nil)
			if got := keys(filesIn(t, dir)); len(got) != 1 || got[0] != tc.titled {
				t.Fatalf("install with renaming wrote %v, want only %s", got, tc.titled)
			}
			entry, _ := inv.Lookup(collisionGame.URL)
			if got := originalName(entry.Files[0]); got != tc.plain {
				t.Fatalf("original name = %q, want %q", got, tc.plain)
			}
			flow, model, err := NewCatManageFlow(inv, invPath, collisionGame.URL, sources, catalog)
			if err != nil {
				t.Fatal(err)
			}
			index, ok := renameRows(model)["Use original name for "+tc.titled]
			if !ok {
				t.Fatalf("rename rows = %v", renameRows(model))
			}
			model.Cursor = index
			rename, renameModel, err := flow.Activate(model)
			if err != nil || rename == nil {
				t.Fatalf("rename = %v, %v", rename, err)
			}
			if err := rename.Confirm(renameModel); err != nil || renameModel.State != appui.RenameDone {
				t.Fatalf("confirm = state %v, %v", renameModel.State, err)
			}
			if got := keys(filesIn(t, dir)); len(got) != 1 || got[0] != tc.plain {
				t.Fatalf("restored %v, want only %s", got, tc.plain)
			}
		})
	}
}

// F18: the name a second game's file takes ("<Title> - <name>") keeps the
// extension once too.
func TestInstallNextToAnotherGamesFileKeepsTheExtensionOnce(t *testing.T) {
	sources, catalog, _ := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	dir := filepath.Join(sources[0].RomsPath, "PICO8")
	inv, invPath := collisionInventory(t)
	taken := filepath.Join(dir, "GAME.p8.png")
	addManagedROM(t, inv, "https://other.itch.io/game", "Other Game", taken)

	installArchiveInto(t, inv, invPath, "leafbound.zip",
		zipOf(t, map[string][]byte{"GAME.P8.PNG": []byte("CART")}), &settings.Config{}, nil)
	got := filesIn(t, dir)
	if len(got) != 2 || got["GAME.p8.png"] != "rom" || got["Leafbound - GAME.p8.png"] != "CART" {
		t.Fatalf("PICO8 folder = %v, want the other game's file and Leafbound - GAME.p8.png", keys(got))
	}

	// A download saved under its upload's own name, in capitals.
	tune := filepath.Join(dir, "TUNE.P8.PNG")
	addManagedROM(t, inv, "https://other.itch.io/tune", "Other Tune", tune)
	path, err := newInstallNamer(inv, collisionGame, "TUNE.P8.PNG", nil).ownName(tune)
	if err != nil || path != filepath.Join(dir, "Leafbound - TUNE.p8.png") {
		t.Fatalf("name next to the other game's file = %q, %v; want Leafbound - TUNE.p8.png", path, err)
	}
}

// F18: the stems used to match an upload and to name a copy of it drop the
// compound extension in any letter case.
func TestInstallNamerStemsIgnoreExtensionCase(t *testing.T) {
	namer := newInstallNamer(nil, collisionGame, "GAME.P8.PNG", nil)
	if got := namer.uploadStem(); got != "GAME" {
		t.Fatalf("upload stem = %q, want GAME", got)
	}
	// The page lists the upload by its name without the format suffix.
	namer.listing = offers("GAME")
	file := inventory.DownloadedFile{Filename: "GAME.P8.PNG", OriginalUpload: "GAME.P8.PNG"}
	if namer.superseded(file) {
		t.Fatal("an upload the page still lists was treated as replaced")
	}
}

// F18: saves and states of a cart named in capitals follow it.
func TestRenamePairsIgnoreTheCaseOfTheCartExtension(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, map[string]string{
		filepath.Join(root, "GAME.srm"):          "save",
		filepath.Join(root, "GAME.state1"):       "state",
		filepath.Join(root, "Unrelated.srm"):     "other",
		filepath.Join(root, "GAME.P8.PNG.state"): "whole name",
	})
	pairs, err := discoverRenamePairs(root, "GAME.P8.PNG", "Leafbound.P8.PNG", false)
	if err != nil {
		t.Fatal(err)
	}
	var moved []string
	for _, pair := range pairs {
		moved = append(moved, filepath.Base(pair.oldPath)+" -> "+filepath.Base(pair.newPath))
	}
	if len(moved) != 1 || moved[0] != "GAME.srm -> Leafbound.srm" {
		t.Fatalf("save pairs = %v", moved)
	}
}
