//go:build !headless

package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
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

func TestCacheAgeLabel(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		age  time.Duration
		want string
	}{
		{name: "fresh", age: 10 * time.Second, want: "Cache now"},
		{name: "minutes", age: 17 * time.Minute, want: "Cache 17m old"},
		{name: "hours", age: 7 * time.Hour, want: "Cache 7h old"},
		{name: "days", age: 3 * 24 * time.Hour, want: "Cache 3d old"},
		{name: "dated", age: 45 * 24 * time.Hour, want: "Cache 2026-05-28"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := cacheAgeLabel(now, now.Add(-test.age).Unix()); got != test.want {
				t.Fatalf("cacheAgeLabel = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBackgroundRefreshKeepsCommittedCacheOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	controller := &CatalogController{
		client:        itchio.NewClientWithBase(srv.URL),
		cachePath:     filepath.Join(t.TempDir(), "games_cache.json"),
		cacheUpdateCh: make(chan []itchio.Game, 1),
	}
	controller.cacheCommitted.Store(true)
	controller.buildCache()
	select {
	case partial := <-controller.cacheUpdateCh:
		t.Fatalf("failed refresh replaced committed cache with %d partial games", len(partial))
	default:
	}
}

func TestCatalogControllerSearchesUncachedPreviewLocally(t *testing.T) {
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		pageUpdateCh: make(chan pageResult, 1), ownedURLs: make(map[string]bool),
	}
	controller.pageUpdateCh <- pageResult{games: []itchio.Game{
		{Title: "Cat Quest", Author: "someone", URL: "https://example.invalid/cat"},
		{Title: "Dog Run", Author: "else", URL: "https://example.invalid/dog"},
	}}
	controller.consumeUpdates()
	if len(controller.viewGames) != 2 {
		t.Fatalf("preview view = %d games, want 2", len(controller.viewGames))
	}

	controller.searchQuery = "dog"
	controller.rebuildView()
	if len(controller.viewGames) != 1 || controller.viewGames[0].Title != "Dog Run" {
		t.Fatalf("searched preview = %#v, want Dog Run only", controller.viewGames)
	}
	controller.searchQuery = ""
	controller.rebuildView()
	if len(controller.viewGames) != 2 {
		t.Fatalf("cleared search view = %d games, want the full preview page", len(controller.viewGames))
	}
}

// Before the cache exists, a persisted Downloaded sort must still list the
// downloaded games on the preview page instead of an empty list.
func TestCatalogControllerDownloadedSortOnUncachedPreview(t *testing.T) {
	const downloadedURL = "https://example.invalid/downloaded"
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{
		downloadedURL: {GameURL: downloadedURL, Files: []inventory.DownloadedFile{{Filename: "game.gb"}}},
	}}
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: inv, sortMode: itchio.SortModeDL,
		pageUpdateCh: make(chan pageResult, 1), ownedURLs: make(map[string]bool),
	}
	controller.pageUpdateCh <- pageResult{games: []itchio.Game{
		{Title: "Not Downloaded", URL: "https://example.invalid/other"},
		{Title: "Downloaded", URL: downloadedURL},
	}}
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	if model.State != appui.ListReady || len(model.Items) != 1 || model.Items[0].Title != "Downloaded" {
		t.Fatalf("Downloaded sort on the preview = state %v, items %#v; want the one downloaded game",
			model.State, model.Items)
	}
}

// On first launch the full fetch reports progress while its first feed is
// still paging and nothing is merged. That must not replace the preview page
// with an empty catalogue; the first snapshot with games replaces it.
func TestCatalogControllerKeepsPreviewUntilCacheHasGames(t *testing.T) {
	page1, err := os.ReadFile("../../testdata/rss_page1.xml")
	if err != nil {
		t.Fatalf("read rss_page1.xml: %v", err)
	}
	page2Requested := make(chan struct{})
	release := make(chan struct{})
	var requestedOnce, releaseOnce sync.Once
	releasePage2 := func() { releaseOnce.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/games/made-with-gb-studio.xml" {
			switch r.URL.Query().Get("page") {
			case "1":
				w.Write(page1)
				return
			case "2":
				requestedOnce.Do(func() { close(page2Requested) })
				<-release
			}
		}
		w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`))
	}))
	defer srv.Close()
	done := make(chan struct{})
	defer func() { <-done }()
	defer releasePage2()

	controller := &CatalogController{
		client: itchio.NewClientWithBase(srv.URL), cfg: &settings.Config{},
		inv:          &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		cachePath:    filepath.Join(t.TempDir(), "games_cache.json"),
		pageUpdateCh: make(chan pageResult, 1), cacheUpdateCh: make(chan []itchio.Game, 1),
		ownedURLs: make(map[string]bool),
	}
	controller.pageUpdateCh <- pageResult{games: []itchio.Game{
		{Title: "Preview One", URL: "https://example.invalid/preview-one"},
		{Title: "Preview Two", URL: "https://example.invalid/preview-two"},
	}}
	controller.consumeUpdates()

	go func() {
		defer close(done)
		controller.buildCache()
	}()
	select {
	case <-page2Requested:
	case <-time.After(5 * time.Second):
		t.Fatal("the second GB Studio page was never requested")
	}
	// Page 1 reported its games before page 2 was requested; nothing is
	// merged until the feed finishes. Watch the screen while page 2 is held.
	for deadline := time.Now().Add(200 * time.Millisecond); time.Now().Before(deadline); {
		controller.consumeUpdates()
		if controller.cacheReady {
			t.Fatalf("catalogue became ready with %d games before any feed finished", len(controller.cachedGames))
		}
		time.Sleep(10 * time.Millisecond)
	}
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	if model.State != appui.ListReady || len(model.Items) != 2 {
		t.Fatalf("screen while the first feed pages = state %v, %d items; want the 2 preview games",
			model.State, len(model.Items))
	}

	releasePage2()
	<-done
	controller.SyncCatModel(model)
	if !controller.cacheReady || model.State != appui.ListReady || len(model.Items) != itchio.PerPage {
		t.Fatalf("after the first feed merged: ready=%v state %v, %d items; want %d catalogue games",
			controller.cacheReady, model.State, len(model.Items), itchio.PerPage)
	}
}
