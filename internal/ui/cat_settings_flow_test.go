//go:build !headless

package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func settingsFixture(t *testing.T, cfg *settings.Config, client *itchio.Client) (*CatSettingsFlow, *appui.SettingsModel, string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	ownedPath := filepath.Join(dir, "owned_cache.json")
	if client == nil {
		client = itchio.NewClientWithBase("http://127.0.0.1")
	}
	sources := leaf.SourceList{
		{ID: "primary", Primary: true, Root: "/mnt/primary"},
		{ID: "secondary_sd", Root: "/mnt/secondary"},
	}
	flow, model := NewCatSettingsFlow(cfg, cfgPath, ownedPath, dir, sources, client, nil)
	return flow, model, cfgPath, ownedPath
}

func selectSettingsKey(t *testing.T, model *appui.SettingsModel, key appui.SettingsKey) {
	t.Helper()
	for index, row := range model.Rows {
		if row.Key == key {
			model.Cursor = index
			return
		}
	}
	t.Fatalf("settings key %d not found", key)
}

func settingRow(t *testing.T, model *appui.SettingsModel, key appui.SettingsKey) appui.SettingsRow {
	t.Helper()
	for _, row := range model.Rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("settings key %d not found", key)
	return appui.SettingsRow{}
}

func TestCatSettingsExposeOnlyLeafChoicesAndSourceLabels(t *testing.T) {
	cfg := &settings.Config{
		ROMSelection: "auto", ROMLocation: "auto", MusicDownload: "auto", MusicLocation: "ask", UnifiedNaming: true,
		ROMDestinations: map[string]settings.RememberedDestination{
			"GBC": {SourceID: "secondary_sd", RelativePath: "RPG"},
			"GB":  {SourceID: "primary", RelativePath: "."},
		},
		MusicDestination: &settings.RememberedDestination{SourceID: "secondary_sd", RelativePath: "Albums"},
	}
	_, model, _, _ := settingsFixture(t, cfg, nil)
	for _, row := range model.Rows {
		label := strings.ToLower(row.Label)
		if strings.Contains(label, "nextui") || strings.Contains(label, "pico-8 core") {
			t.Fatalf("legacy setting leaked into Leaf settings: %q", row.Label)
		}
	}
	if got := settingRow(t, model, appui.SettingsROMDestination).Value; got != "Primary SD + Secondary SD · 2 systems" {
		t.Fatalf("ROM source label = %q", got)
	}
	if got := settingRow(t, model, appui.SettingsMusicDestination).Value; got != "Secondary SD / Albums" {
		t.Fatalf("Music source label = %q", got)
	}
	if row := settingRow(t, model, appui.SettingsAppData); row.ActionEnabled || row.Value == "" {
		t.Fatalf("App Data row = %+v", row)
	}
	if got := settingRow(t, model, appui.SettingsROMSelection).Value; got != "auto" {
		t.Fatalf("ROM Selection = %q", got)
	}
}

func TestCatSettingsToggleROMSelection(t *testing.T) {
	cfg := &settings.Config{ROMSelection: "auto", ROMLocation: "auto", MusicDownload: "off"}
	flow, model, cfgPath, _ := settingsFixture(t, cfg, nil)
	selectSettingsKey(t, model, appui.SettingsROMSelection)
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if cfg.ROMSelection != "ask" || settingRow(t, model, appui.SettingsROMSelection).Value != "ask" {
		t.Fatalf("ROM Selection after toggle = %q", cfg.ROMSelection)
	}
	loaded, err := settings.Load(cfgPath)
	if err != nil || loaded.ROMSelection != "ask" {
		t.Fatalf("persisted ROM Selection = %q, %v", loaded.ROMSelection, err)
	}
}

