package itchio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// knownNonROMExts lists extensions that are definitely not supported ROM/disc files.
// Uploads with these extensions are silently dropped when scanning a game's
// upload list, except the ones roms.UnsupportedSystem names (".nds", ".exe"
// and the like): those are kept and marked with their system, so the picker
// can say why it offers nothing. Anything not in this map (including no
// extension, version-number suffixes like ".0", and ".zip") is returned with
// NeedsFormat=true so the user can classify it manually.
var knownNonROMExts = map[string]bool{
	".tar": true, ".gz": true, ".rar": true, ".bz2": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".webp": true,
	".mp3": true, ".ogg": true, ".wav": true, ".flac": true, ".aac": true,
	".pdf": true, ".txt": true, ".md": true, ".epub": true, ".mobi": true,
	".mp4": true, ".avi": true, ".mkv": true, ".mov": true,
	".exe": true, ".dmg": true, ".apk": true,
	".pocket": true, ".nes": true, ".nds": true, ".sfc": true, ".smc": true,
}

func isSkippableExt(ext string) bool {
	return knownNonROMExts[strings.ToLower(ext)]
}

// unsupportedSystemOf names the system filename is for, when the app cannot
// install that system, or returns "". The extension at the end of the name
// decides first, so ".nds" wins even though it is also a skippable one. A
// name whose extension is not one that is known either way is searched for an
// extension inside it, as in "Hidden_palace.nds v0.1 (Post-jam bug fix)".
// Supported formats never count, and neither does a name that is plainly
// another kind of file, such as "notes.nds.txt".
func unsupportedSystemOf(filename string) string {
	ext := strings.ToLower(roms.ROMExt(filename))
	switch {
	case roms.IsSupportedUploadExt(ext):
		return ""
	case roms.UnsupportedSystem(filename) != "":
		return roms.UnsupportedSystem(filename)
	case isSkippableExt(ext):
		return ""
	}
	return roms.UnsupportedSystemInName(filename)
}

// classifyByName decides how an upload listed by name is offered, and reports
// whether it is kept at all: a supported format as it is, a file for a system
// the app cannot install marked with that system, a known non-ROM dropped,
// and anything else flagged for you to classify.
func (u *Upload) classifyByName() (keep bool) {
	ext := strings.ToLower(roms.ROMExt(u.Filename))
	if roms.IsSupportedUploadExt(ext) {
		return true
	}
	if system := unsupportedSystemOf(u.Filename); system != "" {
		u.UnsupportedSystem = system
		return true
	}
	if isSkippableExt(ext) {
		return false
	}
	u.NeedsFormat = true
	return true
}

// presentAbsent returns "present" when s is non-empty, "absent" otherwise.
// Used to log whether a token exists without logging its value.
func presentAbsent(s string) string {
	if s != "" {
		return "present"
	}
	return "absent"
}

// FetchUploads returns the supported ROM, disc-image, and archive files
// available for free download.
//
// Flow:
//  1. GET game page → CSRF token
//  2. POST gameURL/download_url → signed download page URL containing the key
//  3. GET signed page → parse upload IDs + filenames via ParseDownloadPage
//  4. Construct a resolver URL for each upload: gameURL/file/UPLOAD_ID?key=KEY
//
// The resolver URL is stored as Upload.URL. Pass it to DownloadFree to resolve
// the actual CDN link and stream the file.
func (c *Client) FetchUploads(gameURL string) ([]Upload, error) {
	return c.FetchWebUploadsContext(context.Background(), gameURL)
}

