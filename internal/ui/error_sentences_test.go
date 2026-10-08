//go:build !headless

package ui

import (
	"archive/zip"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/netlimit"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

const cantReachItchio = "Can't reach itch.io. Check the connection and try again."

// assertScreenSentence fails when a screen shows an error's own text: an
// operation prefix ("fetch game page: "), a URL, or Go's wording.
func assertScreenSentence(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("screen shows %q, want %q", got, want)
	}
	if strings.Contains(got, ":") || strings.Contains(got, "://") {
		t.Fatalf("screen shows %q, which carries a prefix or a URL", got)
	}
}

// The device showed "fetch game page: network request failed" on the
// download screen with the network down. The same lookup now says what is
// wrong in a sentence.
func TestDownloadDiscoveryOfflineSaysItCannotReachItchio(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	gameURL := srv.URL + "/game"
	srv.Close()
	done := make(chan struct{}, 1)
	flow := NewCatDownloadFlow(itchio.NewClientWithBase(srv.URL), &settings.Config{ROMLocation: "auto"},
		itchio.Game{Title: "Offline", URL: gameURL, IsFree: true}, nil,
		&inventory.Inventory{Entries: map[string]*inventory.Entry{}}, func() { done <- struct{}{} })
	defer flow.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("discovery did not finish")
	}
	model := appui.NewDownloadSelectModel("Offline")
	if !flow.Sync(model) || model.State != appui.DownloadSelectError {
		t.Fatalf("model = %+v, want the error state", model)
	}
	assertScreenSentence(t, model.Message, cantReachItchio)
}

func TestDownloadDiscoveryErrorsReadAsSentences(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not owned", itchio.ErrNotOwned, "Your itch.io account doesn't own this game."},
		{"rate limited", &netlimit.RateLimitedError{Host: "api.itch.io"},
			"itch.io is limiting requests. Wait a minute, then try again."},
		{"removed", fmt.Errorf("fetch game page: %w", itchio.ErrGameRemoved), "This game was removed from itch.io."},
		{"no web download", fmt.Errorf("download_url POST: %w", itchio.ErrNoWebDownload),
			"This download needs an itch.io account. Sign in from Settings, then try again."},
		{"server error", errors.New("fetch uploads: HTTP 502"), "Something went wrong. Try again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flow, model := newCatDownloadFlowForTest(t)
			flow.updates = make(chan catDownloadUpdate, 1)
			flow.updates <- catDownloadUpdate{err: tc.err}
			if !flow.Sync(model) || model.State != appui.DownloadSelectError {
				t.Fatalf("model = %+v, want the error state", model)
			}
			assertScreenSentence(t, model.Message, tc.want)
		})
	}
}

func TestArchiveInspectionErrorsReadAsSentences(t *testing.T) {
	tooMany := roms.ZIPManifest{}
	for range DefaultArchiveLimits.MaxEntries + 1 {
		tooMany.Entries = append(tooMany.Entries, roms.ZIPEntry{Name: "x.gbc", Size: 1})
	}
	for _, tc := range []struct {
		name   string
		update catArchiveUpdate
		want   string
	}{
		{"offline", catArchiveUpdate{err: fmt.Errorf("zip-inspect: range probe: %w", netlimit.ErrNetwork)},
			cantReachItchio},
		{"not an archive", catArchiveUpdate{err: fmt.Errorf("zip.NewReader: %w", roms.UnreadableArchive(zip.ErrFormat))},
			"Leaf can't read this archive. It may be damaged or in an unsupported format."},
		{"too many files", catArchiveUpdate{plan: ZIPPlan{Manifest: tooMany}},
			"This archive has more files than Leaf can install at once."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flow := &CatArchiveFlow{cfg: &settings.Config{}, updates: make(chan catArchiveUpdate, 1)}
			flow.updates <- tc.update
			model := &appui.DownloadProgressModel{}
			if !flow.Sync(model) || model.State != appui.DownloadProgressError {
				t.Fatalf("model = %+v, want the error state", model)
			}
			assertScreenSentence(t, model.Detail, tc.want)
		})
	}
}

// The game page names the problem and how to retry it, here by reopening
// the page.
func TestDetailOfflineSaysItCannotReachItchio(t *testing.T) {
	model := syncFailedDetail(t, fmt.Errorf("fetch game detail: %w", netlimit.ErrNetwork))
	assertScreenSentence(t, model.ErrorDetail, "Can't reach itch.io. Check the connection, then reopen this game.")
}

func TestDownloadFailuresReadAsSentences(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"offline", fmt.Errorf("fetch file: %w", netlimit.ErrNetwork), cantReachItchio},
		{"stalled", itchio.ErrDownloadStalled, "Download stalled. Check the connection and try again."},
		{"card full", fmt.Errorf("download storage preflight: %w", fmt.Errorf("%w: need 5 bytes", leaf.ErrNoSpace)),
			"The SD card is full. Free up some space, then try again."},
		{"server error", errors.New("file download status 500"), "Something went wrong. Try again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			direct := &DirectDownloadWorker{state: dlError, err: tc.err}
			assertScreenSentence(t, direct.CatSnapshot().Detail, tc.want)
			multi := &MultiDownloadWorker{state: int32(multiDLError), err: tc.err}
			assertScreenSentence(t, multi.CatSnapshot().Detail, tc.want)
			archive := &ArchiveDownloadWorker{state: zipDLError, err: fmt.Errorf("download ZIP: %w", tc.err)}
			assertScreenSentence(t, archive.CatSnapshot().Detail, tc.want)
		})
	}
}

