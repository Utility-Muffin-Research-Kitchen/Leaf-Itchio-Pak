//go:build !headless

package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// freeGameSite serves a free game both ways: the API upload list for game 42
// (answered by api) and the anonymous web download flow, which lists
// web.gb unless webPaid makes download_url refuse.
type freeGameSite struct {
	srv      *httptest.Server
	api      http.HandlerFunc
	webPaid  bool
	apiHits  atomic.Int32
	webHits  atomic.Int32
	keyInURL atomic.Bool
}

func newFreeGameSite(t *testing.T, api http.HandlerFunc) *freeGameSite {
	t.Helper()
	site := &freeGameSite{api: api}
	site.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.String(), sessionTestKey) {
			site.keyInURL.Store(true)
		}
		switch r.URL.Path {
		case "/games/42/uploads":
			site.apiHits.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+sessionTestKey {
				http.Error(w, "", http.StatusUnauthorized)
				return
			}
			site.api(w, r)
		case "/game":
			site.webHits.Add(1)
			fmt.Fprint(w, `<html><head><meta name="csrf_token" value="CSRF"/></head></html>`)
		case "/game/download_url":
			url := site.srv.URL + "/dl/KEY"
			if site.webPaid {
				url = ""
			}
			json.NewEncoder(w).Encode(map[string]string{"url": url})
		case "/dl/KEY":
			fmt.Fprint(w, `<html><head><meta name="csrf_token" value="DL"/></head><body>`+
				`<div class="upload"><div class="info_column"><div class="upload_name">`+
				`<strong class="name" title="web.gb">web.gb</strong></div></div><div class="actions">`+
				`<a class="button download_btn" href="javascript:void(0);" data-upload_id="7">Download</a>`+
				`</div></div></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.srv.Close)
	return site
}

func (site *freeGameSite) discover(t *testing.T, apiKey string) catDownloadUpdate {
	t.Helper()
	flow := &CatDownloadFlow{
		client: itchio.NewClientWithBase(site.srv.URL), cfg: &settings.Config{APIKey: apiKey},
		game:   itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game", IsFree: true},
		detail: &itchio.GameDetail{GameID: "42"}, updates: make(chan catDownloadUpdate, 1),
	}
	flow.discover()
	return <-flow.updates
}

func apiUploads(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }
}

func TestFreeGameWithAKeyIsListedThroughTheAPI(t *testing.T) {
	site := newFreeGameSite(t, apiUploads(`{"uploads":[{"id":5,"filename":"api.gb","traits":{}}]}`))
	update := site.discover(t, sessionTestKey)
	if update.err != nil || len(update.uploads) != 1 || update.uploads[0].Filename != "api.gb" {
		t.Fatalf("update = %+v", update)
	}
	install := update.uploads[0].Install
	if install == nil || install.DownloadKeyID() != "" {
		t.Fatal("a free API upload needs an install with no purchase ID")
	}
	if site.webHits.Load() != 0 || site.keyInURL.Load() {
		t.Fatalf("web hits %d, key in URL %v; want the API only, with the key in the header", site.webHits.Load(), site.keyInURL.Load())
	}
}

func TestFreeGameWithoutAKeyUsesTheWebFlow(t *testing.T) {
	site := newFreeGameSite(t, apiUploads(`{"uploads":[{"id":5,"filename":"api.gb"}]}`))
	update := site.discover(t, "")
	if update.err != nil || len(update.uploads) != 1 || update.uploads[0].Filename != "web.gb" || update.uploads[0].ViaAPI() {
		t.Fatalf("update = %+v", update)
	}
	if site.apiHits.Load() != 0 {
		t.Fatal("a signed-out user reached the API")
	}
}

func TestFreeGameFallsBackToTheWebFlowOnce(t *testing.T) {
	for name, api := range map[string]http.HandlerFunc{
		"server error": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "", http.StatusInternalServerError) },
		"empty list":   apiUploads(`{"uploads":{}}`),
		"no access":    func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "", http.StatusForbidden) },
	} {
		site := newFreeGameSite(t, api)
		update := site.discover(t, sessionTestKey)
		if update.err != nil || len(update.uploads) != 1 || update.uploads[0].Filename != "web.gb" || update.uploads[0].ViaAPI() {
			t.Fatalf("%s: update = %+v, want the web listing", name, update)
		}
		if site.apiHits.Load() != 1 || site.webHits.Load() != 1 {
			t.Fatalf("%s: api %d web %d, want one attempt each", name, site.apiHits.Load(), site.webHits.Load())
		}
	}
}

func TestFreeGameRateLimitDoesNotTryTheWebFlow(t *testing.T) {
	site := newFreeGameSite(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	update := site.discover(t, sessionTestKey)
	if !errors.Is(update.err, itchio.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", update.err)
	}
	if site.webHits.Load() != 0 {
		t.Fatal("a rate-limited API listing fell back to the web flow")
	}
}

// When the API refuses access and the web flow cannot list the game either,
// the user sees the access error rather than the web flow's generic one.
func TestFreeGameReportsTheAPIAccessErrorWhenTheWebFlowAlsoFails(t *testing.T) {
	site := newFreeGameSite(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "", http.StatusForbidden) })
	site.webPaid = true
	update := site.discover(t, sessionTestKey)
	if !errors.Is(update.err, itchio.ErrNoAccess) {
		t.Fatalf("err = %v, want the API access error", update.err)
	}
	if site.apiHits.Load() != 1 || site.webHits.Load() != 1 {
		t.Fatalf("api %d web %d, want one attempt each", site.apiHits.Load(), site.webHits.Load())
	}
}
