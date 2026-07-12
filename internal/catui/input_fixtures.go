package catui

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

type InputFixtureConfig struct {
	Screen         string
	Frames         int
	ScreenshotPath string
}

func RunInputFixture(config InputFixtureConfig) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx, err := Init(Config{
		Title:            "Itch.io input migration",
		FontPath:         os.Getenv("CAT_FONT_PATH"),
		FallbackFontsDir: os.Getenv("ITCHIO_RES_DIR"),
	})
	if err != nil {
		return err
	}
	defer ctx.Close()

	cache := NewImageCache(8, nil)
	defer cache.Clear()
	cache.SetNotify(func() { _ = ctx.Wake() })
	delays := []time.Duration{120 * time.Millisecond, 120 * time.Millisecond}
	if config.Frames == 1 {
		delays = []time.Duration{10 * time.Second, 10 * time.Second}
	}
	if err := cache.Seed(ctx, "fixture://detail-cover", []image.Image{fixtureImage(0), fixtureImage(1)}, delays); err != nil {
		return err
	}
	if err := cache.Seed(ctx, "fixture://detail-shot", []image.Image{fixtureImage(2)}, nil); err != nil {
		return err
	}

	var draw func() error
	var handleIntent func(InputEvent) bool
	var closeScreen func()
	switch config.Screen {
	case "filter", "filter-psx":
		platform := "GBC"
		if config.Screen == "filter-psx" {
			platform = "PSX"
		}
		model := appui.NewFilterModel(platform, "paid", "leaf 葉")
		model.Section = appui.FilterPlatform
		screen, screenErr := NewFilterScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.FilterIntentCancel
		}
	case "detail", "warning":
		model := appui.NewDetailModel(appui.DetailGame{
			Title: "Leafbound 葉", Author: "UMRK fixture", URL: "https://example.itch.io/leafbound",
			Platform: "GBC", IsFree: true, CanDownload: true, Downloaded: config.Screen == "detail",
		})
		model.SetReady(`<h2>A pocket-sized journey</h2><p>Explore a multilingual forest, collect lost seeds, and bring music back to every clearing.</p><ul><li>Controller ready</li><li>Offline after install</li></ul>`,
			[]string{"Game Boy Color", "Adventure", "日本語", "GIF gallery"},
			[]string{"fixture://detail-cover", "fixture://detail-shot"}, false, config.Screen == "warning")
		screen, screenErr := NewDetailScreen(ctx, model, cache)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		closeScreen = screen.Close
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.DetailIntentBack
		}
	case "download-select", "archive-contents":
		model := appui.NewDownloadSelectModel("Leafbound 葉")
		if config.Screen == "archive-contents" {
			model.SetChoices("Choose one .GBC ROM (1/1)", []appui.DownloadChoice{
				{Title: "release/leafbound-v1.gbc", Badge: "GBC"},
				{Title: "release/leafbound-v2.gbc", Badge: "GBC"},
			})
		} else {
			model.SetChoices("Choose file and format", []appui.DownloadChoice{
				{Title: "leafbound.gbc", Badge: "GBC"},
				{Title: "soundtrack-and-game.zip", Badge: "ZIP"},
				{Title: "mystery-download", Badge: "AUTO", FormatOptions: []string{"AUTO", "P8.PNG", "P8", "GBC", "GB"}},
			})
		}
		screen, screenErr := NewDownloadSelectScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.DownloadSelectIntentBack
		}
	case "download-progress", "download-done", "download-error", "download-inhibit", "download-cancelled", "archive-inspect":
		model := &appui.DownloadProgressModel{
			State: appui.DownloadProgressRunning, Title: "Leafbound 葉", Filename: "leafbound.gbc",
			Downloaded: 584 * 1024, Total: 1024 * 1024, FileIndex: 0, FileCount: 2,
		}
		switch config.Screen {
		case "archive-inspect":
			model.Filename = "Inspecting soundtrack-and-game.zip"
			model.Downloaded, model.Total, model.FileCount = 128*1024, 640*1024, 1
		case "download-done":
			model.State = appui.DownloadProgressDone
			model.SavedPaths = []string{"/Roms/GBC/Leafbound.gbc", "/Roms/GBC/Leafbound Bonus.gb"}
			model.LibraryStatus = "Leaf library rescan requested."
		case "download-error":
			model.State = appui.DownloadProgressError
			model.Detail = "The signed download URL expired before the transfer completed. Return to Detail and try again."
		case "download-inhibit":
			model.State = appui.DownloadProgressInhibitBlocked
			model.Detail = "Jawaka is unavailable, so Leaf cannot prevent suspend during this transfer. Continue without protection or cancel."
		case "download-cancelled":
			model.State = appui.DownloadProgressCancelled
			model.Detail = "No partial file was installed."
		}
		screen, screenErr := NewDownloadProgressScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		closeScreen = screen.Close
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.DownloadProgressIntentBack
		}
	case "power-wait":
		screen, screenErr := NewWaitScreen(ctx, "Itch.io", "Please wait", "Finishing protected work before sleep…")
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(InputEvent) bool { return true }
	case "destination-source", "destination-folder", "destination-music", "destination-confirm":
		model := appui.NewDestinationModel("Leafbound 葉")
		switch config.Screen {
		case "destination-source":
			model.SetSources([]appui.DestinationItem{
				{Kind: appui.DestinationItemSource, Label: "Primary SD", Detail: "Available", Enabled: true},
				{Kind: appui.DestinationItemSource, Label: "Secondary SD", Detail: "Available", Enabled: true},
				{Kind: appui.DestinationItemSource, Label: "SD card 3", Detail: "Not mounted", Enabled: false},
			})
		case "destination-folder":
			model.SetFolders("Choose GBC folder (1/2)", "Secondary SD / RPG", []appui.DestinationItem{
				{Kind: appui.DestinationItemSave, Label: "Save here", Detail: "Use this folder", Enabled: true},
				{Kind: appui.DestinationItemUp, Label: "..", Detail: "Parent folder", Enabled: true},
				{Kind: appui.DestinationItemFolder, Label: "Action", Detail: "Folder", Value: "Action", Enabled: true},
				{Kind: appui.DestinationItemFolder, Label: "日本語", Detail: "Folder", Value: "日本語", Enabled: true},
			})
		case "destination-music":
			model.SetFolders("Choose Music folder", "Primary SD / Albums", []appui.DestinationItem{
				{Kind: appui.DestinationItemSave, Label: "Save here", Detail: "Use this folder", Enabled: true},
				{Kind: appui.DestinationItemUp, Label: "..", Detail: "Parent folder", Enabled: true},
				{Kind: appui.DestinationItemFolder, Label: "Game Soundtracks", Detail: "Folder", Value: "Game Soundtracks", Enabled: true},
			})
		case "destination-confirm":
			model.SetConfirm("Confirm download destination", "Secondary SD", []string{
				"Roms/GBC/RPG/Leafbound.gbc",
				"Roms/GB/Leafbound Bonus.gb",
			})
		}
		screen, screenErr := NewDestinationScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.DestinationIntentBack
		}
	case "manage-list", "manage-confirm", "manage-result":
		model := appui.NewManageModel("Leafbound 葉")
		model.SetItems("3 managed files · source-owned paths only", []appui.ManageItem{
			{Kind: appui.ManageItemFile, Label: "Leafbound.gbc", Badge: "ROM", Detail: "Primary SD / Roms/GBC/Leafbound.gbc", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "bonus.gb", Badge: "UNAVAILABLE", Detail: "Secondary SD / Roms/GB/bonus.gb", Enabled: false},
			{Kind: appui.ManageItemFile, Label: "forest-theme.ogg", Badge: "MUSIC", Detail: "Primary SD / Music/Leafbound/forest-theme.ogg", Enabled: true},
			{Kind: appui.ManageItemDeleteROMs, Label: "Delete ROM files", Badge: "2 ROM", Enabled: false},
			{Kind: appui.ManageItemDeleteMusic, Label: "Delete soundtrack", Badge: "1 MUSIC", Enabled: true},
			{Kind: appui.ManageItemDeleteAll, Label: "Delete all downloads", Badge: "3 FILES", Enabled: false},
			{Kind: appui.ManageItemRename, Label: "Use title for Leafbound.gbc", Badge: "RENAME", Enabled: true},
		})
		if config.Screen == "manage-confirm" {
			model.SetConfirm("Delete selected file?", []string{"Leafbound.gbc", "Primary SD / Roms/GBC/Leafbound.gbc"})
		} else if config.Screen == "manage-result" {
			model.SetResult("Deleted 2 managed ROM files.")
			model.SetLibraryStatus("Leaf library rescan queued.")
		}
		screen, screenErr := NewManageScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.ManageIntentBack
		}
	case "rename-saves", "rename-states", "rename-done":
		model := appui.NewRenameModel("Leafbound 葉")
		state, subtitle, heading := appui.RenameConfirmSaves, "Save files", "Rename these save files?"
		lines := []string{"Saves/GBC/leafbound.srm", "→ Saves/GBC/Leafbound 葉.srm"}
		if config.Screen == "rename-states" {
			state, subtitle, heading = appui.RenameConfirmStates, "Save states", "Rename these state files?"
			lines = []string{
				"States/GBC-gambatte/leafbound.state1", "→ States/GBC-gambatte/Leafbound 葉.state1",
				"States/GBC-gambatte/leafbound.state1.png", "→ States/GBC-gambatte/Leafbound 葉.state1.png",
			}
		}
		model.SetPrompt(state, subtitle, heading, lines)
		if config.Screen == "rename-done" {
			model.SetDone("ROM renamed, 1 save, 2 state files.")
			model.SetLibraryStatus("Files changed · automatic rescan failed; use Rescan in Leaf.")
		}
		screen, screenErr := NewRenameScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.RenameIntentBack
		}
	case "settings", "settings-confirm", "moderation", "moderation-tags":
		title, subtitle := "Settings", "Leaf settings · changes save immediately"
		rows := []appui.SettingsRow{
			{Key: appui.SettingsAPIKey, Label: "API Key", Value: "••••7f2a", ActionEnabled: true},
			{Key: appui.SettingsEditAPIKey, Label: "Edit API Key", ActionEnabled: true},
			{Key: appui.SettingsRemoveAPIKey, Label: "Remove API Key", ActionEnabled: true},
			{Key: appui.SettingsROMSelection, Label: "ROM Selection", Value: "ask", ActionEnabled: true},
			{Key: appui.SettingsROMLocation, Label: "ROM Location", Value: "ask", ActionEnabled: true},
			{Key: appui.SettingsMusicDownload, Label: "Music Download", Value: "auto", ActionEnabled: true},
			{Key: appui.SettingsMusicLocation, Label: "Music Location", Value: "ask", ActionEnabled: true},
			{Key: appui.SettingsUnifiedNaming, Label: "Use game title", Value: "On", ActionEnabled: true},
			{Key: appui.SettingsROMDestination, Label: "Remembered ROM folder", Value: "Primary SD + Secondary SD · 3 systems"},
			{Key: appui.SettingsAppData, Label: "App Data", Value: "/.userdata/shared/Itch-io"},
			{Key: appui.SettingsContentModeration, Label: "Content Moderation", Value: ">", ActionEnabled: true},
			{Key: appui.SettingsAbout, Label: "About", Value: ">", ActionEnabled: true},
		}
		if config.Screen == "moderation" {
			title, subtitle = "Content Moderation", "Local advisory filters · creator tagging may be incomplete"
			rows = []appui.SettingsRow{
				{Key: appui.SettingsAdultContent, Label: "Adult Content", Value: "14 blocked >", ActionEnabled: true},
				{Key: appui.SettingsQueerContent, Label: "Queer Content", Value: "Allowed >", ActionEnabled: true},
				{Key: appui.SettingsHeavyThemes, Label: "Heavy Themes", Value: "9 blocked >", ActionEnabled: true},
				{Key: appui.SettingsSubstanceUse, Label: "Substance Use", Value: "Blocked", ActionEnabled: true},
			}
		} else if config.Screen == "moderation-tags" {
			title, subtitle = "Adult Content", "A toggles · category coverage depends on creator tags"
			rows = []appui.SettingsRow{
				{Key: appui.SettingsTagMaster, Label: "All category tags", Value: "Blocked", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Adult", Value: "Blocked", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Erotic", Value: "Allowed", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Mature", Value: "Blocked", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Nudity", Value: "Blocked", ActionEnabled: true},
			}
		}
		model := appui.NewSettingsModel(title)
		model.SetRows(subtitle, rows)
		if config.Screen == "settings-confirm" {
			model.SetConfirm("Store an itch.io API key?", []string{
				"The key is stored in App Data on the SD card.",
				"FAT32 cannot protect it from someone with physical access to the card.",
				"The key is masked in the UI and redacted from logs.",
			})
		}
		screen, screenErr := NewSettingsScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.SettingsIntentBack
		}
	case "about":
		screen, screenErr := NewAboutScreen(ctx, "0.1.0", "fixture")
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		closeScreen = screen.Close
		handleIntent = func(event InputEvent) bool { return !screen.HandleInput(event) }
	case "refresh", "refresh-done":
		model := appui.NewRefreshModel("Refreshing Game List")
		model.Fetched = 184
		if config.Screen == "refresh-done" {
			model.State, model.Total = appui.RefreshDone, 427
		}
		screen, screenErr := NewRefreshScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.RefreshIntentBack
		}
	default:
		return fmt.Errorf("unknown input fixture %q", config.Screen)
	}
	if closeScreen != nil {
		defer closeScreen()
	}
	handle := func(event InputEvent) (bool, error) {
		if event.Wake {
			return true, nil
		}
		return handleIntent(event), nil
	}

	running, redraw, drawn := true, true, 0
	for running {
		for {
			event, ok, pollErr := ctx.PollInput()
			if pollErr != nil {
				return pollErr
			}
			if !ok {
				break
			}
			running, err = handle(event)
			if err != nil {
				return err
			}
			redraw = true
		}
		if !running {
			break
		}
		if uploaded, processErr := cache.ProcessPending(ctx); processErr != nil {
			return processErr
		} else if uploaded {
			redraw = true
		}
		if redraw {
			if err := draw(); err != nil {
				return err
			}
			drawn++
			if config.Frames > 0 && drawn >= config.Frames {
				if config.ScreenshotPath != "" {
					if err := ctx.BeginCapture(); err != nil {
						return err
					}
					if err := draw(); err != nil {
						_ = ctx.EndCapture()
						return err
					}
					if err := ctx.ScreenshotPNG(config.ScreenshotPath); err != nil {
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
		if config.Frames > 0 {
			ctx.RequestFrame()
			redraw = true
		}
		if delay, animated := cache.NextFrameIn(); animated {
			milliseconds := delay.Milliseconds()
			if milliseconds < 1 {
				milliseconds = 1
			}
			ctx.RequestFrameIn(uint32(milliseconds))
			redraw = true
		}
		if err := ctx.Present(); err != nil {
			return err
		}
	}
	return nil
}
