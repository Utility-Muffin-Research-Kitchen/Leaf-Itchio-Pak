//go:build !headless

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/catui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/power"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/renderer"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/theme"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/ui"
	"github.com/veandco/go-sdl2/sdl"
)

const (
	userEventInventoryUpdate = int32(0) // UpdateService finished a check
	userEventPowerSleep      = int32(1) // power: short press
	userEventPowerShutdown   = int32(2) // power: long press
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
	systemDirs := make(map[string]string, 6)
	for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8"} {
		dir, resolveErr := catalog.ROMDir(primary, id)
		if resolveErr != nil {
			logger.Error("leaf systems: %v", resolveErr)
			os.Exit(1)
		}
		systemDirs[id] = dir
	}
	sourcePaths := make([]roms.SourcePathConfig, 0, len(runtimeEnv.Sources))
	for _, source := range runtimeEnv.Sources {
		dirs := make(map[string]string, 6)
		for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8"} {
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
	if os.Getenv("ITCHIO_CAT_LIVE_LIST") == "1" {
		if err := runCatLiveList(client, cfg, cfgPath, cachePath, ownedCachePath, inv, inventoryPath,
			runtimeEnv.Sources, catalog); err != nil {
			logger.Error("Catastrophe live main list: %v", err)
			os.Exit(1)
		}
		return
	}

	level := cfg.LogLevel
	if level == "" {
		level = "info"
	}
	logger.Info("log_level:  %s", level)

	// Pre-init SDL2 to detect display resolution before creating the window.
	// Include JOYSTICK + GAMECONTROLLER so the device's physical buttons are
	// delivered as ControllerButtonEvents (the device SDL2 has built-in
	// mappings for TrimUI/Miyoo hardware). renderer.New will call sdl.Init
	// again — that is idempotent.
	if err := sdl.Init(sdl.INIT_VIDEO | sdl.INIT_JOYSTICK | sdl.INIT_GAMECONTROLLER); err != nil {
		logger.Error("sdl pre-init: %v", err)
		os.Exit(1)
	}

	// Open all connected game controllers so button events are delivered.
	for i := 0; i < sdl.NumJoysticks(); i++ {
		if sdl.IsGameController(i) {
			if gc := sdl.GameControllerOpen(i); gc != nil {
				defer gc.Close()
			}
		} else {
			if js := sdl.JoystickOpen(i); js != nil {
				defer js.Close()
			}
		}
	}

	w, h := int32(1024), int32(768) // sensible default for TrimUI Brick
	if dm, err := sdl.GetCurrentDisplayMode(0); err == nil {
		w, h = dm.W, dm.H
	}
	logger.Info("display: %dx%d", w, h)

	// Leaf appearance will be inherited through Catastrophe. Until that bridge
	// lands, keep the default legacy palette without reading NextUI settings.
	nextUITheme, themeAvailable := theme.Defaults(), false
	defaultTheme := theme.Defaults()

	activeTheme := defaultTheme
	if cfg.NextUITheme && themeAvailable {
		activeTheme = nextUITheme
	}
	logger.Info("theme: available=%v, active=%v", themeAvailable, cfg.NextUITheme && themeAvailable)

	r, err := renderer.New("Itch.io", int(w), int(h), activeTheme)
	if err != nil {
		logger.Error("renderer init: %v", err)
		os.Exit(1)
	}
	defer r.Close()

	onThemeToggle := func(enabled bool) {
		if enabled && themeAvailable {
			r.Theme = nextUITheme
		} else {
			r.Theme = defaultTheme
		}
		logger.Debug("renderer: theme updated (NextUI active: %v)", enabled && themeAvailable)
	}

	cache := renderer.NewImageCache(50, client.HTTPClient())
	defer cache.Clear()
	cache.SetNotify(func() {
		sdl.PushEvent(&sdl.UserEvent{Type: sdl.USEREVENT, Code: -1})
	})

	updateSvc := inventory.NewUpdateService(inv, inventoryPath, client, func() {
		sdl.PushEvent(&sdl.UserEvent{Type: sdl.USEREVENT, Code: userEventInventoryUpdate})
	})
	updateSvc.Start(nil)
	defer updateSvc.Stop()

	powerMgr := power.NewManager(func(action power.Action) {
		code := userEventPowerSleep
		if action == power.ActionShutdown {
			code = userEventPowerShutdown
		}
		sdl.PushEvent(&sdl.UserEvent{Type: sdl.USEREVENT, Code: code})
	})
	powerMgr.Start()

	listScreen := ui.NewListScreen(client, cfg, cfgPath, cache, cachePath, inv, inventoryPath, updateSvc, nextUITheme, defaultTheme, themeAvailable, onThemeToggle, ownedCachePath)
	var current ui.Screen
	if devScreen := os.Getenv("DEV_START_SCREEN"); devScreen != "" {
		logger.Info("dev: DEV_START_SCREEN=%q", devScreen)
		current = ui.NewDevStartScreen(devScreen, listScreen, client, cfg, cfgPath, cache, inv, inventoryPath, updateSvc, nextUITheme, defaultTheme, themeAvailable, onThemeToggle)
	} else {
		current = listScreen
	}

	// pendingQuit and pendingAction are set together; only read when pendingQuit is true.
	var (
		pendingQuit   bool
		pendingAction = power.ActionSleep
	)

	platform := readPlatform()
	if platform == "my355" {
		const joyTypePath = "/sys/class/miyooio_chr_dev/joy_type"
		logger.Debug("input: checking for my355 joy_type workaround at %s", joyTypePath)
		if _, err := os.Stat(joyTypePath); err == nil {
			logger.Info("input: applying my355 joy_type workaround (-1)")
			if err := os.WriteFile(joyTypePath, []byte("-1"), 0644); err != nil {
				logger.Error("input: failed to apply joy_type workaround: %v", err)
			}
			defer func() {
				logger.Info("input: restoring my355 joy_type (0)")
				if err := os.WriteFile(joyTypePath, []byte("0"), 0644); err != nil {
					logger.Error("input: failed to restore joy_type: %v", err)
				}
			}()
		}
	}

loop:
	for current != nil {
		// Upload any images that background goroutines finished fetching.
		// Returns true if at least one texture was uploaded this call.
		newImages := cache.ProcessPending(r)

		// Block until an SDL event arrives.
		// Four modes:
		//   16ms  — screen needs continuous redraws (download progress, spinners)
		//   poll  — textures just uploaded; draw them before blocking again
		//  500ms  — screen is static but has a pending timed animation (e.g. title
		//           scroll delay). This guarantees the loop wakes before the
		//           animation window opens even if no other events fire.
		//  ∞      — truly idle: no redraws needed, image-cache notify and user
		//           input are the only expected wakeups.
		//
		// The poll case fixes a race where ProcessPending uploads a texture in
		// iteration N+1 (after the notify UserEvent woke iteration N's WaitEvent),
		// but then WaitEvent blocks indefinitely because no further event arrives.
		gotEvent := false
		var e sdl.Event
		if current.NeedsRedraw() {
			e = sdl.WaitEventTimeout(16)
		} else if newImages {
			e = sdl.PollEvent()
		} else if current.HasPendingAnimation() {
			e = sdl.WaitEventTimeout(500)
		} else {
			e = sdl.WaitEvent()
		}
		for e != nil {
			gotEvent = true
			if pendingQuit {
				e = sdl.PollEvent()
				continue // drain input while waiting for tasks
			}
			// Intercept SDL_QUIT (SIGTERM from NextUI) before screens see it.
			if _, ok := e.(*sdl.QuitEvent); ok {
				current = nil
				break loop
			}
			// Intercept UserEvents before screens see them.
			if uev, ok := e.(*sdl.UserEvent); ok {
				switch uev.Code {
				case userEventInventoryUpdate:
					// Update-svc finished a check; rebuild the list view so
					// new [UP]/[!] badges and DL-sort order are immediately visible.
					listScreen.ScheduleRebuild()
					// Fall through — do NOT continue. FetchUploadsScreen also uses
					// UserEvent code 0 for its goroutine-done signal, so the event
					// must still reach current.HandleEvent(e).
				case userEventPowerSleep:
					logger.Info("power: sleep requested, waiting for tasks")
					pendingQuit = true
					pendingAction = power.ActionSleep
					updateSvc.Stop()
					e = sdl.PollEvent()
					continue
				case userEventPowerShutdown:
					logger.Info("power: shutdown requested, waiting for tasks")
					pendingQuit = true
					pendingAction = power.ActionShutdown
					updateSvc.Stop()
					e = sdl.PollEvent()
					continue
				}
			}
			current = current.HandleEvent(e)
			if current == nil {
				break loop
			}
			e = sdl.PollEvent()
		}
		if current == nil {
			break loop
		}
		if pendingQuit {
			var busy bool
			if bc, ok := current.(ui.BusyChecker); ok {
				busy = bc.IsBusy()
			}
			if !busy && !updateSvc.IsRunning() {
				if pendingAction == power.ActionShutdown {
					logger.Info("power: all tasks done, writing /tmp/poweroff")
					if err := os.WriteFile("/tmp/poweroff", []byte{}, 0644); err != nil {
						logger.Error("power: /tmp/poweroff: %v", err)
					}
					break loop // exit cleanly; NextUI detects /tmp/poweroff and shuts down
				}
				suspendPath := filepath.Join(os.Getenv("SYSTEM_PATH"), "bin", "suspend")
				if _, err := os.Stat(suspendPath); err != nil {
					logger.Warn("power: suspend script not found at %s, exiting instead", suspendPath)
					current = nil
				} else {
					logger.Info("power: all tasks done, calling %s", suspendPath)
					if err := exec.Command(suspendPath).Run(); err != nil {
						logger.Error("power: suspend: %v", err)
					}
					logger.Info("power: resumed from sleep")
					powerMgr.PostWake()
					// Flush any power UserEvents the goroutine queued while
					// processing the wake-up key press. They arrived before
					// suspend.Run() returned, so PostWake() alone is too late.
					for e := sdl.PollEvent(); e != nil; e = sdl.PollEvent() {
						if uev, ok := e.(*sdl.UserEvent); ok &&
							(uev.Code == userEventPowerSleep || uev.Code == userEventPowerShutdown) {
							logger.Info("power: discarding buffered wake-up event")
							continue
						}
						current = current.HandleEvent(e)
						if current == nil {
							break loop
						}
					}
					pendingQuit = false
				}
			} else {
				drawPowerPendingOverlay(r, pendingAction)
			}
		} else if gotEvent || newImages || current.NeedsRedraw() {
			current.Draw(r)
		}
	}
}

