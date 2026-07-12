//go:build !headless

package ui

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

const cacheTTL = 24 * time.Hour

// UpdateServicer is the catalogue controller's narrow view of Jawaka's
// inventory update service.
type UpdateServicer interface {
	TriggerNow()
	IsRunning() bool
}

type pageResult struct {
	games []itchio.Game
	err   error
}

// CatalogController owns the live catalogue, filters, owned-game state, and
// background cache lifecycle. Rendering and input belong to Catastrophe.
type CatalogController struct {
	client  *itchio.Client
	cfg     *settings.Config
	cfgPath string

	cursor       int
	loading      atomic.Bool
	err          error
	pageUpdateCh chan pageResult

	cachedGames []itchio.Game
	cacheReady  bool
	cachePath   string
	viewGames   []itchio.Game

	inv           *inventory.Inventory
	inventoryPath string
	updateSvc     UpdateServicer

	cacheBuilding atomic.Bool
	cacheUpdateCh chan []itchio.Game
	needsRebuild  bool

	ownedUpdateCh  chan map[string]bool
	ownedURLs      map[string]bool
	ownedCachePath string

	sortMode       itchio.SortMode
	platformFilter string
	searchQuery    string

	wakeMu sync.RWMutex
	wake   func()
}

func NewCatalogController(client *itchio.Client, cfg *settings.Config, cfgPath, cachePath string,
	inv *inventory.Inventory, inventoryPath string, updateSvc UpdateServicer,
	ownedCachePath string) *CatalogController {
	controller := &CatalogController{
		client: client, cfg: cfg, cfgPath: cfgPath, cachePath: cachePath,
		inv: inv, inventoryPath: inventoryPath, updateSvc: updateSvc,
		pageUpdateCh: make(chan pageResult, 1), cacheUpdateCh: make(chan []itchio.Game, 1),
		ownedUpdateCh: make(chan map[string]bool, 1), ownedURLs: make(map[string]bool),
		ownedCachePath: ownedCachePath, sortMode: itchio.SortMode(cfg.SortMode),
		platformFilter: cfg.PlatformFilter,
	}

	if urls, err := itchio.LoadOwnedCache(ownedCachePath); err == nil && len(urls) > 0 {
		for _, url := range urls {
			controller.ownedURLs[url] = true
		}
		logger.Info("owned: loaded %d owned game URL(s) from cache", len(controller.ownedURLs))
	} else if err != nil {
		logger.Warn("owned: failed to load owned cache: %v", err)
	}

	if cfg.APIKey != "" {
		go func() {
			_, owned, err := client.ValidateAPIKey(cfg.APIKey)
			if err != nil {
				logger.Warn("owned: startup key validation failed: %v", err)
				return
			}
			controller.publishOwned(owned)
		}()
	}

	gameCache, err := itchio.LoadGamesCache(cachePath)
	if err == nil && len(gameCache.Games) > 0 {
		logger.Info("cache: loaded %d games from %s (age=%v)", len(gameCache.Games), cachePath,
			time.Since(gameCache.Meta.FetchedAt).Round(time.Second))
		controller.cachedGames = gameCache.Games
		controller.cacheReady = true
		controller.rebuildView()
		if !gameCache.CurrentRevision() {
			logger.Info("cache: catalogue revision %d is older than %d; refreshing platform coverage in background",
				gameCache.Meta.Revision, itchio.GamesCacheRevision)
			go controller.buildCache()
		} else {
			go controller.refreshCacheIfStale(gameCache.Meta.FetchedAt)
		}
	} else {
		if err != nil {
			logger.Debug("cache: no cache found (%v), using live feed", err)
		} else {
			logger.Debug("cache: file exists but contains no games, using live feed")
		}
		go controller.loadPage(1, "")
		go controller.buildCache()
	}
	return controller
}

func (controller *CatalogController) publishOwned(owned []itchio.OwnedGame) {
	urls := make([]string, len(owned))
	for index, game := range owned {
		urls[index] = game.URL
	}
	if err := itchio.SaveOwnedCache(controller.ownedCachePath, urls); err != nil {
		logger.Warn("owned: failed to save owned cache: %v", err)
	}
	ownedURLs := make(map[string]bool, len(urls))
	for _, url := range urls {
		ownedURLs[url] = true
	}
	select {
	case <-controller.ownedUpdateCh:
	default:
	}
	controller.ownedUpdateCh <- ownedURLs
	controller.wakeUI()
	logger.Info("owned: %d owned game URL(s) received from key validation", len(ownedURLs))
}

