package itchio_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func BenchmarkLoadGamesCache1000(b *testing.B) {
	path := filepath.Join(b.TempDir(), "games_cache.json")
	games := make([]itchio.Game, 1000)
	for i := range games {
		games[i] = itchio.Game{
			Title:    fmt.Sprintf("Game %04d", i),
			Author:   fmt.Sprintf("Author %03d", i%100),
			URL:      fmt.Sprintf("https://author-%d.itch.io/game-%d", i%100, i),
			CoverURL: fmt.Sprintf("https://img.example/%d.png", i),
			Platform: []string{"GB", "GBC", "GBA", "NES", "MD", "P8"}[i%6],
			IsFree:   i%4 != 0,
		}
	}
	if err := itchio.SaveGamesCache(path, games); err != nil {
		b.Fatalf("SaveGamesCache: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache, err := itchio.LoadGamesCache(path)
		if err != nil {
			b.Fatalf("LoadGamesCache: %v", err)
		}
		if len(cache.Games) != len(games) {
			b.Fatalf("loaded %d games, want %d", len(cache.Games), len(games))
		}
	}
}

func TestSaveAndLoadGamesCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "games_cache.json")

	games := []itchio.Game{
		{Title: "Alpha", Author: "dev1", URL: "https://dev1.itch.io/alpha", IsFree: true},
		{Title: "Beta", Author: "dev2", URL: "https://dev2.itch.io/beta", Price: 4.99},
	}

	if err := itchio.SaveGamesCache(path, games); err != nil {
		t.Fatalf("SaveGamesCache: %v", err)
	}

	cache, err := itchio.LoadGamesCache(path)
	if err != nil {
		t.Fatalf("LoadGamesCache: %v", err)
	}
	if len(cache.Games) != 2 {
		t.Fatalf("got %d games, want 2", len(cache.Games))
	}
	if cache.Games[0].Title != "Alpha" {
		t.Errorf("Games[0].Title = %q, want %q", cache.Games[0].Title, "Alpha")
	}
	if cache.Games[1].Price != 4.99 {
		t.Errorf("Games[1].Price = %v, want 4.99", cache.Games[1].Price)
	}
	if cache.Meta.TotalGames != 2 {
		t.Errorf("Meta.TotalGames = %d, want 2", cache.Meta.TotalGames)
	}
	if cache.Meta.Revision != itchio.GamesCacheRevision || !cache.CurrentRevision() {
		t.Errorf("cache revision = %d, want current %d", cache.Meta.Revision, itchio.GamesCacheRevision)
	}
	if cache.Meta.FetchedAt.IsZero() {
		t.Error("Meta.FetchedAt should not be zero")
	}
	if time.Since(cache.Meta.FetchedAt) > 5*time.Second {
		t.Error("Meta.FetchedAt should be recent")
	}
}