// FetchWebUploadsContext is FetchUploads bounded by ctx, so leaving the
// download screen stops every step.
func (c *Client) FetchWebUploadsContext(ctx context.Context, gameURL string) ([]Upload, error) {
	// Step 1: get CSRF token from game page
	pageReq, err := http.NewRequestWithContext(ctx, http.MethodGet, gameURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build game page request: %w", err)
	}
	resp, err := c.http.Do(pageReq)
	if err != nil {
		return nil, safeRequestError("fetch game page", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return nil, netlimit.FromResponse("uploads: game page", resp)
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		resp.Body.Close()
		logger.Error("uploads: game page HTTP %d", resp.StatusCode)
		return nil, fmt.Errorf("fetch game page: %w", ErrGameRemoved)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		logger.Error("uploads: game page HTTP %d", resp.StatusCode)
		return nil, fmt.Errorf("fetch game page: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("read game page: %w", err)
	}

	csrfM := csrfRegex.FindStringSubmatch(string(body))
	if len(csrfM) < 2 {
		return nil, fmt.Errorf("csrf_token not found on game page")
	}
	csrf := csrfM[1]

	// Step 2: POST to get the signed download page URL
	postURL := strings.TrimRight(gameURL, "/") + "/download_url"
	form := url.Values{"csrf_token": {csrf}, "suggested_amount": {"0"}}
	postReq, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build download_url request: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postResp, err := c.http.Do(postReq)
	if err != nil {
		return nil, safeRequestError("download_url POST", err)
	}
	defer postResp.Body.Close()
	if postResp.StatusCode == http.StatusTooManyRequests {
		return nil, netlimit.FromResponse("uploads: download_url POST", postResp)
	}
	if postResp.StatusCode != http.StatusOK {
		logger.Error("uploads: download_url POST HTTP %d", postResp.StatusCode)
		return nil, fmt.Errorf("download_url POST: HTTP %d", postResp.StatusCode)
	}

	var dlResult struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(postResp.Body).Decode(&dlResult); err != nil {
		return nil, fmt.Errorf("parse download_url response: %w", err)
	}
	if dlResult.URL == "" {
		return nil, ErrNoWebDownload
	}
	// The signed URL contains a download key — do not log it.
	logger.Debug("uploads: signed download URL received")

	// Step 3: extract the download key from the signed URL path
	// Format: https://author.itch.io/game/download/KEY
	key := extractDownloadKey(dlResult.URL)
	if key == "" {
		return nil, fmt.Errorf("could not extract download key from signed URL")
	}
	// The key value is sensitive — do not log it.
	logger.Debug("uploads: download key extracted")

	// Step 4: parse the signed download page for upload IDs + filenames + CSRF token
	dlPage, err := c.parseDownloadPage(ctx, dlResult.URL)
	if errors.Is(err, ErrRateLimited) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("parse download page: %w", err)
	}

	// Step 5: construct resolver URLs for each .gb/.gbc upload.
	// The resolver URL embeds both the download key and the signed-page CSRF token
	// so that DownloadFree can include both in its POST body.
	base := strings.TrimRight(gameURL, "/")
	var uploads []Upload
	for _, u := range dlPage.Uploads {
		resolverURL := base + "/file/" + u.UploadID +
			"?key=" + url.QueryEscape(key) +
			"&csrf=" + url.QueryEscape(dlPage.CSRFToken)
		logger.Debug("uploads: found %s id=%s", u.Filename, u.UploadID)
		uploads = append(uploads, Upload{
			Filename:          u.Filename,
			UploadID:          u.UploadID,
			URL:               resolverURL,
			NeedsFormat:       u.NeedsFormat,
			UnsupportedSystem: u.UnsupportedSystem,
		})
	}
	return uploads, nil
}

// extractDownloadKey pulls the last path segment from a signed download URL.
// e.g. "https://author.itch.io/game/download/ABCDEF" → "ABCDEF"
//
// Uses EscapedPath (not Path) so that %2F-encoded slashes within the key are
// not treated as path separators before the final segment is URL-decoded.
func extractDownloadKey(signedURL string) string {
	parsed, err := url.Parse(signedURL)
	if err != nil {
		return ""
	}
	rawPath := parsed.EscapedPath()
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	key, err := url.PathUnescape(parts[len(parts)-1])
	if err != nil {
		return parts[len(parts)-1]
	}
	return key
}

// extractKeyID parses the itch.io download key JWT and returns the numeric
// download key ID embedded in its payload.
func extractKeyID(jwtKey string) string {
	dotIdx := strings.Index(jwtKey, ".")
	if dotIdx < 0 {
		return ""
	}
	payload := jwtKey[:dotIdx]
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return ""
		}
	}
	var p struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(data, &p); err != nil || p.ID == 0 {
		return ""
	}
	return fmt.Sprintf("%d", p.ID)
}

func (c *Client) ResolveFreeURL(upload Upload) (string, error) {
	return c.ResolveFreeURLContext(context.Background(), upload)
}

