//go:build !headless

package ui

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

const day = 24 * time.Hour

func newestPath(slug string) string { return "/games/newest/" + slug + ".xml" }

// readKinds reports which kinds of feed were requested: "newest" for the
// daily check's newest-first feeds, "full" for the full crawl's feeds. The
// preview page, the GB Studio feed's first page, counts as full.
func (server *catalogueServer) readKinds() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	kinds := make(map[string]bool)
	for path := range server.requested {
		if strings.HasPrefix(path, "/games/newest/") {
			kinds["newest"] = true
		} else {
			kinds["full"] = true
		}
	}
	var out []string
	for kind := range kinds {
		out = append(out, kind)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

// requestedEveryNewestFeed reports whether every newest-first feed was
// requested.
func (server *catalogueServer) requestedEveryNewestFeed() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	for _, platform := range itchio.AllPlatforms {
		for _, slug := range platform.FeedSlugs {
			if !server.requested[newestPath(slug)] {
				return false
			}
		}
	}
	return true
}

func waitForCheckEnd(t *testing.T, server *catalogueServer, controller *CatalogController) {
	t.Helper()
	waitFor(t, server.requestedEveryNewestFeed)
	waitFor(t, func() bool { return !controller.IsBusy() })
}

// D2 with a fake clock: at launch, a cache checked within a day is left
// alone, a cache checked a day ago gets the daily check of the newest feeds,
// and a cache whose last full crawl is a week old, or of revision 1, gets a
// full crawl.
func TestRefreshKindFollowsTheCacheAge(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	at := func(age time.Duration) time.Time { return now.Add(-age) }
	for _, tc := range []struct {
		name string
		meta itchio.CacheMeta
		want string
	}{
		{"checked an hour ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FetchedAt: at(time.Hour), FullFetchedAt: at(2 * day), CheckedAt: at(time.Hour)}, ""},
		{"checked 25 h ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FetchedAt: at(25 * time.Hour), FullFetchedAt: at(2 * day), CheckedAt: at(25 * time.Hour)}, "newest"},
		{"full crawl 8 days ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FetchedAt: at(time.Hour), FullFetchedAt: at(8 * day), CheckedAt: at(time.Hour)}, "full"},
		{"revision 1", itchio.CacheMeta{Revision: 1, FetchedAt: at(time.Hour)}, "full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newCatalogueServer(t, nil, nil)
			controller := &CatalogController{
				client:    itchio.NewClientWithBase(server.URL),
				cachePath: filepath.Join(t.TempDir(), "games_cache.json"), cacheUpdateCh: make(chan []itchio.Game, 1),
			}
			controller.cacheCommitted.Store(true)
			controller.refreshCacheIfStale(&itchio.GameCache{Meta: tc.meta, Games: []itchio.Game{catalogueGame("GB", "old-gb")}}, now)
			if got := server.readKinds(); got != tc.want {
				t.Fatalf("feeds read = %q, want %q", got, tc.want)
			}
		})
	}
}

// Settings > Refresh Game List stays a full crawl, even right after a check.
func TestManualRefreshReadsEveryFeed(t *testing.T) {
	server := newCatalogueServer(t, map[string][]itchio.Game{
		feedPath("tag-pico-8"): {catalogueGame("P8", "p8")},
	}, nil)
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	checkedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	fullAt := time.Now().Add(-2 * day).Truncate(time.Second)
	writeCatalogueCacheMeta(t, cachePath, itchio.CacheMeta{Revision: itchio.GamesCacheRevision,
		FetchedAt: checkedAt, FullFetchedAt: fullAt, CheckedAt: checkedAt}, catalogueGame("P8", "old-p8"))

	flow, model := NewCatCacheRefreshFlow(itchio.NewClientWithBase(server.URL), cachePath, nil)
	for deadline := time.Now().Add(10 * time.Second); model.State == appui.RefreshLoading && time.Now().Before(deadline); {
		flow.Sync(model)
		time.Sleep(time.Millisecond)
	}
	if model.State != appui.RefreshDone {
		t.Fatalf("refresh state = %v (%s), want done", model.State, model.Detail)
	}
	if got := server.readKinds(); got != "full" {
		t.Fatalf("feeds read = %q, want only the full feeds", got)
	}
	cache := waitForCacheSave(t, cachePath, checkedAt)
	if !cache.Meta.FullFetchedAt.After(fullAt) || !cache.Meta.CheckedAt.Equal(cache.Meta.FullFetchedAt) {
		t.Fatalf("meta after a manual refresh = %+v, want a new full crawl time", cache.Meta)
	}
}

// The daily check reads the newest feeds, puts each system's new games on
// top of its cached games, leaves the cached games as they were, keeps the
// last full crawl's time, and logs one line with what it did. GB's three
// feeds and a game listed under GBC and GB still give one entry each.
func TestDailyCheckPutsNewGamesOnTopOfTheirSystem(t *testing.T) {
	logs := captureLogs(t)
	server := newCatalogueServer(t, map[string][]itchio.Game{
		newestPath("tag-gameboy-color"):   {catalogueGame("GBC", "shared"), catalogueGame("GBC", "old-gbc")},
		newestPath("made-with-gb-studio"): {catalogueGame("GB", "gb-new"), catalogueGame("GB", "shared"), catalogueGame("GB", "old-gb-1")},
		newestPath("tag-gbstudio"):        {catalogueGame("GB", "gb-new"), catalogueGame("GB", "old-gb-2")},
		newestPath("tag-pico-8"):          {catalogueGame("P8", "p8-new"), catalogueGame("P8", "old-p8")},
	}, nil)
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	checkedAt := time.Now().Add(-25 * time.Hour).Truncate(time.Second)
	fullAt := time.Now().Add(-3 * day).Truncate(time.Second)
	cachedGB := catalogueGame("GB", "old-gb-1")
	cachedGB.Title, cachedGB.Price, cachedGB.IsFree = "Cached Title", 3, false
	writeCatalogueCacheMeta(t, cachePath, itchio.CacheMeta{Revision: itchio.GamesCacheRevision,
		FetchedAt: checkedAt, FullFetchedAt: fullAt, CheckedAt: checkedAt},
		catalogueGame("GBC", "old-gbc"), cachedGB, catalogueGame("GB", "old-gb-2"), catalogueGame("P8", "old-p8"))

	controller := startCatalogController(t, server.URL, cachePath)
	cache := waitForCacheSave(t, cachePath, checkedAt)
	waitForCheckEnd(t, server, controller)

	got := gamesByPlatform(cache.Games)
	for platform, want := range map[string][]string{
		"GBC": {"shared", "old-gbc"},
		"GB":  {"gb-new", "old-gb-1", "old-gb-2"},
		"P8":  {"p8-new", "old-p8"},
	} {
		if !sameStrings(got[platform], want) {
			t.Errorf("%s games after the check = %v, want %v", platform, got[platform], want)
		}
	}
	for _, game := range cache.Games {
		if game.URL == cachedGB.URL && (game.Title != "Cached Title" || game.Price != 3) {
			t.Errorf("the check changed a cached entry: %+v", game)
		}
	}
	if kinds := server.readKinds(); kinds != "newest" {
		t.Errorf("feeds read = %q, want only the newest feeds", kinds)
	}
	meta := cache.Meta
	if !meta.FullFetchedAt.Equal(fullAt) || !meta.CheckedAt.After(checkedAt) || !meta.FetchedAt.Equal(meta.CheckedAt) {
		t.Errorf("meta after the check = %+v; want the full crawl kept at %v, checked and saved now", meta, fullAt)
	}
	if !hasLogLine(logs.String(), "[INFO]", "cache: refresh mode=incremental result=saved", "new_games=3",
		"pages=11", "http_429=0", "duration=") {
		t.Errorf("no summary line for the check; log:\n%s", logs.String())
	}
	controller.consumeUpdates()
	if len(controller.cachedGames) != len(cache.Games) {
		t.Errorf("list shows %d games, want the %d saved", len(controller.cachedGames), len(cache.Games))
	}
	// The header's cache age is the last save.
	if label := cacheAgeLabel(time.Now(), controller.cacheFetched.Load()); label != "Cache now" {
		t.Errorf("header after the check = %q, want Cache now", label)
	}
}

// A daily check that finds nothing new still records the check, so the next
// launch does not check again.
func TestDailyCheckWithNothingNewRecordsTheCheck(t *testing.T) {
	server := newCatalogueServer(t, map[string][]itchio.Game{
		newestPath("tag-pico-8"): {catalogueGame("P8", "old-p8")},
	}, nil)
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	checkedAt := time.Now().Add(-30 * time.Hour).Truncate(time.Second)
	writeCatalogueCacheMeta(t, cachePath, itchio.CacheMeta{Revision: itchio.GamesCacheRevision,
		FetchedAt: checkedAt, FullFetchedAt: checkedAt, CheckedAt: checkedAt}, catalogueGame("P8", "old-p8"))

	controller := startCatalogController(t, server.URL, cachePath)
	cache := waitForCacheSave(t, cachePath, checkedAt)
	waitForCheckEnd(t, server, controller)
	if len(cache.Games) != 1 || cache.DueRefresh(time.Now()) != itchio.CacheRefreshNone {
		t.Fatalf("cache after an empty check = %d games, %+v; want the game and a recorded check", len(cache.Games), cache.Meta)
	}
}

// The partial-failure rule holds for the daily check: a system with a failed
// feed takes no new games and keeps its cached ones, the other systems take
// theirs, and the log names the feed.
func TestDailyCheckKeepsTheSystemOfAFailedFeed(t *testing.T) {
	logs := captureLogs(t)
	server := newCatalogueServer(t, map[string][]itchio.Game{
		newestPath("tag-nes-rom"):         {catalogueGame("NES", "nes-new")},
		newestPath("tag-sega-mega-drive"): {catalogueGame("MD", "md-new")},
	}, map[string]int{newestPath("tag-genesis-rom"): http.StatusNotFound})
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	checkedAt := time.Now().Add(-25 * time.Hour).Truncate(time.Second)
	writeCatalogueCacheMeta(t, cachePath, itchio.CacheMeta{Revision: itchio.GamesCacheRevision,
		FetchedAt: checkedAt, FullFetchedAt: checkedAt, CheckedAt: checkedAt},
		catalogueGame("NES", "old-nes"), catalogueGame("MD", "old-md"))

	controller := startCatalogController(t, server.URL, cachePath)
	cache := waitForCacheSave(t, cachePath, checkedAt)
	waitForCheckEnd(t, server, controller)

	got := gamesByPlatform(cache.Games)
	if !sameStrings(got["NES"], []string{"nes-new", "old-nes"}) || !sameStrings(got["MD"], []string{"old-md"}) {
		t.Fatalf("games after the check = %v, want the new NES game and the cached MD game only", got)
	}
	if !hasLogLine(logs.String(), "[WARN]", "cache:", "slug=tag-genesis-rom") {
		t.Errorf("no warning names the failed feed; log:\n%s", logs.String())
	}
}

// A revision-1 cache gets one full crawl after the update. That also
// replaces the slug titles older versions saved for titles without letters
// ("35" for "35!").
func TestRevisionOneCacheGetsOneFullCrawl(t *testing.T) {
	fixed := itchio.Game{Title: "35!", Author: "dev", URL: "https://dev.itch.io/35", IsFree: true, Platform: "GB"}
	server := newCatalogueServer(t, map[string][]itchio.Game{feedPath("made-with-gb-studio"): {fixed}}, nil)
	cachePath := filepath.Join(t.TempDir(), "games_cache.json")
	savedAt := time.Now().Add(-time.Hour).Truncate(time.Second)
	legacy := `{"meta":{"revision":1,"fetched_at":"` + savedAt.Format(time.RFC3339) + `","total_games":1},` +
		`"games":[{"title":"35","author":"dev","url":"https://dev.itch.io/35","platform":"GB","is_free":true}]}`
	if err := os.WriteFile(cachePath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	controller := startCatalogController(t, server.URL, cachePath)
	cache := waitForCacheSave(t, cachePath, savedAt)
	waitForRefreshEnd(t, server, controller)

	if !cache.CurrentRevision() || time.Since(cache.Meta.FullFetchedAt) > time.Minute || !cache.Meta.CheckedAt.Equal(cache.Meta.FullFetchedAt) {
		t.Fatalf("meta after the update = %+v, want the current revision and a full crawl now", cache.Meta)
	}
	if len(cache.Games) != 1 || cache.Games[0].Title != "35!" {
		t.Fatalf("games after the full crawl = %+v, want the feed's title 35!", cache.Games)
	}
	if kinds := server.readKinds(); kinds != "full" {
		t.Errorf("feeds read = %q, want only the full feeds", kinds)
	}
}
