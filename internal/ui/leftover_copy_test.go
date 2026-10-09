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
		if _, _, err := flow.Activate(model); err != nil || model.State != appui.ManageConfirm || len(model.Prompt) == 0 {
			t.Fatalf("left-over action = state %v, %v", model.State, err)
		}
		return item.Label + " / " + item.Badge, item.Detail, model.Prompt[0].Text
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

// leftOverRow returns the "Delete left-over files" row of the game's Manage
// list, or nil when it offers none.
func leftOverRow(t *testing.T, f *artSetFixture) *appui.ManageItem {
	t.Helper()
	_, model, err := NewCatManageFlow(f.inv, f.invPath, f.game.URL, f.sources, f.catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range model.Items {
		if item.Kind == appui.ManageItemDeleteLeftOver {
			return &item
		}
	}
	return nil
}

// The same upload installed into a second folder leaves the first copy
// flagged. Deleting the second copy in Manage makes the first the only copy,
// and Manage must stop offering it for deletion, or "Delete left-over files"
// would delete the last copy of the game.
func TestManageDoesNotOfferTheOnlyRemainingCopyAsLeftOver(t *testing.T) {
	for name, data := range map[string][]byte{"moss.zip": pico8ArtSetArchives(t)["moss.zip"], "moss.7z": pico8ArtSetArchives(t)["moss.7z"]} {
		t.Run(name, func(t *testing.T) {
			f := newArtSetFixture(t, name, data)
			roms := filepath.Join(f.sources[0].RomsPath, "PICO8")
			f.installInto(filepath.Join(roms, "Moss Garden"), "build:5", data)
			firstCopy := len(f.records())
			f.installInto(filepath.Join(roms, "Moss Garden 2"), "build:5", data)
			row := leftOverRow(t, f)
			if row == nil || !strings.Contains(row.Badge, " OLD") {
				t.Fatalf("no left-over row after the second install: %+v", row)
			}

			// Deleting one cart of the second copy frees only the same cart of
			// the first copy: its other files still have a replacement.
			f.manageDelete(deleteFile("level2.p8", "Moss Garden 2/"))
			before := row.Badge
			row = leftOverRow(t, f)
			if row == nil || row.Badge == before {
				t.Fatalf("left-over row after deleting one replacement = %+v, want one file fewer than %q", row, before)
			}

			// Delete the rest of the second copy, one file at a time.
			for {
				var next *appui.ManageItem
				_, model, err := NewCatManageFlow(f.inv, f.invPath, f.game.URL, f.sources, f.catalog)
				if err != nil {
					t.Fatal(err)
				}
				for _, item := range model.Items {
					if item.Kind == appui.ManageItemFile && strings.Contains(item.Detail, "Moss Garden 2/") {
						next = &item
						break
					}
				}
				if next == nil {
					break
				}
				label, detail := next.Label, next.Detail
				f.manageDelete(func(item appui.ManageItem) bool {
					return item.Kind == appui.ManageItemFile && item.Label == label && item.Detail == detail
				})
			}

			if row := leftOverRow(t, f); row != nil {
				t.Fatalf("%s: Manage still offers %+v after the second copy was deleted", name, row)
			}
			for rel, file := range f.records() {
				if file.LeftOver {
					t.Fatalf("%s: %s is still flagged left over", name, rel)
				}
			}
			if got := len(f.records()); got != firstCopy {
				t.Fatalf("%s: %d records, want the first copy's %d", name, got, firstCopy)
			}
		})
	}
}
