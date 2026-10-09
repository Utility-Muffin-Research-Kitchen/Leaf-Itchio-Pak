package itchio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"golang.org/x/net/html"
)

// SetAuthToken supplies background checks with the current credential. It is
// held only in memory; replacing it invalidates work from the previous account.
func (c *Client) SetAuthToken(token string) {
	c.ownedMu.Lock()
	defer c.ownedMu.Unlock()
	previous := c.authToken.Load()
	if previous == nil && token == "" || previous != nil && *previous == token {
		return
	}
	c.resetAPIKeyStateLocked()
	c.authToken.Store(&token)
}

func (c *Client) AuthToken() string {
	if token := c.authToken.Load(); token != nil {
		return *token
	}
	return ""
}

func (c *Client) AuthGeneration() uint64 { return c.keyGeneration.Load() }

// AuthSnapshot reads the credential and its generation as one coherent state.
func (c *Client) AuthSnapshot() (string, uint64) {
	c.ownedMu.Lock()
	defer c.ownedMu.Unlock()
	return c.AuthToken(), c.keyGeneration.Load()
}

// ApplyIfAuthGeneration publishes completed background work atomically with
// respect to credential replacement. apply must not perform I/O or call Client.
func (c *Client) ApplyIfAuthGeneration(generation uint64, apply func()) bool {
	c.ownedMu.Lock()
	defer c.ownedMu.Unlock()
	if c.keyGeneration.Load() != generation {
		return false
	}
	apply()
	return true
}

// Fingerprint identifies a listed upload without resolving or downloading it.
func (u Upload) Fingerprint() string {
	switch {
	case u.BuildID != 0:
		return "build:" + strconv.FormatInt(u.BuildID, 10)
	case u.MD5 != "":
		return "md5:" + u.MD5
	case !u.UpdatedAt.IsZero():
		return "upd:" + u.UpdatedAt.UTC().Format(time.RFC3339Nano) + "/" + strconv.FormatInt(u.Size, 10)
	default:
		return ""
	}
}

// OwnedKeysForGames gets access keys for installed games without doing the
// full-library bundle enrichment used by the interactive purchase picker.
// Keys are ephemeral and must never be persisted in the inventory.
func (c *Client) OwnedKeysForGames(token string, gameIDs []string) (map[string]string, error) {
	ids := make([]int64, 0, len(gameIDs))
	wanted := make(map[int64]bool, len(gameIDs))
	for _, value := range gameIDs {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid game ID in update check")
		}
		if !wanted[id] {
			wanted[id] = true
			ids = append(ids, id)
		}
	}
	out := make(map[string]string)
	for start := 0; start < len(ids); start += 50 {
		keys, complete, err := c.scanOwnedKeys(context.Background(), token, ids[start:min(start+50, len(ids))])
		if err != nil {
			if errors.Is(err, ErrSignInRejected) {
				return nil, ErrNoAccess
			}
			return nil, err
		}
		if !complete {
			return nil, fmt.Errorf("update ownership lookup exceeded page limit")
		}
		for _, key := range keys {
			if wanted[key.GameID] && key.ID > 0 {
				id := strconv.FormatInt(key.GameID, 10)
				if out[id] == "" {
					out[id] = strconv.FormatInt(key.ID, 10)
				}
			}
		}
	}
	return out, nil
}

// FetchPageUploadNames is the signed-out update fallback. A plain public GET
// reveals new listed names, but cannot detect changed content or hidden files.
// An empty list therefore establishes reachability, not removal.
func (c *Client) FetchPageUploadNames(gameURL string) ([]string, error) {
	resp, err := c.http.Get(gameURL)
	if err != nil {
		return nil, safeRequestError("fetch update page", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusGone:
		return nil, fmt.Errorf("fetch update page: %w", ErrGameRemoved)
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("fetch update page: %w", ErrRateLimited)
	default:
		return nil, fmt.Errorf("fetch update page: HTTP %d", resp.StatusCode)
	}
	const maxPageSize = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageSize+1))
	if err != nil {
		return nil, safeRequestError("read update page", err)
	}
	if len(body) > maxPageSize {
		return nil, fmt.Errorf("update page exceeds size limit")
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("parse update page: %w", err)
	}
	names := make([]string, 0)
	seen := make(map[string]bool)
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inUpload bool) {
		if n.Type == html.ElementNode {
			inUpload = inUpload || nodeHasClass(n, "upload")
			if inUpload && n.Data == "strong" && nodeHasClass(n, "name") {
				name := ""
				for _, attr := range n.Attr {
					if attr.Key == "title" {
						name = strings.TrimSpace(attr.Val)
					}
				}
				if name == "" && n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
					name = strings.TrimSpace(n.FirstChild.Data)
				}
				ext := strings.ToLower(roms.ROMExt(name))
				if name != "" && !seen[name] && (roms.IsSupportedUploadExt(ext) || !isSkippableExt(ext)) &&
					unsupportedSystemOf(name) == "" {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch, inUpload)
		}
	}
	walk(doc, false)
	return names, nil
}
