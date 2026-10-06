//go:build !headless

package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
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
