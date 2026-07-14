//go:build !headless

package ui

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

type catCacheRefreshResult struct {
	games     []itchio.Game
	err       error
	cancelled bool
}

type CatCacheRefreshFlow struct {
	cancel  context.CancelFunc
	wake    func()
	result  chan catCacheRefreshResult
	fetched atomic.Int64
	shown   int
	done    atomic.Bool
}

func NewCatCacheRefreshFlow(client *itchio.Client, cachePath string, wake func()) (*CatCacheRefreshFlow, *appui.RefreshModel) {
	ctx, cancel := context.WithCancel(context.Background())
	flow := &CatCacheRefreshFlow{cancel: cancel, wake: wake, result: make(chan catCacheRefreshResult, 1)}
	model := appui.NewRefreshModel("Refreshing Game List")
	go func() {
		games, err := client.FetchAllGames(ctx, func(partial []itchio.Game) {
			flow.fetched.Store(int64(len(partial)))
			if flow.wake != nil {
				flow.wake()
			}
		})
		result := catCacheRefreshResult{games: games, err: err, cancelled: errors.Is(err, context.Canceled)}
		if err == nil {
			result.err = itchio.SaveGamesCache(cachePath, games)
		}
		flow.done.Store(true)
		flow.result <- result
		if flow.wake != nil {
			flow.wake()
		}
	}()
	return flow, model
}

func (flow *CatCacheRefreshFlow) Cancel() { flow.cancel() }

func (flow *CatCacheRefreshFlow) Busy() bool { return !flow.done.Load() }

func (flow *CatCacheRefreshFlow) Sync(model *appui.RefreshModel) ([]itchio.Game, bool) {
	changed := false
	fetched := int(flow.fetched.Load())
	if fetched != flow.shown {
		flow.shown, model.Fetched, changed = fetched, fetched, true
	}
	select {
	case result := <-flow.result:
		switch {
		case result.cancelled:
			model.State = appui.RefreshCancelled
		case result.err != nil:
			model.State, model.Detail = appui.RefreshError, "The catalogue could not be refreshed. The previous cache remains available."
		default:
			model.State, model.Total = appui.RefreshDone, len(result.games)
			if result.games == nil {
				result.games = []itchio.Game{}
			}
			return result.games, true
		}
		return nil, true
	default:
		return nil, changed
	}
}
