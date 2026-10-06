package itchio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// newClockedClient returns a client for srv whose rate limiter runs on clock,
// so cooldowns cost no real time.
func newClockedClient(srv *httptest.Server, clock *fakeClock) *Client {
	limiter := newRateLimitTransport(srv.Client().Transport)
	limiter.now = clock.Now
	limiter.sleepUntil = clock.SleepUntil
	limiter.jitter = func(d time.Duration) time.Duration { return 0 }
	return &Client{http: &http.Client{Transport: limiter}, base: srv.URL, butler: srv.URL}
}

// rateLimitedServer answers 429 on every path in limited and a working
// itch.io page elsewhere, counting the 429s.
func rateLimitedServer(t *testing.T, limited ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	limit := map[string]bool{}
	for _, path := range limited {
		limit[path] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if limit[r.URL.Path] {
			count.Add(1)
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		switch r.URL.Path {
		case "/game":
			w.Write([]byte(`<html><body><input name="csrf_token" value="tok"/></body></html>`))
		case "/game/download_url":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"url":"http://` + r.Host + `/game/download/eyJpZCI6NDJ9.SIG"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func assertRateLimitedForScreen(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	// The download screens show the error text as is.
	if err.Error() != ErrRateLimited.Error() {
		t.Fatalf("on-screen text = %q, want %q", err.Error(), ErrRateLimited.Error())
	}
}

// A 429 that outlasts the transport's replays ends every download step with
// the typed error and the on-screen sentence, never raw status text.
func TestFinal429IsTypedOnEveryDownloadPath(t *testing.T) {
	ctx := context.Background()
	t.Run("file stream", func(t *testing.T) {
		srv, _ := rateLimitedServer(t, "/file.gb")
		err := newClockedClient(srv, newFakeClock()).streamToFileContext(ctx, srv.URL+"/file.gb", filepath.Join(t.TempDir(), "file.gb"), nil)
		assertRateLimitedForScreen(t, err)
	})
	t.Run("header probe", func(t *testing.T) {
		srv, _ := rateLimitedServer(t, "/file.gb")
		_, err := newClockedClient(srv, newFakeClock()).FetchFileHeader(srv.URL+"/file.gb", 8)
		assertRateLimitedForScreen(t, err)
	})
	t.Run("free resolver", func(t *testing.T) {
		srv, count := rateLimitedServer(t, "/game/file/1")
		_, err := newClockedClient(srv, newFakeClock()).ResolveFreeURLContext(ctx, Upload{URL: srv.URL + "/game/file/1?key=k&csrf=c"})
		assertRateLimitedForScreen(t, err)
		if count.Load() != 1 {
			t.Fatalf("resolver POSTs = %d, want 1", count.Load())
		}
	})
	t.Run("game page", func(t *testing.T) {
		srv, _ := rateLimitedServer(t, "/game")
		_, err := newClockedClient(srv, newFakeClock()).FetchUploads(srv.URL + "/game")
		assertRateLimitedForScreen(t, err)
	})
	t.Run("download_url POST", func(t *testing.T) {
		srv, count := rateLimitedServer(t, "/game/download_url")
		_, err := newClockedClient(srv, newFakeClock()).FetchUploads(srv.URL + "/game")
		assertRateLimitedForScreen(t, err)
		if count.Load() != 1 {
			t.Fatalf("download_url POSTs = %d, want 1", count.Load())
		}
	})
	t.Run("download page", func(t *testing.T) {
		srv, _ := rateLimitedServer(t, "/game/download/eyJpZCI6NDJ9.SIG")
		_, err := newClockedClient(srv, newFakeClock()).FetchUploads(srv.URL + "/game")
		assertRateLimitedForScreen(t, err)
	})
}

// A non-200 answer to the download_url POST is reported by status instead of
// as a JSON decoding failure.
func TestFetchUploadsChecksDownloadURLStatusBeforeDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/game" {
			w.Write([]byte(`<html><body><input name="csrf_token" value="tok"/></body></html>`))
			return
		}
		http.Error(w, "<html>server error</html>", http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := NewClientWithBase(srv.URL).FetchUploads(srv.URL + "/game")
	if err == nil || err.Error() != "download_url POST: HTTP 502" {
		t.Fatalf("err = %v, want the HTTP status", err)
	}
}

// The transport's own deadline refusal keeps the on-screen sentence too.
func TestSafeRequestErrorShowsTheRateLimitSentence(t *testing.T) {
	err := safeRequestError("fetch file", &RateLimitedError{Host: "cdn.example"})
	assertRateLimitedForScreen(t, err)
}
