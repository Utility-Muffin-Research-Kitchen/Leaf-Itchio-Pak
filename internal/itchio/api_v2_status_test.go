package itchio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The API v2 calls a download makes turn every expected failure status into
// a sentence for the screen, never "HTTP 404" or "status 410".
func TestAPIV2StatusesBecomeScreenText(t *testing.T) {
	calls := map[string]func(*Client) error{
		"upload list": func(client *Client) error {
			_, err := client.FetchUploadsContext(context.Background(), "key", "42", "777")
			return err
		},
		"resolve": func(client *Client) error {
			_, err := client.ResolveUploadURLContext(context.Background(), "key", "9", NewInstallSession("", "777"))
			return err
		},
	}
	statuses := map[int]error{
		http.StatusNotFound:        ErrUploadGone,
		http.StatusGone:            ErrUploadGone,
		http.StatusTooManyRequests: ErrRateLimited,
	}
	for name, call := range calls {
		for status, want := range statuses {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			err := call(newClockedClient(srv, newFakeClock()))
			srv.Close()
			if !errors.Is(err, want) || err.Error() != want.Error() {
				t.Errorf("%s HTTP %d: err = %v, want %q", name, status, err, want)
			}
		}
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			err := call(newClockedClient(srv, newFakeClock()))
			srv.Close()
			if err == nil || !strings.Contains(err.Error(), "does not grant access") {
				t.Errorf("%s HTTP %d: err = %v, want the no-access error", name, status, err)
			}
		}
	}
}

func TestOwnedKeysRateLimitIsScreenText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := newClockedClient(srv, newFakeClock()).FetchOwnedKeysContext(context.Background(), "key", "42")
	if !errors.Is(err, ErrRateLimited) || err.Error() != ErrRateLimited.Error() {
		t.Fatalf("err = %v, want %q", err, ErrRateLimited)
	}
}
