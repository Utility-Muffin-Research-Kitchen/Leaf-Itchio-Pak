//go:build !headless

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type CatSettingsAction uint8

const (
	CatSettingsNone CatSettingsAction = iota
	CatSettingsEditAPIKey
	CatSettingsClearImages
	CatSettingsRefreshGames
	CatSettingsUpdateInventory
	CatSettingsModeration
	CatSettingsAbout
)

type catSettingsConfirm uint8

const (
	catSettingsConfirmNone catSettingsConfirm = iota
	catSettingsConfirmAPIWarning
	catSettingsConfirmRemoveAPI
	catSettingsConfirmResetDestinations
)

type catAPIResult struct {
	owned []itchio.OwnedGame
	err   error
}

type CatSettingsFlow struct {
	cfg            *settings.Config
	cfgPath        string
	ownedCachePath string
	appDataPath    string
	sources        leaf.SourceList
	client         *itchio.Client
	wake           func()
	pending        catSettingsConfirm
	apiResults     chan catAPIResult
	validating     atomic.Bool
}

func NewCatSettingsFlow(cfg *settings.Config, cfgPath, ownedCachePath, appDataPath string,
	sources leaf.SourceList, client *itchio.Client, wake func()) (*CatSettingsFlow, *appui.SettingsModel) {
	flow := &CatSettingsFlow{
		cfg: cfg, cfgPath: cfgPath, ownedCachePath: ownedCachePath, appDataPath: appDataPath,
		sources: sources, client: client, wake: wake, apiResults: make(chan catAPIResult, 1),
	}
	model := appui.NewSettingsModel("Settings")
	flow.Refresh(model)
	return flow, model
}

func (flow *CatSettingsFlow) Refresh(model *appui.SettingsModel) {
	rows := []appui.SettingsRow{{
		Key: appui.SettingsAPIKey, Label: "API Key", Value: maskedAPIKey(flow.cfg.APIKey), ActionEnabled: true,
	}}
	if flow.cfg.APIKey != "" {
		rows = append(rows,
			appui.SettingsRow{Key: appui.SettingsEditAPIKey, Label: "Edit API Key", Value: "", ActionEnabled: true},
			appui.SettingsRow{Key: appui.SettingsRemoveAPIKey, Label: "Remove API Key", Value: "", ActionEnabled: true},
		)
	}
	rows = append(rows,
		appui.SettingsRow{Key: appui.SettingsROMSelection, Label: "ROM Selection", Value: settingValue(flow.cfg.ROMSelection, "auto"), ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsROMLocation, Label: "ROM Location", Value: settingValue(flow.cfg.ROMLocation, "auto"), ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsMusicDownload, Label: "Music Download", Value: settingValue(flow.cfg.MusicDownload, "off"), ActionEnabled: true},
	)
	if flow.cfg.MusicDownload != "off" {
		rows = append(rows, appui.SettingsRow{Key: appui.SettingsMusicLocation, Label: "Music Location", Value: settingValue(flow.cfg.MusicLocation, "auto"), ActionEnabled: true})
	}
	rows = append(rows,
		appui.SettingsRow{Key: appui.SettingsUnifiedNaming, Label: "Use game title", Value: onOff(flow.cfg.UnifiedNaming), ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsLogLevel, Label: "Log Level", Value: logLevelValue(flow.cfg.LogLevel), ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsROMDestination, Label: "Remembered ROM folder", Value: flow.romPreference(), ActionEnabled: false},
		appui.SettingsRow{Key: appui.SettingsMusicDestination, Label: "Remembered Music folder", Value: flow.musicPreference(), ActionEnabled: false},
		appui.SettingsRow{Key: appui.SettingsResetDestinations, Label: "Reset remembered folders", Value: "", ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsAppData, Label: "App Data", Value: filepath.ToSlash(flow.appDataPath), ActionEnabled: false},
		appui.SettingsRow{Key: appui.SettingsClearImages, Label: "Clear Image Cache", Value: "", ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsRefreshGames, Label: "Refresh Game List", Value: "", ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsUpdateInventory, Label: "Update Inventory", Value: "", ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsContentModeration, Label: "Content Moderation", Value: ">", ActionEnabled: true},
		appui.SettingsRow{Key: appui.SettingsAbout, Label: "About", Value: ">", ActionEnabled: true},
	)
	model.SetRows("Leaf settings · changes save immediately", rows)
}

