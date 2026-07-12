//go:build !headless

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/catui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/power"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/ui"
)

func runSDL() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if os.Getenv("ITCHIO_CAT_FIXTURES") == "1" {
		page, _ := strconv.Atoi(os.Getenv("ITCHIO_CAT_FIXTURE_PAGE"))
		frames, _ := strconv.Atoi(os.Getenv("ITCHIO_CAT_FIXTURE_FRAMES"))
		if err := catui.RunFixtures(catui.FixtureConfig{
			Page:           page,
			Frames:         frames,
			ScreenshotPath: os.Getenv("ITCHIO_CAT_FIXTURE_SCREENSHOT"),
		}); err != nil {
			logger.Error("Catastrophe fixtures: %v", err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("ITCHIO_CAT_MAIN_LIST") == "1" {
		frames, _ := strconv.Atoi(os.Getenv("ITCHIO_CAT_MAIN_LIST_FRAMES"))
		if err := catui.RunMainListFixture(catui.MainListFixtureConfig{
			State:          os.Getenv("ITCHIO_CAT_MAIN_LIST_STATE"),
			Frames:         frames,
			ScreenshotPath: os.Getenv("ITCHIO_CAT_MAIN_LIST_SCREENSHOT"),
		}); err != nil {
			logger.Error("Catastrophe main-list slice: %v", err)
			os.Exit(1)
		}
		return
	}
	if inputFixture := os.Getenv("ITCHIO_CAT_INPUT"); inputFixture != "" {
		frames, _ := strconv.Atoi(os.Getenv("ITCHIO_CAT_INPUT_FRAMES"))
		if err := catui.RunInputFixture(catui.InputFixtureConfig{
			Screen:         inputFixture,
			Frames:         frames,
			ScreenshotPath: os.Getenv("ITCHIO_CAT_INPUT_SCREENSHOT"),
		}); err != nil {
			logger.Error("Catastrophe input slice: %v", err)
			os.Exit(1)
		}
		return
	}
	runtimeEnv, err := leaf.LoadEnvironment()
	if err != nil {
		logger.Error("leaf runtime: %v", err)
		os.Exit(1)
	}
	if err := leaf.ConfigureDaemon(runtimeEnv); err != nil {
		logger.Warn("leaf daemon: %v", err)
	}
	if err := runtimeEnv.EnsureAppDirs(); err != nil {
		logger.Error("leaf runtime: %v", err)
		os.Exit(1)
	}
	catalog, err := leaf.LoadCatalog(runtimeEnv)
	if err != nil {
		logger.Error("leaf systems: %v", err)
		os.Exit(1)
	}
	primary, ok := runtimeEnv.Sources.Primary()
	if !ok {
		logger.Error("leaf runtime: no primary content source")
		os.Exit(1)
	}
	systemDirs := make(map[string]string, 7)
	for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"} {
		dir, resolveErr := catalog.ROMDir(primary, id)
		if resolveErr != nil {
			logger.Error("leaf systems: %v", resolveErr)
			os.Exit(1)
		}
		systemDirs[id] = dir
	}
	sourcePaths := make([]roms.SourcePathConfig, 0, len(runtimeEnv.Sources))
	for _, source := range runtimeEnv.Sources {
		dirs := make(map[string]string, 7)
		for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"} {
			dir, resolveErr := catalog.ROMDir(source, id)
			if resolveErr != nil {
				logger.Error("leaf systems: source %s: %v", source.ID, resolveErr)
				os.Exit(1)
			}
			dirs[id] = dir
		}
		sourcePaths = append(sourcePaths, roms.SourcePathConfig{
			SourceID: source.ID, Root: source.Root, MusicRoot: source.MusicPath,
			StatesRoot: source.StatesPath, SystemDirs: dirs,
		})
	}
	if err := roms.ConfigurePaths(roms.PathConfig{
		SystemDirs:  systemDirs,
		SourceID:    primary.ID,
		PrimaryRoot: primary.Root,
		MusicRoot:   primary.MusicPath,
		StatesRoot:  primary.StatesPath,
		Sources:     sourcePaths,
	}); err != nil {
		logger.Error("leaf destinations: %v", err)
		os.Exit(1)
	}

	cfgPath := filepath.Join(runtimeEnv.StateDir(), "config.json")
	cachePath := filepath.Join(filepath.Dir(cfgPath), "games_cache.json")
	ownedCachePath := filepath.Join(filepath.Dir(cfgPath), "owned_cache.json")
	cfg, _ := settings.Load(cfgPath)

	// Apply log level and register the API key for redaction before anything
	// else is logged. LOG_LEVEL env var overrides the config value so the
	// dev-screenshot script can force debug logging without editing config.
	logger.SetLevel(logger.LevelFromString(cfg.LogLevel))
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		logger.SetLevel(logger.LevelFromString(envLevel))
	}
	logger.RegisterSecret(cfg.APIKey, "[API-KEY]")

	inventoryPath := filepath.Join(filepath.Dir(cfgPath), "inventory.json")
	inv, inventoryErr := inventory.Load(inventoryPath)
	if inventoryErr != nil {
		logger.Warn("inventory reset: %v", inventoryErr)
	}
	inv.VerifyAndCleanWithSources(inventoryPath, runtimeEnv.Sources)
	client := itchio.NewClient()
	if err := runCatApp(client, cfg, cfgPath, cachePath, ownedCachePath, inv, inventoryPath,
		runtimeEnv.Sources, catalog); err != nil {
		logger.Error("Catastrophe app: %v", err)
		os.Exit(1)
	}
}

