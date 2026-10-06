package itchio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// ResetAPIKeyState clears account-derived state after the sign-in changes.
// It never logs or retains the previous credential.
// Validations and owned-library scans already running under the old key
// discard their account-derived results instead of storing them.
func (c *Client) ResetAPIKeyState() {
	c.keyGeneration.Add(1)
	c.ownedMu.Lock()
	c.purchaseCounts = nil
	c.ownedMu.Unlock()
}

// hasItchIOErrors reports whether body is itch.io's JSON error answer, a
// non-empty "errors" list, as opposed to a page from something in between.
func hasItchIOErrors(body io.Reader) bool {
	var answer struct {
		Errors []string `json:"errors"`
	}
	data, err := io.ReadAll(io.LimitReader(body, 64<<10))
	return err == nil && json.Unmarshal(data, &answer) == nil && len(answer.Errors) > 0
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
	return c.ValidateAPIKeyContext(context.Background(), apiKey)
}

// ValidateAPIKeyContext is ValidateAPIKey bounded by ctx, including the
// owned-keys scan and any wait before a rate-limit retry.
func (c *Client) ValidateAPIKeyContext(ctx context.Context, apiKey string) (username string, owned []OwnedGame, err error) {
	generation := c.keyGeneration.Load()

	// Step 1: verify key and fetch username.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.butler+"/profile", nil)
	if err != nil {
		return "", nil, fmt.Errorf("build profile request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, safeRequestError("fetch profile", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return "", nil, fmt.Errorf("fetch profile (HTTP 401): %w", ErrSignInRejected)
	case resp.StatusCode == http.StatusForbidden && hasItchIOErrors(resp.Body):
		return "", nil, fmt.Errorf("fetch profile (HTTP 403): %w", ErrSignInRejected)
	case resp.StatusCode == http.StatusForbidden:
		// Not itch.io's own answer (a proxy or Cloudflare page): keep the
		// sign-in, as for any other transient failure.
		logger.Warn("validate: profile HTTP 403 without itch.io errors; keeping the sign-in")
		return "", nil, fmt.Errorf("fetch profile: HTTP 403")
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
	keys, complete, scanErr := c.scanOwnedKeys(ctx, apiKey, nil)
	if ctx.Err() != nil {
		return "", nil, safeRequestError("scan owned keys", ctx.Err())
	}
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
