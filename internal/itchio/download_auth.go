package itchio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// All authenticated requests use itch.io API v2 on api.itch.io with the key
// in the Authorization header, never in a URL
// (carroarmato0/NextUI-Itchio-Pak#4). The header reaches only that origin:
// download redirects are read, not followed, and CDN requests are separate.

// OwnedKey represents one purchase granting download access to a game.
// A game may appear multiple times (once per purchase transaction) — e.g.
// once for an individual purchase and once from a bundle.
type OwnedKey struct {
	ID         int64     // numeric download key ID — pass as download_key_id
	PurchaseID int64     // ties this key to a specific purchase transaction
	CreatedAt  time.Time // when this purchase was made
	Downloads  int       // how many times this key has been used to download
	// BundleSize is the number of distinct games in the same purchase.
	// 1 = individual purchase; >1 = bundle purchase.
	BundleSize int
	// BundleName is the human-readable bundle name, populated by AnnotateBundleNames.
	// Empty for individual purchases.
	BundleName string
}

// rawOwnedKey is one entry from the API before enrichment.
type rawOwnedKey struct {
	ID         int64
	GameID     int64
	PurchaseID int64
	Downloads  int
	CreatedAt  string
	GameTitle  string
	GameURL    string
}

const ownedKeysMaxPages = 20

var errAPIKeyRejected = errors.New("API key invalid or does not grant access")

