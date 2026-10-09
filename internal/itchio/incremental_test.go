package itchio_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

// newestServer serves paged feeds: pages[path][n] lists the URL slugs of page
// n+1 (an item "slug@2026-10-05" also carries a publish date). A page past
// the list answers missing (404) when missingPast, else an empty feed. It
// records the pages requested per path.
type newestServer struct {
	*httptest.Server
	mu        sync.Mutex
	requested map[string][]int
}

func newNewestServer(t *testing.T, pages map[string][][]string, missingPast map[string]bool) *newestServer {
	t.Helper()
	server := &newestServer{requested: make(map[string][]int)}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var page int
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		server.mu.Lock()
		server.requested[r.URL.Path] = append(server.requested[r.URL.Path], page)
		server.mu.Unlock()
		feed := pages[r.URL.Path]
		if page < 1 || page > len(feed) {
			if missingPast[r.URL.Path] {
				http.NotFound(w, r)
				return
			}
			feed, page = [][]string{nil}, 1
		}
		var items strings.Builder
		for _, entry := range feed[page-1] {
			slug, date, _ := strings.Cut(entry, "@")
			pubDate := ""
			if date != "" {
				day, err := time.Parse("2006-01-02", date)
				if err != nil {
					t.Errorf("bad date %q", date)
				}
				pubDate = "<pubDate>" + day.Format(time.RFC1123) + "</pubDate>"
			}
			fmt.Fprintf(&items, "<item><title>%s</title><link>https://dev.itch.io/%s</link><price>0.0</price>%s</item>",
				slug, slug, pubDate)
		}
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel>%s</channel></rss>`, items.String())
	}))
	t.Cleanup(server.Close)
	return server
}

func (server *newestServer) pagesRequested(path string) []int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]int(nil), server.requested[path]...)
}

func (server *newestServer) paths() []string {
	server.mu.Lock()
	defer server.mu.Unlock()
	var paths []string
	for path := range server.requested {
		paths = append(paths, path)
	}
	return paths
}

// slugs returns count URL slugs prefix-from, prefix-from+1, ...
func slugs(prefix string, from, count int) []string {
	out := make([]string, count)
	for index := range out {
		out[index] = fmt.Sprintf("%s-%d", prefix, from+index)
	}
	return out
}

func page(parts ...[]string) []string {
	var out []string
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func knownURL(url string) bool { return strings.Contains(url, "/known-") }

func feedSlugs(feed *itchio.FeedFetch) string {
	parts := make([]string, len(feed.Games))
	for index, game := range feed.Games {
		parts[index] = strings.TrimPrefix(game.URL, "https://dev.itch.io/")
	}
	return strings.Join(parts, " ")
}

// The daily check reads the newest-first feeds and stops a feed after two
// pages in a row without a game the cache lacks. Only new games are
// reported, and nothing reads the popularity-ordered feeds.
func TestFetchNewGames_StopsAfterTwoPagesWithoutANewGame(t *testing.T) {
	const p8 = "/games/newest/tag-pico-8.xml"
	server := newNewestServer(t, map[string][][]string{
		p8: {
			page([]string{"new-a", "new-b", "new-c"}, slugs("known", 0, itchio.PerPage-3)),
			slugs("known", 100, itchio.PerPage),
			slugs("known", 200, itchio.PerPage),
			slugs("known", 300, itchio.PerPage),
		},
	}, nil)

	fetch, err := itchio.NewClientWithBase(server.URL).FetchNewGames(context.Background(), knownURL)
	if err != nil {
		t.Fatalf("FetchNewGames: %v", err)
	}
	if got := server.pagesRequested(p8); fmt.Sprint(got) != "[1 2 3]" {
		t.Errorf("Pico-8 newest pages read = %v, want [1 2 3]", got)
	}
	feed := feedOf(t, fetch, "tag-pico-8")
	if got := feedSlugs(feed); got != "new-a new-b new-c" {
		t.Errorf("Pico-8 new games = %s, want new-a new-b new-c", got)
	}
	if feed.Pages != 3 || feed.Games[0].Platform != "P8" {
		t.Errorf("Pico-8 feed pages %d, platform %q; want 3 pages of P8 games", feed.Pages, feed.Games[0].Platform)
	}
	if !fetch.Incremental {
		t.Error("an incremental fetch is not marked incremental")
	}
	for _, path := range server.paths() {
		if !strings.HasPrefix(path, "/games/newest/") {
			t.Errorf("the daily check read %s", path)
		}
	}
	if pages := fetch.Pages(); pages != 3+len(fetch.Feeds)-1 {
		t.Errorf("pages read = %d, want 3 for Pico-8 and 1 for every other feed", pages)
	}
}

// The newest feeds are mostly, not strictly, newest first. A known game on
// page 1 among new ones, and a new game after a page of known games, must not
// end the feed early.
func TestFetchNewGames_KnownGameOutOfOrderDoesNotStopTheFeed(t *testing.T) {
	const gb = "/games/newest/made-with-gb-studio.xml"
	server := newNewestServer(t, map[string][][]string{
		gb: {
			page([]string{"known-old", "new-a", "new-b"}, slugs("known", 0, itchio.PerPage-3)),
			slugs("known", 100, itchio.PerPage),
			page(slugs("known", 200, 20), []string{"new-late"}, slugs("known", 220, itchio.PerPage-21)),
			slugs("known", 300, itchio.PerPage),
			slugs("known", 400, itchio.PerPage),
			slugs("known", 500, itchio.PerPage),
		},
	}, nil)

	fetch, err := itchio.NewClientWithBase(server.URL).FetchNewGames(context.Background(), knownURL)
	if err != nil {
		t.Fatalf("FetchNewGames: %v", err)
	}
	if got := feedSlugs(feedOf(t, fetch, "made-with-gb-studio")); got != "new-a new-b new-late" {
		t.Errorf("GB Studio new games = %s, want new-a new-b new-late", got)
	}
	if got := server.pagesRequested(gb); fmt.Sprint(got) != "[1 2 3 4 5]" {
		t.Errorf("GB Studio newest pages read = %v, want [1 2 3 4 5]", got)
	}
}

// A feed also ends at a short page, at a missing later page, and when
// itch.io repeats a page past the end. A missing first page is an error.
func TestFetchNewGames_EndsAtAShortMissingOrRepeatedPage(t *testing.T) {
	const nes = "/games/newest/tag-nes-rom.xml"
	repeated := slugs("new", 0, itchio.PerPage)
	for _, tc := range []struct {
		name    string
		pages   [][]string
		missing bool
		read    string
		games   int
		failed  bool
	}{
		{name: "short page", pages: [][]string{{"new-a", "new-b"}, slugs("new", 10, itchio.PerPage)}, read: "[1]", games: 2},
		{name: "missing later page", pages: [][]string{slugs("new", 0, itchio.PerPage)}, missing: true, read: "[1 2]", games: itchio.PerPage},
		{name: "repeated page", pages: [][]string{repeated, repeated, repeated}, read: "[1 2]", games: itchio.PerPage},
		{name: "missing first page", pages: nil, missing: true, read: "[1]", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newNewestServer(t, map[string][][]string{nes: tc.pages}, map[string]bool{nes: tc.missing})
			fetch, _ := itchio.NewClientWithBase(server.URL).FetchNewGames(context.Background(), knownURL)
			feed := feedOf(t, fetch, "tag-nes-rom")
			if (feed.Err != nil) != tc.failed {
				t.Fatalf("NES feed error = %v, want failed=%v", feed.Err, tc.failed)
			}
			if got := fmt.Sprint(server.pagesRequested(nes)); got != tc.read {
				t.Errorf("pages read = %s, want %s", got, tc.read)
			}
			if len(feed.Games) != tc.games {
				t.Errorf("new games = %d, want %d", len(feed.Games), tc.games)
			}
		})
	}
}

// An incremental fetch puts each system's new games on top of its cached
// games, newest first, and leaves every cached entry as it was. A system with
// a failed feed keeps its cached games and takes no new ones. New games found
// by several feeds, GB's three or a GBC and a GB feed, are listed once, under
// the first system.
func TestCatalogFetchMergePutsNewGamesOnTopOfTheirSystem(t *testing.T) {
	dated := func(platform, slug string, day int) itchio.Game {
		game := platformGame(platform, slug)
		game.PublishedAt = time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC)
		return game
	}
	cachedGB := platformGame("GB", "old-gb-1")
	cachedGB.Price, cachedGB.Title = 2.5, "Cached Title"
	previous := []itchio.Game{
		platformGame("PSX", "old-psx"), platformGame("GBC", "old-gbc"), cachedGB,
		platformGame("GB", "old-gb-2"), platformGame("P8", "old-p8"),
	}
	fetch := everyFeed()
	fetch.Incremental = true
	feedOf(t, fetch, "tag-gameboy-color").Games = []itchio.Game{dated("GBC", "shared", 5)}
	feedOf(t, fetch, "made-with-gb-studio").Games = []itchio.Game{dated("GB", "gb-new-1", 3), dated("GB", "shared", 5)}
	feedOf(t, fetch, "tag-gbstudio").Games = []itchio.Game{dated("GB", "gb-new-2", 4), dated("GB", "gb-new-1", 3)}
	feedOf(t, fetch, "tag-pico-8").Games = []itchio.Game{dated("P8", "p8-new", 6)}
	feedOf(t, fetch, "tag-pico-8").Err = fmt.Errorf("platform=P8 slug=tag-pico-8 page 2: fetch feed: HTTP 500")

	merge := fetch.Merge(previous)
	want := "PSX:old-psx GBC:shared GBC:old-gbc GB:gb-new-2 GB:gb-new-1 GB:old-gb-1 GB:old-gb-2 P8:old-p8"
	if got := gameURLs(merge.Games); got != want {
		t.Fatalf("merged games = %s\nwant %s", got, want)
	}
	for _, game := range merge.Games {
		if game.URL == cachedGB.URL && (game.Price != 2.5 || game.Title != "Cached Title") {
			t.Errorf("cached entry changed: %+v", game)
		}
	}
	if got := strings.Join(merge.Kept, " "); got != "P8" {
		t.Errorf("systems that kept their cached games = %s, want P8", got)
	}
}
