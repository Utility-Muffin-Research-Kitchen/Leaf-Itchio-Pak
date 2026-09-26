//go:build !headless

package ui

import (
	"fmt"
	"os"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// tokenSecretLabel replaces the sign-in key in logs.
const tokenSecretLabel = "[TOKEN]"

// Account owns the itch.io sign-in credential. Every change to it (a new
// sign-in, signing out, or itch.io rejecting it) resets the state derived
// from the previous account: cached bundle sizes and key generation in the
// client, the live owned-game list, and owned_cache.json. Call it only from
// the UI goroutine, which owns cfg.
type Account struct {
	cfg            *settings.Config
	cfgPath        string
	ownedCachePath string
	client         *itchio.Client
	ownedChanged   func([]itchio.OwnedGame)
}

func NewAccount(cfg *settings.Config, cfgPath, ownedCachePath string, client *itchio.Client) *Account {
	return &Account{cfg: cfg, cfgPath: cfgPath, ownedCachePath: ownedCachePath, client: client}
}

// SetOwnedChanged registers the catalogue's live owned-game update
// (CatalogController.ReplaceOwnedGames).
func (account *Account) SetOwnedChanged(callback func([]itchio.OwnedGame)) {
	account.ownedChanged = callback
}

// Store saves the key from a completed sign-in and clears the previous
// account's state. The account name arrives later through Validated.
func (account *Account) Store(token string) error {
	previous := *account.cfg
	account.cfg.AuthToken, account.cfg.AuthUser = token, ""
	account.cfg.LegacyKeyRemoved = false
	if err := account.cfg.Save(account.cfgPath); err != nil {
		*account.cfg = previous
		return fmt.Errorf("save sign-in: %w", err)
	}
	logger.RegisterSecret(token, tokenSecretLabel)
	return account.reset()
}

// SignOut forgets the key locally. itch.io has no revoke endpoint, so the
// key stays valid on the website until the user deletes it there.
func (account *Account) SignOut() error {
	previous := *account.cfg
	account.cfg.AuthToken, account.cfg.AuthUser = "", ""
	if err := account.cfg.Save(account.cfgPath); err != nil {
		*account.cfg = previous
		return fmt.Errorf("sign out: %w", err)
	}
	logger.RemoveSecret(tokenSecretLabel)
	return account.reset()
}

// Validated records the account name and owned games from a successful
// profile check of the current key.
func (account *Account) Validated(user string, owned []itchio.OwnedGame) error {
	if user != "" && user != account.cfg.AuthUser {
		account.cfg.AuthUser = user
		if err := account.cfg.Save(account.cfgPath); err != nil {
			return fmt.Errorf("save account name: %w", err)
		}
	}
	urls := make([]string, 0, len(owned))
	for _, game := range owned {
		urls = append(urls, game.URL)
	}
	if err := itchio.SaveOwnedCache(account.ownedCachePath, urls); err != nil {
		return fmt.Errorf("save owned games: %w", err)
	}
	if account.ownedChanged != nil {
		account.ownedChanged(owned)
	}
	return nil
}

func (account *Account) reset() error {
	account.client.ResetAPIKeyState()
	if account.ownedChanged != nil {
		account.ownedChanged(nil)
	}
	if err := os.Remove(account.ownedCachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove the previous owned-game cache: %w", err)
	}
	return nil
}
