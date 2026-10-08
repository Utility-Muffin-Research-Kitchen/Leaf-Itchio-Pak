//go:build !headless

package ui

import (
	"errors"
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
		cacheReady: true, platformFilter: "PSX", sortMode: itchio.SortModeNew, ownedURLs: make(map[string]bool),
	}
	controller.rebuildView()
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	if model.State != appui.ListReady || len(model.Items) != 1 || model.Items[0].Title != "PSX Homebrew" {
		t.Fatalf("Cat catalogue model = %#v", model)
	}
	// The header uses the filter screen's names.
	if model.Platform != "PlayStation" || model.Sort != "Newest" {
		t.Fatalf("header labels = %q, %q; want PlayStation, Newest", model.Platform, model.Sort)
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

func TestCatalogControllerDiscardsValidationFromAReplacedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned_cache.json")
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		ownedUpdateCh: make(chan map[string]bool, 1), ownedURLs: make(map[string]bool), ownedCachePath: path,
	}
	generation := controller.ownedGeneration.Load()
	controller.ReplaceOwnedGames(nil) // the key changed while validating
	<-controller.ownedUpdateCh
	stale := []itchio.OwnedGame{{GameID: 1, URL: "https://old-account.itch.io/game"}}
	if controller.publishOwnedIfCurrent(generation, stale) {
		t.Fatal("stale validation was published")
	}
	if urls, err := itchio.LoadOwnedCache(path); err != nil || urls != nil {
		t.Fatalf("owned cache = %v, %v; want nothing written", urls, err)
	}
	if !controller.publishOwnedIfCurrent(controller.ownedGeneration.Load(), stale) {
		t.Fatal("current validation was discarded")
	}
}

// R21-3: the detail page's action follows the current account: signing in
// or out anywhere, or the owned list arriving, updates an open page. A paid
// game you do not own offers no Download.
func TestDetailAccessFollowsTheAccount(t *testing.T) {
	dir := t.TempDir()
	cfg := &settings.Config{}
	controller := &CatalogController{
		cfg: cfg, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		ownedUpdateCh: make(chan map[string]bool, 1),
	}
	account := NewAccount(cfg, filepath.Join(dir, "config.json"), filepath.Join(dir, "owned_cache.json"), itchio.NewClientWithBase("https://example.invalid"))
	account.SetOwnedChanged(controller.ReplaceOwnedGames)
	paid := appui.DetailGame{Title: "Paid", URL: "https://dev.itch.io/paid"}
	free := appui.DetailGame{Title: "Free", URL: "https://dev.itch.io/free", IsFree: true}
	check := func(step string, want appui.DetailGame) {
		t.Helper()
		controller.consumeUpdates()
		got := paid
		controller.ApplyDetailAccess(&got)
		if got.CanDownload != want.CanDownload || got.NeedsSignIn != want.NeedsSignIn || got.Owned != want.Owned {
			t.Fatalf("%s: paid game = download %v sign-in %v owned %v; want %v %v %v", step,
				got.CanDownload, got.NeedsSignIn, got.Owned, want.CanDownload, want.NeedsSignIn, want.Owned)
		}
		gotFree := free
		controller.ApplyDetailAccess(&gotFree)
		if !gotFree.CanDownload || gotFree.NeedsSignIn {
			t.Fatalf("%s: a free game must always download", step)
		}
	}

	check("signed out", appui.DetailGame{NeedsSignIn: true})
	if err := account.Store("new-key"); err != nil {
		t.Fatal(err)
	}
	// The owned list is not known until the account check finishes: the
	// purchase lookup decides, so Download stays.
	check("signed in, owned list loading", appui.DetailGame{CanDownload: true})
	if err := account.Validated("tester", []itchio.OwnedGame{{URL: "https://dev.itch.io/other"}}); err != nil {
		t.Fatal(err)
	}
	check("signed in, not owned", appui.DetailGame{})
	if err := account.Validated("tester", []itchio.OwnedGame{{URL: paid.URL}}); err != nil {
		t.Fatal(err)
	}
	check("signed in, owned", appui.DetailGame{CanDownload: true, Owned: true})
	if err := account.SignOut(); err != nil {
		t.Fatal(err)
	}
	check("signed out again", appui.DetailGame{NeedsSignIn: true})
}

