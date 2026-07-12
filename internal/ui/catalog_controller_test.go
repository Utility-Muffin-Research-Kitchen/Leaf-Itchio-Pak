//go:build !headless

package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func TestCatalogControllerBindsFilteredGamesToCatModel(t *testing.T) {
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		cachedGames: []itchio.Game{
			{Title: "PSX Homebrew", URL: "https://example.invalid/psx", Platform: "PSX", IsFree: true},
			{Title: "Game Boy Homebrew", URL: "https://example.invalid/gb", Platform: "GB", IsFree: true},
		},
		cacheReady: true, platformFilter: "PSX", ownedURLs: make(map[string]bool),
	}
	controller.rebuildView()
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	if model.State != appui.ListReady || len(model.Items) != 1 || model.Items[0].Title != "PSX Homebrew" {
		t.Fatalf("Cat catalogue model = %#v", model)
	}
	if model.Platform != "PSX" {
		t.Fatalf("platform label = %q, want PSX", model.Platform)
	}
}

func TestCatalogControllerReplaceOwnedGamesClearsLiveCredentialState(t *testing.T) {
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		cachedGames: []itchio.Game{{Title: "Owned", URL: "https://example.invalid/owned"}},
		cacheReady:  true, ownedURLs: map[string]bool{"https://example.invalid/owned": true},
		ownedUpdateCh: make(chan map[string]bool, 1),
	}
	controller.rebuildView()
	controller.ReplaceOwnedGames(nil)
	controller.consumeUpdates()
	if len(controller.ownedURLs) != 0 {
		t.Fatalf("live owned URLs = %v", controller.ownedURLs)
	}
}

func TestCyclePlatformFilterWrapsAcrossAllSystems(t *testing.T) {
	tests := []struct {
		current   string
		direction int
		want      string
	}{
		{current: "", direction: 1, want: "GB"},
		{current: "GB", direction: 1, want: "GBC"},
		{current: "P8", direction: 1, want: "PSX"},
		{current: "PSX", direction: 1, want: ""},
		{current: "", direction: -1, want: "PSX"},
		{current: "GBC", direction: -1, want: "GB"},
	}
	for _, test := range tests {
		if got := cyclePlatformFilter(test.current, test.direction); got != test.want {
			t.Errorf("cyclePlatformFilter(%q, %d) = %q, want %q",
				test.current, test.direction, got, test.want)
		}
	}
}

func TestCatalogControllerDismissesUpdateNotice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	gameURL := "https://example.invalid/update"
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		gameURL: {GameURL: gameURL, KnownUpstreamFiles: []inventory.UpstreamFile{
			{Filename: "new.gb", SeenAt: time.Now(), IsNew: true},
		}},
	}}
	controller := &CatalogController{inv: inv, inventoryPath: path,
		cachedGames: []itchio.Game{{Title: "Updated Game", URL: gameURL}},
		viewGames:   []itchio.Game{{Title: "Updated Game", URL: gameURL}},
		ownedURLs:   make(map[string]bool)}
	controller.DismissNotice(0)
	if inv.HasPendingUpdates(gameURL) {
		t.Fatal("update notice remains after dismissal")
	}
	if _, err := inventory.Load(path); err != nil {
		t.Fatalf("dismissed inventory was not persisted: %v", err)
	}
}
