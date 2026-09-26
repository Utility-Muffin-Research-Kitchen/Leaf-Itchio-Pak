package settings

import (
	"encoding/json"
	"os"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// CategoryFilter holds the enabled state and individually-disabled tags for
// one content filter category.
type CategoryFilter struct {
	Enabled  bool     `json:"enabled"`
	Disabled []string `json:"disabled,omitempty"`
}

// HasActiveTag reports whether at least one tag from tagList would be filtered
// (Enabled is true and the tag is not in Disabled).
func (cf CategoryFilter) HasActiveTag(tagList []string) bool {
	if !cf.Enabled {
		return false
	}
	for _, tag := range tagList {
		inDisabled := false
		for _, d := range cf.Disabled {
			if d == tag {
				inDisabled = true
				break
			}
		}
		if !inDisabled {
			return true
		}
	}
	return false
}

// ContentFilter holds the complete content filter configuration.
// AdultContent, HeavyThemes, and SubstanceUse default to enabled.
// QueerContent defaults to disabled.
type ContentFilter struct {
	AdultContent CategoryFilter `json:"adult_content"`
	QueerContent CategoryFilter `json:"queer_content"`
	HeavyThemes  CategoryFilter `json:"heavy_themes"`
	SubstanceUse CategoryFilter `json:"substance_use"`
}

// RememberedDestination is stable across mount-point changes: RelativePath is
// rooted at the selected canonical system directory (or Music root).
type RememberedDestination struct {
	SourceID     string `json:"source_id"`
	RelativePath string `json:"relative_path"`
}

// Config is the top-level application configuration.
type Config struct {
	// AuthToken is the itch.io key from QR sign-in, sent as Authorization:
	// Bearer. AuthUser is the account's display name for Settings.
	AuthToken string `json:"auth_token,omitempty"`
	AuthUser  string `json:"auth_user,omitempty"`
	// CredentialWarningAccepted records that the physical-access warning was
	// shown; it keeps the JSON name used when keys were typed in.
	CredentialWarningAccepted bool `json:"api_key_physical_warning_accepted,omitempty"`
	// LegacyKeyRemoved is set when Load removed a manually entered API key,
	// so the app can explain once that sign-in replaces it.
	LegacyKeyRemoved bool                             `json:"legacy_key_removed,omitempty"`
	ROMSelection     string                           `json:"rom_selection"`
	ROMLocation      string                           `json:"rom_location"`
	ROMDestinations  map[string]RememberedDestination `json:"remembered_rom_destinations,omitempty"`
	MusicDestination *RememberedDestination           `json:"remembered_music_destination,omitempty"`
	Filter           ContentFilter                    `json:"content_filter"`
	LogLevel         string                           `json:"log_level,omitempty"`       // "debug" | "" (resolves to "info")
	SortMode         string                           `json:"sort_mode,omitempty"`       // "az" | "za" | "new" | "dl" | "free" | "paid" | "" (empty = [RSS])
	PlatformFilter   string                           `json:"platform_filter,omitempty"` // "" = All; persisted to config.json
	UnifiedNaming    bool                             `json:"unified_naming"`            // default true — no omitempty so false survives save/load
	MusicDownload    string                           `json:"music_download,omitempty"`  // "auto" | "ask" | "off"
	MusicLocation    string                           `json:"music_location,omitempty"`  // "auto" | "ask"
}

func defaults() *Config {
	return &Config{
		ROMSelection:  "auto",
		ROMLocation:   "auto",
		UnifiedNaming: true,
		MusicDownload: "off",
		MusicLocation: "auto",
		Filter: ContentFilter{
			AdultContent: CategoryFilter{Enabled: true},
			HeavyThemes:  CategoryFilter{Enabled: true},
			SubstanceUse: CategoryFilter{Enabled: true},
			// QueerContent defaults to disabled (zero value).
		},
	}
}

// Load reads the config from path. If the file is missing or corrupted,
// defaults are returned without an error.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Debug("settings: config not found at %s, using defaults", path)
		return defaults(), nil
	}
	// Best effort: POSIX filesystems can keep credentials owner-only. FAT32
	// ignores Unix mode bits, which is disclosed before the first key save.
	if err := os.Chmod(path, 0o600); err != nil {
		logger.Debug("settings: owner-only config mode unavailable: %v", err)
	}
	cfg := defaults()
	if err := json.Unmarshal(data, cfg); err != nil {
		logger.Warn("settings: config at %s is invalid, using defaults: %v", path, err)
		return defaults(), nil
	}
	removeLegacyAPIKey(path, data, cfg)
	return cfg, nil
}

// removeLegacyAPIKey drops a manually entered API key from the file: QR
// sign-in replaces it. The key is never logged. Inventory and every other
// setting are untouched.
func removeLegacyAPIKey(path string, data []byte, cfg *Config) {
	var legacy struct {
		APIKey *string `json:"api_key"`
	}
	if json.Unmarshal(data, &legacy) != nil || legacy.APIKey == nil {
		return
	}
	if *legacy.APIKey != "" {
		cfg.LegacyKeyRemoved = true
	}
	if err := cfg.Save(path); err != nil {
		logger.Warn("settings: could not remove the saved API key yet: %v", err)
		return
	}
	logger.Info("settings: removed the saved API key; sign in with itch.io instead")
}

// Credential is the key sent as Authorization: Bearer, empty when signed out.
func (c *Config) Credential() string { return c.AuthToken }

// SignedIn reports whether a QR sign-in key is stored.
func (c *Config) SignedIn() bool { return c.AuthToken != "" }

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		logger.Error("settings: failed to write tmp config %s: %v", tmp, err)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		logger.Debug("settings: owner-only temporary config mode unavailable: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		logger.Error("settings: failed to rename config %s → %s: %v", tmp, path, err)
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		logger.Debug("settings: owner-only config mode unavailable: %v", err)
	}
	return nil
}
