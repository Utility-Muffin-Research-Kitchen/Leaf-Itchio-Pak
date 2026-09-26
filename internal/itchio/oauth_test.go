package itchio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

func TestPKCEChallengeMatchesRFC7636(t *testing.T) {
	// RFC 7636, Appendix B.
	if got := PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge = %q", got)
	}
	verifier, challenge, err := NewPKCE()
	if err != nil || len(verifier) != 43 || strings.ContainsAny(verifier, "+/=") || challenge != PKCEChallenge(verifier) {
		t.Fatalf("NewPKCE = %q, %q, %v", verifier, challenge, err)
	}
}

// fakeOAuth is an offline api.itch.io for the device grant. Each poll takes
// the next scripted answer; the last one repeats.
type fakeOAuth struct {
	t     *testing.T
	srv   *httptest.Server
	start func(w http.ResponseWriter, form url.Values)
	polls []func(w http.ResponseWriter)
	token func(w http.ResponseWriter, form url.Values)

	mu        sync.Mutex
	startForm url.Values
	pollCount int
	tokenForm url.Values
}

func newFakeOAuth(t *testing.T) *fakeOAuth {
	f := &fakeOAuth{t: t}
	f.start = func(w http.ResponseWriter, _ url.Values) {
		writeJSON(w, http.StatusOK, map[string]any{
			"device_code": "device-secret-6a1f", "user_code": "ABCD-1234",
			"verification_uri": "https://itch.io/device", "verification_uri_complete": "https://itch.io/device?code=ABCD-1234",
			"expires_in": 600, "interval": 5,
		})
	}
	f.token = func(w http.ResponseWriter, _ url.Values) {
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "token-secret-9c3e", "token_type": "bearer", "scope": OAuthScope})
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("%s %s with %q, want a form POST", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		if r.URL.RawQuery != "" {
			t.Errorf("%s carried a query string", r.URL.Path)
		}
		r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/oauth/device":
			f.startForm = r.PostForm
			f.start(w, r.PostForm)
		case "/oauth/device/poll":
			if r.PostForm.Get("device_code") != "device-secret-6a1f" || r.PostForm.Get("client_id") != OAuthClientID {
				t.Errorf("poll form = %v", r.PostForm)
			}
			answer := f.polls[min(f.pollCount, len(f.polls)-1)]
			f.pollCount++
			answer(w)
		case "/oauth/token":
			f.tokenForm = r.PostForm
			f.token(w, r.PostForm)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func pollStatus(status string, extra map[string]any) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		body := map[string]any{"status": status}
		for key, value := range extra {
			body[key] = value
		}
		writeJSON(w, http.StatusOK, body)
	}
}

func (f *fakeOAuth) client() *Client { return NewClientWithBaseAndButler(f.srv.URL, f.srv.URL) }

// begin starts a login whose sleeps are recorded instead of taken.
func (f *fakeOAuth) begin(t *testing.T) (*DeviceLogin, *[]time.Duration) {
	t.Helper()
	login, err := f.client().BeginDeviceLogin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var slept []time.Duration
	login.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return ctx.Err()
	}
	return login, &slept
}

func TestBeginDeviceLoginSendsPKCEAndReturnsTheCode(t *testing.T) {
	f := newFakeOAuth(t)
	login, _ := f.begin(t)
	form := f.startForm
	if form.Get("client_id") != OAuthClientID || form.Get("scope") != OAuthScope || form.Get("code_challenge_method") != "S256" {
		t.Fatalf("start form = %v", form)
	}
	if form.Get("code_challenge") != PKCEChallenge(login.verifier) || form.Has("code_verifier") || form.Has("client_secret") {
		t.Fatal("start request must carry only the S256 challenge, never the verifier or a secret")
	}
	if login.UserCode != "ABCD-1234" || login.QRURL != "https://itch.io/device?code=ABCD-1234" ||
		login.ManualURL != "https://itch.io/device" || login.interval != 5*time.Second {
		t.Fatalf("login = %+v", login)
	}
	if left := time.Until(login.Expires); left < 9*time.Minute || left > 10*time.Minute {
		t.Fatalf("expires in %v, want about 10 minutes", left)
	}
}

func TestBeginDeviceLoginErrors(t *testing.T) {
	for name, test := range map[string]struct {
		answer func(http.ResponseWriter, url.Values)
		want   error
	}{
		"client not approved": {func(w http.ResponseWriter, _ url.Values) { http.NotFound(w, nil) }, ErrSignInUnavailable},
		"rate limited": {func(w http.ResponseWriter, _ url.Values) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		}, ErrRateLimited},
		"incomplete": {func(w http.ResponseWriter, _ url.Values) {
			writeJSON(w, http.StatusOK, map[string]any{"device_code": "x"})
		}, nil},
	} {
		f := newFakeOAuth(t)
		f.start = test.answer
		_, err := f.client().BeginDeviceLogin(context.Background())
		if err == nil || test.want != nil && !errors.Is(err, test.want) {
			t.Errorf("%s: err = %v, want %v", name, err, test.want)
		}
	}
}