func (flow *CatSettingsFlow) Activate(model *appui.SettingsModel) (CatSettingsAction, error) {
	row, ok := model.Selected()
	if !ok || !row.ActionEnabled {
		return CatSettingsNone, nil
	}
	switch row.Key {
	case appui.SettingsAPIKey:
		if flow.cfg.APIKey != "" {
			flow.startAPIValidation(model, flow.cfg.APIKey)
			return CatSettingsNone, nil
		}
		if !flow.cfg.APIKeyWarningAccepted {
			flow.pending = catSettingsConfirmAPIWarning
			model.SetConfirm("Store an itch.io API key?", []string{
				"The key is stored in App Data on the SD card.",
				"FAT32 cannot protect it from someone with physical access to the card.",
				"The key is masked in the UI and redacted from logs.",
			})
			return CatSettingsNone, nil
		}
		return CatSettingsEditAPIKey, nil
	case appui.SettingsRemoveAPIKey:
		flow.pending = catSettingsConfirmRemoveAPI
		model.SetConfirm("Remove the API key?", []string{"Owned-game authentication data is cleared.", "Downloaded content and inventory remain installed."})
	case appui.SettingsEditAPIKey:
		return CatSettingsEditAPIKey, nil
	case appui.SettingsROMSelection:
		flow.cfg.ROMSelection = toggleTwo(flow.cfg.ROMSelection, "auto", "ask")
		return CatSettingsNone, flow.saveAndRefresh(model)
	case appui.SettingsROMLocation:
		flow.cfg.ROMLocation = toggleTwo(flow.cfg.ROMLocation, "auto", "ask")
		return CatSettingsNone, flow.saveAndRefresh(model)
	case appui.SettingsMusicDownload:
		switch flow.cfg.MusicDownload {
		case "off":
			flow.cfg.MusicDownload = "auto"
		case "auto":
			flow.cfg.MusicDownload = "ask"
		default:
			flow.cfg.MusicDownload = "off"
		}
		return CatSettingsNone, flow.saveAndRefresh(model)
	case appui.SettingsMusicLocation:
		flow.cfg.MusicLocation = toggleTwo(flow.cfg.MusicLocation, "auto", "ask")
		return CatSettingsNone, flow.saveAndRefresh(model)
	case appui.SettingsUnifiedNaming:
		flow.cfg.UnifiedNaming = !flow.cfg.UnifiedNaming
		return CatSettingsNone, flow.saveAndRefresh(model)
	case appui.SettingsLogLevel:
		flow.cfg.LogLevel = toggleTwo(flow.cfg.LogLevel, "debug", "")
		logger.SetLevel(logger.LevelFromString(flow.cfg.LogLevel))
		return CatSettingsNone, flow.saveAndRefresh(model)
	case appui.SettingsResetDestinations:
		flow.pending = catSettingsConfirmResetDestinations
		model.SetConfirm("Reset remembered folders?", []string{"ROM and Music folder preferences are cleared.", "No downloads or inventory entries are deleted."})
	case appui.SettingsClearImages:
		return CatSettingsClearImages, nil
	case appui.SettingsRefreshGames:
		return CatSettingsRefreshGames, nil
	case appui.SettingsUpdateInventory:
		return CatSettingsUpdateInventory, nil
	case appui.SettingsContentModeration:
		return CatSettingsModeration, nil
	case appui.SettingsAbout:
		return CatSettingsAbout, nil
	}
	return CatSettingsNone, nil
}

func (flow *CatSettingsFlow) Confirm(model *appui.SettingsModel) (CatSettingsAction, error) {
	pending := flow.pending
	flow.pending = catSettingsConfirmNone
	switch pending {
	case catSettingsConfirmAPIWarning:
		flow.cfg.APIKeyWarningAccepted = true
		if err := flow.cfg.Save(flow.cfgPath); err != nil {
			flow.cfg.APIKeyWarningAccepted = false
			return CatSettingsNone, err
		}
		flow.Refresh(model)
		return CatSettingsEditAPIKey, nil
	case catSettingsConfirmRemoveAPI:
		oldKey := flow.cfg.APIKey
		flow.cfg.APIKey = ""
		if err := flow.cfg.Save(flow.cfgPath); err != nil {
			flow.cfg.APIKey = oldKey
			return CatSettingsNone, err
		}
		flow.client.ResetAPIKeyState()
		logger.RemoveSecret("[API-KEY]")
		if err := os.Remove(flow.ownedCachePath); err != nil && !os.IsNotExist(err) {
			return CatSettingsNone, fmt.Errorf("remove owned cache: %w", err)
		}
		flow.Refresh(model)
		model.SetMessage("API key and owned authentication cache removed. Downloads were not changed.")
	case catSettingsConfirmResetDestinations:
		flow.cfg.ROMDestinations = nil
		flow.cfg.MusicDestination = nil
		flow.cfg.LastROMDirs = nil
		if err := flow.cfg.Save(flow.cfgPath); err != nil {
			return CatSettingsNone, err
		}
		flow.Refresh(model)
		model.SetMessage("Remembered ROM and Music folders were cleared. Downloads were not changed.")
	}
	return CatSettingsNone, nil
}

