//go:build !headless

package ui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	pollStatus    string
	profileStatus int
	polls         atomic.Int32
}

func newSignInSite(t *testing.T) *signInSite {
	t.Helper()
	site := &signInSite{deviceStatus: http.StatusOK, pollStatus: "approved", profileStatus: http.StatusOK}
	site.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/device":
			if site.deviceStatus != http.StatusOK {
				w.WriteHeader(site.deviceStatus)
				return
			}
			fmt.Fprint(w, `{"device_code":"device-code","user_code":"ABCD-1234","verification_uri":"https://itch.io/device",`+
				`"verification_uri_complete":"https://itch.io/device?code=ABCD-1234","expires_in":600,"interval":1}`)
		case "/oauth/device/poll":
			site.polls.Add(1)
			fmt.Fprintf(w, `{"status":%q,"code":"approval"}`, site.pollStatus)
		case "/oauth/token":
			fmt.Fprintf(w, `{"access_token":%q,"token_type":"bearer"}`, signInKey)
		case "/profile":
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

func settled(model *appui.SignInModel) bool { return !SignInBusy(model) }

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
	site.pollStatus = "pending"
	f := startSignIn(t, site, &settings.Config{})
	f.syncUntil(t, func(m *appui.SignInModel) bool { return m.State == appui.SignInWaiting })
	if f.model.UserCode != "ABCD-1234" || f.model.QRURL != "https://itch.io/device?code=ABCD-1234" || f.model.Remaining(time.Now()) <= 0 {
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
		"not approved yet": {func(s *signInSite) { s.deviceStatus = http.StatusNotFound }, "Sign-in isn't available yet"},
		"declined":         {func(s *signInSite) { s.pollStatus = "denied" }, "Sign-in was declined"},
		"expired":          {func(s *signInSite) { s.pollStatus = "expired" }, "The code expired"},
		"key rejected":     {func(s *signInSite) { s.profileStatus = http.StatusUnauthorized }, "itch.io didn't accept the sign-in"},
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
	site.pollStatus = "expired"
	f := startSignIn(t, site, &settings.Config{})
	f.syncUntil(t, settled)
	site.pollStatus = "approved"
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
