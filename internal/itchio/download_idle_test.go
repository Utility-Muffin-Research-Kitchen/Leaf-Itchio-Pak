package itchio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestStreamIdleTimeoutPreservesDestination(t *testing.T) {
	for _, headers := range []bool{false, true} {
		t.Run(map[bool]string{false: "before headers", true: "during body"}[headers], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client := &Client{http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if !headers {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					reader, writer := io.Pipe()
					go func() {
						_, _ = io.WriteString(writer, "partial")
						<-req.Context().Done()
						_ = writer.CloseWithError(req.Context().Err())
					}()
					return &http.Response{StatusCode: http.StatusOK, ContentLength: 100, Body: reader}, nil
				})}}
				dir := t.TempDir()
				dest := filepath.Join(dir, "game.gbc")
				if err := os.WriteFile(dest, []byte("installed"), 0o644); err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				err := client.streamToFile("https://cdn.example/game?token=private", dest, nil)
				if !errors.Is(err, os.ErrDeadlineExceeded) || errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want idle timeout, not cancellation", err)
				}
				if strings.Contains(err.Error(), "cdn.example") || strings.Contains(err.Error(), "private") {
					t.Fatalf("error exposes source URL: %v", err)
				}
				if elapsed := time.Since(start); elapsed != streamIdleTimeout {
					t.Fatalf("stalled for %v, want %v", elapsed, streamIdleTimeout)
				}
				data, readErr := os.ReadFile(dest)
				if readErr != nil || string(data) != "installed" {
					t.Fatalf("installed file = %q, %v", data, readErr)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 1 || entries[0].Name() != "game.gbc" {
					t.Fatalf("files after timeout = %v, %v", entries, err)
				}
			})
		})
	}
}

func TestStreamProgressHasNoTotalDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := &Client{http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			reader, writer := io.Pipe()
			go func() {
				defer writer.Close()
				for range 5 {
					select {
					case <-req.Context().Done():
						_ = writer.CloseWithError(req.Context().Err())
						return
					case <-time.After(streamIdleTimeout / 2):
					}
					if _, err := io.WriteString(writer, "x"); err != nil {
						return
					}
				}
			}()
			return &http.Response{StatusCode: http.StatusOK, ContentLength: 5, Body: reader}, nil
		})}}
		dest := filepath.Join(t.TempDir(), "game.gbc")
		start := time.Now()
		if err := client.streamToFile("https://cdn.example/game", dest, nil); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) <= streamIdleTimeout {
			t.Fatal("test did not stream longer than the idle timeout")
		}
		data, err := os.ReadFile(dest)
		if err != nil || string(data) != "xxxxx" {
			t.Fatalf("download = %q, %v", data, err)
		}
	})
}

// A CDN 429 is not replayed, but the next request to that host still waits
// out its cooldown. The idle clock stays paused for that wait and starts again
// once the request can reach the network.
func TestStreamIdleClockPausesForCooldownAndResumes(t *testing.T) {
	for _, stall := range []bool{false, true} {
		t.Run(map[bool]string{false: "download", true: "stalled after cooldown"}[stall], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				limiter := newRateLimitTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					if stall {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ROM"))}, nil
				}))
				limiter.jitter = func(time.Duration) time.Duration { return 0 }
				limiter.record429("cdn.example", "60")
				client := &Client{http: &http.Client{Transport: limiter}}
				dest := filepath.Join(t.TempDir(), "game.gbc")
				start := time.Now()
				err := client.streamToFile("https://cdn.example/game", dest, nil)
				wantElapsed := 60 * time.Second
				if stall {
					wantElapsed += streamIdleTimeout
					if !errors.Is(err, os.ErrDeadlineExceeded) {
						t.Fatalf("error = %v, want timeout after cooldown", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if elapsed := time.Since(start); elapsed != wantElapsed || calls != 1 {
					t.Fatalf("elapsed %v, calls %d; want %v, 1 call", elapsed, calls, wantElapsed)
				}
			})
		})
	}
}

func TestStreamCancellationDuringCooldownStaysCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		limiter := newRateLimitTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("cancelled request reached the network")
			return nil, nil
		}))
		limiter.record429("cdn.example", "60")
		client := &Client{http: &http.Client{Transport: limiter}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		time.AfterFunc(streamIdleTimeout/2, cancel)
		err := client.streamToFileContext(ctx, "https://cdn.example/game", filepath.Join(t.TempDir(), "game.gbc"), nil)
		if !errors.Is(err, context.Canceled) || errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("error = %v, want caller cancellation", err)
		}
	})
}