func (controller *CatalogController) loadPage(page int, query string) {
	controller.loading.Store(true)
	logger.Debug("feed: loading page %d query=%q", page, query)
	games, err := controller.client.FetchGames(page, query)
	if err != nil {
		logger.Error("feed: page %d error: %v", page, err)
	} else {
		logger.Info("feed: page %d returned %d games", page, len(games))
	}
	select {
	case <-controller.pageUpdateCh:
	default:
	}
	controller.pageUpdateCh <- pageResult{games: games, err: err}
	controller.wakeUI()
}

func (controller *CatalogController) SetWake(wake func()) {
	controller.wakeMu.Lock()
	controller.wake = wake
	controller.wakeMu.Unlock()
}

func (controller *CatalogController) wakeUI() {
	controller.wakeMu.RLock()
	wake := controller.wake
	controller.wakeMu.RUnlock()
	if wake != nil {
		wake()
	}
}

func (controller *CatalogController) consumeUpdates() {
	select {
	case games := <-controller.cacheUpdateCh:
		controller.cachedGames = games
		controller.cacheReady = true
		controller.needsRebuild = false
		controller.rebuildView()
	default:
	}
	select {
	case owned := <-controller.ownedUpdateCh:
		controller.ownedURLs = owned
		controller.rebuildView()
	default:
	}
	select {
	case result := <-controller.pageUpdateCh:
		controller.loading.Store(false)
		controller.viewGames = result.games
		controller.err = result.err
		controller.cursor = 0
	default:
	}
	if controller.needsRebuild {
		controller.needsRebuild = false
		controller.rebuildView()
	}
}

func (controller *CatalogController) SyncCatModel(model *appui.MainListModel) {
	if model == nil {
		return
	}
	controller.consumeUpdates()
	model.Platform = "All platforms"
	if controller.platformFilter != "" {
		model.Platform = controller.platformFilter
	}
	model.Sort = itchio.SortModeBadge(controller.sortMode)
	if controller.loading.Load() {
		model.SetLoading()
		return
	}
	if controller.err != nil {
		model.SetError(controller.err.Error())
		return
	}
	items := make([]appui.ListItem, 0, len(controller.viewGames))
	for _, game := range controller.viewGames {
		badge := "Free"
		switch {
		case controller.inv.HasPendingUpdates(game.URL):
			badge = "UP"
		case controller.inv.IsRemoved(game.URL):
			badge = "!"
		case controller.inv.IsPresent(game.URL):
			badge = "DL"
		case controller.ownedURLs[game.URL]:
			badge = "OWNED"
		case !game.IsFree:
			badge = "$" + strconv.FormatFloat(game.Price, 'f', 2, 64)
		}
		items = append(items, appui.ListItem{Title: game.Title, Author: game.Author,
			CoverKey: game.CoverURL, Badge: badge, Tags: append([]string(nil), game.Tags...)})
	}
	model.SetItems(items)
}

func (controller *CatalogController) CatSelected(index int) (itchio.Game, bool) {
	if index < 0 || index >= len(controller.viewGames) {
		return itchio.Game{}, false
	}
	controller.cursor = index
	return controller.viewGames[index], true
}

func (controller *CatalogController) DismissNotice(index int) {
	game, ok := controller.CatSelected(index)
	if !ok {
		return
	}
	if controller.inv.HasPendingUpdates(game.URL) {
		controller.inv.DismissUpdate(game.URL)
		logger.Info("update-svc: update dismissed for game=%q", game.Title)
	} else if controller.inv.IsRemoved(game.URL) {
		controller.inv.DismissRemoval(game.URL)
		logger.Info("update-svc: removal dismissed for game=%q", game.Title)
	} else {
		return
	}
	if err := controller.inv.Save(controller.inventoryPath); err != nil {
		logger.Warn("inventory: save after dismiss: %v", err)
	}
	controller.rebuildView()
}

func (controller *CatalogController) RetryCatLoad() { go controller.loadPage(1, "") }

func (controller *CatalogController) ApplyCatCache(games []itchio.Game) {
	snapshot := append([]itchio.Game(nil), games...)
	select {
	case controller.cacheUpdateCh <- snapshot:
	default:
		select {
		case <-controller.cacheUpdateCh:
		default:
		}
		select {
		case controller.cacheUpdateCh <- snapshot:
		default:
		}
	}
	controller.wakeUI()
	if controller.updateSvc != nil {
		controller.updateSvc.TriggerNow()
	}
}

func (controller *CatalogController) CatFilter() (platform, sort, query string) {
	return controller.platformFilter, string(controller.sortMode), controller.searchQuery
}