func (c *Client) ResolveFreeURLContext(ctx context.Context, upload Upload) (string, error) {
	// Parse the resolver URL to extract base path, key, and csrf.
	parsed, err := url.Parse(upload.URL)
	if err != nil {
		return "", fmt.Errorf("parse resolver URL: %w", err)
	}
	key := parsed.Query().Get("key")
	csrf := parsed.Query().Get("csrf")

	keyID := extractKeyID(key)
	baseURL := parsed.Scheme + "://" + parsed.Host + parsed.Path
	// Log token presence only — never log CSRF or key values.
	logger.Debug("uploads: POST resolver csrf=%s key=%s", presentAbsent(csrf), presentAbsent(key))

	form := url.Values{"csrf_token": {csrf}, "download_key_id": {keyID}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("build resolver request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", safeRequestError("resolve CDN URL", err)
	}
	defer resp.Body.Close()

	rawBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", fmt.Errorf("read resolver response: %w", readErr)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", netlimit.FromResponse("uploads: resolver", resp)
	}
	if resp.StatusCode != http.StatusOK {
		logger.Error("uploads: resolver HTTP %d: %.200s", resp.StatusCode, rawBody)
		return "", fmt.Errorf("resolve CDN URL: HTTP %d", resp.StatusCode)
	}

	var result struct {
		URL    string   `json:"url"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal(rawBody, &result); err != nil {
		logger.Error("uploads: parse resolver response: %v (body: %.200s)", err, rawBody)
		return "", fmt.Errorf("parse CDN URL response: %w", err)
	}
	if len(result.Errors) > 0 {
		logger.Error("uploads: resolver error: %s", strings.Join(result.Errors, "; "))
		return "", fmt.Errorf("resolver rejected the download request")
	}
	if result.URL == "" {
		logger.Error("uploads: empty CDN URL from resolver (file may require purchase)")
		return "", fmt.Errorf("empty CDN URL from resolver (file may require purchase)")
	}

	// CDN URL may contain signed tokens — do not log it.
	return result.URL, nil
}

// DownloadFree resolves the CDN URL for a free game upload and streams it.
//
// upload.URL must be a resolver endpoint of the form:
//
//	gameURL/file/UPLOAD_ID?key=KEY&csrf=CSRF
func (c *Client) DownloadFree(upload Upload, dest string, progress func(int64, int64)) error {
	return c.DownloadFreeContext(context.Background(), upload, dest, progress)
}

func (c *Client) DownloadFreeContext(ctx context.Context, upload Upload, dest string, progress func(int64, int64)) error {
	return c.streamFreshURL(ctx, func(ctx context.Context) (string, error) {
		return c.ResolveFreeURLContext(ctx, upload)
	}, dest, progress)
}

// streamFreshURL resolves a signed CDN URL and streams it to dest. The
// transport never replays a CDN 429, because the signed URL can expire
// during the cooldown. Instead this waits the cooldown out, resolves a fresh
// URL and tries once more; a second 429 is returned.
func (c *Client) streamFreshURL(ctx context.Context, resolve func(context.Context) (string, error), dest string, progress func(int64, int64)) error {
	for attempt := 0; ; attempt++ {
		cdnURL, err := resolve(ctx)
		if err != nil {
			return err
		}
		err = c.streamToFileContext(ctx, cdnURL, dest, progress)
		var limited *RateLimitedError
		limiter := c.rateLimiter()
		if attempt > 0 || !errors.As(err, &limited) || limiter == nil || limiter.replays(limited.Host) {
			return err
		}
		logger.Info("stream: %s is rate limiting; resolving a fresh URL after its cooldown", limited.Host)
		if err := limiter.waitTurn(ctx, limited.Host); err != nil {
			return err
		}
	}
}

func (c *Client) streamToFile(srcURL, dest string, progress func(int64, int64)) error {
	return c.streamToFileContext(context.Background(), srcURL, dest, progress)
}

const streamIdleTimeout = 30 * time.Second

type downloadIdleTimeout struct{}

func (downloadIdleTimeout) Error() string { return ErrDownloadStalled.Error() }
func (downloadIdleTimeout) Unwrap() error { return os.ErrDeadlineExceeded }
func (downloadIdleTimeout) Is(target error) bool {
	return target == ErrDownloadStalled
}

// idleGuard bounds a silent connection, not the duration of a healthy download.
// Its deadline also makes an already-running timer callback harmless after a
// reset or pause: time.Timer.Stop cannot stop that callback on its own.
type idleGuard struct {
	mu       sync.Mutex
	timer    *time.Timer
	deadline time.Time
	timeout  time.Duration
}

func newIdleGuard(timeout time.Duration, cancel func()) *idleGuard {
	g := &idleGuard{timeout: timeout, deadline: time.Now().Add(timeout)}
	g.timer = time.AfterFunc(timeout, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if !g.deadline.IsZero() && !time.Now().Before(g.deadline) {
			g.deadline = time.Time{}
			cancel()
		}
	})
	return g
}

func (g *idleGuard) reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.deadline = time.Now().Add(g.timeout)
	g.timer.Reset(g.timeout)
}

func (g *idleGuard) pause() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.deadline = time.Time{}
	g.timer.Stop()
}

// stalled reports whether a download request or body read failed because the
// connection went quiet: the idle guard fired, or a transport timeout fired
// first (the h1 response-header timeout, or the h2 ping that ends in "client
// connection lost"). Caller cancellation is never a stall.
func stalled(ctx context.Context, err error) bool {
	if context.Cause(ctx) == (downloadIdleTimeout{}) {
		return true
	}
	if ctx.Err() != nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return strings.Contains(err.Error(), "http2: client connection lost")
}

// stallError logs the cause of a stall and returns the message you see. The
// request URL, which may be signed, stays out of the log.
func stallError(ctx context.Context, downloaded int64, err error) error {
	cause := fmt.Sprintf("no data for %s", streamIdleTimeout)
	if context.Cause(ctx) != (downloadIdleTimeout{}) {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		cause = err.Error()
	}
	logger.Warn("stream: stalled after %d bytes: %s", downloaded, cause)
	return downloadIdleTimeout{}
}

func (c *Client) streamToFileContext(ctx context.Context, srcURL, dest string, progress func(int64, int64)) error {
	lease, guardErr := leaf.BeginOperation(ctx, "HTTP body write", false)
	if guardErr != nil {
		return fmt.Errorf("protect HTTP body write: %w", guardErr)
	}
	defer lease.Release()
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	idle := newIdleGuard(streamIdleTimeout, func() { cancel(downloadIdleTimeout{}) })
	defer idle.pause()
	ctx = withCooldownHooks(ctx, idle.pause, idle.reset)

	// c.http has a 30-second Timeout that covers the entire response body read —
	// fine for API calls but fatal for large file downloads. Create a per-call
	// client with no overall timeout (Timeout: 0) that shares the same
	// transport so UA injection, h2/h1 fallback and dial timeouts still apply.
	dlClient := &http.Client{
		Transport:     c.http.Transport,
		Jar:           c.http.Jar,
		CheckRedirect: c.http.CheckRedirect,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srcURL, nil)
	if err != nil {
		return fmt.Errorf("build file request: %w", err)
	}
	resp, err := dlClient.Do(req)
	if err != nil {
		if stalled(ctx, err) {
			return stallError(ctx, 0, err)
		}
		return safeRequestError("fetch file", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return netlimit.FromResponse("stream", resp)
	}
	if resp.StatusCode != http.StatusOK {
		logger.Error("stream: HTTP %d fetching file", resp.StatusCode)
		return fmt.Errorf("file download status %d", resp.StatusCode)
	}

	// Log the destination and size but not the CDN source URL (may contain tokens).
	if resp.ContentLength >= 0 {
		logger.Info("stream: → %s (%d bytes)", dest, resp.ContentLength)
	} else {
		logger.Info("stream: → %s (unknown size)", dest)
	}

	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".itchio-download-*.part")
	if err != nil {
		return fmt.Errorf("create download temp: %w", err)
	}
	tmpPath := tmp.Name()
	logger.Info("stream: writing %s", tmpPath)
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	total := resp.ContentLength
	if err := leaf.RequireFreeSpace(dir, total); err != nil {
		return fmt.Errorf("download storage preflight: %w", err)
	}
	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			idle.reset()
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				logger.Error("stream: write error after %d bytes: %v", downloaded, werr)
				return fmt.Errorf("write: %w", werr)
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if stalled(ctx, err) {
				return stallError(ctx, downloaded, err)
			}
			// Not a stall, so the caller cancelled: an expected end, not a failure.
			if ctx.Err() != nil {
				logger.Info("stream: cancelled after %d bytes", downloaded)
				return fmt.Errorf("read stream: %w", err)
			}
			logger.Error("stream: read error after %d bytes: %v", downloaded, err)
			return fmt.Errorf("read stream: %w", err)
		}
	}
	idle.pause()
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync download temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close download temp: %w", err)
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return fmt.Errorf("commit download: %w", err)
	}
	committed = true
	logger.Info("stream: done, wrote %d bytes", downloaded)
	return nil
}

// FetchFileHeader fetches the first n bytes of a CDN URL via an HTTP Range
// request. Falls back to reading the start of a full response when the server
// does not honour Range. Used for magic-byte detection before a full download.
func (c *Client) FetchFileHeader(cdnURL string, n int) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, cdnURL, nil)
	if err != nil {
		return nil, fmt.Errorf("header fetch: %w", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", n-1))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, safeRequestError("header fetch", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, netlimit.FromResponse("header fetch", resp)
	}
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		logger.Error("header fetch: HTTP %d", resp.StatusCode)
		return nil, fmt.Errorf("header fetch: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(n)))
	if err != nil {
		return nil, fmt.Errorf("header fetch: read: %w", err)
	}
	// cdnURL may contain signed credentials; never include it in logs.
	logger.Debug("header fetch: read %d bytes", len(data))
	return data, nil
}
