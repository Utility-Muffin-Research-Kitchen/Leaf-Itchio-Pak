package itchio

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// Sign-in uses itch.io's QR-code login: the OAuth device authorization grant
// with mandatory PKCE (https://itch.io/docs/api/oauth). itch.io enables it per
// app, so an unapproved client gets HTTP 404 from /oauth/device. No client
// secret is involved; the resulting key is a normal itch.io API key sent as
// Authorization: Bearer, and it does not expire.
const (
	// OAuthClientID is Leaf's own itch.io OAuth application. Client IDs are
	// public; the application's secret is never used and must never ship.
	OAuthClientID = "d3db7e0abe68c873577d904cf35cb0bd"
	// OAuthScope covers the profile and owned-game list used to find
	// purchases, and the upload list, install sessions, and downloads.
	OAuthScope = "profile:me profile:owned game:view:uploads"
	// DeviceRedirectURI is the redirect URI itch.io requires for the device
	// grant once an app is approved for it.
	DeviceRedirectURI = "urn:itchio:poll"

	defaultPollInterval = 5 * time.Second
	oauthDevice         = "MINILOONG Pocket 1"
)

var (
	// ErrSignInUnavailable means itch.io has not enabled QR sign-in for
	// Leaf's application yet.
	ErrSignInUnavailable = errors.New("sign-in with itch.io isn't available yet")
	// ErrSignInDenied means the user declined on their phone.
	ErrSignInDenied = errors.New("sign-in was declined on itch.io")
	// ErrSignInExpired means the code timed out or was already used; a new
	// code is needed.
	ErrSignInExpired = errors.New("the sign-in code expired")
	// ErrSignInRejected means itch.io no longer accepts the stored sign-in,
	// usually because the key was revoked on the website.
	ErrSignInRejected = errors.New("itch.io no longer accepts this sign-in")
)

// NewPKCE returns an RFC 7636 verifier (32 random bytes, base64url without
// padding) and its S256 challenge.
func NewPKCE() (verifier, challenge string, err error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", fmt.Errorf("generate PKCE verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(random[:])
	return verifier, PKCEChallenge(verifier), nil
}

// PKCEChallenge is the S256 challenge for verifier.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// DeviceLogin is one sign-in attempt, from the QR code to the key. The device
// code, PKCE verifier, and approval code are never logged or returned.
type DeviceLogin struct {
	// UserCode is shown beside the QR code; the phone shows the same code.
	UserCode string
	// QRURL is the approval page with the code filled in.
	QRURL string
	// ManualURL is the approval page for people who type the code instead.
	ManualURL string
	// Expires is when the code stops working.
	Expires time.Time

	client     *Client
	deviceCode string
	verifier   string
	interval   time.Duration
	sleep      func(ctx context.Context, d time.Duration) error // replaced in tests
}

// BeginDeviceLogin asks itch.io for a sign-in code.
func (c *Client) BeginDeviceLogin(ctx context.Context) (*DeviceLogin, error) {
	verifier, challenge, err := NewPKCE()
	if err != nil {
		return nil, err
	}
	var resp struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int64  `json:"expires_in"`
		Interval                int64  `json:"interval"`
	}
	status, err := c.oauthPost(ctx, "/oauth/device", url.Values{
		"client_id":             {OAuthClientID},
		"scope":                 {OAuthScope},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}, &resp)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNotFound:
		logger.Warn("oauth: itch.io has not enabled QR sign-in for client %s", OAuthClientID)
		return nil, ErrSignInUnavailable
	case status == http.StatusTooManyRequests:
		return nil, fmt.Errorf("start sign-in: %w", ErrRateLimited)
	case status != http.StatusOK:
		return nil, fmt.Errorf("start sign-in: HTTP %d", status)
	case resp.DeviceCode == "" || resp.UserCode == "" || resp.VerificationURIComplete == "" || resp.ExpiresIn <= 0:
		return nil, fmt.Errorf("start sign-in: incomplete response from itch.io")
	}
	interval := time.Duration(resp.Interval) * time.Second
	if interval <= 0 {
		interval = defaultPollInterval
	}
	logger.Info("oauth: sign-in code issued, expires in %ds, polling every %s", resp.ExpiresIn, interval)
	return &DeviceLogin{
		UserCode:   resp.UserCode,
		QRURL:      resp.VerificationURIComplete,
		ManualURL:  resp.VerificationURI,
		Expires:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		client:     c,
		deviceCode: resp.DeviceCode,
		verifier:   verifier,
		interval:   interval,
		sleep:      waitForRetry,
	}, nil
}