func (controller *CatalogController) ApplyCatFilter(platform, sort, query string) {
	controller.SetFilter(platform, sort, query)
}

func (controller *CatalogController) CycleCatSort(direction int) {
	if !controller.cacheReady {
		return
	}
	next := controller.nextSortMode()
	if direction < 0 {
		next = controller.previousSortMode()
	}
	controller.SetFilter(controller.platformFilter, string(next), controller.searchQuery)
}

func (controller *CatalogController) SetFilter(platform, sort, query string) {
	controller.platformFilter = platform
	controller.sortMode = itchio.SortMode(sort)
	controller.searchQuery = query
	controller.rebuildView()
	controller.cursor = 0
	controller.cfg.PlatformFilter = platform
	controller.cfg.SortMode = string(controller.sortMode)
	go controller.cfg.Save(controller.cfgPath)
	logger.Info("filter: platform=%q sort=%q query=%q", platform, sort, query)
}

func (controller *CatalogController) nextSortMode() itchio.SortMode {
	mode := itchio.NextSortMode(controller.sortMode)
	if mode == itchio.SortModeOwned && len(controller.ownedURLs) == 0 {
		mode = itchio.NextSortMode(mode)
	}
	return mode
}

func (controller *CatalogController) previousSortMode() itchio.SortMode {
	mode := itchio.PrevSortMode(controller.sortMode)
	if mode == itchio.SortModeOwned && len(controller.ownedURLs) == 0 {
		mode = itchio.PrevSortMode(mode)
	}
	return mode
}

func (controller *CatalogController) ScheduleRebuild() { controller.needsRebuild = true }

func (controller *CatalogController) rebuildView() {
	selectedURL := ""
	selectedIndex := controller.cursor
	if controller.cursor >= 0 && controller.cursor < len(controller.viewGames) {
		selectedURL = controller.viewGames[controller.cursor].URL
	}
	downloaded := make(map[string]bool)
	pending := make(map[string]bool)
	removed := make(map[string]bool)
	for _, game := range controller.cachedGames {
		if controller.inv.IsPresent(game.URL) {
			downloaded[game.URL] = true
		}
		if controller.inv.HasPendingUpdates(game.URL) {
			pending[game.URL] = true
		}
		if controller.inv.IsRemoved(game.URL) {
			removed[game.URL] = true
		}
	}
	filtered := controller.cachedGames
	if controller.platformFilter != "" {
		filtered = applyPlatformFilter(filtered, controller.platformFilter)
	}
	if controller.searchQuery != "" {
		filtered = applySearchFilter(filtered, controller.searchQuery)
	}
	controller.viewGames = itchio.ApplySort(filtered, controller.sortMode, downloaded, pending, removed, controller.ownedURLs)
	if selectedURL != "" {
		for index, game := range controller.viewGames {
			if game.URL == selectedURL {
				controller.cursor = index
				return
			}
		}
	}
	if len(controller.viewGames) == 0 {
		controller.cursor = 0
		return
	}
	if selectedIndex >= len(controller.viewGames) {
		selectedIndex = len(controller.viewGames) - 1
	}
	if selectedIndex < 0 {
		selectedIndex = 0
	}
	controller.cursor = selectedIndex
}

func (controller *CatalogController) IsBusy() bool { return controller.cacheBuilding.Load() }

func (controller *CatalogController) buildCache() {
	if !controller.cacheBuilding.CompareAndSwap(false, true) {
		return
	}
	defer controller.cacheBuilding.Store(false)
	logger.Info("cache: starting background full fetch")
	games, err := controller.client.FetchAllGames(context.Background(), func(partial []itchio.Game) {
		snapshot := append([]itchio.Game(nil), partial...)
		select {
		case controller.cacheUpdateCh <- snapshot:
		default:
		}
		controller.wakeUI()
	})
	if err != nil {
		logger.Error("cache: full fetch failed after %d games: %v", len(games), err)
		return
	}
	if err := itchio.SaveGamesCache(controller.cachePath, games); err != nil {
		logger.Error("cache: save failed: %v", err)
		return
	}
	logger.Info("cache: saved %d games to %s", len(games), controller.cachePath)
	controller.ApplyCatCache(games)
}

func (controller *CatalogController) refreshCacheIfStale(fetchedAt time.Time) {
	age := time.Since(fetchedAt)
	if age < cacheTTL {
		logger.Debug("cache: fresh (age=%v), skipping background refresh", age.Round(time.Second))
		return
	}
	logger.Info("cache: stale (age=%v), refreshing in background", age.Round(time.Second))
	controller.buildCache()
}
