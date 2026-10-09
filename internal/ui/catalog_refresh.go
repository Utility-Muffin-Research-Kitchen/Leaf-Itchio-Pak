//go:build !headless

package ui

import (
	"context"
	"errors"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// errNoSystemRefreshed reports a refresh in which no system's feeds all
// finished, so there was nothing to save.
var errNoSystemRefreshed = errors.New("no system was refreshed")

// commitCatalogFetch applies a refresh to previous, the cache it started
// from, and saves the result to cachePath. A system whose feeds all finished
// takes the new games; a system with a feed that failed, or that rate
// limiting stopped, keeps its games from previous, and the log names the
// feed. Nothing is saved when the refresh was cancelled or no system
// finished; the error then says why. It returns the saved games.
func commitCatalogFetch(cachePath string, previous []itchio.Game, fetch *itchio.CatalogFetch, fetchErr error) ([]itchio.Game, error) {
	if errors.Is(fetchErr, context.Canceled) {
		logger.Info("cache: refresh cancelled; nothing saved")
		return nil, fetchErr
	}
	merge := fetch.Merge(previous)
	failed := fetch.Failed()
	if len(merge.Updated) == 0 {
		// The feed loop already logged each feed's error.
		logger.Error("cache: no system refreshed (%d of %d feeds failed); nothing saved", len(failed), len(fetch.Feeds))
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
	if err := itchio.SaveGamesCache(cachePath, merge.Games); err != nil {
		logger.Error("cache: save failed: %v", err)
		return nil, err
	}
	if len(merge.Kept) > 0 {
		logger.Info("cache: saved %d games to %s; refreshed %s, kept cached %s", len(merge.Games), cachePath,
			strings.Join(merge.Updated, " "), strings.Join(merge.Kept, " "))
	} else {
		logger.Info("cache: saved %d games to %s", len(merge.Games), cachePath)
	}
	return merge.Games, nil
}