func TestCatSettingsWarnBeforeFirstSignIn(t *testing.T) {
	cfg, err := settings.Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	flow, model, cfgPath, _ := settingsFixture(t, cfg, nil)
	if row := settingRow(t, model, appui.SettingsAccount); row.Value != "Not signed in" {
		t.Fatalf("account row = %+v", row)
	}
	selectSettingsKey(t, model, appui.SettingsAccount)
	action, err := flow.Activate(model)
	if err != nil || action != CatSettingsNone || model.State != appui.SettingsConfirm {
		t.Fatalf("warning activation = action %v state %v err %v", action, model.State, err)
	}
	action, err = flow.Confirm(model)
	if err != nil || action != CatSettingsSignIn || !cfg.CredentialWarningAccepted {
		t.Fatalf("warning confirm = action %v accepted=%v err=%v", action, cfg.CredentialWarningAccepted, err)
	}
	loaded, err := settings.Load(cfgPath)
	if err != nil || !loaded.CredentialWarningAccepted {
		t.Fatalf("persisted warning = %v err=%v", loaded.CredentialWarningAccepted, err)
	}
	selectSettingsKey(t, model, appui.SettingsAccount)
	if action, _ := flow.Activate(model); action != CatSettingsSignIn {
		t.Fatalf("second activation = %v, want sign-in without another warning", action)
	}
}

