//go:build !headless

package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// catalogueGame is a feed game of one system, linked under dev.itch.io.
func catalogueGame(platform, slug string) itchio.Game {
	return itchio.Game{Title: slug, Author: "dev", URL: "https://dev.itch.io/" + slug, IsFree: true, Platform: platform}
}

// feedXML renders one feed page with an item per game.
func feedXML(games ...itchio.Game) string {
	var items strings.Builder
	for _, game := range games {
		fmt.Fprintf(&items, "<item><title>%s</title><link>%s</link><price>0.0</price></item>\n",
			html.EscapeString(game.Title), html.EscapeString(game.URL))
	}
	return `<?xml version="1.0"?><rss version="2.0"><channel>` + items.String() + `</channel></rss>`
}

// catalogueServer stands in for itch.io's browse feeds. Each feed path
// answers page 1 with its games, a short page that ends the feed; a path in
// status answers that HTTP status on every page instead. It records which
// feed paths were requested.
type catalogueServer struct {
	*httptest.Server
	mu        sync.Mutex
	requested map[string]bool
}

func newCatalogueServer(t *testing.T, feeds map[string][]itchio.Game, status map[string]int) *catalogueServer {
	t.Helper()
	server := &catalogueServer{requested: make(map[string]bool)}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server.mu.Lock()
		server.requested[r.URL.Path] = true
		server.mu.Unlock()
		if code, ok := status[r.URL.Path]; ok {
			w.WriteHeader(code)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		if r.URL.Query().Get("page") == "1" {
			_, _ = w.Write([]byte(feedXML(feeds[r.URL.Path]...)))
			return
		}
		_, _ = w.Write([]byte(feedXML()))
	}))
	t.Cleanup(server.Close)
	return server
}

// requestedEveryFeed reports whether every catalogue feed was requested.
func (server *catalogueServer) requestedEveryFeed() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	for _, platform := range itchio.AllPlatforms {
		for _, slug := range platform.FeedSlugs {
			if !server.requested["/games/"+slug+".xml"] {
				return false
			}
		}
	}
	return true
}

func feedPath(slug string) string { return "/games/" + slug + ".xml" }

// writeCatalogueCache writes a games cache whose last full crawl, check and
// save were at savedAt.
func writeCatalogueCache(t *testing.T, path string, savedAt time.Time, games ...itchio.Game) {
	t.Helper()
	writeCatalogueCacheMeta(t, path, itchio.CacheMeta{
		Revision: itchio.GamesCacheRevision, FetchedAt: savedAt, FullFetchedAt: savedAt, CheckedAt: savedAt,
	}, games...)
}

