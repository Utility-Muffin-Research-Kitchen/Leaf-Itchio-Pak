//go:build !headless

package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

func manageDeleteAllBadge(t *testing.T, model *appui.ManageModel) string {
	t.Helper()
	for _, item := range model.Items {
		if item.Kind == appui.ManageItemDeleteAll {
			return item.Badge
		}
	}
	t.Fatalf("items = %+v, want a delete-all row", model.Items)
	return ""
}

// F29: Manage counts the files of one game with the right number.
func TestCatManageCountsFiles(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/game"
	addManagedROM(t, inv, gameURL, "Game", filepath.Join(sources[0].RomsPath, "GBC", "Game.gbc"))
	flow, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(model.Subtitle, "1 managed file ") {
		t.Fatalf("one file: subtitle = %q", model.Subtitle)
	}
	if got := manageDeleteAllBadge(t, model); got != "1 FILE" {
		t.Fatalf("one file: delete-all badge = %q, want 1 FILE", got)
	}

	addManagedROM(t, inv, gameURL, "Game", filepath.Join(sources[0].RomsPath, "GBA", "Game.gba"))
	flow, model, err = NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(model.Subtitle, "2 managed files ") {
		t.Fatalf("two files: subtitle = %q", model.Subtitle)
	}
	if got := manageDeleteAllBadge(t, model); got != "2 FILES" {
		t.Fatalf("two files: delete-all badge = %q, want 2 FILES", got)
	}

	// Deleting one file reports one file.
	model.Cursor = 0
	if _, _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Confirm(model); err != nil {
		t.Fatal(err)
	}
	if model.Message != "Deleted 1 managed file." {
		t.Fatalf("result = %q, want %q", model.Message, "Deleted 1 managed file.")
	}
}
