package itchio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
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
			_, err := client.ResolveUploadURLContext(context.Background(), "key", "9", roms.NewInstallSession("", "777"))
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
		if name == "upload list" {
			// A game whose upload list is gone was removed, which the update
			// checks rely on.
			for _, status := range []int{http.StatusNotFound, http.StatusGone} {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(status)
				}))
				err := call(newClockedClient(srv, newFakeClock()))
				srv.Close()
				if !errors.Is(err, ErrGameRemoved) {
					t.Errorf("%s HTTP %d: err = %v, want it to match ErrGameRemoved", name, status, err)
				}
			}
		}
		noAccess := map[string]error{"upload list": ErrNoAccess, "resolve": ErrDownloadRefused}[name]
		for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
			}))
			err := call(newClockedClient(srv, newFakeClock()))
			srv.Close()
			if !errors.Is(err, noAccess) || err.Error() != noAccess.Error() {
				t.Errorf("%s HTTP %d: err = %v, want the no-access error %q", name, status, err, noAccess)
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
