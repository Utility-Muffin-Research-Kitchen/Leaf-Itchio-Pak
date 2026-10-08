package itchio

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
)

// A request that got no answer is marked with netlimit.ErrNetwork, so the
// screens can say "Can't reach itch.io" without the operation or the URL.
// The log text stays "<operation>: network request failed" or "...timeout".
func TestSafeRequestErrorMarksNetworkFailures(t *testing.T) {
	const signed = "https://itch.io/api/1/key/upload/1/download?api_key=secret"
	for _, tc := range []struct {
		name, want string
		cause      error
	}{
		{"dropped", "fetch game page: network request failed", errors.New("http2: client connection lost")},
		{"timeout", "fetch game page: network timeout", os.ErrDeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := safeRequestError("fetch game page", &url.Error{Op: "Get", URL: signed, Err: tc.cause})
			if !errors.Is(err, netlimit.ErrNetwork) || err.Error() != tc.want {
				t.Fatalf("error = %q (network %v), want %q marked ErrNetwork", err, errors.Is(err, netlimit.ErrNetwork), tc.want)
			}
		})
	}
	if err := safeRequestError("fetch game page", context.Canceled); errors.Is(err, netlimit.ErrNetwork) {
		t.Fatalf("cancellation %q is marked as a network failure", err)
	}
}

// The stalled-download message is matched by ErrDownloadStalled, whatever
// step reports it, and reads the same as before.
func TestDownloadStallMatchesErrDownloadStalled(t *testing.T) {
	var stall error = downloadIdleTimeout{}
	if !errors.Is(stall, ErrDownloadStalled) || stall.Error() != "Download stalled. Check the connection and try again." {
		t.Fatalf("stall = %q, want ErrDownloadStalled", stall)
	}
	if !errors.Is(stall, os.ErrDeadlineExceeded) {
		t.Fatal("stall no longer unwraps to a deadline")
	}
	if strings.Contains(ErrDownloadStalled.Error(), ":") {
		t.Fatalf("stall text %q has a prefix", ErrDownloadStalled)
	}
}