func TestLoadGamesCache_LegacyRevisionRemainsUsableButRequestsRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games_cache.json")
	legacy := `{"meta":{"fetched_at":"2026-07-12T00:00:00Z","total_games":1},"games":[{"title":"Existing Game","url":"https://dev.itch.io/existing","platform":"GB"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	cache, err := itchio.LoadGamesCache(path)
	if err != nil {
		t.Fatalf("LoadGamesCache legacy: %v", err)
	}
	if len(cache.Games) != 1 || cache.Games[0].Title != "Existing Game" {
		t.Fatalf("legacy cache content lost: %#v", cache.Games)
	}
	if cache.CurrentRevision() {
		t.Fatalf("legacy revision %d treated as current", cache.Meta.Revision)
	}
}

func TestLoadGamesCache_MissingFile(t *testing.T) {
	_, err := itchio.LoadGamesCache("/tmp/does-not-exist-xyz.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoadGamesCache_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad_cache.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := itchio.LoadGamesCache(path)
	if err == nil {
		t.Fatal("expected error for corrupt file, got nil")
	}
}

func TestLoadGamesCache_repairsTitlesOnLoad(t *testing.T) {
	// Caches written before the slug-fallback fix may contain games with empty
	// or undrawable titles. LoadGamesCache must repair them on load, and keep
	// titles without letters that still draw.
	dir := t.TempDir()
	path := filepath.Join(dir, "games_cache.json")

	stale := []itchio.Game{
		{Title: "", URL: "https://soyouz.itch.io/spread"},
		{Title: "🔴", URL: "https://iansundstrom.itch.io/redcircle"},
		{Title: "Normal Game", URL: "https://dev.itch.io/normal-game"},
		{Title: "35!", URL: "https://dev.itch.io/thirty-five"},
		{Title: "\u200b\ue000", URL: "https://dev.itch.io/garbage-title"},
	}
	if err := itchio.SaveGamesCache(path, stale); err != nil {
		t.Fatalf("SaveGamesCache: %v", err)
	}

	cache, err := itchio.LoadGamesCache(path)
	if err != nil {
		t.Fatalf("LoadGamesCache: %v", err)
	}
	if cache.Games[0].Title != "Spread" {
		t.Errorf("empty title: got %q, want %q", cache.Games[0].Title, "Spread")
	}
	if cache.Games[1].Title != "🔴" {
		t.Errorf("emoji title: got %q, want %q", cache.Games[1].Title, "🔴")
	}
	if cache.Games[2].Title != "Normal Game" {
		t.Errorf("normal title: got %q, want %q", cache.Games[2].Title, "Normal Game")
	}
	if cache.Games[3].Title != "35!" {
		t.Errorf("letterless title: got %q, want %q", cache.Games[3].Title, "35!")
	}
	if cache.Games[4].Title != "Garbage Title" {
		t.Errorf("undrawable title: got %q, want %q", cache.Games[4].Title, "Garbage Title")
	}
}

func TestSaveGamesCache_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "games_cache.json")

	// Save once, then overwrite — previous data should not leak.
	_ = itchio.SaveGamesCache(path, []itchio.Game{{Title: "Old"}})
	if err := itchio.SaveGamesCache(path, []itchio.Game{{Title: "New"}}); err != nil {
		t.Fatalf("second SaveGamesCache: %v", err)
	}
	cache, err := itchio.LoadGamesCache(path)
	if err != nil {
		t.Fatalf("LoadGamesCache after overwrite: %v", err)
	}
	if cache.Games[0].Title != "New" {
		t.Errorf("got %q, want %q", cache.Games[0].Title, "New")
	}
	// Temp file must not linger.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file .tmp should not exist after successful save")
	}
}

// D2: a launch reads every feed again when the last full crawl is a week
// old or the cache is of another revision, checks the newest feeds when the
// last check is a day old, and otherwise does nothing.
func TestGameCacheDueRefresh(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	const day = 24 * time.Hour
	for _, tc := range []struct {
		name string
		meta itchio.CacheMeta
		want itchio.CacheRefresh
	}{
		{"checked an hour ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FullFetchedAt: ago(2 * day), CheckedAt: ago(time.Hour)}, itchio.CacheRefreshNone},
		{"checked a day ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FullFetchedAt: ago(2 * day), CheckedAt: ago(day)}, itchio.CacheRefreshIncremental},
		{"checked 25 h ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FullFetchedAt: ago(6 * day), CheckedAt: ago(25 * time.Hour)}, itchio.CacheRefreshIncremental},
		{"full crawl a week ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FullFetchedAt: ago(7 * day), CheckedAt: ago(time.Hour)}, itchio.CacheRefreshFull},
		{"full crawl 8 days ago, checked 25 h ago", itchio.CacheMeta{Revision: itchio.GamesCacheRevision, FullFetchedAt: ago(8 * day), CheckedAt: ago(25 * time.Hour)}, itchio.CacheRefreshFull},
		{"revision 1 saved an hour ago", itchio.CacheMeta{Revision: 1, FetchedAt: ago(time.Hour)}, itchio.CacheRefreshFull},
		{"newer revision", itchio.CacheMeta{Revision: itchio.GamesCacheRevision + 1, FullFetchedAt: ago(time.Hour), CheckedAt: ago(time.Hour)}, itchio.CacheRefreshFull},
	} {
		cache := &itchio.GameCache{Meta: tc.meta}
		if got := cache.DueRefresh(now); got != tc.want {
			t.Errorf("%s: DueRefresh = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Revision 2 records the last full crawl and the last check next to the
// last save. A full crawl's save sets all three; a check's save keeps the
// full crawl's time.
func TestSaveGamesCacheRecordsTheFullCrawlAndTheCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "games_cache.json")
	games := []itchio.Game{{Title: "Alpha", URL: "https://dev.itch.io/alpha", Platform: "GB"}}
	if err := itchio.SaveGamesCache(path, games); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"revision":2`, `"fetched_at":`, `"full_fetched_at":`, `"checked_at":`, `"total_games":1`} {
		if !strings.Contains(string(data), field) {
			t.Errorf("saved meta lacks %s: %.200s", field, data)
		}
	}
	full, err := itchio.LoadGamesCache(path)
	if err != nil {
		t.Fatal(err)
	}
	meta := full.Meta
	if time.Since(meta.FullFetchedAt) > 5*time.Second || !meta.CheckedAt.Equal(meta.FullFetchedAt) || !meta.FetchedAt.Equal(meta.CheckedAt) {
		t.Fatalf("full crawl meta = %+v, want fetched, checked and full crawl all now", meta)
	}

	lastFull := time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)
	if err := itchio.SaveCheckedGamesCache(path, games, lastFull); err != nil {
		t.Fatal(err)
	}
	checked, err := itchio.LoadGamesCache(path)
	if err != nil {
		t.Fatal(err)
	}
	meta = checked.Meta
	if !meta.FullFetchedAt.Equal(lastFull) || time.Since(meta.CheckedAt) > 5*time.Second || !meta.FetchedAt.Equal(meta.CheckedAt) {
		t.Fatalf("check meta = %+v, want the full crawl kept at %v and fetched/checked now", meta, lastFull)
	}
	if !checked.CurrentRevision() || meta.TotalGames != 1 {
		t.Fatalf("check meta = %+v", meta)
	}
}