func TestDeviceLoginPollsUntilApprovedThenExchanges(t *testing.T) {
	f := newFakeOAuth(t)
	f.polls = []func(http.ResponseWriter){
		pollStatus("pending", nil),
		pollStatus("pending", map[string]any{"interval": 3}),
		pollStatus("approved", map[string]any{"code": "approval-secret-1d7b"}),
	}
	login, slept := f.begin(t)
	token, err := login.Wait(context.Background())
	if err != nil || token != "token-secret-9c3e" {
		t.Fatalf("Wait = %q, %v", token, err)
	}
	if want := []time.Duration{5 * time.Second, 3 * time.Second}; fmt.Sprint(*slept) != fmt.Sprint(want) {
		t.Fatalf("slept %v, want %v (adopting the server's interval)", *slept, want)
	}
	form := f.tokenForm
	for key, want := range map[string]string{
		"grant_type": "authorization_code", "code": "approval-secret-1d7b", "code_verifier": login.verifier,
		"redirect_uri": DeviceRedirectURI, "client_id": OAuthClientID,
		"device_info": "MINILOONG Pocket 1, Leaf-Itchio-Pak dev",
	} {
		if form.Get(key) != want {
			t.Errorf("token %s = %q, want %q", key, form.Get(key), want)
		}
	}
	if form.Has("client_secret") {
		t.Error("token exchange sent a client secret")
	}
}

func TestDeviceLoginOutcomes(t *testing.T) {
	invalidGrant := func(w http.ResponseWriter) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": []string{"invalid_grant"}})
	}
	for name, test := range map[string]struct {
		poll  func(http.ResponseWriter)
		token func(http.ResponseWriter, url.Values)
		want  error
	}{
		"denied":                 {poll: pollStatus("denied", nil), want: ErrSignInDenied},
		"expired":                {poll: pollStatus("expired", nil), want: ErrSignInExpired},
		"unknown device code":    {poll: invalidGrant, want: ErrSignInExpired},
		"approval already spent": {poll: pollStatus("approved", map[string]any{"code": "c"}), token: func(w http.ResponseWriter, _ url.Values) { invalidGrant(w) }, want: ErrSignInExpired},
		"server error":           {poll: func(w http.ResponseWriter) { http.Error(w, "", http.StatusBadGateway) }},
	} {
		f := newFakeOAuth(t)
		f.polls = []func(http.ResponseWriter){test.poll}
		if test.token != nil {
			f.token = test.token
		}
		login, _ := f.begin(t)
		token, err := login.Wait(context.Background())
		if token != "" || err == nil || test.want != nil && !errors.Is(err, test.want) {
			t.Errorf("%s: Wait = %q, %v; want %v", name, token, err, test.want)
		}
	}
}

// A 429 while polling slows down instead of failing, and the POST is never
// replayed by the transport.
func TestDeviceLoginSlowsDownOnHTTP429(t *testing.T) {
	f := newFakeOAuth(t)
	f.polls = []func(http.ResponseWriter){
		func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		},
		pollStatus("approved", map[string]any{"code": "c"}),
	}
	login, slept := f.begin(t)
	if _, err := login.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*slept) != 1 || (*slept)[0] != 10*time.Second || f.pollCount != 2 {
		t.Fatalf("slept %v after %d polls; want one 10s wait and no replayed POST", *slept, f.pollCount)
	}
}

func TestDeviceLoginStopsAtExpiryAndOnCancel(t *testing.T) {
	f := newFakeOAuth(t)
	f.polls = []func(http.ResponseWriter){pollStatus("pending", nil)}
	login, _ := f.begin(t)
	login.Expires = time.Now().Add(-time.Second)
	if _, err := login.Wait(context.Background()); !errors.Is(err, ErrSignInExpired) || f.pollCount != 0 {
		t.Fatalf("expired code: err %v after %d polls", err, f.pollCount)
	}

	login, _ = f.begin(t)
	ctx, cancel := context.WithCancel(context.Background())
	login.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := login.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: err = %v", err)
	}
}

func TestDeviceLoginKeepsItsSecretsOutOfLogsAndErrors(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	logger.SetLevel(logger.LevelDebug)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
		logger.SetLevel(logger.LevelInfo)
		logger.RemoveSecret("[TOKEN]")
	})

	f := newFakeOAuth(t)
	f.polls = []func(http.ResponseWriter){
		pollStatus("pending", nil),
		pollStatus("approved", map[string]any{"code": "approval-secret-1d7b"}),
	}
	login, _ := f.begin(t)
	verifier := login.verifier
	token, err := login.Wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("after sign-in: %s", token) // the token is registered for redaction
	for _, secret := range []string{"device-secret-6a1f", verifier, "approval-secret-1d7b", "token-secret-9c3e"} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("log contains %q:\n%s", secret, buf.String())
		}
	}

	f.polls = []func(http.ResponseWriter){func(w http.ResponseWriter) { http.Error(w, "device-secret-6a1f", http.StatusInternalServerError) }}
	login, _ = f.begin(t)
	if _, err := login.Wait(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v", err)
	}
}