// R21-4: an upgrade from 0.1.0 removes the typed API key, so the app starts
// signed out. The owned-game cache of that key must not show OWNED badges or
// fill the Owned sort, and is deleted.
func TestUpgradeFromATypedKeyDropsItsOwnedCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer srv.Close()
	dir := t.TempDir()
	cfgPath, ownedPath := filepath.Join(dir, "config.json"), filepath.Join(dir, "owned_cache.json")
	if err := os.WriteFile(cfgPath, []byte(`{"api_key":"typed-key-0-1-0","rom_selection":"auto","rom_location":"auto","unified_naming":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := itchio.SaveOwnedCache(ownedPath, []string{"https://dev.itch.io/owned-by-the-old-key"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := settings.Load(cfgPath)
	if err != nil || cfg.SignedIn() || !cfg.LegacyKeyRemoved {
		t.Fatalf("loaded config = %+v, %v", cfg, err)
	}
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	controller := NewCatalogController(itchio.NewClientWithBase(srv.URL), cfg, cfgPath,
		filepath.Join(dir, "games_cache.json"), inv, filepath.Join(dir, "inventory.json"), nil, ownedPath)
	if controller.Owned("https://dev.itch.io/owned-by-the-old-key") || controller.ownedKnown() {
		t.Fatal("the old key's owned games were loaded while signed out")
	}
	if _, err := os.Stat(ownedPath); !os.IsNotExist(err) {
		t.Fatalf("the old key's owned cache is still on the card: %v", err)
	}
}

func TestListPriceBadgeUsesFetchedGameData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/paid/data.json":
			w.Write([]byte(`{"id":1,"price":"€4,99"}`))
		case "/now-free/data.json":
			w.Write([]byte(`{"id":2,"price":"$0.00"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := itchio.NewClient()
	for _, path := range []string{"/paid", "/now-free"} {
		if _, err := client.FetchGameData(srv.URL + path); err != nil {
			t.Fatal(err)
		}
	}
	controller := &CatalogController{
		client: client, cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		cachedGames: []itchio.Game{
			{Title: "Paid", URL: srv.URL + "/paid", Price: 5},
			{Title: "Now free", URL: srv.URL + "/now-free", Price: 3},
			{Title: "Not opened", URL: srv.URL + "/other", Price: 2},
		},
		cacheReady: true, ownedURLs: make(map[string]bool),
	}
	controller.rebuildView()
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	badges := make(map[string]string)
	for _, item := range model.Items {
		badges[item.Title] = item.Badge
	}
	if badges["Paid"] != "€4,99" || badges["Now free"] != "Free" || badges["Not opened"] != "$2.00" {
		t.Fatalf("badges = %v", badges)
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

// A preview page that returns after the catalogue is ready must neither
// replace the list with its error nor move the selection.
func TestCatalogControllerIgnoresLatePreviewOnceCatalogueIsReady(t *testing.T) {
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		cachedGames: []itchio.Game{
			{Title: "One", URL: "https://example.invalid/one"},
			{Title: "Two", URL: "https://example.invalid/two"},
			{Title: "Three", URL: "https://example.invalid/three"},
		},
		cacheReady: true, pageUpdateCh: make(chan pageResult, 1), ownedURLs: make(map[string]bool),
	}
	controller.rebuildView()
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	model.Cursor = 1
	if _, ok := controller.CatSelected(model.Cursor); !ok {
		t.Fatal("row 2 is not selectable")
	}

	late := []pageResult{
		{err: errors.New("fetch feed: connection reset")},
		{games: []itchio.Game{{Title: "Preview", URL: "https://example.invalid/preview"}}},
	}
	for _, result := range late {
		controller.pageUpdateCh <- result
		controller.SyncCatModel(model)
		if model.State != appui.ListReady || len(model.Items) != 3 {
			t.Fatalf("after a late preview (err=%v): state %v, %d items; want the 3 catalogue games",
				result.err, model.State, len(model.Items))
		}
		if controller.cursor != 1 || model.Cursor != 1 {
			t.Fatalf("after a late preview (err=%v): cursor controller=%d model=%d, want row 2",
				result.err, controller.cursor, model.Cursor)
		}
	}
}

// A failed preview no longer matters once the catalogue has games to show.
func TestCatalogControllerCatalogueReplacesPreviewError(t *testing.T) {
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		pageUpdateCh: make(chan pageResult, 1), cacheUpdateCh: make(chan []itchio.Game, 1),
		ownedURLs: make(map[string]bool),
	}
	controller.pageUpdateCh <- pageResult{err: errors.New("fetch feed: connection reset")}
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	if model.State != appui.ListError {
		t.Fatalf("failed preview state = %v, want the error screen", model.State)
	}
	controller.cacheUpdateCh <- []itchio.Game{{Title: "Catalogue", URL: "https://example.invalid/catalogue"}}
	controller.SyncCatModel(model)
	if model.State != appui.ListReady || len(model.Items) != 1 {
		t.Fatalf("after the catalogue arrived: state %v, %d items; want the catalogue game",
			model.State, len(model.Items))
	}
}

// The preview page is unfiltered, so the header must not name a saved
// platform filter until the catalogue that honours it is ready.
func TestCatalogControllerHeaderShowsAllPlatformsOnUncachedPreview(t *testing.T) {
	controller := &CatalogController{
		cfg: &settings.Config{}, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
		platformFilter: "PSX", pageUpdateCh: make(chan pageResult, 1),
		cacheUpdateCh: make(chan []itchio.Game, 1), ownedURLs: make(map[string]bool),
	}
	controller.pageUpdateCh <- pageResult{games: []itchio.Game{
		{Title: "GB Studio Game", URL: "https://example.invalid/gb"},
	}}
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	if model.Platform != "All platforms" || len(model.Items) != 1 {
		t.Fatalf("preview header = %q with %d items, want All platforms over the unfiltered page",
			model.Platform, len(model.Items))
	}
	controller.cacheUpdateCh <- []itchio.Game{
		{Title: "PSX Game", URL: "https://example.invalid/psx", Platform: "PSX"},
		{Title: "GB Game", URL: "https://example.invalid/gb", Platform: "GB"},
	}
	controller.SyncCatModel(model)
	if model.Platform != "PlayStation" || len(model.Items) != 1 || model.Items[0].Title != "PSX Game" {
		t.Fatalf("catalogue header = %q with %#v, want PlayStation over the PSX game", model.Platform, model.Items)
	}
}
