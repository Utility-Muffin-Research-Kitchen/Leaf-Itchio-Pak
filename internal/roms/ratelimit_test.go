package roms_test

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// archiveServer serves data, answers 429 to the requests limit selects, and
// counts range reads and whole-file GETs.
type archiveServer struct {
	ranged, whole atomic.Int32
}

func (s *archiveServer) start(t *testing.T, data []byte, limit func(*http.Request) bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.Header.Get("Range") != "" {
				s.ranged.Add(1)
			} else {
				s.whole.Add(1)
			}
		}
		if limit(r) {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		http.ServeContent(w, r, "test.zip", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A 429 on a range read is reported as rate limiting. Downloading the whole
// archive instead would only add load to a server that asked for less.
func TestInspectRemoteZIP_RateLimitedRangeReadDoesNotFallBack(t *testing.T) {
	data := buildTestZIP(t, map[string]string{"game.gbc": "romdata"})
	server := &archiveServer{}
	srv := server.start(t, data, func(r *http.Request) bool {
		return r.Method == http.MethodGet && r.Header.Get("Range") != ""
	})

	_, err := roms.InspectRemoteZIP(srv.Client(), srv.URL+"/test.zip", nil)
	if !errors.Is(err, netlimit.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if server.ranged.Load() == 0 || server.whole.Load() != 0 {
		t.Fatalf("range reads = %d, whole-file GETs = %d; want range reads only", server.ranged.Load(), server.whole.Load())
	}
}

// A 429 on the size probe stops the inspection before any further request.
func TestInspectRemoteZIP_RateLimitedProbeStops(t *testing.T) {
	data := buildTestZIP(t, map[string]string{"game.gbc": "romdata"})
	server := &archiveServer{}
	srv := server.start(t, data, func(*http.Request) bool { return true })

	_, err := roms.InspectRemoteZIP(srv.Client(), srv.URL+"/test.zip", nil)
	if !errors.Is(err, netlimit.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if server.ranged.Load() != 0 || server.whole.Load() != 0 {
		t.Fatalf("range reads = %d, whole-file GETs = %d after a rate-limited HEAD; want none", server.ranged.Load(), server.whole.Load())
	}
}

// A 429 on a whole-archive download is rate limiting, not a corrupt archive.
func TestInspectRemoteArchiveFullDownloadRateLimited(t *testing.T) {
	server := &archiveServer{}
	srv := server.start(t, nil, func(*http.Request) bool { return true })
	if _, err := roms.InspectRemote7z(srv.Client(), srv.URL+"/test.7z"); !errors.Is(err, netlimit.ErrRateLimited) {
		t.Fatalf("7z err = %v, want ErrRateLimited", err)
	}
}

// failingTransport fails every request with err, as the itch.io client's
// rate limiter does when a cooldown outlasts the request.
type failingTransport struct{ err error }

func (f failingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

// The limiter's own refusal passes through the URL-hiding error wrapper.
func TestRemoteRequestErrorKeepsRateLimiting(t *testing.T) {
	client := &http.Client{Transport: failingTransport{&netlimit.RateLimitedError{Host: "cdn.example"}}}
	_, err := roms.InspectRemote7z(client, "https://cdn.example/test.7z?X-Amz-Signature=secret")
	if !errors.Is(err, netlimit.ErrRateLimited) || err.Error() != netlimit.ErrRateLimited.Error() {
		t.Fatalf("err = %v, want the rate-limit sentence", err)
	}
}