func TestCatSettingsSignOutKeepsDownloads(t *testing.T) {
	cfg := &settings.Config{AuthToken: "private-key", AuthUser: "tester", CredentialWarningAccepted: true, ROMLocation: "auto", MusicDownload: "off"}
	flow, model, cfgPath, ownedPath := settingsFixture(t, cfg, nil)
	if row := settingRow(t, model, appui.SettingsAccount); row.Value != "tester" {
		t.Fatalf("account row = %+v", row)
	}
	ownedCleared := false
	flow.SetOwnedChanged(func(owned []itchio.OwnedGame) { ownedCleared = len(owned) == 0 })
	logger.RegisterSecret(cfg.AuthToken, tokenSecretLabel)
	defer logger.RemoveSecret(tokenSecretLabel)
	if err := itchio.SaveOwnedCache(ownedPath, []string{"https://example.itch.io/game"}); err != nil {
		t.Fatal(err)
	}
	download := filepath.Join(filepath.Dir(cfgPath), "Leafbound.gbc")
	if err := os.WriteFile(download, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	selectSettingsKey(t, model, appui.SettingsSignOut)
	if _, err := flow.Activate(model); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Confirm(model); err != nil {
		t.Fatal(err)
	}
	if cfg.SignedIn() || cfg.AuthUser != "" || !ownedCleared {
		t.Fatalf("credential state remains: %+v cleared=%v", cfg, ownedCleared)
	}
	if loaded, _ := settings.Load(cfgPath); loaded.SignedIn() {
		t.Fatal("sign-out was not saved")
	}
	if _, err := os.Stat(ownedPath); !os.IsNotExist(err) {
		t.Fatalf("owned cache remains: %v", err)
	}
	if data, err := os.ReadFile(download); err != nil || string(data) != "rom" {
		t.Fatalf("download changed: %q err=%v", data, err)
	}
	for _, row := range model.Rows {
		if row.Key == appui.SettingsSignOut {
			t.Fatal("Sign Out is still offered after signing out")
		}
	}
}

func accountServer(t *testing.T, profileStatus int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer valid-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/profile":
			w.WriteHeader(profileStatus)
			_, _ = w.Write([]byte(`{"user":{"username":"tester"}}`))
		case "/profile/owned-keys":
			if r.URL.Query().Get("page") == "1" {
				_, _ = w.Write([]byte(`{"owned_keys":[{"game":{"id":7,"title":"Leafbound","url":"https://example.itch.io/leafbound"}}]}`))
			} else {
				_, _ = w.Write([]byte(`{"owned_keys":{}}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func syncSettings(t *testing.T, flow *CatSettingsFlow, model *appui.SettingsModel) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !flow.Sync(model) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func TestCatSettingsAccountCheckStoresNameAndOwnedGames(t *testing.T) {
	server := accountServer(t, http.StatusOK)
	cfg := &settings.Config{AuthToken: "valid-key", CredentialWarningAccepted: true, ROMLocation: "auto", MusicDownload: "off"}
	flow, model, cfgPath, ownedPath := settingsFixture(t, cfg, itchio.NewClientWithBase(server.URL))
	var liveOwned []itchio.OwnedGame
	flow.SetOwnedChanged(func(owned []itchio.OwnedGame) {
		liveOwned = append([]itchio.OwnedGame(nil), owned...)
	})
	selectSettingsKey(t, model, appui.SettingsAccount)
	if action, err := flow.Activate(model); err != nil || action != CatSettingsNone || model.State != appui.SettingsWorking {
		t.Fatalf("account check = action %v state %v err %v", action, model.State, err)
	}
	syncSettings(t, flow, model)
	if model.State != appui.SettingsMessage || !strings.Contains(model.Message, "1 owned game") {
		t.Fatalf("check state=%v message=%q", model.State, model.Message)
	}
	if len(liveOwned) != 1 || liveOwned[0].URL != "https://example.itch.io/leafbound" {
		t.Fatalf("live owned state = %#v", liveOwned)
	}
	urls, err := itchio.LoadOwnedCache(ownedPath)
	if err != nil || len(urls) != 1 || urls[0] != "https://example.itch.io/leafbound" {
		t.Fatalf("owned cache=%v err=%v", urls, err)
	}
	if loaded, _ := settings.Load(cfgPath); loaded.AuthUser != "tester" {
		t.Fatalf("account name = %q", loaded.AuthUser)
	}
}

// When itch.io rejects the stored key (revoked on the website), the app signs
// out instead of retrying a dead credential; a network error keeps it.
func TestCatSettingsSignsOutWhenItchIORejectsTheKey(t *testing.T) {
	server := accountServer(t, http.StatusUnauthorized)
	cfg := &settings.Config{AuthToken: "valid-key", CredentialWarningAccepted: true}
	flow, model, _, _ := settingsFixture(t, cfg, itchio.NewClientWithBase(server.URL))
	selectSettingsKey(t, model, appui.SettingsAccount)
	flow.Activate(model)
	syncSettings(t, flow, model)
	if cfg.SignedIn() || model.State != appui.SettingsError {
		t.Fatalf("after rejection: signed in %v, state %v", cfg.SignedIn(), model.State)
	}

	cfg = &settings.Config{AuthToken: "valid-key", CredentialWarningAccepted: true}
	flow, model, _, _ = settingsFixture(t, cfg, itchio.NewClientWithBase("http://127.0.0.1:1"))
	selectSettingsKey(t, model, appui.SettingsAccount)
	flow.Activate(model)
	syncSettings(t, flow, model)
	if !cfg.SignedIn() || model.State != appui.SettingsError {
		t.Fatalf("offline check: signed in %v, state %v; want still signed in", cfg.SignedIn(), model.State)
	}
}

func TestCatSettingsIgnoresStaleAPIValidationResult(t *testing.T) {
	flow, model, _, _ := settingsFixture(t, &settings.Config{}, nil)
	flow.apiGeneration.Store(2)
	flow.apiResults <- catAPIResult{
		generation: 1,
		owned:      []itchio.OwnedGame{{URL: "https://example.itch.io/stale"}},
	}
	changed := false
	flow.SetOwnedChanged(func([]itchio.OwnedGame) { changed = true })
	if !flow.Sync(model) {
		t.Fatal("stale validation result was not consumed")
	}
	if changed {
		t.Fatal("stale validation repopulated live owned state")
	}
}

func TestCatModerationPreservesDefaultPolicyAndPerTagOverrides(t *testing.T) {
	cfg, err := settings.Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	flow, model := NewCatModerationFlow(cfg, filepath.Join(t.TempDir(), "config.json"))
	if settingRow(t, model, appui.SettingsAdultContent).Value == "Allowed >" ||
		settingRow(t, model, appui.SettingsQueerContent).Value != "Allowed >" ||
		settingRow(t, model, appui.SettingsHeavyThemes).Value == "Allowed >" ||
		settingRow(t, model, appui.SettingsSubstanceUse).Value != "Blocked" {
		t.Fatalf("unexpected default policy rows: %+v", model.Rows)
	}
	selectSettingsKey(t, model, appui.SettingsAdultContent)
	tagFlow, tagModel, err := flow.Activate(model)
	if err != nil || tagFlow == nil {
		t.Fatalf("open tag flow: %v", err)
	}
	tagModel.Cursor = 1
	tag := tagModel.Rows[1].Label
	if err := tagFlow.Activate(tagModel); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Filter.AdultContent.Disabled) != 1 || cfg.Filter.AdultContent.Disabled[0] != tag {
		t.Fatalf("disabled tags = %v", cfg.Filter.AdultContent.Disabled)
	}
}