// Without Jawaka the download asks whether to go on unprotected. The
// prompt explains that, without the daemon's own error text.
func TestSuspendProtectionPromptIsASentence(t *testing.T) {
	guardErr := fmt.Errorf("%w: dial unix /run/jawakad.sock: connect: no such file or directory", leaf.ErrDaemonUnavailable)
	err := inhibitBlockedError(guardErr)
	direct := &DirectDownloadWorker{state: dlError, err: err}
	direct.inhibitBlocked.Store(true)
	model := direct.CatSnapshot()
	if model.State != appui.DownloadProgressInhibitBlocked {
		t.Fatalf("state = %v, want the suspend prompt", model.State)
	}
	assertScreenSentence(t, model.Detail,
		"Jawaka is unavailable, so Leaf can't prevent suspend during this download. Press A to download without that protection, or B to cancel.")
	if !errors.Is(err, leaf.ErrDaemonUnavailable) || !strings.Contains(err.Error(), "jawakad.sock") {
		t.Fatalf("log text %q lost the cause", err)
	}
}

func TestCatalogErrorReadsAsASentence(t *testing.T) {
	controller := &CatalogController{cfg: &settings.Config{},
		inv: &inventory.Inventory{Entries: map[string]*inventory.Entry{}},
		err: fmt.Errorf("fetch feed: %w", &netlimit.RateLimitedError{Host: "itch.io"})}
	model := appui.NewMainListModel(nil)
	controller.SyncCatModel(model)
	assertScreenSentence(t, model.ErrorDetail, "itch.io is limiting requests. Wait a minute, then try again.")
	// The feed's own request errors arrive unwrapped, URL and all.
	controller.err = fmt.Errorf("platform=gb slug=made-with-gb-studio page 1: fetch feed: %w",
		&url.Error{Op: "Get", URL: "https://itch.io/games/made-with-gb-studio.xml",
			Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "itch.io"}}})
	controller.SyncCatModel(model)
	assertScreenSentence(t, model.ErrorDetail, cantReachItchio)
	controller.err = errors.New("parse feed xml: XML syntax error on line 1: unexpected EOF")
	controller.SyncCatModel(model)
	assertScreenSentence(t, model.ErrorDetail, "Something went wrong. Try again.")
}

// Renames you cannot do say why in a sentence.
func TestRenameRefusalsReadAsSentences(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	gbc := filepath.Join(sources[0].RomsPath, "GBC")
	rename := func(t *testing.T, inv *inventory.Inventory, gameURL string) string {
		t.Helper()
		_, _, err := NewCatRenameFlow(inv, cfgPath, gameURL, 0, sources)
		if err == nil {
			t.Fatal("rename was offered")
		}
		return screentext.FromError(err)
	}
	t.Run("same name", func(t *testing.T) {
		inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
		addManagedROM(t, inv, "https://example.invalid/same", "Leafbound", filepath.Join(gbc, "Leafbound.gbc"))
		assertScreenSentence(t, rename(t, inv, "https://example.invalid/same"), "This ROM already has that name.")
	})
	t.Run("disc file", func(t *testing.T) {
		inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
		addManagedROM(t, inv, "https://example.invalid/disc", "Leafbound Deluxe", filepath.Join(gbc, "Leafbound.cue"))
		assertScreenSentence(t, rename(t, inv, "https://example.invalid/disc"),
			"PlayStation disc files keep their original names, so the game still finds them.")
	})
	t.Run("upload name taken", func(t *testing.T) {
		// Going back to the upload's own name, which another file now has.
		inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
		dest := filepath.Join(gbc, "Leafbound Two.gbc")
		addManagedROM(t, inv, "https://example.invalid/taken", "Leafbound Two", dest)
		inv.UpdateFile("https://example.invalid/taken", dest, inventory.DownloadedFile{
			Filename: "leafbound_v2.gbc", DestPath: dest, UnifiedName: true,
		})
		if err := os.WriteFile(filepath.Join(gbc, "leafbound_v2.gbc"), []byte("other"), 0o644); err != nil {
			t.Fatal(err)
		}
		assertScreenSentence(t, rename(t, inv, "https://example.invalid/taken"), "A file named leafbound_v2.gbc already exists.")
	})
}

// Two uploads saved under one name stop the download before anything is
// written, and the screen names the clash.
func TestDownloadPlanClashReadsAsASentence(t *testing.T) {
	err := screentext.FromError(fmt.Errorf("plan: %w", duplicateDestinationError("/mnt/sdcard/Roms/GBC/Leafbound.gbc")))
	assertScreenSentence(t, err, "Two files in this download would be saved as Leafbound.gbc.")
}
