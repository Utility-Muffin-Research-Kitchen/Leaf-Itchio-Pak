package itchio

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// GamesCacheRevision identifies the cache format and the feed/platform
// coverage it represents. Bump it whenever AllPlatforms or the format
// changes: a cache of any other revision stays usable, and a full crawl
// replaces it in the background after the app update. Revision 2 added
// full_fetched_at and checked_at for the daily check.
const GamesCacheRevision = 2

const (
	// GamesCheckInterval is how old the last check may get before a launch
	// checks the newest feeds for new games (D2).
	GamesCheckInterval = 24 * time.Hour
	// GamesFullCrawlInterval is how old the last full crawl may get before a
	// launch reads every feed again. Only a full crawl updates prices, titles
	// and tags of known games, and drops removed ones.
	GamesFullCrawlInterval = 7 * 24 * time.Hour
)

// CacheMeta records when the cache was saved, checked and fully crawled.
type CacheMeta struct {
	Revision      int       `json:"revision"`
	FetchedAt     time.Time `json:"fetched_at"`      // last save; the list header's cache age
	FullFetchedAt time.Time `json:"full_fetched_at"` // last full crawl of every feed
	CheckedAt     time.Time `json:"checked_at"`      // last refresh of either kind
	TotalGames    int       `json:"total_games"`
}

// GameCache is the on-disk representation of the full game list.
type GameCache struct {
	Meta  CacheMeta `json:"meta"`
	Games []Game    `json:"games"`
}

func (cache *GameCache) CurrentRevision() bool {
	return cache != nil && cache.Meta.Revision == GamesCacheRevision
}

// CacheRefresh is the kind of refresh a cache needs.
type CacheRefresh int

const (
	CacheRefreshNone        CacheRefresh = iota
	CacheRefreshIncremental              // check the newest feeds for new games
	CacheRefreshFull                     // read every page of every feed
)

func (refresh CacheRefresh) String() string {
	switch refresh {
	case CacheRefreshIncremental:
		return "incremental"
	case CacheRefreshFull:
		return "full"
	default:
		return "none"
	}
}

// DueRefresh says which refresh a launch at now should start: a full crawl
// when the cache is of another revision or its last full crawl is
// GamesFullCrawlInterval old, a check of the newest feeds when its last check
// is GamesCheckInterval old, else none. A time in the future counts as recent.
func (cache *GameCache) DueRefresh(now time.Time) CacheRefresh {
	switch {
	case !cache.CurrentRevision(), now.Sub(cache.Meta.FullFetchedAt) >= GamesFullCrawlInterval:
		return CacheRefreshFull
	case now.Sub(cache.Meta.CheckedAt) >= GamesCheckInterval:
		return CacheRefreshIncremental
	default:
		return CacheRefreshNone
	}
}

// SaveGamesCache writes games after a full crawl: the save, the check and the
// full crawl are all now.
func SaveGamesCache(path string, games []Game) error {
	now := time.Now()
	return writeGamesCache(path, games, CacheMeta{FetchedAt: now, FullFetchedAt: now, CheckedAt: now})
}

// SaveCheckedGamesCache writes games after a check of the newest feeds. The
// save and the check are now; fullFetchedAt, the last full crawl, is kept.
func SaveCheckedGamesCache(path string, games []Game, fullFetchedAt time.Time) error {
	now := time.Now()
	return writeGamesCache(path, games, CacheMeta{FetchedAt: now, FullFetchedAt: fullFetchedAt, CheckedAt: now})
}

// writeGamesCache writes games and meta to path atomically (write to .tmp
// then rename), at the current revision.
func writeGamesCache(path string, games []Game, meta CacheMeta) error {
	meta.Revision, meta.TotalGames = GamesCacheRevision, len(games)
	cache := GameCache{Meta: meta, Games: games}
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("marshal game cache: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write game cache tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename game cache: %w", err)
	}
	return nil
}

// LoadGamesCache reads and parses the cache file at path.
// Returns an error if the file is missing or unparseable.
func LoadGamesCache(path string) (*GameCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read game cache: %w", err)
	}
	var cache GameCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse game cache: %w", err)
	}
	for i := range cache.Games {
		g := &cache.Games[i]
		if !hasDisplayableChar(g.Title) {
			g.Title = SlugToTitle(g.URL)
		}
	}
	return &cache, nil
}
