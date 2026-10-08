package itchio

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newClockedClient returns a client for srv, standing in for itch.io, whose
// rate limiter runs on clock so cooldowns cost no real time.
func newClockedClient(srv *httptest.Server, clock *fakeClock) *Client {
	limiter := newRateLimitTransport(srv.Client().Transport, urlHost(srv.URL))
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

// The details page reports a lasting 429 as rate limiting too, so the
// details screen can say so instead of showing "HTTP 429".
func TestFetchGameDetailFinal429IsTyped(t *testing.T) {
	srv, count := rateLimitedServer(t, "/game")
	_, err := newClockedClient(srv, newFakeClock()).FetchGameDetail(srv.URL + "/game")
	assertRateLimitedForScreen(t, err)
	if count.Load() != 1+rateLimitMaxRetries {
		t.Fatalf("detail page requests = %d, want 1 plus %d replays", count.Load(), rateLimitMaxRetries)
	}
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

// hostMux serves each host from its own handler, so tests can tell itch.io
// from a CDN without a network.
type hostMux map[string]http.Handler

func (m hostMux) RoundTrip(req *http.Request) (*http.Response, error) {
	handler := m[req.URL.Host]
	if handler == nil {
		return nil, fmt.Errorf("unexpected host %s", req.URL.Host)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	resp := recorder.Result()
	resp.Request = req
	return resp, nil
}

func newHostClient(mux hostMux, clock *fakeClock) *Client {
	limiter := newRateLimitTransport(mux)
	limiter.now = clock.Now
	limiter.sleepUntil = clock.SleepUntil
	limiter.jitter = func(time.Duration) time.Duration { return 0 }
	return &Client{http: &http.Client{Transport: limiter}, base: "https://itch.io", butler: "https://api.itch.io"}
}

// signedCDN stands in for itch.io's resolvers and a CDN whose signed URLs
// carry a sequence number. The CDN answers 429 to the first limited
// requests, then serves the file.
type signedCDN struct {
	limited   int32
	resolves  atomic.Int32
	cdnServed atomic.Int32
	mu        sync.Mutex
	signed    []string
}

func (s *signedCDN) mux() hostMux {
	resolver := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := s.resolves.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"url":"https://cdn.example/file.gb?sig=%d"}`, n)
	})
	return hostMux{
		"itch.io":     resolver,
		"api.itch.io": resolver,
		"cdn.example": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			s.signed = append(s.signed, r.URL.Query().Get("sig"))
			s.mu.Unlock()
			if s.cdnServed.Add(1) <= s.limited {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Write([]byte("ROM"))
		}),
	}
}

func (s *signedCDN) Signed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.signed...)
}

// A CDN 429 is never replayed: its signed URL can expire during the
// cooldown. The request returns at once so the caller can resolve a new one.
func TestCDNRateLimitIsNotReplayed(t *testing.T) {
	clock := newFakeClock()
	cdn := &signedCDN{limited: 1}
	client := newHostClient(cdn.mux(), clock)
	start := time.Now()
	err := client.streamToFileContext(context.Background(), "https://cdn.example/file.gb?sig=0", filepath.Join(t.TempDir(), "file.gb"), nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if got := cdn.cdnServed.Load(); got != 1 || len(clock.Slept()) != 0 || time.Since(start) > time.Second {
		t.Fatalf("CDN requests = %d, slept %v in %v; want one request and no wait", got, clock.Slept(), time.Since(start))
	}
}

// After a CDN 429 a download waits the cooldown out, resolves a fresh signed
// URL and tries once more.
func TestCDNRateLimitResolvesAFreshURLOnce(t *testing.T) {
	downloads := map[string]func(*Client, string) error{
		"free": func(client *Client, dest string) error {
			return client.DownloadFreeContext(context.Background(), Upload{URL: "https://itch.io/game/file/7?key=k&csrf=c"}, dest, nil)
		},
		"owned": func(client *Client, dest string) error {
			return client.DownloadAuthUploadContext(context.Background(), "api-key", "7", "42", dest, nil)
		},
	}
	for name, download := range downloads {
		t.Run(name, func(t *testing.T) {
			clock := newFakeClock()
			cdn := &signedCDN{limited: 1}
			dest := filepath.Join(t.TempDir(), "file.gb")
			if err := download(newHostClient(cdn.mux(), clock), dest); err != nil {
				t.Fatal(err)
			}
			if got := cdn.Signed(); cdn.resolves.Load() != 2 || len(got) != 2 || got[0] != "1" || got[1] != "2" {
				t.Fatalf("resolves = %d, CDN saw signatures %v; want the second request on a fresh URL", cdn.resolves.Load(), got)
			}
			if got := clock.Slept(); len(got) != 1 || got[0] != 60*time.Second {
				t.Fatalf("slept %v, want the 60s CDN cooldown before resolving again", got)
			}
			if data, err := os.ReadFile(dest); err != nil || string(data) != "ROM" {
				t.Fatalf("file = %q, %v", data, err)
			}
		})
	}
}

func TestCDNRateLimitResolvesAtMostOnce(t *testing.T) {
	clock := newFakeClock()
	cdn := &signedCDN{limited: 100}
	err := newHostClient(cdn.mux(), clock).DownloadFreeContext(context.Background(),
		Upload{URL: "https://itch.io/game/file/7?key=k&csrf=c"}, filepath.Join(t.TempDir(), "file.gb"), nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if cdn.resolves.Load() != 2 || cdn.cdnServed.Load() != 2 {
		t.Fatalf("resolves = %d, CDN requests = %d; want 2 and 2", cdn.resolves.Load(), cdn.cdnServed.Load())
	}
}