func (flow *CatSettingsFlow) Cancel(model *appui.SettingsModel) {
	flow.pending = catSettingsConfirmNone
	flow.Refresh(model)
}

func (flow *CatSettingsFlow) Back(model *appui.SettingsModel) bool {
	if model.State == appui.SettingsConfirm {
		flow.Cancel(model)
		return false
	}
	if model.State == appui.SettingsMessage || model.State == appui.SettingsError {
		flow.Refresh(model)
		return false
	}
	return true
}

func (flow *CatSettingsFlow) SetAPIKey(model *appui.SettingsModel, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("API key cannot be empty")
	}
	oldKey := flow.cfg.APIKey
	flow.cfg.APIKey = value
	if err := flow.cfg.Save(flow.cfgPath); err != nil {
		flow.cfg.APIKey = oldKey
		return err
	}
	logger.RegisterSecret(value, "[API-KEY]")
	flow.client.ResetAPIKeyState()
	flow.startAPIValidation(model, value)
	return nil
}

func (flow *CatSettingsFlow) Sync(model *appui.SettingsModel) bool {
	select {
	case result := <-flow.apiResults:
		flow.validating.Store(false)
		if result.err != nil {
			flow.client.StoreAPIKeyStatus(itchio.APIKeyStatusRejected)
			model.SetError("API key validation failed. The masked key remains stored so it can be edited or removed.")
			return true
		}
		flow.client.StoreAPIKeyStatus(itchio.APIKeyStatusWorking)
		urls := make([]string, 0, len(result.owned))
		for _, game := range result.owned {
			urls = append(urls, game.URL)
		}
		if err := itchio.SaveOwnedCache(flow.ownedCachePath, urls); err != nil {
			model.SetError("API key is valid, but the owned-game cache could not be saved.")
			return true
		}
		model.SetMessage(fmt.Sprintf("API key validated. %d owned game(s) found.", len(result.owned)))
		return true
	default:
		return false
	}
}

func (flow *CatSettingsFlow) startAPIValidation(model *appui.SettingsModel, key string) {
	model.State, model.Message = appui.SettingsWorking, "Validating the masked API key with itch.io…"
	flow.validating.Store(true)
	go func() {
		_, owned, err := flow.client.ValidateAPIKey(key)
		flow.apiResults <- catAPIResult{owned: owned, err: err}
		if flow.wake != nil {
			flow.wake()
		}
	}()
}

func (flow *CatSettingsFlow) Busy() bool { return flow != nil && flow.validating.Load() }

func (flow *CatSettingsFlow) saveAndRefresh(model *appui.SettingsModel) error {
	if err := flow.cfg.Save(flow.cfgPath); err != nil {
		return err
	}
	flow.Refresh(model)
	return nil
}

func (flow *CatSettingsFlow) romPreference() string {
	if len(flow.cfg.ROMDestinations) == 0 {
		return "None"
	}
	systems := make([]string, 0, len(flow.cfg.ROMDestinations))
	for system := range flow.cfg.ROMDestinations {
		systems = append(systems, system)
	}
	sort.Strings(systems)
	if len(systems) == 1 {
		return systems[0] + ": " + preferenceLabel(flow.sources, flow.cfg.ROMDestinations[systems[0]])
	}
	labels := make([]string, 0, len(systems))
	seen := make(map[string]bool, len(systems))
	for _, system := range systems {
		pref := flow.cfg.ROMDestinations[system]
		source, ok := flow.sources.ByID(pref.SourceID)
		label := pref.SourceID
		if ok {
			label = destinationSourceLabel(source)
		}
		if !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	return fmt.Sprintf("%s · %d systems", strings.Join(labels, " + "), len(systems))
}

func (flow *CatSettingsFlow) musicPreference() string {
	if flow.cfg.MusicDestination == nil {
		return "None"
	}
	return preferenceLabel(flow.sources, *flow.cfg.MusicDestination)
}

func preferenceLabel(sources leaf.SourceList, pref settings.RememberedDestination) string {
	source, ok := sources.ByID(pref.SourceID)
	label := pref.SourceID
	if ok {
		label = destinationSourceLabel(source)
	}
	if pref.RelativePath != "" && pref.RelativePath != "." {
		label += " / " + filepath.ToSlash(pref.RelativePath)
	}
	return label
}

func maskedAPIKey(key string) string {
	if key == "" {
		return "Not set"
	}
	runes := []rune(key)
	suffix := string(runes)
	if len(runes) > 4 {
		suffix = string(runes[len(runes)-4:])
	}
	return "••••" + suffix
}

func settingValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func onOff(value bool) string {
	if value {
		return "On"
	}
	return "Off"
}

func logLevelValue(value string) string {
	if value == "debug" {
		return "Debug"
	}
	return "Info"
}

func toggleTwo(value, first, second string) string {
	if value == first {
		return second
	}
	return first
}
