package itchio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// APIKeyStatus is the result of the background API key validation.
type APIKeyStatus int32

const (
	APIKeyStatusUnknown  APIKeyStatus = 0 // not yet tested, or network unavailable
	APIKeyStatusWorking  APIKeyStatus = 1 // accepted by itch.io
	APIKeyStatusRejected APIKeyStatus = 2 // explicitly rejected by itch.io
)

// GetAPIKeyStatus returns the cached result of the most recent key check.
func (c *Client) GetAPIKeyStatus() APIKeyStatus {
	return APIKeyStatus(atomic.LoadInt32(&c.apiKeyStatus))
}

// StoreAPIKeyStatus saves the result of a completed key check.
func (c *Client) StoreAPIKeyStatus(s APIKeyStatus) {
	atomic.StoreInt32(&c.apiKeyStatus, int32(s))
}

// MarkAPIKeyCheckStarted atomically marks the background check as started.
// Returns true only on the first call — the caller should then launch the check.
func (c *Client) MarkAPIKeyCheckStarted() bool {
	return atomic.CompareAndSwapInt32(&c.apiKeyChecking, 0, 1)
}

// ResetAPIKeyState clears cached validation state after a key is replaced or
// removed. It never logs or retains the previous credential.
// Validations and owned-library scans already running under the old key
// discard their account-derived results instead of storing them.
func (c *Client) ResetAPIKeyState() {
	c.keyGeneration.Add(1)
	c.ownedMu.Lock()
	c.purchaseCounts = nil
	c.ownedMu.Unlock()
	atomic.StoreInt32(&c.apiKeyStatus, int32(APIKeyStatusUnknown))
	atomic.StoreInt32(&c.apiKeyChecking, 0)
}

// CheckAPIKey does a lightweight /profile fetch to determine whether apiKey is
// accepted. Returns APIKeyStatusWorking on success, APIKeyStatusRejected when
// the server explicitly rejects the key, and APIKeyStatusUnknown on network or
// other transient errors (so the UI can show "PRESENT" rather than "REJECTED").
func (c *Client) CheckAPIKey(apiKey string) APIKeyStatus {
	logger.Debug("validate: background API key check starting")
	req, err := http.NewRequest("GET", c.butler+"/profile", nil)
	if err != nil {
		logger.Error("validate: build profile request: %v", err)
		return APIKeyStatusUnknown
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		logger.Debug("validate: background key check network error (device may be offline): %v", err)
		return APIKeyStatusUnknown
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		logger.Info("validate: API key valid")
		return APIKeyStatusWorking
	case http.StatusUnauthorized, http.StatusForbidden:
		logger.Warn("validate: API key rejected by itch.io (HTTP %d)", resp.StatusCode)
		return APIKeyStatusRejected
	default:
		logger.Warn("validate: background key check unexpected HTTP %d", resp.StatusCode)
		return APIKeyStatusUnknown
	}
}

// OwnedGame is a public summary of a game the user owns.
// Download key IDs are never included here — they grant download access and
// must not appear in logs.
type OwnedGame struct {
	GameID int64
	Title  string
	URL    string
}

// ValidateAPIKey checks that apiKey is valid by fetching the caller's itch.io
// profile, then pages through all owned-game keys and returns the account
// username and the full owned-game list. A complete scan also seeds the
// per-purchase game counts FetchOwnedKeys uses to tell bundles apart, unless
// the key was replaced while it ran.
//
// Each owned game title and public game ID are logged at DEBUG level.
// Download key IDs are never logged.
func (c *Client) ValidateAPIKey(apiKey string) (username string, owned []OwnedGame, err error) {
	generation := c.keyGeneration.Load()

	// Step 1: verify key and fetch username.
	req, err := http.NewRequest("GET", c.butler+"/profile", nil)
	if err != nil {
		return "", nil, fmt.Errorf("build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, safeRequestError("fetch profile", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return "", nil, fmt.Errorf("API key invalid or expired (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("fetch profile: HTTP %d", resp.StatusCode)
	}

	var profileResp struct {
		User struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profileResp); err != nil {
		return "", nil, fmt.Errorf("decode profile: %w", err)
	}
	username = profileResp.User.Username
	if profileResp.User.DisplayName != "" {
		username = profileResp.User.DisplayName
	}
	// The account name is not needed to diagnose authentication and may be a
	// local/private identifier. Keep it in memory for callers but never log it.
	logger.Info("validate: authenticated itch.io account")

	// Step 2: page through all owned-game keys. A failed page keeps the games
	// found so far, as before, but only a complete scan seeds bundle sizes.
	keys, complete, scanErr := c.scanOwnedKeys(context.Background(), apiKey, nil)
	if scanErr != nil {
		logger.Warn("validate: owned-keys scan stopped early: %v", scanErr)
	}
	seen := make(map[int64]bool)
	for _, key := range keys {
		if seen[key.GameID] {
			continue
		}
		seen[key.GameID] = true
		g := OwnedGame{GameID: key.GameID, Title: key.GameTitle, URL: key.GameURL}
		owned = append(owned, g)
		logger.Debug("validate: owned game id=%d %q", g.GameID, g.Title)
	}
	if scanErr == nil && complete {
		c.storePurchaseCounts(generation, purchaseGameCounts(keys))
	}

	logger.Info("validate: %d owned game(s) found", len(owned))
	return username, owned, nil
}
