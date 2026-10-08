//go:build !headless

package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func TestAccountChangesRefreshBackgroundCredentialAndInvalidateScans(t *testing.T) {
	t.Cleanup(func() { logger.RemoveSecret(tokenSecretLabel) })
	dir := t.TempDir()
	cfg := &settings.Config{AuthToken: "previous-token"}
	client := itchio.NewClientWithBase("http://127.0.0.1:1")
	account := NewAccount(cfg, filepath.Join(dir, "config.json"), filepath.Join(dir, "owned.json"), client)
	token, generation := client.AuthSnapshot()
	if token != cfg.Credential() {
		t.Fatal("account did not seed the background token")
	}
	NewAccount(cfg, account.cfgPath, account.ownedCachePath, client) // opening Settings
	if client.AuthGeneration() != generation {
		t.Fatal("opening Settings invalidated an unchanged account scan")
	}
	var checkedTokens []string
	account.SetCredentialChanged(func() {
		token, _ := client.AuthSnapshot()
		checkedTokens = append(checkedTokens, token)
	})
	for _, next := range []string{"approved-token", "approved-token", ""} {
		var err error
		if next == "" {
			err = account.SignOut()
		} else {
			err = account.Store(next)
		}
		if err != nil {
			t.Fatal(err)
		}
		if client.ApplyIfAuthGeneration(generation, func() { t.Error("stale scan was applied") }) {
			t.Fatal("account mutation accepted an older background scan")
		}
		token, generation = client.AuthSnapshot()
		if token != next || cfg.Credential() != next {
			t.Fatal("background credential differs from the saved account")
		}
		loaded, err := settings.Load(account.cfgPath)
		if err != nil || loaded.Credential() != next {
			t.Fatal("account change was not saved")
		}
	}
	if len(checkedTokens) != 3 || checkedTokens[0] != "approved-token" || checkedTokens[1] != "approved-token" || checkedTokens[2] != "" {
		t.Fatal("account changes did not schedule checks with the new token")
	}
	if err := account.Validated("", nil); err != nil {
		t.Fatal(err)
	}
	if len(checkedTokens) != 3 {
		t.Fatal("validation alone restarted the update checker")
	}
}

func TestAccountSaveFailureKeepsBackgroundCredential(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &settings.Config{AuthToken: "existing-token"}
	client := itchio.NewClientWithBase("http://127.0.0.1:1")
	account := NewAccount(cfg, filepath.Join(blocked, "config.json"), filepath.Join(dir, "owned.json"), client)
	_, generation := client.AuthSnapshot()
	account.SetCredentialChanged(func() { t.Error("failed save scheduled an update check") })
	if err := account.Store("unsaved-token"); err == nil {
		t.Fatal("expected failed account save")
	}
	if err := account.SignOut(); err == nil {
		t.Fatal("expected failed sign out save")
	}
	if token, after := client.AuthSnapshot(); token != "existing-token" || after != generation || cfg.Credential() != token {
		t.Fatal("failed save changed the background credential or generation")
	}
}