// writeCatalogueCacheMeta writes a games cache with meta.
func writeCatalogueCacheMeta(t *testing.T, path string, meta itchio.CacheMeta, games ...itchio.Game) {
	t.Helper()
	meta.TotalGames = len(games)
	data, err := json.Marshal(itchio.GameCache{Meta: meta, Games: games})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func startCatalogController(t *testing.T, baseURL, cachePath string) *CatalogController {
	t.Helper()
	dir := t.TempDir()
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	return NewCatalogController(itchio.NewClientWithBase(baseURL), &settings.Config{},
		filepath.Join(dir, "config.json"), cachePath, inv, filepath.Join(dir, "inventory.json"),
		nil, filepath.Join(dir, "owned_cache.json"))
}

// waitForCacheSave waits until the cache at path was saved after since.
func waitForCacheSave(t *testing.T, path string, since time.Time) *itchio.GameCache {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cache, err := itchio.LoadGamesCache(path); err == nil && cache.Meta.FetchedAt.After(since) {
			return cache
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the refresh saved no cache")
	return nil
}

// waitForRefreshEnd waits until every feed was requested and the background
// refresh returned.
func waitForRefreshEnd(t *testing.T, server *catalogueServer, controller *CatalogController) {
	t.Helper()
	waitFor(t, server.requestedEveryFeed)
	waitFor(t, func() bool { return !controller.IsBusy() })
}

// gamesByPlatform lists the URL slugs of games per system, in list order.
func gamesByPlatform(games []itchio.Game) map[string][]string {
	byPlatform := make(map[string][]string)
	for _, game := range games {
		byPlatform[game.Platform] = append(byPlatform[game.Platform], strings.TrimPrefix(game.URL, "https://dev.itch.io/"))
	}
	return byPlatform
}

func sameStrings(got, want []string) bool {
	return strings.Join(got, ",") == strings.Join(want, ",")
}

// One failing feed no longer throws away a whole refresh: the systems whose
// feeds all finished take the new games, the failing feed's system keeps its
// cached games, the result is saved and the log names the feed.
func TestStaleRefreshKeepsTheCachedGamesOfAFailedFeed(t *testing.T) {
	logs := captureLogs(t)
	server := newCatalogueServer(t, map[string][]itchio.Game{
		feedPath("tag-homebrew/tag-psx"): {catalogueGame("PSX", "new-psx")},
		feedPath("made-with-gb-studio"):  {catalogueGame("GB", "new-gb")},
		feedPath("tag-pico-8"):           {catalogueGame("P8", "new-p8-1"), catalogueGame("P8", "new-p8-2")},
	}, map[string]int{feedPath("tag-gbstudio"): http.StatusInternalServerError})
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	savedAt := time.Now().Add(-8 * 24 * time.Hour).Truncate(time.Second)
	writeCatalogueCache(t, cachePath, savedAt,
		catalogueGame("PSX", "old-psx"), catalogueGame("GB", "old-gb-1"), catalogueGame("GB", "old-gb-2"),
		catalogueGame("P8", "old-p8"))

	controller := startCatalogController(t, server.URL, cachePath)
	cache := waitForCacheSave(t, cachePath, savedAt)
	waitForRefreshEnd(t, server, controller)

	got := gamesByPlatform(cache.Games)
	for platform, want := range map[string][]string{
		"PSX": {"new-psx"},
		"GB":  {"old-gb-1", "old-gb-2"},
		"P8":  {"new-p8-1", "new-p8-2"},
	} {
		if !sameStrings(got[platform], want) {
			t.Errorf("saved %s games = %v, want %v", platform, got[platform], want)
		}
	}
	if cache.Meta.TotalGames != len(cache.Games) || len(cache.Games) != 5 {
		t.Errorf("saved %d games (meta %d), want 5", len(cache.Games), cache.Meta.TotalGames)
	}
	if !hasLogLine(logs.String(), "[WARN]", "cache:", "slug=tag-gbstudio", "HTTP 500") {
		t.Errorf("no warning names the failed feed; log:\n%s", logs.String())
	}
	controller.consumeUpdates()
	if len(controller.cachedGames) != 5 {
		t.Errorf("list shows %d games, want the 5 saved", len(controller.cachedGames))
	}
}

// A refresh in which every feed fails saves nothing and keeps the cache.
func TestStaleRefreshSavesNothingWhenEveryFeedFails(t *testing.T) {
	status := make(map[string]int)
	for _, platform := range itchio.AllPlatforms {
		for _, slug := range platform.FeedSlugs {
			status[feedPath(slug)] = http.StatusNotFound
		}
	}
	server := newCatalogueServer(t, nil, status)
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	savedAt := time.Now().Add(-8 * 24 * time.Hour).Truncate(time.Second)
	writeCatalogueCache(t, cachePath, savedAt, catalogueGame("GB", "old-gb"))
	before, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}

	controller := startCatalogController(t, server.URL, cachePath)
	waitForRefreshEnd(t, server, controller)

	after, err := os.ReadFile(cachePath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("cache after a refresh in which every feed failed changed (err %v):\n%s", err, after)
	}
}

// On first launch the list fills in while feeds finish, as before, and the
// systems that finished are saved even when another feed failed.
func TestFirstLaunchSavesTheSystemsThatFinished(t *testing.T) {
	server := newCatalogueServer(t, map[string][]itchio.Game{
		feedPath("tag-homebrew/tag-psx"): {catalogueGame("PSX", "new-psx")},
		feedPath("tag-pico-8"):           {catalogueGame("P8", "new-p8")},
	}, map[string]int{feedPath("tag-nes-rom"): http.StatusNotFound})
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")

	controller := startCatalogController(t, server.URL, cachePath)
	cache := waitForCacheSave(t, cachePath, time.Time{})
	waitForRefreshEnd(t, server, controller)

	got := gamesByPlatform(cache.Games)
	if len(cache.Games) != 2 || !sameStrings(got["PSX"], []string{"new-psx"}) || !sameStrings(got["P8"], []string{"new-p8"}) {
		t.Fatalf("first launch saved %v, want the PSX and Pico-8 games", got)
	}
	// The preview page may still be loading; the list settles on the catalogue.
	model := appui.NewMainListModel(nil)
	waitFor(t, func() bool {
		controller.SyncCatModel(model)
		return controller.cacheReady && model.State == appui.ListReady && len(model.Items) == 2
	})
}

// Settings > Refresh Game List follows the same rule: a failing feed keeps
// its system's cached games and the rest is saved and shown.
func TestManualRefreshKeepsTheCachedGamesOfAFailedFeed(t *testing.T) {
	logs := captureLogs(t)
	server := newCatalogueServer(t, map[string][]itchio.Game{
		feedPath("tag-gameboy-advance"): {catalogueGame("GBA", "new-gba")},
		feedPath("tag-sega-mega-drive"): {catalogueGame("MD", "new-md")},
	}, map[string]int{feedPath("tag-pico-8"): http.StatusNotFound})
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	savedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	writeCatalogueCache(t, cachePath, savedAt,
		catalogueGame("GBA", "old-gba"), catalogueGame("P8", "old-p8-1"), catalogueGame("P8", "old-p8-2"))

	flow, model := NewCatCacheRefreshFlow(itchio.NewClientWithBase(server.URL), cachePath, nil)
	var games []itchio.Game
	deadline := time.Now().Add(10 * time.Second)
	for model.State == appui.RefreshLoading && time.Now().Before(deadline) {
		if result, changed := flow.Sync(model); changed && result != nil {
			games = result
		}
		time.Sleep(time.Millisecond)
	}
	if model.State != appui.RefreshDone {
		t.Fatalf("refresh state = %v (%s), want done", model.State, model.Detail)
	}
	got := gamesByPlatform(games)
	if !sameStrings(got["GBA"], []string{"new-gba"}) || !sameStrings(got["MD"], []string{"new-md"}) ||
		!sameStrings(got["P8"], []string{"old-p8-1", "old-p8-2"}) || len(games) != 4 || model.Total != 4 {
		t.Fatalf("refreshed games = %v (total %d), want new GBA and MD games and the cached Pico-8 games", got, model.Total)
	}
	cache := waitForCacheSave(t, cachePath, savedAt)
	if !sameStrings(gamesByPlatform(cache.Games)["P8"], []string{"old-p8-1", "old-p8-2"}) || len(cache.Games) != 4 {
		t.Fatalf("saved games = %v, want the 4 refreshed games", gamesByPlatform(cache.Games))
	}
	if !hasLogLine(logs.String(), "[WARN]", "cache:", "slug=tag-pico-8") {
		t.Errorf("no warning names the failed feed; log:\n%s", logs.String())
	}
}

// A refresh that rate limiting stopped mid-run is saved like any other
// partial refresh: the systems whose feeds finished take their new games,
// the rest keep their cached games.
func TestCommitKeepsTheSystemsFinishedBeforeARateLimitStop(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	previous := []itchio.Game{catalogueGame("GB", "old-gb"), catalogueGame("MD", "old-md"), catalogueGame("P8", "old-p8")}
	stop := &itchio.RateLimitedError{Host: "itch.io"}
	fetch := &itchio.CatalogFetch{}
	for _, platform := range itchio.AllPlatforms {
		for _, slug := range platform.FeedSlugs {
			feed := itchio.FeedFetch{Platform: platform.Code, Slug: slug}
			switch slug {
			case "made-with-gb-studio":
				feed.Games = []itchio.Game{catalogueGame("GB", "new-gb")}
			case "tag-pico-8":
				feed.Err = fmt.Errorf("platform=P8 slug=tag-pico-8 page 2: fetch feed: %w", stop)
			case "tag-genesis-rom":
				feed.Err = fmt.Errorf("platform=MD slug=tag-genesis-rom not finished, the refresh stopped: %w", stop)
			}
			fetch.Feeds = append(fetch.Feeds, feed)
		}
	}

	logs := captureLogs(t)
	games, err := commitCatalogFetch(cachePath, &itchio.GameCache{Games: previous}, fetch, stop)
	if err != nil {
		t.Fatalf("commit after a rate-limit stop: %v", err)
	}
	for _, slug := range []string{"tag-pico-8", "tag-genesis-rom"} {
		if !hasLogLine(logs.String(), "[WARN]", "cache:", "slug="+slug) {
			t.Errorf("no warning names the stopped feed %s; log:\n%s", slug, logs.String())
		}
	}
	got := gamesByPlatform(games)
	if len(games) != 3 || !sameStrings(got["GB"], []string{"new-gb"}) ||
		!sameStrings(got["MD"], []string{"old-md"}) || !sameStrings(got["P8"], []string{"old-p8"}) {
		t.Fatalf("committed games = %v, want the new GB game and the cached MD and Pico-8 games", got)
	}
	cache, err := itchio.LoadGamesCache(cachePath)
	if err != nil || len(cache.Games) != 3 {
		t.Fatalf("saved cache = %v, %v; want the 3 committed games", cache, err)
	}
}

// Nothing is saved when no system finished, or when the refresh was
// cancelled.
func TestCommitSavesNothingWithoutAFinishedSystem(t *testing.T) {
	previous := []itchio.Game{catalogueGame("GB", "old-gb")}
	failed := &itchio.CatalogFetch{}
	cancelled := &itchio.CatalogFetch{}
	for _, platform := range itchio.AllPlatforms {
		for _, slug := range platform.FeedSlugs {
			failed.Feeds = append(failed.Feeds, itchio.FeedFetch{Platform: platform.Code, Slug: slug,
				Err: fmt.Errorf("platform=%s slug=%s page 1: fetch feed: HTTP 404", platform.Code, slug)})
			cancelled.Feeds = append(cancelled.Feeds, itchio.FeedFetch{Platform: platform.Code, Slug: slug})
		}
	}
	cancelled.Feeds[0].Err = context.Canceled
	for name, run := range map[string]struct {
		fetch *itchio.CatalogFetch
		err   error
	}{
		"every feed failed": {failed, errors.Join(errors.New("platform=GB slug=tag-gbstudio page 1: fetch feed: HTTP 404"))},
		"cancelled":         {cancelled, context.Canceled},
	} {
		cachePath := filepath.Join(t.TempDir(), "games_cache.json")
		if games, err := commitCatalogFetch(cachePath, &itchio.GameCache{Games: previous}, run.fetch, run.err); err == nil || games != nil {
			t.Errorf("%s: commit = %d games, %v; want nothing committed", name, len(games), err)
		}
		if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
			t.Errorf("%s: a cache was saved: %v", name, err)
		}
	}
}