// Wait polls until the user decides, then exchanges the approval for a key.
// It returns ErrSignInDenied or ErrSignInExpired for those outcomes and the
// context's error when cancelled. No request outlives the code.
func (l *DeviceLogin) Wait(ctx context.Context) (string, error) {
	ctx, cancel := context.WithDeadline(ctx, l.Expires)
	defer cancel()
	for {
		if !time.Now().Before(l.Expires) {
			logger.Info("oauth: sign-in code expired")
			return "", ErrSignInExpired
		}
		code, approved, err := l.poll(ctx)
		if err != nil {
			return "", l.outcome(ctx, err)
		}
		if approved {
			key, err := l.exchange(ctx, code)
			return key, l.outcome(ctx, err)
		}
		// Wait after each answer rather than on a fixed timer, in case
		// itch.io turns the poll into a long poll.
		if err := l.sleep(ctx, l.interval); err != nil {
			return "", l.outcome(ctx, err)
		}
	}
}

// outcome reports the code's own deadline as an expiry, and leaves the
// caller's cancellation and every other error as they are.
func (l *DeviceLogin) outcome(ctx context.Context, err error) error {
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) && !time.Now().Before(l.Expires) {
		return ErrSignInExpired
	}
	return err
}

// poll asks once; approved is true with the approval code.
func (l *DeviceLogin) poll(ctx context.Context) (code string, approved bool, err error) {
	var resp struct {
		Status   string   `json:"status"`
		Code     string   `json:"code"`
		Interval int64    `json:"interval"`
		Errors   []string `json:"errors"`
	}
	status, err := l.client.oauthPost(ctx, "/oauth/device/poll", url.Values{
		"client_id":   {OAuthClientID},
		"device_code": {l.deviceCode},
	}, &resp)
	switch {
	case err != nil:
		return "", false, err
	case status == http.StatusTooManyRequests:
		l.interval *= 2
		logger.Info("oauth: polling too fast, slowing to every %s", l.interval)
		return "", false, nil
	case status == http.StatusBadRequest && slices.Contains(resp.Errors, "invalid_grant"):
		return "", false, ErrSignInExpired
	case status != http.StatusOK:
		return "", false, fmt.Errorf("check sign-in: HTTP %d", status)
	}
	switch resp.Status {
	case "pending":
		if resp.Interval > 0 {
			l.interval = time.Duration(resp.Interval) * time.Second
		}
		return "", false, nil
	case "approved":
		if resp.Code == "" {
			return "", false, fmt.Errorf("check sign-in: approval without a code")
		}
		logger.Info("oauth: sign-in approved")
		return resp.Code, true, nil
	case "denied":
		logger.Info("oauth: sign-in declined")
		return "", false, ErrSignInDenied
	case "expired":
		return "", false, ErrSignInExpired
	default:
		return "", false, fmt.Errorf("check sign-in: unexpected status from itch.io")
	}
}

// exchange trades the single-use approval code for a key, straight after
// approval.
func (l *DeviceLogin) exchange(ctx context.Context, code string) (string, error) {
	var resp struct {
		AccessToken string   `json:"access_token"`
		Errors      []string `json:"errors"`
	}
	status, err := l.client.oauthPost(ctx, "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {l.verifier},
		"redirect_uri":  {DeviceRedirectURI},
		"client_id":     {OAuthClientID},
		"device_info":   {l.client.deviceInfo()},
	}, &resp)
	switch {
	case err != nil:
		return "", err
	case status == http.StatusBadRequest && slices.Contains(resp.Errors, "invalid_grant"):
		return "", ErrSignInExpired
	case status != http.StatusOK || resp.AccessToken == "":
		return "", fmt.Errorf("finish sign-in: HTTP %d", status)
	}
	logger.RegisterSecret(resp.AccessToken, "[TOKEN]")
	logger.Info("oauth: signed in")
	return resp.AccessToken, nil
}

// deviceInfo labels the key on itch.io's side. It names the fixed handheld
// and app only; no firmware or device detection is involved.
func (c *Client) deviceInfo() string {
	return oauthDevice + ", " + strings.Replace(c.product, "/", " ", 1)
}

// oauthPost sends a form POST to api.itch.io and decodes a JSON answer into
// out whatever the status. Only ctx bounds it (Wait ties that to the code's
// expiry), so a poll may become a long poll. The rate-limit transport never
// replays these POSTs.
func (c *Client) oauthPost(ctx context.Context, path string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.butler+path, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("build sign-in request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Transport: c.http.Transport}).Do(req)
	if err != nil {
		return 0, safeRequestError("sign in", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if len(body) > 0 && json.Unmarshal(body, out) != nil {
		logger.Debug("oauth: %s answered HTTP %d without JSON", path, resp.StatusCode)
	}
	return resp.StatusCode, nil
}