func runCatApp(client *itchio.Client, cfg *settings.Config, cfgPath, cachePath, ownedCachePath string,
	inv *inventory.Inventory, inventoryPath string, sources leaf.SourceList, catalog *leaf.Catalog) error {
	resourceDir := os.Getenv("ITCHIO_RES_DIR")
	fontPath := os.Getenv("CAT_FONT_PATH")
	if fontPath == "" && resourceDir != "" {
		fontPath = filepath.Join(resourceDir, "font.ttf")
	}
	ctx, err := catui.Init(catui.Config{
		Title:            "Itch.io",
		FontPath:         fontPath,
		FallbackFontsDir: resourceDir,
	})
	if err != nil {
		return err
	}
	defer ctx.Close()
	powerActions := make(chan power.Action, 1)
	powerMgr := power.NewManager(func(action power.Action) {
		select {
		case powerActions <- action:
		default:
		}
		_ = ctx.Wake()
	})
	powerMgr.Start()
	waitSleep, err := catui.NewWaitScreen(ctx, "Itch.io", "Please wait", "Finishing protected work before sleep…")
	if err != nil {
		return err
	}
	waitShutdown, err := catui.NewWaitScreen(ctx, "Itch.io", "Please wait", "Finishing protected work before shutdown…")
	if err != nil {
		return err
	}
	powerPending := false
	pendingPowerAction := power.ActionSleep

	imageCache := catui.NewImageCache(50, client.HTTPClient())
	defer imageCache.Clear()
	imageCache.SetNotify(func() { _ = ctx.Wake() })
	updateSvc := inventory.NewUpdateService(inv, inventoryPath, client, func() { _ = ctx.Wake() })
	updateSvc.SetSources(sources)
	updateSvc.Start(nil)
	defer updateSvc.Stop()
	list := ui.NewCatalogController(client, cfg, cfgPath, cachePath, inv, inventoryPath,
		updateSvc, ownedCachePath)
	list.SetWake(func() { _ = ctx.Wake() })
	model := appui.NewMainListModel(nil)
	model.SetLoading()
	screen, err := catui.NewMainListScreen(ctx, model, imageCache)
	if err != nil {
		return err
	}
	type catRoute uint8
	const (
		catRouteList catRoute = iota
		catRouteFilter
		catRouteDetail
		catRouteDownloadSelect
		catRouteArchiveInspect
		catRouteArchiveContents
		catRouteDestination
		catRouteDownloadProgress
		catRouteManage
		catRouteRename
		catRouteSettings
		catRouteModeration
		catRouteTags
		catRouteAbout
		catRouteCacheRefresh
	)
	route := catRouteList
	var filterModel *appui.FilterModel
	var filterScreen *catui.FilterScreen
	var detailModel *appui.DetailModel
	var detailScreen *catui.DetailScreen
	var detailLoader *ui.CatDetailLoader
	var activeGame itchio.Game
	var activeDetail *itchio.GameDetail
	var downloadSelectModel *appui.DownloadSelectModel
	var downloadSelectScreen *catui.DownloadSelectScreen
	var downloadFlow *ui.CatDownloadFlow
	var downloadProgressModel *appui.DownloadProgressModel
	var downloadProgressScreen *catui.DownloadProgressScreen
	var downloadBackend ui.CatDownloadBackend
	type libraryScanResult struct {
		generation int
		message    string
		err        error
	}
	libraryScanResults := make(chan libraryScanResult, 1)
	downloadGeneration := 0
	downloadScanStarted := false
	downloadLibraryStatus := ""
	type managementScanResult struct {
		manage  *appui.ManageModel
		rename  *appui.RenameModel
		message string
		err     error
	}
	managementScanResults := make(chan managementScanResult, 2)
	managementScansPending := 0
	var archiveFlow *ui.CatArchiveFlow
	var archiveInspectModel *appui.DownloadProgressModel
	var archiveInspectScreen *catui.DownloadProgressScreen
	var archiveContentsModel *appui.DownloadSelectModel
	var archiveContentsScreen *catui.DownloadSelectScreen
	var destinationModel *appui.DestinationModel
	var destinationScreen *catui.DestinationScreen
	var destinationFlow *ui.CatDestinationFlow
	var destinationPlan *ui.CatDownloadPlan
	type destinationPurpose uint8
	const (
		destinationRegular destinationPurpose = iota
		destinationArchiveROM
		destinationArchiveMusic
	)
	var destinationUse destinationPurpose
	var manageModel *appui.ManageModel
	var manageScreen *catui.ManageScreen
	var manageFlow *ui.CatManageFlow
	var renameModel *appui.RenameModel
	var renameScreen *catui.RenameScreen
	var renameFlow *ui.CatRenameFlow
	var settingsModel *appui.SettingsModel
	var settingsScreen *catui.SettingsScreen
	var settingsFlow *ui.CatSettingsFlow
	var settingsReturn catRoute
	var moderationModel *appui.SettingsModel
	var moderationScreen *catui.SettingsScreen
	var moderationFlow *ui.CatModerationFlow
	var moderationReturn catRoute
	var tagModel *appui.SettingsModel
	var tagScreen *catui.SettingsScreen
	var tagFlow *ui.CatTagFlow
	var aboutScreen *catui.AboutScreen
	var cacheRefreshModel *appui.RefreshModel
	var cacheRefreshScreen *catui.RefreshScreen
	var cacheRefreshFlow *ui.CatCacheRefreshFlow
	openDetail := func(index int) error {
		game, ok := list.CatSelected(index)
		if !ok {
			return nil
		}
		activeGame, activeDetail = game, nil
		detailModel = appui.NewDetailModel(appui.DetailGame{
			Title: game.Title, Author: game.Author, URL: game.URL, Platform: game.Platform,
			Price: game.Price, IsFree: game.IsFree, Downloaded: inv.IsPresent(game.URL),
			CanDownload: game.IsFree || cfg.APIKey != "",
		})
		var screenErr error
		detailScreen, screenErr = catui.NewDetailScreen(ctx, detailModel, imageCache)
		if screenErr != nil {
			return screenErr
		}
		detailLoader = ui.NewCatDetailLoader(client, cfg, game, func() { _ = ctx.Wake() })
		route = catRouteDetail
		return nil
	}
	openSettings := func(back catRoute) error {
		settingsReturn = back
		settingsFlow, settingsModel = ui.NewCatSettingsFlow(cfg, cfgPath, ownedCachePath,
			filepath.Dir(cfgPath), sources, client, func() { _ = ctx.Wake() })
		settingsFlow.SetOwnedChanged(list.ReplaceOwnedGames)
		var screenErr error
		settingsScreen, screenErr = catui.NewSettingsScreen(ctx, settingsModel)
		if screenErr != nil {
			return screenErr
		}
		route = catRouteSettings
		return nil
	}
	openModeration := func(back catRoute) error {
		moderationReturn = back
		moderationFlow, moderationModel = ui.NewCatModerationFlow(cfg, cfgPath)
		var screenErr error
		moderationScreen, screenErr = catui.NewSettingsScreen(ctx, moderationModel)
		if screenErr != nil {
			return screenErr
		}
		route = catRouteModeration
		return nil
	}
	handleSettingsAction := func(action ui.CatSettingsAction) error {
		switch action {
		case ui.CatSettingsEditAPIKey:
			value, accepted, keyboardErr := ctx.SecretKeyboard()
			if keyboardErr != nil {
				return keyboardErr
			}
			if accepted {
				if flowErr := settingsFlow.SetAPIKey(settingsModel, value); flowErr != nil {
					settingsModel.SetError(flowErr.Error())
				}
			}
		case ui.CatSettingsClearImages:
			imageCache.Clear()
			settingsModel.SetMessage("Decoded image and GIF frames were cleared. They will be fetched again when needed.")
		case ui.CatSettingsRefreshGames:
			if list.IsBusy() {
				settingsModel.SetMessage("A game-list refresh is already running.")
				break
			}
			cacheRefreshFlow, cacheRefreshModel = ui.NewCatCacheRefreshFlow(client, cachePath, func() { _ = ctx.Wake() })
			var screenErr error
			cacheRefreshScreen, screenErr = catui.NewRefreshScreen(ctx, cacheRefreshModel)
			if screenErr != nil {
				return screenErr
			}
			route = catRouteCacheRefresh
		case ui.CatSettingsUpdateInventory:
			updateSvc.TriggerNow()
			settingsModel.SetMessage("Inventory update started. Local downloads stay available during the check.")
		case ui.CatSettingsModeration:
			return openModeration(catRouteSettings)
		case ui.CatSettingsAbout:
			var screenErr error
			aboutScreen, screenErr = catui.NewAboutScreen(ctx, version, readLeafVersion())
			if screenErr != nil {
				return screenErr
			}
			route = catRouteAbout
		}
		return nil
	}
	openManage := func() error {
		var flowErr error
		manageFlow, manageModel, flowErr = ui.NewCatManageFlow(inv, inventoryPath, activeGame.URL, sources, catalog)
		if flowErr != nil {
			return flowErr
		}
		manageScreen, flowErr = catui.NewManageScreen(ctx, manageModel)
		if flowErr != nil {
			return flowErr
		}
		route = catRouteManage
		return nil
	}
	requestManagementScan := func(manage *appui.ManageModel, rename *appui.RenameModel) {
		const pending = "Requesting Leaf library rescan…"
		if manage != nil {
			manage.SetLibraryStatus(pending)
		}
		if rename != nil {
			rename.SetLibraryStatus(pending)
		}
		managementScansPending++
		go func() {
			requestCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			message, scanErr := leaf.RequestLibraryScan(requestCtx)
			managementScanResults <- managementScanResult{
				manage: manage, rename: rename, message: message, err: scanErr,
			}
			_ = ctx.Wake()
		}()
	}
	syncManagementScans := func() bool {
		changed := false
		for {
			select {
			case result := <-managementScanResults:
				if managementScansPending > 0 {
					managementScansPending--
				}
				status := "Leaf library rescan requested."
				if result.err != nil {
					logger.Warn("manage: automatic library rescan failed: %v", result.err)
					status = "Files changed · automatic rescan failed; use Rescan in Leaf."
				} else if strings.Contains(strings.ToLower(result.message), "queued") {
					logger.Info("manage: Leaf library rescan queued")
					status = "Leaf library rescan queued."
				} else {
					logger.Info("manage: Leaf library rescan requested")
				}
				if result.manage != nil {
					result.manage.SetLibraryStatus(status)
				}
				if result.rename != nil {
					result.rename.SetLibraryStatus(status)
				}
				changed = true
			default:
				return changed
			}
		}
	}
	startBackend := func(backend ui.CatDownloadBackend) error {
		downloadBackend = backend
		downloadGeneration++
		downloadScanStarted = false
		downloadLibraryStatus = ""
		snapshot := downloadBackend.CatSnapshot()
		downloadProgressModel = &snapshot
		var screenErr error
		downloadProgressScreen, screenErr = catui.NewDownloadProgressScreen(ctx, downloadProgressModel)
		if screenErr != nil {
			return screenErr
		}
		route = catRouteDownloadProgress
		return nil
	}
	syncDownloadProgress := func() {
		if route != catRouteDownloadProgress || downloadBackend == nil || downloadProgressModel == nil {
			return
		}
		snapshot := downloadBackend.CatSnapshot()
		if snapshot.State == appui.DownloadProgressDone && downloadBackend.CatNeedsLibraryScan() && !downloadScanStarted {
			downloadScanStarted = true
			downloadLibraryStatus = "Requesting Leaf library rescan…"
			generation := downloadGeneration
			go func() {
				requestCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				message, scanErr := leaf.RequestLibraryScan(requestCtx)
				libraryScanResults <- libraryScanResult{generation: generation, message: message, err: scanErr}
				_ = ctx.Wake()
			}()
		}
		for {
			select {
			case result := <-libraryScanResults:
				if result.generation != downloadGeneration {
					continue
				}
				if result.err != nil {
					logger.Warn("download: automatic library rescan failed: %v", result.err)
					downloadLibraryStatus = "ROM installed · automatic rescan failed; use Rescan in Leaf."
				} else if strings.Contains(strings.ToLower(result.message), "queued") {
					logger.Info("download: Leaf library rescan queued")
					downloadLibraryStatus = "Leaf library rescan queued."
				} else {
					logger.Info("download: Leaf library rescan requested")
					downloadLibraryStatus = "Leaf library rescan requested."
				}
			default:
				snapshot.LibraryStatus = downloadLibraryStatus
				*downloadProgressModel = snapshot
				return
			}
		}
	}
	var startDownloadPlan func(*ui.CatDownloadPlan) error
	var handleArchiveAction func(ui.CatArchiveAction) error
	startDownloadPlan = func(plan *ui.CatDownloadPlan) error {
		if plan == nil {
			return nil
		}
		if plan.Kind == ui.CatDownloadPlanDirect || plan.Kind == ui.CatDownloadPlanMulti {
			sealed, sealErr := plan.Seal(activeGame, activeDetail)
			if sealErr != nil {
				if downloadSelectModel != nil {
					downloadSelectModel.SetError(sealErr.Error())
					route = catRouteDownloadSelect
					return nil
				}
				return sealErr
			}
			plan = sealed
		}
		switch plan.Kind {
		case ui.CatDownloadPlanArchive:
			archiveFlow = ui.NewCatArchiveFlow(client, cfg, activeGame, plan.Uploads[0], inv,
				func() { _ = ctx.Wake() })
			snapshot := archiveFlow.Snapshot()
			archiveInspectModel = &snapshot
			var screenErr error
			archiveInspectScreen, screenErr = catui.NewDownloadProgressScreen(ctx, archiveInspectModel)
			if screenErr != nil {
				return screenErr
			}
			route = catRouteArchiveInspect
			return nil
		case ui.CatDownloadPlanDestination:
			var flowErr error
			if len(plan.LogicalExts) > 0 {
				destinationFlow, destinationModel, flowErr = ui.NewCatLogicalROMDestinationFlow(
					sources, catalog, cfg, cfgPath, activeGame.Title, plan.Uploads, plan.LogicalExts)
			} else {
				destinationFlow, destinationModel, flowErr = ui.NewCatROMDestinationFlow(
					sources, catalog, cfg, cfgPath, activeGame.Title, plan.Uploads)
			}
			if flowErr != nil {
				downloadSelectModel.SetError(flowErr.Error())
				return nil
			}
			destinationScreen, flowErr = catui.NewDestinationScreen(ctx, destinationModel)
			if flowErr != nil {
				return flowErr
			}
			destinationPlan = plan
			destinationUse = destinationRegular
			route = catRouteDestination
			return nil
		case ui.CatDownloadPlanDirect:
			file := plan.Transaction.Files[0]
			return startBackend(ui.NewCatDirectDownloadBackend(client, cfg, activeGame, activeDetail,
				file.Upload, file.FinalPath, inv, inventoryPath))
		case ui.CatDownloadPlanMulti:
			uploads := make([]roms.Upload, 0, len(plan.Transaction.Files))
			destPaths := make([]string, 0, len(plan.Transaction.Files))
			for _, file := range plan.Transaction.Files {
				uploads = append(uploads, file.Upload)
				destPaths = append(destPaths, file.FinalPath)
			}
			return startBackend(ui.NewCatMultiDownloadBackend(client, cfg, activeGame, activeDetail,
				uploads, destPaths, inv, inventoryPath))
		default:
			return nil
		}
	}
	handleArchiveAction = func(action ui.CatArchiveAction) error {
		switch action {
		case ui.CatArchiveChooseContents:
			archiveContentsModel = appui.NewDownloadSelectModel(activeGame.Title)
			archiveFlow.PrepareChoices(archiveContentsModel)
			var screenErr error
			archiveContentsScreen, screenErr = catui.NewDownloadSelectScreen(ctx, archiveContentsModel)
			if screenErr != nil {
				return screenErr
			}
			route = catRouteArchiveContents
		case ui.CatArchiveChooseROMDestination:
			var flowErr error
			destinationFlow, destinationModel, flowErr = ui.NewCatArchiveROMDestinationFlow(
				sources, catalog, cfg, cfgPath, activeGame.Title, archiveFlow.ROMExtensions())
			if flowErr != nil {
				archiveInspectModel.State = appui.DownloadProgressError
				archiveInspectModel.Detail = flowErr.Error()
				route = catRouteArchiveInspect
				return nil
			}
			destinationScreen, flowErr = catui.NewDestinationScreen(ctx, destinationModel)
			if flowErr != nil {
				return flowErr
			}
			destinationUse = destinationArchiveROM
			route = catRouteDestination
		case ui.CatArchiveChooseMusicDestination:
			var flowErr error
			destinationFlow, destinationModel, flowErr = ui.NewCatMusicDestinationFlow(
				sources, cfg, cfgPath, activeGame.Title)
			if flowErr != nil {
				archiveInspectModel.State = appui.DownloadProgressError
				archiveInspectModel.Detail = flowErr.Error()
				route = catRouteArchiveInspect
				return nil
			}
			destinationScreen, flowErr = catui.NewDestinationScreen(ctx, destinationModel)
			if flowErr != nil {
				return flowErr
			}
			destinationUse = destinationArchiveMusic
			route = catRouteDestination
		case ui.CatArchiveStartDirect:
			return startDownloadPlan(archiveFlow.TakeDirectPlan())
		case ui.CatArchiveStartExtraction:
			return startBackend(ui.NewCatArchiveDownloadBackend(client, cfg, activeGame, activeDetail,
				archiveFlow.ExtractionPlan(), inv, inventoryPath))
		}
		return nil
	}
	devDetailPending := os.Getenv("DEV_START_SCREEN") == "detail"
	if devScreen := os.Getenv("DEV_START_SCREEN"); devScreen != "" {
		logger.Info("dev: DEV_START_SCREEN=%q through Catastrophe graph", devScreen)
		if devScreen == "settings" {
			if err := openSettings(catRouteList); err != nil {
				return err
			}
		}
	}
	drawCurrent := func() error {
		if powerPending {
			if pendingPowerAction == power.ActionShutdown {
				return waitShutdown.Draw()
			}
			return waitSleep.Draw()
		}
		switch route {
		case catRouteFilter:
			imageCache.BeginFrame()
			return filterScreen.Draw()
		case catRouteDetail:
			return detailScreen.Draw()
		case catRouteDownloadSelect:
			return downloadSelectScreen.Draw()
		case catRouteArchiveInspect:
			return archiveInspectScreen.Draw()
		case catRouteArchiveContents:
			return archiveContentsScreen.Draw()
		case catRouteDestination:
			return destinationScreen.Draw()
		case catRouteDownloadProgress:
			return downloadProgressScreen.Draw()
		case catRouteManage:
			return manageScreen.Draw()
		case catRouteRename:
			return renameScreen.Draw()
		case catRouteSettings:
			return settingsScreen.Draw()
		case catRouteModeration:
			return moderationScreen.Draw()
		case catRouteTags:
			return tagScreen.Draw()
		case catRouteAbout:
			return aboutScreen.Draw()
		case catRouteCacheRefresh:
			return cacheRefreshScreen.Draw()
		default:
			return screen.Draw()
		}
	}

	running, redraw := true, true
	targetFrames, _ := strconv.Atoi(os.Getenv("ITCHIO_APP_FRAMES"))
	screenshotPath := os.Getenv("ITCHIO_APP_SCREENSHOT")
	drawn := 0
	for running {
		select {
		case action := <-powerActions:
			if !powerPending || action == power.ActionShutdown {
				powerPending, pendingPowerAction = true, action
				updateSvc.Stop()
				logger.Info("power: waiting for Cat routes before action=%d", action)
			}
		default:
		}
		// cat_present() blocks on the raw evdev wake fd. A release or noisy
		// analog sample can wake it without producing a normalized app event.
		// SDL does not preserve backbuffer contents across RenderPresent, so a
		// second present without a complete draw can flash an undefined frame.
		// Always rebuild one complete frame after every wake before presenting.
		redraw = true
		list.SyncCatModel(model)
		if devDetailPending && route == catRouteList && model.State == appui.ListReady && len(model.Items) > 0 {
			if err := openDetail(0); err != nil {
				return err
			}
			devDetailPending = false
			redraw = true
		}
		if detailLoader != nil && detailModel != nil && detailLoader.Sync(detailModel, cfg) {
			activeDetail = detailLoader.Detail()
			redraw = true
		}
		if downloadFlow != nil && downloadSelectModel != nil && downloadFlow.Sync(downloadSelectModel) {
			if err := startDownloadPlan(downloadFlow.TakePlan()); err != nil {
				return err
			}
			redraw = true
		}
		if route == catRouteArchiveInspect && archiveFlow != nil && archiveFlow.Sync(archiveInspectModel) {
			if err := handleArchiveAction(archiveFlow.TakeAction()); err != nil {
				return err
			}
			redraw = true
		}
		if settingsFlow != nil && settingsModel != nil && settingsFlow.Sync(settingsModel) {
			redraw = true
		}
		if cacheRefreshFlow != nil && cacheRefreshModel != nil {
			if games, changed := cacheRefreshFlow.Sync(cacheRefreshModel); changed {
				if games != nil {
					list.ApplyCatCache(games)
				}
				redraw = true
			}
		}
		if route == catRouteDownloadProgress && downloadBackend != nil {
			syncDownloadProgress()
			redraw = true
		}
		if syncManagementScans() {
			redraw = true
		}
		if uploaded, processErr := imageCache.ProcessPending(ctx); processErr != nil {
			return processErr
		} else if uploaded {
			redraw = true
		}
		for {
			event, ok, pollErr := ctx.PollInput()
			if pollErr != nil {
				return pollErr
			}
			if !ok {
				break
			}
			if event.Wake {
				redraw = true
				continue
			}
			if powerPending {
				continue
			}
			switch route {
			case catRouteFilter:
				switch filterScreen.HandleInput(event) {
				case appui.FilterIntentEditSearch:
					value, accepted, keyboardErr := ctx.Keyboard(filterModel.Query)
					if keyboardErr != nil {
						return keyboardErr
					}
					if accepted {
						filterModel.Query = value
					}
				case appui.FilterIntentApply:
					list.ApplyCatFilter(filterModel.Platform, filterModel.Sort, filterModel.Query)
					route = catRouteList
				case appui.FilterIntentCancel:
					route = catRouteList
				}
			case catRouteDetail:
				switch detailScreen.HandleInput(event) {
				case appui.DetailIntentBack:
					detailScreen.Close()
					detailScreen, detailModel, detailLoader = nil, nil, nil
					route = catRouteList
				case appui.DetailIntentSettings:
					open := openSettings
					if detailModel.State == appui.DetailWarning {
						open = openModeration
					}
					if err := open(catRouteDetail); err != nil {
						return err
					}
				case appui.DetailIntentDownload:
					if activeDetail == nil {
						break
					}
					downloadSelectModel = appui.NewDownloadSelectModel(activeGame.Title)
					downloadSelectModel.SetLoading("Finding available files")
					downloadSelectScreen, err = catui.NewDownloadSelectScreen(ctx, downloadSelectModel)
					if err != nil {
						return err
					}
					downloadFlow = ui.NewCatDownloadFlow(client, cfg, activeGame, activeDetail, inv,
						func() { _ = ctx.Wake() })
					route = catRouteDownloadSelect
				case appui.DetailIntentManage:
					if err := openManage(); err != nil {
						logger.Warn("cat manage: %v", err)
						detailModel.Game.Downloaded = inv.IsPresent(activeGame.URL)
						list.ScheduleRebuild()
					}
				}
			case catRouteDownloadSelect:
				switch downloadSelectScreen.HandleInput(event) {
				case appui.DownloadSelectIntentBack:
					downloadFlow, downloadSelectScreen, downloadSelectModel = nil, nil, nil
					route = catRouteDetail
				case appui.DownloadSelectIntentChoose:
					downloadFlow.Choose(downloadSelectModel)
					if err := startDownloadPlan(downloadFlow.TakePlan()); err != nil {
						return err
					}
				}
			case catRouteArchiveInspect:
				switch archiveInspectScreen.HandleInput(event) {
				case appui.DownloadProgressIntentCancel, appui.DownloadProgressIntentBack:
					archiveFlow, archiveInspectModel, archiveInspectScreen = nil, nil, nil
					downloadFlow, downloadSelectModel, downloadSelectScreen = nil, nil, nil
					route = catRouteDetail
				}
			case catRouteArchiveContents:
				switch archiveContentsScreen.HandleInput(event) {
				case appui.DownloadSelectIntentBack:
					archiveFlow, archiveContentsModel, archiveContentsScreen = nil, nil, nil
					downloadFlow, downloadSelectModel, downloadSelectScreen = nil, nil, nil
					route = catRouteDetail
				case appui.DownloadSelectIntentChoose:
					archiveFlow.Choose(archiveContentsModel)
					if err := handleArchiveAction(archiveFlow.TakeAction()); err != nil {
						return err
					}
				}
			case catRouteDestination:
				switch destinationScreen.HandleInput(event) {
				case appui.DestinationIntentBack:
					if destinationFlow.Back(destinationModel) {
						destinationFlow, destinationModel, destinationScreen, destinationPlan = nil, nil, nil, nil
						downloadFlow, downloadSelectModel, downloadSelectScreen = nil, nil, nil
						route = catRouteDetail
					}
				case appui.DestinationIntentActivate:
					complete, destinationErr := destinationFlow.Activate(destinationModel)
					if destinationErr != nil {
						destinationModel.SetError(destinationErr.Error())
						break
					}
					if complete {
						dirs := destinationFlow.DestPaths()
						if destinationUse == destinationArchiveROM {
							archiveFlow.SetROMDirs(destinationFlow.ArchiveROMDirs())
							destinationFlow, destinationModel, destinationScreen = nil, nil, nil
							if err := handleArchiveAction(archiveFlow.TakeAction()); err != nil {
								return err
							}
							break
						}
						if destinationUse == destinationArchiveMusic {
							if len(dirs) != 1 {
								destinationModel.SetError("Music destination was not resolved.")
								break
							}
							archiveFlow.SetMusicDir(dirs[0])
							destinationFlow, destinationModel, destinationScreen = nil, nil, nil
							if err := handleArchiveAction(archiveFlow.TakeAction()); err != nil {
								return err
							}
							break
						}
						resolved := &ui.CatDownloadPlan{Uploads: append([]roms.Upload(nil), destinationPlan.Uploads...)}
						if len(resolved.Uploads) == 1 {
							resolved.Kind = ui.CatDownloadPlanDirect
						} else {
							resolved.Kind = ui.CatDownloadPlanMulti
						}
						for index, upload := range resolved.Uploads {
							dest := filepath.Join(dirs[index], upload.Filename)
							if existing := inv.ExistingDestPath(activeGame.URL, upload.Filename); existing != "" {
								dest = existing
							}
							resolved.DestPaths = append(resolved.DestPaths, dest)
						}
						destinationFlow, destinationModel, destinationScreen, destinationPlan = nil, nil, nil, nil
						if err := startDownloadPlan(resolved); err != nil {
							return err
						}
					}
				}
			case catRouteDownloadProgress:
				switch downloadProgressScreen.HandleInput(event) {
				case appui.DownloadProgressIntentContinue:
					downloadBackend.CatContinueWithoutProtection()
				case appui.DownloadProgressIntentCancel:
					downloadBackend.CatCancel()
				case appui.DownloadProgressIntentBack:
					list.ScheduleRebuild()
					detailModel.Game.Downloaded = inv.IsPresent(activeGame.URL)
					downloadProgressScreen.Close()
					downloadBackend, downloadProgressModel, downloadProgressScreen = nil, nil, nil
					downloadFlow, downloadSelectModel, downloadSelectScreen = nil, nil, nil
					route = catRouteDetail
				}
			case catRouteManage:
				switch manageScreen.HandleInput(event) {
				case appui.ManageIntentBack:
					if manageFlow.Back(manageModel) {
						manageFlow, manageModel, manageScreen = nil, nil, nil
						entry, present := inv.Lookup(activeGame.URL)
						detailModel.Game.Downloaded = present && len(entry.Files) > 0
						list.ScheduleRebuild()
						route = catRouteDetail
					}
				case appui.ManageIntentCancel:
					manageFlow.Cancel(manageModel)
				case appui.ManageIntentActivate:
					var flowErr error
					renameFlow, renameModel, flowErr = manageFlow.Activate(manageModel)
					if flowErr != nil {
						manageModel.SetError(flowErr.Error())
						break
					}
					if renameFlow != nil {
						renameScreen, flowErr = catui.NewRenameScreen(ctx, renameModel)
						if flowErr != nil {
							return flowErr
						}
						route = catRouteRename
					}
				case appui.ManageIntentConfirm:
					if _, flowErr := manageFlow.Confirm(manageModel); flowErr != nil {
						manageModel.SetError(flowErr.Error())
					} else if manageFlow.TakeLibraryScanRequest() {
						requestManagementScan(manageModel, nil)
					}
					list.ScheduleRebuild()
				}
			case catRouteRename:
				switch renameScreen.HandleInput(event) {
				case appui.RenameIntentBack:
					renameFlow, renameModel, renameScreen = nil, nil, nil
					if err := openManage(); err != nil {
						logger.Warn("cat manage after rename: %v", err)
						manageFlow, manageModel, manageScreen = nil, nil, nil
						detailModel.Game.Downloaded = inv.IsPresent(activeGame.URL)
						route = catRouteDetail
					}
					list.ScheduleRebuild()
				case appui.RenameIntentConfirm:
					if flowErr := renameFlow.Confirm(renameModel); flowErr != nil {
						renameModel.SetError(flowErr.Error())
					} else if renameFlow.TakeLibraryScanRequest() {
						requestManagementScan(nil, renameModel)
					}
				case appui.RenameIntentSkip:
					if flowErr := renameFlow.Skip(renameModel); flowErr != nil {
						renameModel.SetError(flowErr.Error())
					} else if renameFlow.TakeLibraryScanRequest() {
						requestManagementScan(nil, renameModel)
					}
				}
			case catRouteSettings:
				switch settingsScreen.HandleInput(event) {
				case appui.SettingsIntentBack:
					if settingsFlow.Back(settingsModel) {
						settingsFlow, settingsModel, settingsScreen = nil, nil, nil
						route = settingsReturn
					}
				case appui.SettingsIntentCancel:
					settingsFlow.Cancel(settingsModel)
				case appui.SettingsIntentActivate:
					action, flowErr := settingsFlow.Activate(settingsModel)
					if flowErr != nil {
						settingsModel.SetError(flowErr.Error())
					} else if err := handleSettingsAction(action); err != nil {
						return err
					}
				case appui.SettingsIntentConfirm:
					action, flowErr := settingsFlow.Confirm(settingsModel)
					if flowErr != nil {
						settingsModel.SetError(flowErr.Error())
					} else if err := handleSettingsAction(action); err != nil {
						return err
					}
				}
			case catRouteModeration:
				switch moderationScreen.HandleInput(event) {
				case appui.SettingsIntentBack:
					moderationFlow, moderationModel, moderationScreen = nil, nil, nil
					if moderationReturn == catRouteSettings {
						settingsFlow.Refresh(settingsModel)
					}
					route = moderationReturn
				case appui.SettingsIntentActivate:
					var flowErr error
					tagFlow, tagModel, flowErr = moderationFlow.Activate(moderationModel)
					if flowErr != nil {
						moderationModel.SetError(flowErr.Error())
					} else if tagFlow != nil {
						tagScreen, flowErr = catui.NewSettingsScreen(ctx, tagModel)
						if flowErr != nil {
							return flowErr
						}
						route = catRouteTags
					}
				}
			case catRouteTags:
				switch tagScreen.HandleInput(event) {
				case appui.SettingsIntentBack:
					tagFlow, tagModel, tagScreen = nil, nil, nil
					moderationFlow.Refresh(moderationModel)
					route = catRouteModeration
				case appui.SettingsIntentActivate:
					if flowErr := tagFlow.Activate(tagModel); flowErr != nil {
						tagModel.SetError(flowErr.Error())
					}
				}
			case catRouteAbout:
				if aboutScreen.HandleInput(event) {
					aboutScreen.Close()
					aboutScreen = nil
					route = catRouteSettings
				}
			case catRouteCacheRefresh:
				switch cacheRefreshScreen.HandleInput(event) {
				case appui.RefreshIntentCancel:
					cacheRefreshFlow.Cancel()
				case appui.RefreshIntentBack:
					cacheRefreshFlow, cacheRefreshModel, cacheRefreshScreen = nil, nil, nil
					settingsFlow.Refresh(settingsModel)
					route = catRouteSettings
				}
			default:
				switch screen.HandleInput(event) {
				case appui.ListIntentExit:
					running = false
				case appui.ListIntentRetry:
					list.RetryCatLoad()
				case appui.ListIntentPreviousSort:
					list.CycleCatSort(-1)
				case appui.ListIntentNextSort:
					list.CycleCatSort(1)
				case appui.ListIntentPreviousPlatform:
					list.CycleCatPlatform(-1)
				case appui.ListIntentNextPlatform:
					list.CycleCatPlatform(1)
				case appui.ListIntentFilter:
					platform, sort, query := list.CatFilter()
					filterModel = appui.NewFilterModel(platform, sort, query)
					filterScreen, err = catui.NewFilterScreen(ctx, filterModel)
					if err != nil {
						return err
					}
					route = catRouteFilter
				case appui.ListIntentOpen:
					if err := openDetail(model.Cursor); err != nil {
						return err
					}
				case appui.ListIntentSettings:
					if err := openSettings(catRouteList); err != nil {
						return err
					}
				case appui.ListIntentDismissNotice:
					list.DismissNotice(model.Cursor)
				}
			}
			redraw = true
		}
		if !running {
			break
		}
		list.SyncCatModel(model)
		if detailLoader != nil && detailModel != nil && detailLoader.Sync(detailModel, cfg) {
			activeDetail = detailLoader.Detail()
			redraw = true
		}
		if downloadFlow != nil && downloadSelectModel != nil && downloadFlow.Sync(downloadSelectModel) {
			if err := startDownloadPlan(downloadFlow.TakePlan()); err != nil {
				return err
			}
			redraw = true
		}
		if route == catRouteArchiveInspect && archiveFlow != nil && archiveFlow.Sync(archiveInspectModel) {
			if err := handleArchiveAction(archiveFlow.TakeAction()); err != nil {
				return err
			}
			redraw = true
		}
		if settingsFlow != nil && settingsModel != nil && settingsFlow.Sync(settingsModel) {
			redraw = true
		}
		if cacheRefreshFlow != nil && cacheRefreshModel != nil {
			if games, changed := cacheRefreshFlow.Sync(cacheRefreshModel); changed {
				if games != nil {
					list.ApplyCatCache(games)
				}
				redraw = true
			}
		}
		if route == catRouteDownloadProgress && downloadBackend != nil {
			syncDownloadProgress()
			redraw = true
		}
		if powerPending {
			busy := list.IsBusy() || updateSvc.IsRunning()
			busy = busy || detailModel != nil && detailModel.State == appui.DetailLoading
			busy = busy || downloadSelectModel != nil && downloadSelectModel.State == appui.DownloadSelectLoading
			busy = busy || archiveInspectModel != nil && archiveInspectModel.State == appui.DownloadProgressRunning
			busy = busy || downloadProgressModel != nil && downloadProgressModel.State == appui.DownloadProgressRunning
			busy = busy || downloadLibraryStatus == "Requesting Leaf library rescan…"
			busy = busy || managementScansPending > 0
			busy = busy || cacheRefreshFlow != nil && cacheRefreshFlow.Busy()
			busy = busy || settingsFlow != nil && settingsFlow.Busy()
			if !busy {
				if pendingPowerAction == power.ActionShutdown {
					logger.Info("power: Cat routes idle, writing /tmp/poweroff")
					if err := os.WriteFile("/tmp/poweroff", []byte{}, 0o644); err != nil {
						return err
					}
					running = false
					continue
				}
				suspendPath := filepath.Join(os.Getenv("SYSTEM_PATH"), "bin", "suspend")
				if _, err := os.Stat(suspendPath); err != nil {
					logger.Warn("power: suspend script not found at %s, exiting instead", suspendPath)
					running = false
					continue
				}
				logger.Info("power: Cat routes idle, calling %s", suspendPath)
				if err := exec.Command(suspendPath).Run(); err != nil {
					logger.Error("power: suspend: %v", err)
				}
				powerMgr.PostWake()
				for {
					select {
					case <-powerActions:
					default:
						powerPending = false
						redraw = true
						goto powerDrainComplete
					}
				}
			powerDrainComplete:
			}
		}
		if redraw {
			if err := drawCurrent(); err != nil {
				return err
			}
			drawn++
			if targetFrames > 0 && drawn >= targetFrames {
				if screenshotPath != "" {
					if err := ctx.BeginCapture(); err != nil {
						return err
					}
					if err := drawCurrent(); err != nil {
						_ = ctx.EndCapture()
						return err
					}
					if err := ctx.ScreenshotPNG(screenshotPath); err != nil {
						_ = ctx.EndCapture()
						return err
					}
					if err := ctx.EndCapture(); err != nil {
						return err
					}
				}
				ctx.RequestFrame()
				return ctx.Present()
			}
			redraw = false
		}
		if targetFrames > 0 && drawn < targetFrames {
			ctx.RequestFrame()
			redraw = true
		}
		if delay, animated := imageCache.NextFrameIn(); animated {
			milliseconds := delay.Milliseconds()
			if milliseconds < 1 {
				milliseconds = 1
			}
			ctx.RequestFrameIn(uint32(milliseconds))
			redraw = true
		} else if imageCache.Busy() {
			ctx.RequestFrameIn(50)
			redraw = true
		} else if route == catRouteList && model.State == appui.ListLoading {
			ctx.RequestFrameIn(100)
			redraw = true
		} else if route == catRouteDetail && detailModel != nil && detailModel.State == appui.DetailLoading {
			ctx.RequestFrameIn(100)
			redraw = true
		} else if route == catRouteDownloadSelect && downloadSelectModel != nil && downloadSelectModel.State == appui.DownloadSelectLoading {
			ctx.RequestFrameIn(100)
			redraw = true
		} else if route == catRouteArchiveInspect && archiveInspectModel != nil && archiveInspectModel.State == appui.DownloadProgressRunning {
			ctx.RequestFrameIn(100)
			redraw = true
		} else if route == catRouteDownloadProgress && downloadProgressModel != nil && downloadProgressModel.State == appui.DownloadProgressRunning {
			ctx.RequestFrameIn(50)
			redraw = true
		} else if route == catRouteCacheRefresh && cacheRefreshFlow != nil && cacheRefreshFlow.Busy() {
			ctx.RequestFrameIn(100)
			redraw = true
		} else if list.IsBusy() {
			ctx.RequestFrameIn(250)
			redraw = true
		}
		if err := ctx.Present(); err != nil {
			return err
		}
	}
	return nil
}