// isJSONArray reports whether raw holds a JSON array. itch.io answers an
// empty collection as {} (or omits it) instead of [].
func isJSONArray(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

func (c *Client) newAPIRequest(ctx context.Context, method, rawURL string, body io.Reader, apiKey string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// scanOwnedKeys pages through api.itch.io/profile/owned-keys, restricted to
// gameIDs when any are given. complete is false when the scan stopped early:
// on an error (returned with the keys read so far) or at the page cap.
func (c *Client) scanOwnedKeys(ctx context.Context, apiKey string, gameIDs []int64) (keys []rawOwnedKey, complete bool, err error) {
	for page := 1; page <= ownedKeysMaxPages; page++ {
		query := url.Values{"page": {strconv.Itoa(page)}}
		if len(gameIDs) > 0 {
			ids := make([]string, len(gameIDs))
			for index, id := range gameIDs {
				ids[index] = strconv.FormatInt(id, 10)
			}
			query.Set("game_ids", strings.Join(ids, ","))
		}
		req, err := c.newAPIRequest(ctx, http.MethodGet, c.butler+"/profile/owned-keys?"+query.Encode(), nil, apiKey)
		if err != nil {
			return keys, false, fmt.Errorf("build owned-keys request: %w", err)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return keys, false, safeRequestError(fmt.Sprintf("fetch owned keys page %d", page), err)
		}
		var envelope struct {
			PerPage   int             `json:"per_page"`
			OwnedKeys json.RawMessage `json:"owned_keys"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&envelope)
		resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			logger.Warn("auth: owned-keys HTTP %d (page %d)", resp.StatusCode, page)
			return keys, false, errAPIKeyRejected
		case resp.StatusCode == http.StatusTooManyRequests:
			return keys, false, fmt.Errorf("fetch owned keys: %w", ErrRateLimited)
		case resp.StatusCode != http.StatusOK:
			logger.Error("auth: owned-keys HTTP %d (page %d)", resp.StatusCode, page)
			return keys, false, fmt.Errorf("fetch owned keys: HTTP %d", resp.StatusCode)
		case decodeErr != nil:
			return keys, false, fmt.Errorf("decode owned keys (page %d): %w", page, decodeErr)
		}
		if !isJSONArray(envelope.OwnedKeys) {
			return keys, true, nil // {} or absent: no more pages
		}

		var items []struct {
			ID         int64  `json:"id"`
			GameID     int64  `json:"game_id"`
			PurchaseID int64  `json:"purchase_id"`
			Downloads  int    `json:"downloads"`
			CreatedAt  string `json:"created_at"`
			Game       struct {
				ID    int64  `json:"id"`
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"game"`
		}
		if err := json.Unmarshal(envelope.OwnedKeys, &items); err != nil {
			return keys, false, fmt.Errorf("decode owned keys (page %d): %w", page, err)
		}
		logger.Debug("auth: owned-keys page %d: %d entries", page, len(items))
		for _, item := range items {
			gameID := item.GameID
			if gameID == 0 {
				gameID = item.Game.ID
			}
			keys = append(keys, rawOwnedKey{
				ID: item.ID, GameID: gameID, PurchaseID: item.PurchaseID,
				Downloads: item.Downloads, CreatedAt: item.CreatedAt,
				GameTitle: item.Game.Title, GameURL: item.Game.URL,
			})
		}
		if len(items) == 0 || envelope.PerPage == 0 || len(items) < envelope.PerPage {
			return keys, true, nil
		}
	}
	logger.Warn("auth: owned-keys scan stopped at the %d-page cap", ownedKeysMaxPages)
	return keys, false, nil
}

// purchaseGameCounts counts distinct games per purchase ID, which tells
// bundles (>1) from individual purchases (1).
func purchaseGameCounts(keys []rawOwnedKey) map[int64]int {
	games := map[int64]map[int64]bool{}
	for _, key := range keys {
		if games[key.PurchaseID] == nil {
			games[key.PurchaseID] = map[int64]bool{}
		}
		games[key.PurchaseID][key.GameID] = true
	}
	counts := make(map[int64]int, len(games))
	for purchaseID, ids := range games {
		counts[purchaseID] = len(ids)
	}
	return counts
}

// storePurchaseCounts caches counts from a complete library scan, unless the
// key changed after the scan started.
func (c *Client) storePurchaseCounts(generation uint64, counts map[int64]int) {
	c.ownedMu.Lock()
	defer c.ownedMu.Unlock()
	if generation != c.keyGeneration.Load() {
		logger.Debug("auth: discarded purchase counts from a replaced API key")
		return
	}
	c.purchaseCounts = counts
}

// cachedPurchaseCounts returns the cached counts when they cover every
// purchase in keys, else nil.
func (c *Client) cachedPurchaseCounts(keys []rawOwnedKey) map[int64]int {
	c.ownedMu.Lock()
	defer c.ownedMu.Unlock()
	if c.purchaseCounts == nil {
		return nil
	}
	for _, key := range keys {
		if _, ok := c.purchaseCounts[key.PurchaseID]; !ok {
			return nil
		}
	}
	return c.purchaseCounts
}

// FetchOwnedKeys returns every purchase key the user holds for the given game,
// asking api.itch.io/profile/owned-keys with game_ids. The answer is filtered
// here as well, so it works whether or not the server applies the filter.
//
// BundleSize needs the whole library: a server-filtered answer takes the
// counts cached by the last complete scan under the current key (seeded by
// ValidateAPIKey at startup), and scans the library once more on a miss.
//
// Returns a non-empty slice when the game is owned, or an error when it is
// not owned / the API key is invalid.
func (c *Client) FetchOwnedKeys(apiKey, gameID string) ([]OwnedKey, error) {
	return c.FetchOwnedKeysContext(context.Background(), apiKey, gameID)
}

func (c *Client) FetchOwnedKeysContext(ctx context.Context, apiKey, gameID string) ([]OwnedKey, error) {
	targetID, err := strconv.ParseInt(gameID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid game_id %q: %w", gameID, err)
	}
	generation := c.keyGeneration.Load()

	keys, complete, err := c.scanOwnedKeys(ctx, apiKey, []int64{targetID})
	if err != nil {
		return nil, err
	}
	serverFiltered := len(keys) > 0
	for _, key := range keys {
		if key.GameID != targetID {
			serverFiltered = false
			break
		}
	}

	var counts map[int64]int
	switch {
	case !serverFiltered:
		counts = purchaseGameCounts(keys)
		if complete {
			c.storePurchaseCounts(generation, counts)
		}
	default:
		if counts = c.cachedPurchaseCounts(keys); counts == nil {
			logger.Debug("auth: no cached bundle sizes for this purchase, scanning the owned library")
			library, libraryComplete, err := c.scanOwnedKeys(ctx, apiKey, nil)
			if err != nil {
				return nil, err
			}
			counts = purchaseGameCounts(library)
			if libraryComplete {
				c.storePurchaseCounts(generation, counts)
			}
		}
	}

	var matches []OwnedKey
	for _, key := range keys {
		if key.GameID != targetID {
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, key.CreatedAt)
		// An incomplete scan can miss a purchase; the key itself is one game.
		bundleSize := max(counts[key.PurchaseID], 1)
		matches = append(matches, OwnedKey{
			ID:         key.ID,
			PurchaseID: key.PurchaseID,
			CreatedAt:  createdAt,
			Downloads:  key.Downloads,
			BundleSize: bundleSize,
		})
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("Game not owned or API key invalid (game_id=%s not found in owned keys)", gameID)
	}
	logger.Debug("auth: found %d owned key(s) for game_id=%s (server-filtered=%v)", len(matches), gameID, serverFiltered)
	return matches, nil
}

// AnnotateBundleNames sets BundleName on bundle keys (BundleSize > 1) by
// matching them — sorted by CreatedAt ascending — to bundleNames (in page order).
// Individual keys are skipped. If there are more bundle keys than names, the
// excess keys keep an empty BundleName (displayed as "Bundle purchase" fallback).
func AnnotateBundleNames(keys []OwnedKey, bundleNames []string) []OwnedKey {
	if len(bundleNames) == 0 {
		return keys
	}
	// Collect indices of bundle keys in ascending CreatedAt order.
	type indexedKey struct {
		idx int
		t   time.Time
	}
	var bundleIdxs []indexedKey
	for i, k := range keys {
		if k.BundleSize > 1 {
			bundleIdxs = append(bundleIdxs, indexedKey{i, k.CreatedAt})
		}
	}
	// Sort by CreatedAt ascending so we assign names in the same order they
	// appear on the public game page (oldest bundle first).
	sort.Slice(bundleIdxs, func(i, j int) bool {
		return bundleIdxs[i].t.Before(bundleIdxs[j].t)
	})
	for nameIdx, bi := range bundleIdxs {
		if nameIdx >= len(bundleNames) {
			break
		}
		keys[bi.idx].BundleName = bundleNames[nameIdx]
	}
	return keys
}

// FetchUploadsForKey lists a game's downloadable uploads through
// api.itch.io/games/{id}/uploads. downloadKeyID selects the purchase that
// grants access; pass "" for a free or name-your-own-price game.
func (c *Client) FetchUploadsForKey(apiKey, gameID, downloadKeyID string) ([]Upload, error) {
	return c.FetchUploadsContext(context.Background(), apiKey, gameID, downloadKeyID)
}

func (c *Client) FetchUploadsContext(ctx context.Context, apiKey, gameID, downloadKeyID string) ([]Upload, error) {
	uploadsURL := fmt.Sprintf("%s/games/%s/uploads", c.butler, url.PathEscape(gameID))
	if downloadKeyID != "" {
		uploadsURL += "?" + url.Values{"download_key_id": {downloadKeyID}}.Encode()
	}
	// downloadKeyID not logged — it identifies the user's purchase.
	logger.Debug("auth: fetching upload list for game_id=%s purchase=%s", gameID, presentAbsent(downloadKeyID))

	req, err := c.newAPIRequest(ctx, http.MethodGet, uploadsURL, nil, apiKey)
	if err != nil {
		return nil, fmt.Errorf("build uploads request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, safeRequestError("fetch owned uploads", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusUnauthorized:
		logger.Warn("auth: upload list HTTP %d — key may not grant access to this game", resp.StatusCode)
		return nil, fmt.Errorf("Game not owned or API key does not grant access to this game's downloads")
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("fetch uploads: %w", ErrRateLimited)
	default:
		logger.Error("auth: upload list HTTP %d", resp.StatusCode)
		return nil, fmt.Errorf("fetch uploads: HTTP %d", resp.StatusCode)
	}

	var envelope struct {
		Uploads json.RawMessage `json:"uploads"`
		Errors  []string        `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode uploads response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		logger.Error("auth: upload list rejected: %s", strings.Join(envelope.Errors, "; "))
		return nil, fmt.Errorf("itch.io rejected the upload list request")
	}

	// Only the fields used here are decoded. Unstable ones such as "traits"
	// ({} when empty, an array otherwise) are ignored.
	var items []struct {
		ID       int64  `json:"id"`
		Filename string `json:"filename"`
		Size     int64  `json:"size"`
	}
	if isJSONArray(envelope.Uploads) {
		if err := json.Unmarshal(envelope.Uploads, &items); err != nil {
			return nil, fmt.Errorf("decode uploads array: %w", err)
		}
	} else {
		logger.Debug("auth: uploads field is not an array (%.50s) — treating as empty", envelope.Uploads)
	}

	var uploads []Upload
	for _, u := range items {
		upload := Upload{Filename: u.Filename, UploadID: strconv.FormatInt(u.ID, 10), Size: u.Size}
		ext := strings.ToLower(roms.ROMExt(u.Filename))
		if roms.IsSupportedUploadExt(ext) {
			uploads = append(uploads, upload)
			logger.Debug("auth: found ROM %s id=%d size=%d", u.Filename, u.ID, u.Size)
		} else if !isSkippableExt(ext) {
			upload.NeedsFormat = true
			uploads = append(uploads, upload)
			logger.Debug("auth: found unknown-format %s id=%d (user will choose)", u.Filename, u.ID)
		} else {
			logger.Debug("auth: skipping %s (ext=%q)", u.Filename, ext)
		}
	}

	known := 0
	for _, u := range uploads {
		if !u.NeedsFormat {
			known++
		}
	}
	logger.Debug("auth: %d known ROM(s), %d unknown-format from %d total uploads",
		known, len(uploads)-known, len(items))
	return uploads, nil
}

// createInstallSession opens a download session for one install
// (POST api.itch.io/games/{id}/download-sessions) and returns its UUID. The
// transport never replays this POST.
func (c *Client) createInstallSession(ctx context.Context, apiKey, gameID, downloadKeyID string) (string, error) {
	form := url.Values{}
	if downloadKeyID != "" {
		form.Set("download_key_id", downloadKeyID)
	}
	sessionURL := fmt.Sprintf("%s/games/%s/download-sessions", c.butler, url.PathEscape(gameID))
	req, err := c.newAPIRequest(ctx, http.MethodPost, sessionURL, strings.NewReader(form.Encode()), apiKey)
	if err != nil {
		return "", fmt.Errorf("build install session request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", safeRequestError("create install session", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create install session: HTTP %d", resp.StatusCode)
	}
	var result struct {
		UUID   string   `json:"uuid"`
		Errors []string `json:"errors"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return "", fmt.Errorf("decode install session: %w", err)
	}
	if len(result.Errors) > 0 || result.UUID == "" {
		return "", fmt.Errorf("create install session: no session in response")
	}
	logger.Info("auth: install session opened for game_id=%s", gameID)
	return result.UUID, nil
}

// checkDownloadURL accepts only an absolute URL without user info, over
// HTTPS, or over the API's own scheme so offline tests can use plain HTTP.
func (c *Client) checkDownloadURL(location *url.URL) error {
	allowed := location.Scheme == "https"
	if api, err := url.Parse(c.butler); err == nil && location.Scheme == api.Scheme {
		allowed = true
	}
	if !allowed || location.Host == "" || location.User != nil {
		return fmt.Errorf("resolve authenticated download: unusable download location")
	}
	return nil
}

// ResolveUploadURLContext resolves the signed CDN URL for an upload through
// GET api.itch.io/uploads/{id}/download, within session. The endpoint
// answers with a redirect to the CDN, which is read, not followed: the
// caller gets the URL first for archive inspection or the magic-byte probe,
// and the Authorization header never reaches the CDN.
func (c *Client) ResolveUploadURLContext(ctx context.Context, apiKey, uploadID string, session *InstallSession) (string, error) {
	if session == nil {
		return "", fmt.Errorf("resolve upload %s: no install session", uploadID)
	}
	uuid, err := session.resolveUUID(ctx, func(ctx context.Context, gameID, downloadKeyID string) (string, error) {
		return c.createInstallSession(ctx, apiKey, gameID, downloadKeyID)
	})
	if err != nil {
		return "", err
	}
	query := url.Values{}
	if session.downloadKeyID != "" {
		query.Set("download_key_id", session.downloadKeyID)
	}
	if uuid != "" {
		query.Set("uuid", uuid)
	}
	resolveURL := fmt.Sprintf("%s/uploads/%s/download", c.butler, url.PathEscape(uploadID))
	if len(query) > 0 {
		resolveURL += "?" + query.Encode()
	}
	logger.Debug("auth: resolving CDN for upload id=%s session=%s", uploadID, presentAbsent(uuid))

	req, err := c.newAPIRequest(ctx, http.MethodGet, resolveURL, nil, apiKey)
	if err != nil {
		return "", fmt.Errorf("build resolve request: %w", err)
	}
	noFollow := &http.Client{
		Transport: c.http.Transport,
		Jar:       c.http.Jar,
		Timeout:   c.http.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return "", safeRequestError("resolve authenticated CDN URL", err)
	}
	defer resp.Body.Close()

	var location string
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		location = resp.Header.Get("Location")
		if location == "" {
			logger.Error("auth: CDN resolve HTTP %d without a Location", resp.StatusCode)
			return "", fmt.Errorf("resolve authenticated download: no download location")
		}
	case resp.StatusCode == http.StatusOK:
		// Some deployments answer {"url": ...} instead of redirecting.
		var result struct {
			URL    string   `json:"url"`
			Errors []string `json:"errors"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
			return "", fmt.Errorf("decode auth CDN response: %w", err)
		}
		if len(result.Errors) > 0 {
			logger.Error("auth: CDN resolve rejected: %s", strings.Join(result.Errors, "; "))
			return "", fmt.Errorf("authenticated CDN resolver rejected the request")
		}
		if result.URL == "" {
			logger.Error("auth: empty CDN URL from resolver")
			return "", fmt.Errorf("empty CDN URL from auth resolver")
		}
		location = result.URL
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		logger.Warn("auth: CDN resolve HTTP %d", resp.StatusCode)
		return "", fmt.Errorf("Game not owned or API key does not grant access to this download")
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", fmt.Errorf("resolve authenticated download: %w", ErrRateLimited)
	default:
		logger.Error("auth: CDN resolve HTTP %d", resp.StatusCode)
		return "", fmt.Errorf("auth CDN resolve status %d", resp.StatusCode)
	}

	parsed, err := req.URL.Parse(location)
	if err != nil {
		return "", fmt.Errorf("resolve authenticated download: unusable download location")
	}
	if err := c.checkDownloadURL(parsed); err != nil {
		return "", err
	}
	// The CDN URL carries signed tokens; do not log it.
	return parsed.String(), nil
}

// DownloadUploadContext resolves an upload within session and streams it to
// dest. The CDN request carries no Authorization header.
func (c *Client) DownloadUploadContext(ctx context.Context, apiKey, uploadID string, session *InstallSession, dest string, progress func(int64, int64)) error {
	cdnURL, err := c.ResolveUploadURLContext(ctx, apiKey, uploadID, session)
	if err != nil {
		return err
	}
	logger.Info("auth: streaming to %s", dest)
	return c.streamToFileContext(ctx, cdnURL, dest, progress)
}

// ResolveAuthURL, ResolveAuthURLContext and DownloadAuthUploadContext resolve
// through API v2 without grouping requests into an install session. They
// keep the download flows working until each flow carries one session per
// install.
func (c *Client) ResolveAuthURL(apiKey, uploadID, downloadKeyID string) (string, error) {
	return c.ResolveAuthURLContext(context.Background(), apiKey, uploadID, downloadKeyID)
}

func (c *Client) ResolveAuthURLContext(ctx context.Context, apiKey, uploadID, downloadKeyID string) (string, error) {
	return c.ResolveUploadURLContext(ctx, apiKey, uploadID, NewInstallSession("", downloadKeyID))
}

func (c *Client) DownloadAuthUpload(apiKey, uploadID, downloadKeyID, dest string, progress func(int64, int64)) error {
	return c.DownloadAuthUploadContext(context.Background(), apiKey, uploadID, downloadKeyID, dest, progress)
}

func (c *Client) DownloadAuthUploadContext(ctx context.Context, apiKey, uploadID, downloadKeyID, dest string, progress func(int64, int64)) error {
	return c.DownloadUploadContext(ctx, apiKey, uploadID, NewInstallSession("", downloadKeyID), dest, progress)
}