func runCatLiveList(client *itchio.Client, cfg *settings.Config, cfgPath, cachePath, ownedCachePath string,
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

	imageCache := catui.NewImageCache(50, client.HTTPClient())
	defer imageCache.Clear()
	imageCache.SetNotify(func() { _ = ctx.Wake() })
	legacyTheme := theme.Defaults()
	list := ui.NewListScreen(client, cfg, cfgPath, nil, cachePath, inv, inventoryPath,
		nil, legacyTheme, legacyTheme, false, nil, ownedCachePath)
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
		catRouteDestination
		catRouteDownloadProgress
		catRouteManage
		catRouteRename
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
	var destinationModel *appui.DestinationModel
	var destinationScreen *catui.DestinationScreen
	var destinationFlow *ui.CatDestinationFlow
	var destinationPlan *ui.CatDownloadPlan
	var manageModel *appui.ManageModel
	var manageScreen *catui.ManageScreen
	var manageFlow *ui.CatManageFlow
	var renameModel *appui.RenameModel
	var renameScreen *catui.RenameScreen
	var renameFlow *ui.CatRenameFlow
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
	startDownloadPlan := func(plan *ui.CatDownloadPlan) error {
		if plan == nil {
			return nil
		}
		switch plan.Kind {
		case ui.CatDownloadPlanArchive:
			downloadSelectModel.SetHandoff("ZIP/7z inspection must classify ROM and music contents before writing files. That Cat route is scheduled with the archive/destination slice.")
			return nil
		case ui.CatDownloadPlanDestination:
			var flowErr error
			destinationFlow, destinationModel, flowErr = ui.NewCatROMDestinationFlow(
				sources, catalog, cfg, cfgPath, activeGame.Title, plan.Uploads)
			if flowErr != nil {
				downloadSelectModel.SetError(flowErr.Error())
				return nil
			}
			destinationScreen, flowErr = catui.NewDestinationScreen(ctx, destinationModel)
			if flowErr != nil {
				return flowErr
			}
			destinationPlan = plan
			route = catRouteDestination
			return nil
		case ui.CatDownloadPlanDirect:
			downloadBackend = ui.NewCatDirectDownloadBackend(client, cfg, activeGame, activeDetail,
				plan.Uploads[0], plan.DestPaths[0], inv, inventoryPath)
		case ui.CatDownloadPlanMulti:
			downloadBackend = ui.NewCatMultiDownloadBackend(client, cfg, activeGame, activeDetail,
				plan.Uploads, plan.DestPaths, inv, inventoryPath)
		default:
			return nil
		}
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
	drawCurrent := func() error {
		switch route {
		case catRouteFilter:
			imageCache.BeginFrame()
			return filterScreen.Draw()
		case catRouteDetail:
			return detailScreen.Draw()
		case catRouteDownloadSelect:
			return downloadSelectScreen.Draw()
		case catRouteDestination:
			return destinationScreen.Draw()
		case catRouteDownloadProgress:
			return downloadProgressScreen.Draw()
		case catRouteManage:
			return manageScreen.Draw()
		case catRouteRename:
			return renameScreen.Draw()
		default:
			return screen.Draw()
		}
	}

	running, redraw := true, true
	targetFrames, _ := strconv.Atoi(os.Getenv("ITCHIO_CAT_LIVE_LIST_FRAMES"))
	screenshotPath := os.Getenv("ITCHIO_CAT_LIVE_LIST_SCREENSHOT")
	drawn := 0
	for running {
		// cat_present() blocks on the raw evdev wake fd. A release or noisy
		// analog sample can wake it without producing a normalized app event.
		// SDL does not preserve backbuffer contents across RenderPresent, so a
		// second present without a complete draw can flash an undefined frame.
		// Always rebuild one complete frame after every wake before presenting.
		redraw = true
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
		if route == catRouteDownloadProgress && downloadBackend != nil {
			snapshot := downloadBackend.CatSnapshot()
			*downloadProgressModel = snapshot
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
					logger.Debug("cat detail: settings destination is scheduled for a later slice")
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
					}
				case appui.RenameIntentSkip:
					if flowErr := renameFlow.Skip(renameModel); flowErr != nil {
						renameModel.SetError(flowErr.Error())
					}
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
				case appui.ListIntentFilter:
					platform, sort, query := list.CatFilter()
					filterModel = appui.NewFilterModel(platform, sort, query)
					filterScreen, err = catui.NewFilterScreen(ctx, filterModel)
					if err != nil {
						return err
					}
					route = catRouteFilter
				case appui.ListIntentOpen:
					game, ok := list.CatSelected(model.Cursor)
					if !ok {
						break
					}
					activeGame, activeDetail = game, nil
					detailModel = appui.NewDetailModel(appui.DetailGame{
						Title: game.Title, Author: game.Author, URL: game.URL, Platform: game.Platform,
						Price: game.Price, IsFree: game.IsFree, Downloaded: inv.IsPresent(game.URL),
						CanDownload: game.IsFree || cfg.APIKey != "",
					})
					detailScreen, err = catui.NewDetailScreen(ctx, detailModel, imageCache)
					if err != nil {
						return err
					}
					detailLoader = ui.NewCatDetailLoader(client, cfg, game, func() { _ = ctx.Wake() })
					route = catRouteDetail
				case appui.ListIntentSettings:
					logger.Debug("cat live list: settings destination is scheduled for a later slice")
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
		if route == catRouteDownloadProgress && downloadBackend != nil {
			snapshot := downloadBackend.CatSnapshot()
			*downloadProgressModel = snapshot
			redraw = true
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
		} else if route == catRouteDownloadProgress && downloadProgressModel != nil && downloadProgressModel.State == appui.DownloadProgressRunning {
			ctx.RequestFrameIn(50)
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

func drawPowerPendingOverlay(r *renderer.Renderer, action power.Action) {
	r.Clear(20, 20, 20)
	subtitle := "Finishing up before sleep…"
	if action == power.ActionShutdown {
		subtitle = "Finishing up before shutdown…"
	}
	_, mainH := r.TextSize("Ag")
	mid := r.H / 2
	r.DrawTextCentered("Please wait", 0, mid-mainH-6, r.W, 220, 220, 220)
	r.DrawSmallTextCentered(subtitle, 0, mid+6, r.W, 120, 120, 120)
	r.Present()
}
