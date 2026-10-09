package catui

import (
	"fmt"
	"image"
	"math"
	"os"
	"runtime"
	"strings"
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

	// A screen name ending in -end shows that screen's body scrolled to its
	// last line, as Down does on the device. One ending in -page shows it
	// after one press of Right, which pages it.
	var scrollToEnd, pageOnce bool
	config.Screen, scrollToEnd = strings.CutSuffix(config.Screen, "-end")
	if !scrollToEnd {
		config.Screen, pageOnce = strings.CutSuffix(config.Screen, "-page")
	}
	var scroll *appui.BodyScroll
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
	case "detail", "detail-price", "detail-donation", "detail-owned", "detail-minimum", "detail-free-sale", "warning",
		"detail-unavailable", "detail-unavailable-downloaded", "detail-unavailable-offline", "detail-title-emoji", "detail-title-cjk":
		model := appui.NewDetailModel(appui.DetailGame{
			Title: "Leafbound 葉", Author: "UMRK fixture", URL: "https://example.itch.io/leafbound",
			Platform: "GBC", IsFree: true, CanDownload: true,
			Downloaded: config.Screen == "detail" || config.Screen == "detail-owned" ||
				config.Screen == "detail-unavailable-downloaded" || config.Screen == "detail-unavailable-offline",
		})
		switch config.Screen {
		case "detail-price":
			model.Game.IsFree = false
			model.Game.CanDownload = false
			model.Game.PriceLabel = "€2,50 (was €5,00)"
		case "detail-donation":
			model.Game.PriceLabel = "Free / suggested $3.00"
		case "detail-owned":
			model.Game.IsFree, model.Game.Owned = false, true
			model.Game.PriceLabel = "$5.00"
		case "detail-minimum":
			model.Game.IsFree = false
			model.Game.PriceLabel = "$2.00 or more"
		case "detail-free-sale":
			model.Game.PriceLabel = "Free (was $5.00)"
		case "detail-title-emoji":
			// A title with no letters, which the theme font cannot draw.
			model.Game.Title = "↑🐱↑"
		case "detail-title-cjk":
			model.Game.Title = "葉っぱの冒険"
		}
		model.SetReady(`<h2>A pocket-sized journey</h2><p>Explore a multilingual forest, collect lost seeds, and bring music back to every clearing.</p><ul><li>Controller ready</li><li>Offline after install</li></ul>`,
			[]string{"Game Boy Color", "Adventure", "日本語", "GIF gallery"},
			[]string{"fixture://detail-cover", "fixture://detail-shot"}, false, config.Screen == "warning")
		if strings.HasPrefix(config.Screen, "detail-unavailable") {
			model.SetError("Go back and reopen this game to try again.")
			if config.Screen == "detail-unavailable-offline" {
				model.SetError("Can't reach itch.io. Check the connection, then reopen this game.")
			}
			model.Images = nil
			if model.Game.Downloaded {
				model.Images = []string{"fixture://detail-cover"}
			}
		}
		screen, screenErr := NewDetailScreen(ctx, model, cache)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		closeScreen = screen.Close
		scroll = &model.BodyScroll
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.DetailIntentBack
		}
	case "download-select", "download-select-hidden", "download-select-offline", "archive-contents":
		model := appui.NewDownloadSelectModel("Leafbound 葉")
		if config.Screen == "download-select-offline" {
			// The device showed "fetch game page: network request failed" here.
			model.SetError("Can't reach itch.io. Check the connection and try again.")
		} else if config.Screen == "archive-contents" {
			model.SetChoices("Choose one .GBC ROM (1/1)", []appui.DownloadChoice{
				{Title: "release/leafbound-v1.gbc", Badge: "GBC"},
				{Title: "release/leafbound-v2.gbc", Badge: "GBC"},
			})
		} else if config.Screen == "download-select-hidden" {
			model.SetChoices("Choose file to download", []appui.DownloadChoice{
				{Title: "leafbound-ps1.zip", Badge: "ZIP"},
				{Title: "Show all files", Detail: "2 more"},
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
	case "download-progress", "download-done", "download-done-long", "download-error", "download-stalled", "download-inhibit",
		"download-cancelled", "archive-inspect", "archive-inspect-long", "archive-unreadable":
		model := &appui.DownloadProgressModel{
			State: appui.DownloadProgressRunning, Title: "Leafbound 葉", Filename: "leafbound.gbc",
			Downloaded: 584 * 1024, Total: 1024 * 1024, FileIndex: 0, FileCount: 2,
		}
		switch config.Screen {
		case "archive-inspect":
			model.Filename = "Inspecting soundtrack-and-game.zip"
			model.Downloaded, model.Total, model.FileCount = 128*1024, 640*1024, 1
		case "archive-inspect-long":
			// A long upload name, as itch.io creators often publish them.
			model.Filename = "Inspecting Leafbound Deluxe Edition (PlayStation) v1.2.3 English Patch.zip"
			model.Downloaded, model.Total, model.FileCount = 128*1024, 640*1024, 1
		case "download-done":
			model.State = appui.DownloadProgressDone
			model.SavedPaths = []string{"/Roms/GBC/Leafbound.gbc", "/Roms/GBC/Leafbound Bonus.gb"}
			model.Skipped = []string{"leafbound.GBC"}
			model.LibraryStatus = "Leaf library rescan requested."
		case "download-done-long":
			// A game and its 30-track soundtrack from one archive.
			model.State = appui.DownloadProgressDone
			model.SavedPaths = []string{"/Roms/GBC/Leafbound.gbc"}
			for _, track := range fixtureSoundtrack() {
				model.SavedPaths = append(model.SavedPaths, "/Music/Leafbound/"+track)
			}
			model.Skipped = []string{"leafbound.GBC", "cover.png"}
			model.LibraryStatus = "Leaf library rescan requested."
		case "download-error":
			model.State = appui.DownloadProgressError
			model.Detail = "The signed download URL expired before the transfer completed. Return to Detail and try again."
		case "download-stalled":
			model.State = appui.DownloadProgressError
			model.Detail = "Download stalled. Check the connection and try again."
		case "download-inhibit":
			model.State = appui.DownloadProgressInhibitBlocked
			model.Detail = "Jawaka is unavailable, so Leaf can't prevent suspend during this download. " +
				"Press A to download without that protection, or B to cancel."
		case "archive-unreadable":
			model.State = appui.DownloadProgressError
			model.Detail = "Leaf can't read this archive. It may be damaged or in an unsupported format."
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
		scroll = &model.BodyScroll
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
	case "destination-source", "destination-folder", "destination-music", "destination-confirm", "destination-confirm-multi":
		model := appui.NewDestinationModel("Leafbound 葉")
		switch config.Screen {
		case "destination-source":
			model.SetSources([]appui.DestinationItem{
				{Kind: appui.DestinationItemSource, Label: "Primary SD", Detail: "Available", Enabled: true},
				{Kind: appui.DestinationItemSource, Label: "Secondary SD", Detail: "Available", Enabled: true},
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
			// The names the install writes; a reinstall can replace a file on
			// the other card.
			model.SetConfirm("Confirm download destination", "Secondary SD", []appui.BodyBlock{
				appui.ListBlock([]appui.ListEntry{{Text: "Roms/GBC/RPG/Leafbound 葉 (2).gbc"}}),
				appui.Paragraph("Roms/GBC/RPG"),
				appui.ListBlock([]appui.ListEntry{{Text: "Primary SD / Roms/GB/leafbound_bonus_v3.gb"}}),
				appui.Paragraph("Roms/GB"),
			})
		case "destination-confirm-multi":
			// A disc image installs its cue sheet and every track file, one
			// line each, then the folder they go to.
			model.SetConfirm("Confirm download destination", "Primary SD", []appui.BodyBlock{
				appui.ListBlock([]appui.ListEntry{
					{Text: "Roms/PS/Leafbound/Leafbound.cue"},
					{Text: "Roms/PS/Leafbound/Leafbound (Track 1).bin"},
					{Text: "Roms/PS/Leafbound/Leafbound (Track 2).bin"},
					{Text: "Roms/PS/Leafbound/Leafbound (Track 3).bin"},
				}),
				appui.Paragraph("Roms/PS/Leafbound"),
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
	case "manage-list", "manage-confirm", "manage-leftover", "manage-result", "manage-error", "manage-delete-long":
		model := appui.NewManageModel("Leafbound 葉")
		model.SetItems("4 managed files · source-owned paths only", []appui.ManageItem{
			{Kind: appui.ManageItemFile, Label: "Leafbound.gbc", Badge: "ROM", Detail: "Primary SD / Roms/GBC/Leafbound.gbc", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "bonus.gb", Badge: "UNAVAILABLE", Detail: "Secondary SD / Roms/GB/bonus.gb", Enabled: false},
			{Kind: appui.ManageItemFile, Label: "forest-theme.ogg", Badge: "MUSIC", Detail: "Primary SD / Music/Leafbound/cd1/forest-theme.ogg", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "forest-theme.ogg", Badge: "OLD", Detail: "Primary SD / Music/Leafbound/forest-theme.ogg", Enabled: true},
			{Kind: appui.ManageItemDeleteLeftOver, Label: "Delete left-over files", Badge: "1 OLD", Detail: "Left over from an older version", Enabled: true},
			{Kind: appui.ManageItemDeleteROMs, Label: "Delete ROM files", Badge: "2 ROM", Enabled: false},
			{Kind: appui.ManageItemDeleteMusic, Label: "Delete soundtrack", Badge: "2 MUSIC", Enabled: true},
			{Kind: appui.ManageItemDeleteAll, Label: "Delete all downloads", Badge: "4 FILES", Enabled: false},
			{Kind: appui.ManageItemRename, Label: "Use title for Leafbound.gbc", Badge: "RENAME", Enabled: true},
		})
		if config.Screen == "manage-confirm" {
			model.SetConfirm("Delete selected file?", []appui.BodyBlock{appui.ListBlock([]appui.ListEntry{
				{Text: "Leafbound.gbc", Detail: "Primary SD / Roms/GBC/Leafbound.gbc"}})})
		} else if config.Screen == "manage-leftover" {
			model.SetConfirm("Delete selected file?", []appui.BodyBlock{
				appui.Paragraph("Left over from an older version"),
				appui.ListBlock([]appui.ListEntry{{Text: "forest-theme.ogg", Detail: "Primary SD / Music/Leafbound/forest-theme.ogg"}}),
			})
		} else if config.Screen == "manage-result" {
			model.SetResult("Deleted 1 managed file(s). Kept 1 that another game uses.")
			model.SetLibraryStatus("Leaf library rescan queued.")
		} else if config.Screen == "manage-error" {
			model.SetError("Couldn't delete Leafbound Deluxe Edition (PlayStation).bin. Check the SD card, then try again.")
		} else if config.Screen == "manage-delete-long" {
			// "Delete all downloads" for a game with a 30-track soundtrack,
			// as CatManageFlow words it: each file's name, then its path.
			tracks := fixtureSoundtrack()
			files := []appui.ListEntry{{Text: "Leafbound.gbc", Detail: "Primary SD / Roms/GBC/Leafbound.gbc"}}
			for _, track := range tracks {
				files = append(files, appui.ListEntry{Text: track, Detail: "Primary SD / Music/Leafbound/" + track})
			}
			model.SetConfirm(fmt.Sprintf("Delete %d managed files?", len(tracks)+1), []appui.BodyBlock{appui.ListBlock(files)})
		}
		screen, screenErr := NewManageScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		scroll = &model.BodyScroll
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.ManageIntentBack
		}
	case "manage-members":
		// Files installed under a name other than their archive member's say
		// which member they came from (F8).
		model := appui.NewManageModel("Glory Hunters")
		model.SetItems("4 managed files · source-owned paths only", []appui.ManageItem{
			{Kind: appui.ManageItemFile, Label: "Glory Hunters.gba", Badge: "ROM",
				Note: "From Glory Hunters 1.3 EZ IV Patched.gba", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "Glory Hunters (v1.2).gba", Badge: "OLD",
				Note: "From Glory Hunters v1.2 Bonus Levels Edition (Rev A) (English Translation).gba", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "Leafbound 葉.gbc", Badge: "ROM", Note: "From リーフバウンド.gbc", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "01 Theme.ogg", Badge: "MUSIC", Enabled: true},
			{Kind: appui.ManageItemDeleteLeftOver, Label: "Delete left-over files", Badge: "1 OLD", Detail: "Left over from an older version", Enabled: true},
			{Kind: appui.ManageItemDeleteROMs, Label: "Delete ROM files", Badge: "3 ROM", Enabled: true},
			{Kind: appui.ManageItemDeleteAll, Label: "Delete all downloads", Badge: "4 FILES", Enabled: true},
		})
		screen, screenErr := NewManageScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.ManageIntentBack
		}
	case "manage-earlier-copy", "manage-earlier-copy-confirm":
		// A Pico-8 game installed twice, the second time into another folder:
		// the first copy's files are earlier copies, not an older version (F28).
		model := appui.NewManageModel("Moss Garden")
		model.SetItems("4 managed files · source-owned paths only", []appui.ManageItem{
			{Kind: appui.ManageItemFile, Label: "main.p8", Badge: "OLD", Detail: "Primary SD / Roms/PICO8/Moss Garden/main.p8", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "lib.lua", Badge: "OLD", Detail: "Primary SD / Roms/PICO8/Moss Garden/lib.lua", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "main.p8", Badge: "ROM", Detail: "Primary SD / Roms/PICO8/Moss Garden 2/main.p8", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "lib.lua", Badge: "ROM", Detail: "Primary SD / Roms/PICO8/Moss Garden 2/lib.lua", Enabled: true},
			{Kind: appui.ManageItemDeleteLeftOver, Label: "Delete left-over files", Badge: "2 OLD", Detail: "Earlier copy of files you installed again", Enabled: true},
			{Kind: appui.ManageItemDeleteROMs, Label: "Delete ROM files", Badge: "4 ROM", Enabled: true},
			{Kind: appui.ManageItemDeleteAll, Label: "Delete all downloads", Badge: "4 FILES", Enabled: true},
		})
		model.Cursor = 4
		if config.Screen == "manage-earlier-copy-confirm" {
			model.SetConfirm("Delete 2 managed files?", []appui.BodyBlock{
				appui.Paragraph("Earlier copy of files you installed again"),
				appui.ListBlock([]appui.ListEntry{
					{Text: "main.p8", Detail: "Primary SD / Roms/PICO8/Moss Garden/main.p8"},
					{Text: "lib.lua", Detail: "Primary SD / Roms/PICO8/Moss Garden/lib.lua"},
				}),
			})
		}
		screen, screenErr := NewManageScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		handleIntent = func(event InputEvent) bool {
			return screen.HandleInput(event) != appui.ManageIntentBack
		}
	case "manage-rename":
		// One ROM back on its archive name and one still named after the
		// title: each offers the other name (F13).
		model := appui.NewManageModel("Glory Hunters")
		model.SetItems("2 managed files · source-owned paths only", []appui.ManageItem{
			{Kind: appui.ManageItemFile, Label: "Glory Hunters v1.2 Bonus Levels Edition (Rev A) (English Translation).gba",
				Badge: "ROM", Enabled: true},
			{Kind: appui.ManageItemFile, Label: "Glory Hunters.gb", Badge: "ROM", Note: "From Glory Hunters 2.0.1.gb", Enabled: true},
			{Kind: appui.ManageItemDeleteROMs, Label: "Delete ROM files", Badge: "2 ROM", Enabled: true},
			{Kind: appui.ManageItemDeleteAll, Label: "Delete all downloads", Badge: "2 FILES", Enabled: true},
			{Kind: appui.ManageItemRename, Label: "Use title for Glory Hunters v1.2 Bonus Levels Edition (Rev A) (English Translation).gba",
				Badge: "RENAME", Enabled: true},
			{Kind: appui.ManageItemRename, Label: "Use original name for Glory Hunters.gb", Badge: "RENAME", Enabled: true},
		})
		model.Cursor = len(model.Items) - 1
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
		entries := []appui.ListEntry{{Text: "Saves/GBC/leafbound.srm", Detail: "→ Saves/GBC/Leafbound 葉.srm"}}
		if config.Screen == "rename-states" {
			state, subtitle, heading = appui.RenameConfirmStates, "Save states", "Rename these state files?"
			entries = []appui.ListEntry{
				{Text: "States/GBC-gambatte/leafbound.state1", Detail: "→ States/GBC-gambatte/Leafbound 葉.state1"},
				{Text: "States/GBC-gambatte/leafbound.state1.png", Detail: "→ States/GBC-gambatte/Leafbound 葉.state1.png"},
			}
		}
		model.SetPrompt(state, subtitle, heading, entries)
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
	case "settings", "settings-folders", "settings-confirm", "settings-message", "settings-notice", "settings-error",
		"moderation", "moderation-tags":
		title, subtitle := "Settings", "Leaf settings · changes save immediately"
		rows := []appui.SettingsRow{
			{Key: appui.SettingsAccount, Label: "itch.io Account", Value: "leafbound-player", ActionEnabled: true},
			{Key: appui.SettingsSignOut, Label: "Sign Out", ActionEnabled: true},
			{Key: appui.SettingsROMSelection, Label: "ROM Selection", Value: appui.ChoiceLabel("ask"), ActionEnabled: true},
			{Key: appui.SettingsROMLocation, Label: "ROM Location", Value: appui.ChoiceLabel("ask"), ActionEnabled: true},
			{Key: appui.SettingsMusicDownload, Label: "Music Download", Value: appui.ChoiceLabel("auto"), ActionEnabled: true},
			{Key: appui.SettingsMusicLocation, Label: "Music Location", Value: appui.ChoiceLabel("ask"), ActionEnabled: true},
			{Key: appui.SettingsUnifiedNaming, Label: "Rename ROM Files", Value: "On", ActionEnabled: true},
			{Key: appui.SettingsROMDestination, Label: "Remembered ROM Folder", Value: "Primary SD + Secondary SD · 3 systems"},
			{Key: appui.SettingsAppData, Label: "App Data", Value: "/.userdata/shared/Itch-io"},
			{Key: appui.SettingsContentModeration, Label: "Content Moderation", Value: ">", ActionEnabled: true},
			{Key: appui.SettingsAbout, Label: "About", Value: ">", ActionEnabled: true},
		}
		cursor := 0
		if config.Screen == "settings-folders" {
			// The remembered-folder rows below Rename ROM Files, in Settings'
			// own order, with the longest label selected.
			rows = append(rows[:7:7],
				appui.SettingsRow{Key: appui.SettingsLogLevel, Label: "Log Level", Value: "Info", ActionEnabled: true},
				appui.SettingsRow{Key: appui.SettingsROMDestination, Label: "Remembered ROM Folder", Value: "Primary SD + Secondary SD · 3 systems"},
				appui.SettingsRow{Key: appui.SettingsMusicDestination, Label: "Remembered Music Folder", Value: "Secondary SD / Albums"},
				appui.SettingsRow{Key: appui.SettingsResetDestinations, Label: "Reset Remembered Folders", ActionEnabled: true},
				appui.SettingsRow{Key: appui.SettingsAppData, Label: "App Data", Value: "/.userdata/shared/Itch-io"},
			)
			cursor = len(rows) - 2
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
				{Key: appui.SettingsTagMaster, Label: "All Category Tags", Value: "Blocked", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Adult", Value: "Blocked", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Erotic", Value: "Allowed", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Mature", Value: "Blocked", ActionEnabled: true},
				{Key: appui.SettingsTag, Label: "Nudity", Value: "Blocked", ActionEnabled: true},
			}
		}
		model := appui.NewSettingsModel(title)
		model.SetRows(subtitle, rows)
		model.Cursor = cursor
		if config.Screen == "settings-confirm" {
			// The sign-in warning moved to the sign-in screen (signin-warning).
			model.SetConfirm("Sign out of itch.io?", []string{"Owned-game data on this device is cleared.", "Downloaded content and inventory remain installed."})
		}
		switch config.Screen {
		case "settings-message":
			model.SetMessage("Inventory update started. Local downloads stay available during the check.")
		case "settings-notice":
			// The longest settings message.
			model.SetMessage("Signed out. Downloads were not changed. The key stays valid on itch.io until you delete it from your account's API keys.")
		case "settings-error":
			model.SetError("Couldn't check your itch.io account. You're still signed in; try again when online.")
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
		screen, screenErr := NewAboutScreen(ctx, "0.1.0", "v0.13.0-dev (2026-07-20-gabc1234)")
		if screenErr != nil {
			return screenErr
		}
		draw = screen.Draw
		closeScreen = screen.Close
		scroll = &screen.scroll
		handleIntent = func(event InputEvent) bool { return !screen.HandleInput(event) }
	case "signin", "signin-error", "signin-done", "signin-qr-failed", "signin-checking", "signin-warning", "detail-signin", "detail-not-owned":
		model := &appui.SignInModel{
			State: appui.SignInWaiting, UserCode: "KXR4-7PLM",
			QRURL:   "https://itch.io/user/oauth/device?code=fixture-signin-request",
			Expires: time.Now().Add(9*time.Minute + 42*time.Second),
		}
		switch config.Screen {
		case "signin-error":
			model.State, model.CanRetry = appui.SignInError, true
			model.Heading, model.Detail = "The code expired", "Press A for a new code."
		case "signin-done":
			model.State, model.Heading, model.Detail = appui.SignInDone, "Signed in as leafbound-player", "12 owned game(s) found."
		case "signin-checking":
			model.State = appui.SignInChecking
		case "signin-warning":
			model.State = appui.SignInWarning
		case "signin-qr-failed":
			// Too long for any QR code, so drawing it fails for real.
			model.QRURL = "https://itch.io/user/oauth/device?code=" + strings.Repeat("x", 5000)
		}
		if config.Screen == "detail-signin" || config.Screen == "detail-not-owned" {
			// detail-not-owned: signed in, but the account does not own it.
			detail := appui.NewDetailModel(appui.DetailGame{
				Title: "Leafbound Deluxe", Author: "leafdev", URL: "https://leafdev.itch.io/leafbound-deluxe",
				Platform: "GBA", Price: 4.99, NeedsSignIn: config.Screen == "detail-signin",
			})
			detail.SetReady(`<p>A paid Game Boy Advance release. Sign in with itch.io to download it once you own it.</p>`,
				[]string{"Game Boy Advance", "Paid"}, []string{"fixture://detail-cover"}, false, false)
			screen, screenErr := NewDetailScreen(ctx, detail, cache)
			if screenErr != nil {
				return screenErr
			}
			draw, closeScreen = screen.Draw, screen.Close
			handleIntent = func(event InputEvent) bool { return screen.HandleInput(event) != appui.DetailIntentBack }
			break
		}
		screen, screenErr := NewSignInScreen(ctx, model)
		if screenErr != nil {
			return screenErr
		}
		draw, closeScreen = screen.Draw, screen.Close
		scroll = &model.BodyScroll
		handleIntent = func(event InputEvent) bool {
			intent := screen.HandleInput(event)
			return intent != appui.SignInIntentBack && intent != appui.SignInIntentCancel
		}
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
	if pageOnce && scroll == nil {
		return fmt.Errorf("input fixture %q does not scroll", config.Screen)
	}
	if scrollToEnd {
		if scroll == nil {
			return fmt.Errorf("input fixture %q does not scroll", config.Screen)
		}
		// The first draw clamps this to the body's last line.
		scroll.ScrollLine = math.MaxInt32
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
	if pageOnce {
		// The first draw measures the body, which the page is a share of, so
		// the press comes after it and goes through the screen's own input
		// handling, as it does on the device. Later draws show the next page.
		drawBody, pressed := draw, false
		draw = func() error {
			if err := drawBody(); err != nil || pressed {
				return err
			}
			pressed = true
			_, err := handle(InputEvent{Button: ButtonRight, Pressed: true})
			return err
		}
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

// fixtureSoundtrack is a 30-track soundtrack, enough to overflow any screen
// that lists it.
func fixtureSoundtrack() []string {
	names := []string{"Forest Theme", "Seed Vault", "Clearing at Dawn", "Mossy Steps", "Lantern Walk",
		"Rain on Leaves", "The Old Oak", "River Crossing", "Night Birds", "Homecoming"}
	tracks := make([]string, 0, 30)
	for index := range 30 {
		tracks = append(tracks, fmt.Sprintf("%02d %s.ogg", index+1, names[index%len(names)]))
	}
	return tracks
}
