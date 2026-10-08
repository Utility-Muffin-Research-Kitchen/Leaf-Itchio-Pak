//go:build !headless

package ui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// refusingSite lists free game 42 through the API, refuses every API resolve
// with HTTP 403, and still serves the anonymous web flow, which lists the
// same upload (ID 5) under its web name.
type refusingSite struct {
	srv      *httptest.Server
	rom      []byte
	refusals atomic.Int32
	webFiles atomic.Int32
	webBusy  bool // the game page answers HTTP 429
}

func newRefusingSite(t *testing.T, rom []byte) *refusingSite {
	t.Helper()
	site := &refusingSite{rom: rom}
	site.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/games/42/download-sessions":
			fmt.Fprint(w, `{"uuid":"free-install"}`)
		case strings.HasPrefix(r.URL.Path, "/uploads/"):
			site.refusals.Add(1)
			http.Error(w, "", http.StatusForbidden)
		case r.URL.Path == "/game" && site.webBusy:
			w.Header().Set("Retry-After", "60") // past the request deadline: no replay
			w.WriteHeader(http.StatusTooManyRequests)
		case r.URL.Path == "/game":
			fmt.Fprint(w, `<html><head><meta name="csrf_token" value="CSRF"/></head></html>`)
		case r.URL.Path == "/game/download_url":
			json.NewEncoder(w).Encode(map[string]string{"url": site.srv.URL + "/dl/KEY"})
		case r.URL.Path == "/dl/KEY":
			fmt.Fprint(w, `<html><head><meta name="csrf_token" value="DL"/></head><body>`+
				`<div class="upload"><div class="info_column"><div class="upload_name">`+
				`<strong class="name" title="Web Name.gb">Web Name.gb</strong></div></div><div class="actions">`+
				`<a class="button download_btn" href="javascript:void(0);" data-upload_id="5">Download</a>`+
				`</div></div></body></html>`)
		case r.Method == http.MethodPost && r.URL.Path == "/game/file/5":
			site.webFiles.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"url": site.srv.URL + "/cdn/5"})
		case r.URL.Path == "/cdn/5":
			w.Write(site.rom)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.srv.Close)
	return site
}

// nesTestROM starts with the iNES magic, so the format probe detects it.
func nesTestROM() []byte {
	return append([]byte("NES\x1a"), make([]byte, 60)...)
}

// R20-1: itch.io lists a free game through the API but refuses to resolve
// its upload without a purchase. The download retries once through the web
// flow and succeeds.
func TestRefusedFreeAPIDownloadRetriesThroughTheWebFlow(t *testing.T) {
	primary, _ := transactionPaths(t)
	site := newRefusingSite(t, nesTestROM())
	inv, err := inventory.Load(filepath.Join(t.TempDir(), "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	upload := roms.Upload{Filename: "api.gb", UploadID: "5", Install: roms.NewInstallSession("42", "")}
	dest := filepath.Join(primary, "Roms", "GB", "api.gb")
	worker := NewDirectDownloadWorker(itchio.NewClientWithBase(site.srv.URL), &settings.Config{AuthToken: sessionTestKey},
		itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game", IsFree: true}, &itchio.GameDetail{GameID: "42"},
		upload, dest, inv, filepath.Join(t.TempDir(), "inventory.json"))
	waitFor(t, func() bool { return worker.loadState() != dlDownloading })
	if state := worker.CatSnapshot(); state.State != appui.DownloadProgressDone {
		t.Fatalf("download = %+v", state)
	}
	if data, err := os.ReadFile(dest); err != nil || !bytes.Equal(data, site.rom) {
		t.Fatalf("installed file = %d bytes, %v", len(data), err)
	}
	if site.refusals.Load() != 1 || site.webFiles.Load() != 1 {
		t.Fatalf("API refusals %d, web resolves %d; want one of each", site.refusals.Load(), site.webFiles.Load())
	}
}

func TestRefusedFreeAPIFormatProbeRetriesThroughTheWebFlow(t *testing.T) {
	site := newRefusingSite(t, nesTestROM())
	flow := &CatDownloadFlow{
		client: itchio.NewClientWithBase(site.srv.URL), cfg: &settings.Config{AuthToken: sessionTestKey},
		game:    itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game", IsFree: true},
		detail:  &itchio.GameDetail{GameID: "42"},
		updates: make(chan catDownloadUpdate, 1),
	}
	flow.detect(roms.Upload{Filename: "mystery", UploadID: "5", NeedsFormat: true, Install: roms.NewInstallSession("42", "")})
	if probe := <-flow.updates; probe.err != nil || probe.ext != ".nes" {
		t.Fatalf("probe = ext %q err %v, want .nes through the web flow", probe.ext, probe.err)
	}
}

// A purchase is never retried anonymously: the refusal is the answer.
func TestRefusedPurchaseDownloadDoesNotTryTheWebFlow(t *testing.T) {
	site := newRefusingSite(t, nesTestROM())
	flow := &CatDownloadFlow{
		client: itchio.NewClientWithBase(site.srv.URL), cfg: &settings.Config{AuthToken: sessionTestKey},
		game:    itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game"},
		detail:  &itchio.GameDetail{GameID: "42"},
		updates: make(chan catDownloadUpdate, 1),
	}
	flow.detect(roms.Upload{Filename: "mystery", UploadID: "5", NeedsFormat: true, Install: roms.NewInstallSession("42", "7")})
	if probe := <-flow.updates; !errors.Is(probe.err, itchio.ErrDownloadRefused) {
		t.Fatalf("probe err = %v, want the refusal", probe.err)
	}
	if site.webFiles.Load() != 0 {
		t.Fatal("a purchase download fell back to the anonymous web flow")
	}
}

// A rate limit on the web fallback is reported as such: it is final and
// tells you what to do, unlike the API's refusal.
func TestRefusedFreeAPIDownloadReportsAWebRateLimit(t *testing.T) {
	site := newRefusingSite(t, nesTestROM())
	site.webBusy = true
	flow := &CatDownloadFlow{
		client: itchio.NewClientWithBase(site.srv.URL), cfg: &settings.Config{AuthToken: sessionTestKey},
		game:    itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game", IsFree: true},
		detail:  &itchio.GameDetail{GameID: "42"},
		updates: make(chan catDownloadUpdate, 1),
	}
	flow.detect(roms.Upload{Filename: "mystery", UploadID: "5", NeedsFormat: true, Install: roms.NewInstallSession("42", "")})
	if probe := <-flow.updates; !errors.Is(probe.err, itchio.ErrRateLimited) {
		t.Fatalf("probe err = %v, want the rate limit", probe.err)
	}
}
