//go:build !headless

package ui

import (
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// F24: a set that keeps a cart in a subfolder (game/world2/main.p8) offers no
// rename for that cart either. The cart is one of the set's files, from the
// same archive, and the other carts or the playlist may load it by name.
func TestManageOffersNoRenameForACartInASetsSubfolder(t *testing.T) {
	sources, catalog, _ := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv, invPath := collisionInventory(t)
	gameDir := filepath.Join(sources[0].RomsPath, "PICO8", "Leafbound")
	installArchiveInto(t, inv, invPath, "leafbound.zip", zipOf(t, map[string][]byte{
		"game/main.p8": []byte("pico-8 cartridge // MAIN\n"), "game/world2/main.p8": []byte("pico-8 cartridge // WORLD2\n"),
		"game/lib.lua": []byte("-- lib\n"),
	}), &settings.Config{UnifiedNaming: true},
		func(plan *ZIPPlan) { plan.Pico8GameDir = gameDir + string(filepath.Separator) })
	tree := treeOf(t, gameDir)
	if _, ok := tree["world2/main.p8"]; !ok {
		t.Fatalf("game folder = %v, want the nested cart", keys(tree))
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
	if fileRows != len(tree) {
		t.Fatalf("file rows = %d for files %v", fileRows, keys(tree))
	}
	// Each file can still be deleted.
	model.Cursor = 0
	if _, _, err := flow.Activate(model); err != nil || model.State != appui.ManageConfirm {
		t.Fatalf("delete = state %v, %v", model.State, err)
	}
}

// F24: one single-cart upload installed into two folders (the second install
// does not replace the first) is no set. Both carts keep their rename rows,
// the one inside the other's folder included.
func TestManageKeepsRenameRowsForOneCartInstalledIntoTwoFolders(t *testing.T) {
	sources, catalog, invPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	pico8 := filepath.Join(sources[0].RomsPath, "PICO8")
	fromJumpZip := func(file *inventory.DownloadedFile) {
		file.SourceArchive, file.OriginalUpload, file.SourceMember = "jump.zip", "jump.zip", file.Filename
	}
	paths := []string{
		filepath.Join(pico8, "Platformers", "jump.p8"),
		filepath.Join(pico8, "Puzzles", "jump.p8"),
		filepath.Join(pico8, "Platformers", "Copy", "jump.p8"),
	}
	for _, path := range paths {
		recordROM(t, inv, gloryHuntersURL, "Glory Hunters", path, fromJumpZip)
	}

	_, model, err := NewCatManageFlow(inv, invPath, gloryHuntersURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, item := range model.Items {
		if item.Kind == appui.ManageItemRename && item.Label == "Use title for jump.p8" {
			rows++
		}
	}
	if rows != len(paths) {
		t.Fatalf("rename rows for the copies of jump.p8 = %d, want %d", rows, len(paths))
	}
}
