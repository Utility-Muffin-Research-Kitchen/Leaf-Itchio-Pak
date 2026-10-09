//go:build !headless

package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// errNoSystemRefreshed reports a refresh in which no system's feeds all
// finished, so there was nothing to save.
var errNoSystemRefreshed = errors.New("no system was refreshed")

// commitCatalogFetch applies a refresh to previous, the cache it started
// from (nil on first launch), and saves the result to cachePath. A system
// whose feeds all finished takes the new games: a full crawl replaces its
// games, a daily check puts the new ones on top. A system with a feed that
// failed, or that rate limiting stopped, keeps its games from previous, and
// the log names the feed. Nothing is saved when the refresh was cancelled or
// no system finished; the error then says why. Every refresh ends with one
// summary line in the log. It returns the saved games.
func commitCatalogFetch(cachePath string, previous *itchio.GameCache, fetch *itchio.CatalogFetch, fetchErr error) ([]itchio.Game, error) {
	mode := itchio.CacheRefreshFull
	if fetch.Incremental {
		mode = itchio.CacheRefreshIncremental
	}
	var previousGames []itchio.Game
	var lastFullCrawl time.Time
	if previous != nil {
		previousGames, lastFullCrawl = previous.Games, previous.Meta.FullFetchedAt
	}
	summary := func(result string, games []itchio.Game, kept []string) {
		known := make(map[string]bool, len(previousGames))
		for _, game := range previousGames {
			known[game.URL] = true
		}
		newGames := 0
		for _, game := range games {
			if !known[game.URL] {
				newGames++
			}
		}
		line := fmt.Sprintf("cache: refresh mode=%s result=%s games=%d new_games=%d pages=%d http_429=%d duration=%s failed_feeds=%d",
			mode, result, len(games), newGames, fetch.Pages(), fetch.RateLimited,
			fetch.Duration.Round(100*time.Millisecond), len(fetch.Failed()))
		if len(kept) > 0 {
			line += " kept=" + strings.Join(kept, ",")
		}
		logger.Info("%s", line)
	}

	if errors.Is(fetchErr, context.Canceled) {
		summary("cancelled", nil, nil)
		return nil, fetchErr
	}
	merge := fetch.Merge(previousGames)
	failed := fetch.Failed()
	if len(merge.Updated) == 0 {
		// The feed loop already logged each feed's error.
		logger.Error("cache: no system refreshed (%d of %d feeds failed); nothing saved", len(failed), len(fetch.Feeds))
		summary("nothing-saved", nil, merge.Kept)
		if fetchErr == nil {
			fetchErr = errNoSystemRefreshed
		}
		return nil, fetchErr
	}
	perSystem := make(map[string]int)
	for _, game := range merge.Games {
		perSystem[game.Platform]++
	}
	for _, feed := range failed {
		logger.Warn("cache: %s keeps its %d cached game(s): %v", feed.Platform, perSystem[feed.Platform], feed.Err)
	}
	var err error
	if mode == itchio.CacheRefreshIncremental {
		err = itchio.SaveCheckedGamesCache(cachePath, merge.Games, lastFullCrawl)
	} else {
		err = itchio.SaveGamesCache(cachePath, merge.Games)
	}
	if err != nil {
		logger.Error("cache: save failed: %v", err)
		summary("save-failed", nil, merge.Kept)
		return nil, err
	}
	summary("saved", merge.Games, merge.Kept)
	return merge.Games, nil
}
