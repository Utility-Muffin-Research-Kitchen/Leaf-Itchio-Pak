//go:build !headless

package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// leftOverActionText returns what the "Delete left-over files" row and its
// confirmation say about the files.
func leftOverActionText(t *testing.T, inv *inventory.Inventory, invPath string, f *artSetFixture) (string, string, string) {
	t.Helper()
	flow, model, err := NewCatManageFlow(inv, invPath, f.game.URL, f.sources, f.catalog)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range model.Items {
		if item.Kind != appui.ManageItemDeleteLeftOver {
			continue
		}
		model.Cursor = index
		if _, _, err := flow.Activate(model); err != nil || model.State != appui.ManageConfirm || len(model.PromptLines) == 0 {
			t.Fatalf("left-over action = state %v, %v", model.State, err)
		}
		return item.Label + " / " + item.Badge, item.Detail, model.PromptLines[0]
	}
	t.Fatalf("no left-over row in %+v", model.Items)
	return "", "", ""
}

// F28: the same upload installed again into another folder leaves the first
// copy behind, and Manage calls it an earlier copy, not a file of an older
// version. An update of the upload still leaves files of an older version.
func TestManageNamesAnEarlierCopyOfTheSameUpload(t *testing.T) {
	archive := pico8ArtSetArchives(t)["moss.zip"]
	f := newArtSetFixture(t, "moss.zip", archive)
	roms := filepath.Join(f.sources[0].RomsPath, "PICO8")

	f.installInto(filepath.Join(roms, "Moss Garden"), "build:5", archive)
	f.installInto(filepath.Join(roms, "Moss Garden 2"), "build:5", archive)
	label, detail, prompt := leftOverActionText(t, f.inv, f.invPath, f)
	const want = "Earlier copy of files you installed again"
	if !strings.HasPrefix(label, "Delete left-over files / 6 OLD") || detail != want || prompt != want {
		t.Fatalf("copy: label %q detail %q prompt %q, want %q", label, detail, prompt, want)
	}

	// An update installed into the second folder: the first folder's files
	// are an older version of it now, not a copy.
	update := zipOf(t, map[string][]byte{
		"game/main.p8": []byte("pico-8 cartridge // MAIN 2\n"), "game/lib.lua": []byte("-- lib 2\n"),
	})
	f.installInto(filepath.Join(roms, "Moss Garden 2"), "build:6", update)
	_, detail, prompt = leftOverActionText(t, f.inv, f.invPath, f)
	if detail != "Left over from an older version" || prompt != detail {
		t.Fatalf("update: detail %q prompt %q, want the older-version text", detail, prompt)
	}
}
