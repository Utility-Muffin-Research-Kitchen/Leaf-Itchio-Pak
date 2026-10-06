//go:build !headless

package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

const signInKey = "signed-in-key-4b7e"

// signInSite is an offline api.itch.io for the whole sign-in: device code,
// poll, token exchange, then the profile and owned games.
type signInSite struct {
	srv           *httptest.Server
	deviceStatus  int
	pollStatus    atomic.Value // string
	profileStatus int
	polls         atomic.Int32
	starts        atomic.Int32
	// tokenGate, when set, holds the token exchange until it is closed;
	// tokenReached signals that the exchange arrived.
	tokenGate    chan struct{}
	tokenReached chan struct{}
	tokens       atomic.Int32
	// profileGate, when set, holds the account check until it is closed.
	profileGate    chan struct{}
	profileReached chan struct{}
}

func (site *signInSite) setPoll(status string) { site.pollStatus.Store(status) }

// gate returns a channel that holds a handler until release is called, and
// releases it at cleanup too, so a failing test never blocks the server.
func gate(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	held := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(held) }) }
	t.Cleanup(release)
	return held, release
}

func newSignInSite(t *testing.T) *signInSite {
	t.Helper()
	site := &signInSite{deviceStatus: http.StatusOK, profileStatus: http.StatusOK,
		tokenReached: make(chan struct{}, 4), profileReached: make(chan struct{}, 4)}
	site.setPoll("approved")
	site.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/device":
			site.starts.Add(1)
			if site.deviceStatus != http.StatusOK {
				w.WriteHeader(site.deviceStatus)
				return
			}
			fmt.Fprint(w, `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri":"https://itch.io/user/oauth/device",`+
				`"verification_uri_complete":"https://itch.io/user/oauth/device?code=verification-fixture","expires_in":600,"interval":1}`)
		case "/oauth/device/poll":
			site.polls.Add(1)
			fmt.Fprintf(w, `{"status":%q,"code":"approval"}`, site.pollStatus.Load().(string))
		case "/oauth/token":
			site.tokenReached <- struct{}{}
			if site.tokenGate != nil {
				<-site.tokenGate
			}
			site.tokens.Add(1)
			fmt.Fprintf(w, `{"access_token":%q,"token_type":"bearer"}`, signInKey)
		case "/profile":
			site.profileReached <- struct{}{}
			if site.profileGate != nil {
				<-site.profileGate
			}
			if r.Header.Get("Authorization") != "Bearer "+signInKey {
				t.Errorf("profile authorization = %q", r.Header.Get("Authorization"))
			}
			w.WriteHeader(site.profileStatus)
			fmt.Fprint(w, `{"user":{"username":"tester"}}`)
		case "/profile/owned-keys":
			if r.URL.Query().Get("page") == "1" {
				fmt.Fprint(w, `{"per_page":50,"owned_keys":[{"id":1,"game_id":7,"purchase_id":9,"game":{"id":7,"title":"Leafbound","url":"https://dev.itch.io/leafbound"}}]}`)
				return
			}
			fmt.Fprint(w, `{"owned_keys":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.srv.Close)
	return site
}

type signInFixture struct {
	cfg       *settings.Config
	cfgPath   string
	ownedPath string
	owned     [][]itchio.OwnedGame
	flow      *CatSignInFlow
	model     *appui.SignInModel
}

func startSignIn(t *testing.T, site *signInSite, cfg *settings.Config) *signInFixture {
	t.Helper()
	t.Cleanup(func() { logger.RemoveSecret(tokenSecretLabel) })
	dir := t.TempDir()
	f := &signInFixture{cfg: cfg, cfgPath: filepath.Join(dir, "config.json"), ownedPath: filepath.Join(dir, "owned_cache.json")}
	if err := itchio.SaveOwnedCache(f.ownedPath, []string{"https://dev.itch.io/previous-account"}); err != nil {
		t.Fatal(err)
	}
	cfg.CredentialWarningAccepted = true // TestSignInWarnsBeforeTheFirstSignIn covers the warning
	account := NewAccount(cfg, f.cfgPath, f.ownedPath, itchio.NewClientWithBase(site.srv.URL))
	account.SetOwnedChanged(func(owned []itchio.OwnedGame) { f.owned = append(f.owned, owned) })
	f.flow, f.model = NewCatSignInFlow(itchio.NewClientWithBase(site.srv.URL), account, nil)
	t.Cleanup(f.flow.Cancel)
	return f
}

// syncUntil applies results until done reports true.
func (f *signInFixture) syncUntil(t *testing.T, done func(*appui.SignInModel) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done(f.model) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out in state %v (%s: %s)", f.model.State, f.model.Heading, f.model.Detail)
		}
		f.flow.Sync(f.model)
		time.Sleep(2 * time.Millisecond)
	}
}

func settled(model *appui.SignInModel) bool {
	return model.State == appui.SignInDone || model.State == appui.SignInError
}

func TestSignInStoresTheKeyAndLoadsTheAccount(t *testing.T) {
	site := newSignInSite(t)
	f := startSignIn(t, site, &settings.Config{LegacyKeyRemoved: true})
	f.syncUntil(t, settled)
	if f.model.State != appui.SignInDone || f.model.Heading != "Signed in as tester" {
		t.Fatalf("model = %+v", f.model)
	}
	if f.cfg.Credential() != signInKey || f.cfg.AuthUser != "tester" || f.cfg.LegacyKeyRemoved {
		t.Fatalf("config = %+v", f.cfg)
	}
	if loaded, _ := settings.Load(f.cfgPath); loaded.Credential() != signInKey || loaded.AuthUser != "tester" {
		t.Fatalf("saved config = %+v", loaded)
	}
	// The previous account's owned list is cleared first, then replaced.
	if len(f.owned) != 2 || len(f.owned[0]) != 0 || len(f.owned[1]) != 1 || f.owned[1][0].URL != "https://dev.itch.io/leafbound" {
		t.Fatalf("owned updates = %v", f.owned)
	}
	if urls, _ := itchio.LoadOwnedCache(f.ownedPath); len(urls) != 1 || urls[0] != "https://dev.itch.io/leafbound" {
		t.Fatalf("owned cache = %v", urls)
	}
}

func TestSignInShowsTheCodeAndCancelChangesNothing(t *testing.T) {
	site := newSignInSite(t)
	site.setPoll("pending")
	f := startSignIn(t, site, &settings.Config{})
	f.syncUntil(t, func(m *appui.SignInModel) bool { return m.State == appui.SignInWaiting })
	if f.model.UserCode != "ABCD-1234" || f.model.QRURL != "https://itch.io/user/oauth/device?code=verification-fixture" || f.model.Remaining(time.Now()) <= 0 {
		t.Fatalf("waiting model = %+v", f.model)
	}
	f.flow.Cancel()
	time.Sleep(20 * time.Millisecond)
	f.flow.Sync(f.model)
	if f.cfg.SignedIn() || len(f.owned) != 0 {
		t.Fatalf("cancelled sign-in changed the account: %+v", f.cfg)
	}
	if _, err := os.Stat(f.cfgPath); !os.IsNotExist(err) {
		t.Fatal("cancelled sign-in wrote the config")
	}
	if urls, _ := itchio.LoadOwnedCache(f.ownedPath); len(urls) != 1 {
		t.Fatal("cancelled sign-in cleared the previous owned cache")
	}
}

func TestSignInOutcomes(t *testing.T) {
	for name, test := range map[string]struct {
		configure func(*signInSite)
		heading   string
	}{
		"unavailable":  {func(s *signInSite) { s.deviceStatus = http.StatusNotFound }, "Sign-in is unavailable"},
		"declined":     {func(s *signInSite) { s.setPoll("denied") }, "Sign-in was declined"},
		"expired":      {func(s *signInSite) { s.setPoll("expired") }, "The code expired"},
		"key rejected": {func(s *signInSite) { s.profileStatus = http.StatusUnauthorized }, "itch.io didn't accept the sign-in"},
	} {
		site := newSignInSite(t)
		test.configure(site)
		f := startSignIn(t, site, &settings.Config{})
		f.syncUntil(t, settled)
		if f.model.State != appui.SignInError || f.model.Heading != test.heading || !f.model.CanRetry {
			t.Errorf("%s: model = %+v", name, f.model)
		}
		if f.cfg.SignedIn() {
			t.Errorf("%s: left the app signed in", name)
		}
	}
}

// A sign-in whose account check fails for a network reason stays signed in.
func TestSignInKeepsTheKeyWhenTheAccountCheckIsOffline(t *testing.T) {
	site := newSignInSite(t)
	site.profileStatus = http.StatusBadGateway
	f := startSignIn(t, site, &settings.Config{})
	f.syncUntil(t, settled)
	if f.model.State != appui.SignInDone || !f.cfg.SignedIn() {
		t.Fatalf("model = %+v signed in = %v", f.model, f.cfg.SignedIn())
	}
}

func TestSignInRetryStartsAFreshCode(t *testing.T) {
	site := newSignInSite(t)
	site.setPoll("expired")
	f := startSignIn(t, site, &settings.Config{})
	f.syncUntil(t, settled)
	site.setPoll("approved")
	f.flow.Start(f.model)
	f.syncUntil(t, settled)
	if f.model.State != appui.SignInDone || !f.cfg.SignedIn() {
		t.Fatalf("retry model = %+v", f.model)
	}
}

func TestCatalogStartupRejectionIgnoresAReplacedKey(t *testing.T) {
	controller := &CatalogController{ownedUpdateCh: make(chan map[string]bool, 1)}
	generation := controller.ownedGeneration.Load()
	controller.ReplaceOwnedGames(nil) // the user signed in again meanwhile
	controller.rejectSignInIfCurrent(generation)
	if controller.TakeSignInRejected() {
		t.Fatal("a rejection of the previous key signed the new one out")
	}
	controller.rejectSignInIfCurrent(controller.ownedGeneration.Load())
	if !controller.TakeSignInRejected() || controller.TakeSignInRejected() {
		t.Fatal("a current rejection must be reported exactly once")
	}
}

func waitSignal(t *testing.T, signal chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// syncDetached keeps applying results after the screen closed, as the app
// does, until the flow has nothing left to deliver.
func (f *signInFixture) syncDetached(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !f.flow.Idle() {
		if time.Now().After(deadline) {
			t.Fatal("the flow never settled")
		}
		f.flow.Sync(nil)
		time.Sleep(2 * time.Millisecond)
	}
	f.flow.Sync(nil)
}

// R21-8: itch.io already issued the key when B lands between the exchange
// and the next Sync. The key is saved anyway: the app cannot revoke it.
func TestSignInSavesAKeyIssuedJustBeforeYouCancel(t *testing.T) {
	site := newSignInSite(t)
	f := startSignIn(t, site, &settings.Config{})
	waitSignal(t, site.tokenReached, "the token exchange")
	deadline := time.Now().Add(5 * time.Second)
	for len(f.flow.updates) < 2 { // the code, then the key
		if time.Now().After(deadline) {
			t.Fatal("the key never arrived")
		}
		time.Sleep(2 * time.Millisecond)
	}
	f.flow.Cancel()
	f.syncDetached(t)
	if f.cfg.Credential() != signInKey || f.cfg.AuthUser != "tester" {
		t.Fatalf("config = %+v, want the issued key saved and checked", f.cfg)
	}
}

// R21-8: B during the exchange does not abort it, so a key itch.io is
// issuing is never lost.
func TestSignInFinishesTheKeyExchangeAfterYouCancel(t *testing.T) {
	site := newSignInSite(t)
	var release func()
	site.tokenGate, release = gate(t)
	f := startSignIn(t, site, &settings.Config{})
	waitSignal(t, site.tokenReached, "the token exchange")
	f.flow.Cancel()
	release()
	f.syncDetached(t)
	if site.tokens.Load() != 1 || f.cfg.Credential() != signInKey {
		t.Fatalf("exchanges answered %d, config = %+v; want the key saved", site.tokens.Load(), f.cfg)
	}
}

// R21-1: a power press while the QR code waits for approval cancels the
// sign-in at once. Nothing protects that wait, polling stops, and an
// approval that comes later stores no key.
func TestPowerCancelsASignInWaitingForApproval(t *testing.T) {
	site := newSignInSite(t)
	site.setPoll("pending")
	f := startSignIn(t, site, &settings.Config{})
	f.syncUntil(t, func(m *appui.SignInModel) bool { return m.State == appui.SignInWaiting })
	if !f.flow.YieldToPower(f.model) {
		t.Fatal("a sign-in waiting for approval must give way to the power action")
	}
	if f.flow.Busy() {
		t.Fatal("a cancelled sign-in still holds the power action")
	}
	polls := site.polls.Load()
	site.setPoll("approved")
	time.Sleep(1500 * time.Millisecond) // longer than the 1 s poll interval
	f.syncDetached(t)
	if f.cfg.SignedIn() || site.tokens.Load() != 0 || site.polls.Load() != polls {
		t.Fatalf("after the power press: signed in %v, exchanges %d, polls %d -> %d",
			f.cfg.SignedIn(), site.tokens.Load(), polls, site.polls.Load())
	}
}

// R21-1: only the short key exchange holds a power action, until its key
// is saved. The account check after it does not.
func TestPowerWaitsOnlyForTheKeyExchange(t *testing.T) {
	site := newSignInSite(t)
	var releaseToken func()
	site.tokenGate, releaseToken = gate(t)
	site.profileGate, _ = gate(t)
	f := startSignIn(t, site, &settings.Config{})
	waitSignal(t, site.tokenReached, "the token exchange")
	f.flow.Sync(f.model)
	if f.flow.YieldToPower(f.model) || !f.flow.Busy() {
		t.Fatal("the key exchange must finish before the power action")
	}
	releaseToken()
	f.syncUntil(t, func(m *appui.SignInModel) bool { return m.State == appui.SignInChecking })
	if f.flow.Busy() || f.flow.YieldToPower(f.model) || !f.cfg.SignedIn() {
		t.Fatalf("after the key was saved: busy %v, signed in %v; the check must not hold power", f.flow.Busy(), f.cfg.SignedIn())
	}
}

// R21-7: B on the account check leaves the screen; the check finishes in the
// background and still records the account and its owned games.
func TestLeavingTheAccountCheckFinishesItInTheBackground(t *testing.T) {
	site := newSignInSite(t)
	var release func()
	site.profileGate, release = gate(t)
	f := startSignIn(t, site, &settings.Config{})
	f.syncUntil(t, func(m *appui.SignInModel) bool { return m.State == appui.SignInChecking })
	waitSignal(t, site.profileReached, "the account check")
	if f.model.Handle(appui.InputEvent{Button: appui.ButtonB, Pressed: true}) != appui.SignInIntentBack {
		t.Fatal("B must leave the account check")
	}
	f.flow.Cancel() // what closing the screen does
	release()
	f.syncDetached(t)
	if f.cfg.Credential() != signInKey || f.cfg.AuthUser != "tester" {
		t.Fatalf("config = %+v", f.cfg)
	}
	if urls, _ := itchio.LoadOwnedCache(f.ownedPath); len(urls) != 1 || urls[0] != "https://dev.itch.io/leafbound" {
		t.Fatalf("owned cache = %v", urls)
	}
}

// R21-2: every way into sign-in (Settings, or A on a paid game) shows the
// physical-access warning first, and only once it is accepted.
func TestSignInWarnsBeforeTheFirstSignIn(t *testing.T) {
	site := newSignInSite(t)
	dir := t.TempDir()
	cfg := &settings.Config{}
	cfgPath := filepath.Join(dir, "config.json")
	account := NewAccount(cfg, cfgPath, filepath.Join(dir, "owned_cache.json"), itchio.NewClientWithBase(site.srv.URL))
	t.Cleanup(func() { logger.RemoveSecret(tokenSecretLabel) })
	flow, model := NewCatSignInFlow(itchio.NewClientWithBase(site.srv.URL), account, nil)
	t.Cleanup(flow.Cancel)
	if model.State != appui.SignInWarning {
		t.Fatalf("state = %v, want the physical-access warning", model.State)
	}
	time.Sleep(20 * time.Millisecond)
	if site.starts.Load() != 0 {
		t.Fatal("sign-in started before the warning was accepted")
	}
	if err := flow.AcceptWarning(model); err != nil {
		t.Fatal(err)
	}
	if loaded, err := settings.Load(cfgPath); err != nil || !loaded.CredentialWarningAccepted {
		t.Fatalf("accepted warning not saved: %+v, %v", loaded, err)
	}
	f := &signInFixture{cfg: cfg, flow: flow, model: model}
	f.syncUntil(t, settled)
	if site.starts.Load() != 1 || !cfg.SignedIn() {
		t.Fatalf("after accepting: %d code request(s), signed in %v", site.starts.Load(), cfg.SignedIn())
	}

	// Signing in again later skips the warning.
	again := &appui.SignInModel{}
	flow.Open(again)
	if again.State != appui.SignInStarting {
		t.Fatalf("second sign-in state = %v, want no second warning", again.State)
	}
}
