package screentext_test

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
)

const (
	network = "Can't reach itch.io. Check the connection and try again."
	generic = "Something went wrong. Try again."
	storage = "Couldn't read or write the SD card. Check the card, then try again."
)

// signedURL is what a raw request error carries: the full request URL,
// which can hold a download key.
const signedURL = "https://itch.io/api/1/key/upload/123/download?api_key=secret"

func TestFromErrorMapsEachErrorClassToASentence(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		kind screentext.Kind
		want string
	}{
		// The device showed both of these under "Could not continue".
		{"network failure with an operation prefix",
			fmt.Errorf("fetch game page: %w", netlimit.ErrNetwork), screentext.Network, network},
		{"raw request error with a URL",
			fmt.Errorf("fetch game page: %w", &url.Error{Op: "Get", URL: signedURL,
				Err: errors.New("http2: client connection lost")}), screentext.Network, network},
		{"network timeout", fmt.Errorf("fetch owned uploads: %w", netlimit.ErrNetworkTimeout),
			screentext.Network, network},
		{"dropped body read", errors.New("read game page: http2: client connection lost"),
			screentext.Network, network},
		{"refused connection", fmt.Errorf("read feed: %w",
			&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}), screentext.Network, network},
		{"deadline", fmt.Errorf("fetch profile: %w", context.DeadlineExceeded), screentext.Network, network},

		// Sentences other screens already show stay exactly as they are.
		{"rate limited", fmt.Errorf("fetch game page: %w", &netlimit.RateLimitedError{Host: "itch.io"}),
			screentext.RateLimited, "itch.io is limiting requests. Wait a minute, then try again."},
		{"stalled download", fmt.Errorf("download ZIP: %w", itchio.ErrDownloadStalled),
			screentext.Stalled, "Download stalled. Check the connection and try again."},
		{"removed game", fmt.Errorf("fetch game page: %w", itchio.ErrGameRemoved),
			screentext.GameRemoved, "This game was removed from itch.io."},
		{"upload gone", fmt.Errorf("resolve upload: %w", itchio.ErrUploadGone),
			screentext.UploadGone, "This file is no longer on itch.io."},
		{"not owned", itchio.ErrNotOwned,
			screentext.NotOwned, "Your itch.io account doesn't own this game."},
		{"no access", fmt.Errorf("fetch uploads: %w", itchio.ErrNoAccess),
			screentext.NoAccess, "Your itch.io account doesn't have access to this game's downloads."},
		{"download refused", fmt.Errorf("resolve upload: %w", itchio.ErrDownloadRefused),
			screentext.DownloadRefused, "Your itch.io account doesn't have access to this download."},
		{"no web download", fmt.Errorf("download_url POST: %w", itchio.ErrNoWebDownload),
			screentext.SignInNeeded, "This download needs an itch.io account. Sign in from Settings, then try again."},
		{"blocked", fmt.Errorf("fetch total games: %w", itchio.ErrCloudflareBlocked),
			screentext.Blocked, "itch.io blocked the request. Try again later."},
		{"suspend protection", fmt.Errorf("protect delete batch: %w: dial unix /run/jawakad.sock: connect: no such file or directory",
			leaf.ErrDaemonUnavailable), screentext.SuspendUnavailable,
			"Jawaka is unavailable, so Leaf can't prevent suspend. Nothing was changed. Try again."},
		{"cancelled", fmt.Errorf("fetch uploads: %w", context.Canceled), screentext.Cancelled, "Cancelled."},
		{"unreadable zip", fmt.Errorf("zip.NewReader: %w", zip.ErrFormat),
			screentext.UnreadableArchive, "Leaf can't read this archive. It may be damaged or in an unsupported format."},
		{"unreadable 7z", fmt.Errorf("sevenzip.OpenReader: %w",
			roms.UnreadableArchive(errors.New("sevenzip: not a valid 7-zip file"))),
			screentext.UnreadableArchive, "Leaf can't read this archive. It may be damaged or in an unsupported format."},
		{"card full", fmt.Errorf("write temp: %w",
			&fs.PathError{Op: "write", Path: "/mnt/sdcard/Roms/.itchio-download-1.part", Err: syscall.ENOSPC}),
			screentext.StorageFull, "The SD card is full. Free up some space, then try again."},
		{"read-only card", fmt.Errorf("save settings: %w",
			&fs.PathError{Op: "open", Path: "/mnt/sdcard/.userdata/mlp1/Itch-io/config.json", Err: syscall.EROFS}),
			screentext.Storage, storage},

		// Anything else gets the same plain sentence, never its own text.
		{"HTTP status", errors.New("fetch game detail: HTTP 503"), screentext.Unknown, generic},
		{"parse failure", fmt.Errorf("parse feed xml: %w", errors.New("XML syntax error on line 1: unexpected EOF")),
			screentext.Unknown, generic},
		{"internal invariant", errors.New("download transaction has inconsistent file destinations"),
			screentext.Unknown, generic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := screentext.Classify(tc.err); got != tc.kind {
				t.Fatalf("Classify = %v, want %v", got, tc.kind)
			}
			got := screentext.FromError(tc.err)
			if got != tc.want {
				t.Fatalf("FromError = %q, want %q", got, tc.want)
			}
			assertPlainSentence(t, got, tc.err)
		})
	}
}

// A message written for the screen is shown as is, even around an error
// that has a sentence of its own, and the cause stays in the log text.
func TestFromErrorShowsMessagesWrittenForTheScreen(t *testing.T) {
	if got := screentext.FromError(screentext.New("Only ROM files can be renamed.")); got != "Only ROM files can be renamed." {
		t.Fatalf("New = %q", got)
	}
	cause := &fs.PathError{Op: "rename", Path: "/mnt/sdcard/Roms/GBC/Game.gbc", Err: syscall.EROFS}
	err := fmt.Errorf("manage: %w", screentext.Wrap(cause, "Couldn't rename Game.gbc."))
	if got := screentext.FromError(err); got != "Couldn't rename Game.gbc." {
		t.Fatalf("Wrap = %q", got)
	}
	if !errors.Is(err, syscall.EROFS) {
		t.Fatal("the cause no longer matches through Wrap")
	}
	if !strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("log text %q lost the cause %q", err.Error(), cause.Error())
	}
	if got := screentext.FromError(screentext.Wrap(nil, "No SD card was found.")); got != "No SD card was found." {
		t.Fatalf("Wrap(nil) = %q", got)
	}
}

func TestFromErrorOfNoErrorIsEmpty(t *testing.T) {
	if got := screentext.FromError(nil); got != "" {
		t.Fatalf("FromError(nil) = %q, want empty", got)
	}
}

// assertPlainSentence fails when text could be an error's own wording: a
// URL, an operation prefix ("fetch game page: "), or Go's error text.
func assertPlainSentence(t *testing.T, text string, err error) {
	t.Helper()
	if strings.Contains(text, "://") || strings.Contains(text, ":") {
		t.Fatalf("%q carries a URL or an operation prefix", text)
	}
	if !strings.HasSuffix(text, ".") {
		t.Fatalf("%q is not a sentence", text)
	}
	for _, raw := range []string{"secret", "http2", "HTTP", "fetch", "dial", "zip.", "sevenzip", "/mnt/"} {
		if strings.Contains(text, raw) {
			t.Fatalf("%q carries %q from %q", text, raw, err)
		}
	}
}
