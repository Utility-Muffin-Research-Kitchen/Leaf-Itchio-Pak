package itchio_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func platformGame(platform, slug string) itchio.Game {
	return itchio.Game{Title: slug, URL: "https://dev.itch.io/" + slug, Platform: platform}
}

// everyFeed returns a fetch with one finished, empty feed per slug, in
// AllPlatforms order.
func everyFeed() *itchio.CatalogFetch {
	fetch := &itchio.CatalogFetch{}
	for _, platform := range itchio.AllPlatforms {
		for _, slug := range platform.FeedSlugs {
			fetch.Feeds = append(fetch.Feeds, itchio.FeedFetch{Platform: platform.Code, Slug: slug})
		}
	}
	return fetch
}

func feedOf(t *testing.T, fetch *itchio.CatalogFetch, slug string) *itchio.FeedFetch {
	t.Helper()
	for index := range fetch.Feeds {
		if fetch.Feeds[index].Slug == slug {
			return &fetch.Feeds[index]
		}
	}
	t.Fatalf("no feed %s", slug)
	return nil
}

func gameURLs(games []itchio.Game) string {
	parts := make([]string, len(games))
	for index, game := range games {
		parts[index] = game.Platform + ":" + strings.TrimPrefix(game.URL, "https://dev.itch.io/")
	}
	return strings.Join(parts, " ")
}

// A system whose feeds all finished takes the new games; a system with a
// failed feed keeps its previous games, even what its other feeds returned.
// Games are deduplicated by URL in AllPlatforms order, so a game listed by
// GBC and by GB stays a GBC game.
func TestCatalogFetchMergeKeepsTheCachedGamesOfAFailedSystem(t *testing.T) {
	previous := []itchio.Game{
		platformGame("PSX", "old-psx"), platformGame("GB", "old-gb-1"), platformGame("GB", "gb-and-gbc"),
		platformGame("P8", "old-p8"), platformGame("MD", "old-md"),
	}
	fetch := everyFeed()
	feedOf(t, fetch, "tag-homebrew/tag-psx").Games = []itchio.Game{platformGame("PSX", "new-psx")}
	feedOf(t, fetch, "tag-gameboy-color").Games = []itchio.Game{platformGame("GBC", "gb-and-gbc")}
	feedOf(t, fetch, "made-with-gb-studio").Games = []itchio.Game{platformGame("GB", "new-gb")}
	feedOf(t, fetch, "tag-gbstudio").Err = errors.New("fetch feed: HTTP 500")
	feedOf(t, fetch, "tag-pico-8").Games = []itchio.Game{platformGame("P8", "new-p8"), platformGame("P8", "new-psx")}

	merge := fetch.Merge(previous)
	if got, want := gameURLs(merge.Games), "PSX:new-psx GBC:gb-and-gbc GB:old-gb-1 P8:new-p8"; got != want {
		t.Fatalf("merged games = %s\nwant %s", got, want)
	}
	if got := strings.Join(merge.Updated, " "); got != "PSX GBC GBA NES MD P8" {
		t.Errorf("updated systems = %s", got)
	}
	if got := strings.Join(merge.Kept, " "); got != "GB" {
		t.Errorf("systems that kept their cached games = %s, want GB", got)
	}
	if got, want := gameURLs(fetch.Games()), "PSX:new-psx GBC:gb-and-gbc P8:new-p8"; got != want {
		t.Errorf("fetched games = %s, want %s", got, want)
	}
	if failed := fetch.Failed(); len(failed) != 1 || failed[0].Slug != "tag-gbstudio" {
		t.Errorf("failed feeds = %+v, want tag-gbstudio", failed)
	}
}

// When every feed fails no system is updated, and every system keeps its
// previous games.
func TestCatalogFetchMergeWithEveryFeedFailed(t *testing.T) {
	fetch := everyFeed()
	for index := range fetch.Feeds {
		fetch.Feeds[index].Err = errors.New("fetch feed: HTTP 404")
	}
	previous := []itchio.Game{platformGame("GB", "old-gb"), platformGame("P8", "old-p8")}
	merge := fetch.Merge(previous)
	if len(merge.Updated) != 0 || len(merge.Kept) != len(itchio.AllPlatforms) {
		t.Fatalf("updated %v, kept %v; want no system updated", merge.Updated, merge.Kept)
	}
	if got := gameURLs(merge.Games); got != "GB:old-gb P8:old-p8" {
		t.Fatalf("merged games = %s, want the previous games", got)
	}
}

// FetchAllGames reads every feed past a failing one and reports each feed's
// games and error.
func TestFetchAllGames_ReportsEachFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/games/tag-gbstudio.xml":
			w.WriteHeader(http.StatusNotFound)
		case r.URL.Path == "/games/tag-nes-rom.xml" && r.URL.Query().Get("page") == "1":
			w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel>
<item><title>A NES Game</title><link>https://dev.itch.io/nes-game</link><price>0.0</price></item>
</channel></rss>`))
		default:
			w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`))
		}
	}))
	defer srv.Close()

	fetch, err := itchio.NewClientWithBase(srv.URL).FetchAllGames(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "slug=tag-gbstudio") {
		t.Fatalf("err = %v, want the failed feed's error", err)
	}
	want := everyFeed()
	if len(fetch.Feeds) != len(want.Feeds) {
		t.Fatalf("fetch has %d feeds, want %d", len(fetch.Feeds), len(want.Feeds))
	}
	for index, feed := range fetch.Feeds {
		if feed.Platform != want.Feeds[index].Platform || feed.Slug != want.Feeds[index].Slug {
			t.Errorf("feed %d = %s %s, want %s %s", index, feed.Platform, feed.Slug,
				want.Feeds[index].Platform, want.Feeds[index].Slug)
		}
		if failed := feed.Err != nil; failed != (feed.Slug == "tag-gbstudio") {
			t.Errorf("feed %s error = %v", feed.Slug, feed.Err)
		}
	}
	nes := feedOf(t, fetch, "tag-nes-rom")
	if len(nes.Games) != 1 || nes.Games[0].Platform != "NES" {
		t.Errorf("NES feed games = %+v, want the NES game", nes.Games)
	}
	if merge := fetch.Merge(nil); strings.Join(merge.Kept, " ") != "GB" || gameURLs(merge.Games) != "NES:nes-game" {
		t.Errorf("merge = kept %v, games %s; want GB kept and the NES game", merge.Kept, gameURLs(merge.Games))
	}
}
