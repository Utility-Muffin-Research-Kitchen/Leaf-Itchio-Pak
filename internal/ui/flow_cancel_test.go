//go:build !headless

package ui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// stallingSite answers every request only once the client gives up, so a
// lookup that is not cancelled never finishes. reached signals the first
// request; cancelled counts requests the client abandoned.
type stallingSite struct {
	srv       *httptest.Server
	reached   chan string
	cancelled atomic.Int32
}

func newStallingSite(t *testing.T) *stallingSite {
	t.Helper()
	site := &stallingSite{reached: make(chan string, 8)}
	release := make(chan struct{})
	site.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case site.reached <- r.URL.Path:
		default:
		}
		select {
		case <-r.Context().Done():
			site.cancelled.Add(1)
		case <-release:
		}
	}))
	t.Cleanup(site.srv.Close)
	t.Cleanup(func() { close(release) }) // runs first: frees stuck handlers
	return site
}

func (site *stallingSite) waitReached(t *testing.T) string {
	t.Helper()
	select {
	case path := <-site.reached:
		return path
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup never reached itch.io")
		return ""
	}
}

func waitCancelled[T any](t *testing.T, updates <-chan T, errOf func(T) error) {
	t.Helper()
	select {
	case update := <-updates:
		if err := errOf(update); !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Back did not stop the request")
	}
}

func downloadErr(update catDownloadUpdate) error { return update.err }

// R20-3: B on "Finding available files" stops the API listing, including the
// wait before a 429 retry, and does not fall through to the web flow.
func TestBackStopsTheFreeGameAPILookup(t *testing.T) {
	reached := make(chan struct{}, 1)
	site := newFreeGameSite(t, func(w http.ResponseWriter, _ *http.Request) {
		select {
		case reached <- struct{}{}:
		default:
		}
		// Short enough that the transport waits it out and retries.
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	flow := NewCatDownloadFlow(itchio.NewClientWithBase(site.srv.URL), &settings.Config{APIKey: sessionTestKey},
		itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game", IsFree: true},
		&itchio.GameDetail{GameID: "42"}, nil, nil)
	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup never reached the API")
	}
	flow.Close()
	waitCancelled(t, flow.updates, downloadErr)
	if site.webHits.Load() != 0 {
		t.Fatal("a cancelled API lookup fell back to the web flow")
	}
}

func TestBackStopsTheSignedOutWebLookup(t *testing.T) {
	site := newStallingSite(t)
	flow := NewCatDownloadFlow(itchio.NewClientWithBase(site.srv.URL), &settings.Config{},
		itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game", IsFree: true},
		&itchio.GameDetail{GameID: "42"}, nil, nil)
	site.waitReached(t)
	flow.Close()
	waitCancelled(t, flow.updates, downloadErr)
}

func TestBackStopsThePurchaseLookup(t *testing.T) {
	site := newStallingSite(t)
	flow := NewCatDownloadFlow(itchio.NewClientWithBase(site.srv.URL), &settings.Config{APIKey: sessionTestKey},
		itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game"},
		&itchio.GameDetail{GameID: "42"}, nil, nil)
	site.waitReached(t)
	flow.Close()
	waitCancelled(t, flow.updates, downloadErr)
}

// R17-2: Back during a format probe stops its session and resolve requests.
func TestBackStopsTheFormatProbeResolve(t *testing.T) {
	site := newStallingSite(t)
	flow := &CatDownloadFlow{
		client: itchio.NewClientWithBase(site.srv.URL), cfg: &settings.Config{APIKey: sessionTestKey},
		game:    itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game"},
		detail:  &itchio.GameDetail{GameID: "42"},
		updates: make(chan catDownloadUpdate, 2),
	}
	flow.ctx, flow.cancel = context.WithCancel(context.Background())
	go flow.detect(roms.Upload{Filename: "mystery", UploadID: "5", NeedsFormat: true,
		Install: roms.NewInstallSession("42", "")})
	site.waitReached(t)
	flow.Close()
	waitCancelled(t, flow.updates, downloadErr)
}

// R17-2: Back on "Inspecting" stops the archive's resolve.
func TestBackStopsTheArchiveInspectionResolve(t *testing.T) {
	site := newStallingSite(t)
	archive := NewCatArchiveFlow(itchio.NewClientWithBase(site.srv.URL), &settings.Config{APIKey: sessionTestKey},
		itchio.Game{Title: "Leafbound", URL: site.srv.URL + "/game"},
		roms.Upload{Filename: "game.zip", UploadID: "5", Install: roms.NewInstallSession("42", "")}, nil, nil)
	site.waitReached(t)
	archive.Close()
	waitCancelled(t, archive.updates, func(update catArchiveUpdate) error { return update.err })
}
