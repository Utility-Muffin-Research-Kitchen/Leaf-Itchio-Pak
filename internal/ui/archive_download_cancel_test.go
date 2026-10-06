//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// archiveTransfer is an offline web resolver plus a CDN for one archive
// download into a configured primary card.
type archiveTransfer struct {
	client   *itchio.Client
	plan     ZIPPlan
	romDir   string
	inv      *inventory.Inventory
	invPath  string
	resolves *atomic.Int32 // CDN URL resolves the worker asked for
}

func newArchiveTransfer(t *testing.T, cdn http.HandlerFunc) archiveTransfer {
	t.Helper()
	primary, _ := transactionPaths(t)
	cdnServer := httptest.NewServer(cdn)
	t.Cleanup(cdnServer.Close)
	resolves := &atomic.Int32{}
	resolver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resolves.Add(1)
		fmt.Fprintf(w, `{"url":%q}`, cdnServer.URL+"/game.zip")
	}))
	t.Cleanup(resolver.Close)
	invPath := filepath.Join(t.TempDir(), "inventory.json")
	inv, err := inventory.Load(invPath)
	if err != nil {
		t.Fatal(err)
	}
	romDir := filepath.Join(primary, "Roms", "GBC")
	return archiveTransfer{
		client: itchio.NewClientWithBase(resolver.URL),
		plan: ZIPPlan{
			Upload: roms.Upload{Filename: "game.zip", URL: resolver.URL + "/file/5?key=k&csrf=c"},
			CDNURL: cdnServer.URL + "/game.zip",
			Manifest: roms.ZIPManifest{Entries: []roms.ZIPEntry{
				{Name: "game.gbc", Kind: roms.KindROM, Size: 32, CompressedSize: 16},
			}},
			DownloadROMs: true,
			ROMDirs:      map[string]string{".gbc": romDir},
		},
		romDir: romDir, inv: inv, invPath: invPath, resolves: resolves,
	}
}

func (f archiveTransfer) start() *ArchiveDownloadWorker {
	return NewArchiveDownloadWorker(f.client, &settings.Config{},
		itchio.Game{Title: "Leafbound", URL: "https://dev.itch.io/leafbound"}, &itchio.GameDetail{},
		f.plan, f.inv, f.invPath)
}

// stallingCDN sends the first part of a large archive, then goes quiet until
// the client hangs up or the test ends.
func stallingCDN(t *testing.T, streamed *atomic.Int32) http.HandlerFunc {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return func(w http.ResponseWriter, r *http.Request) {
		streamed.Add(1)
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 4096))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}
}

func assertArchiveCancelled(t *testing.T, worker *ArchiveDownloadWorker, dir string) {
	t.Helper()
	model := worker.CatSnapshot()
	if model.State != appui.DownloadProgressCancelled {
		t.Fatalf("state = %v (%q), want cancelled", model.State, model.Detail)
	}
	if strings.Contains(model.Detail, "Download stalled") {
		t.Fatalf("cancel reported a stall: %q", model.Detail)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("file left behind after cancel: %s", entry.Name())
	}
}

func TestArchiveDownloadCancelDuringTransferLeavesNoPartialFile(t *testing.T) {
	var streamed atomic.Int32
	f := newArchiveTransfer(t, stallingCDN(t, &streamed))
	worker := f.start()
	waitFor(t, func() bool { return atomic.LoadInt64(&worker.downloaded) > 0 })
	if model := worker.CatSnapshot(); model.Locked {
		t.Fatal("the archive transfer is locked, so the progress screen offers no B Cancel")
	}

	worker.CatCancel()
	waitFor(t, func() bool { return worker.loadState() != zipDLDownloading })
	assertArchiveCancelled(t, worker, f.romDir)
}

func TestArchiveDownloadCancelDuringCooldownLeavesNoPartialFile(t *testing.T) {
	var streamed atomic.Int32
	stall := stallingCDN(t, &streamed)
	var hits atomic.Int32
	f := newArchiveTransfer(t, func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		stall(w, r)
	})
	// An earlier 429 put the CDN host into a 60-second cooldown.
	primeCooldown(t, f.client, f.plan.CDNURL)

	logs := captureLogs(t)
	worker := f.start()
	waitFor(t, func() bool { return strings.Contains(logs.String(), "ratelimit: waiting") })
	start := time.Now()
	worker.CatCancel()
	waitFor(t, func() bool { return worker.loadState() != zipDLDownloading })
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancel took %v; it waited out the cooldown", elapsed)
	}
	assertArchiveCancelled(t, worker, f.romDir)
}

func TestArchiveExtractionIsLockedAndIgnoresCancel(t *testing.T) {
	worker := &ArchiveDownloadWorker{plan: ZIPPlan{Upload: roms.Upload{Filename: "game.zip"}}}
	worker.ctx, worker.cancel = context.WithCancel(context.Background())
	worker.storeState(zipDLExtracting)
	if model := worker.CatSnapshot(); !model.Locked || model.State != appui.DownloadProgressRunning {
		t.Fatalf("extracting snapshot = %+v, want a locked running screen", model)
	}
	worker.CatCancel()
	if worker.ctx.Err() != nil {
		t.Fatal("cancel reached an extraction that cannot stop halfway")
	}
}

// primeCooldown sends one request that the CDN answers with HTTP 429, so the
// client's shared transport pauses that host. A CDN 429 is not replayed: it
// comes straight back, and later requests to the host wait out the cooldown.
func primeCooldown(t *testing.T, client *itchio.Client, rawURL string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatalf("priming request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("priming request answered HTTP %d, want 429", resp.StatusCode)
	}
}

type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *logCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// captureLogs records every log line, debug included, until the test ends.
func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	capture := &logCapture{}
	log.SetOutput(capture)
	logger.SetLevel(logger.LevelDebug)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		logger.SetLevel(logger.LevelInfo)
	})
	return capture
}

// Every way a run can fail reaches the log at warning level with its cause,
// not only the screen.
func TestArchiveDownloadFailuresAreLoggedWithCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cdn   http.HandlerFunc
		plan  func(*ZIPPlan)
		cause string
	}{
		{
			name:  "transfer",
			cdn:   func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			cause: "file download status 500",
		},
		{
			name:  "preflight",
			cdn:   func(w http.ResponseWriter, r *http.Request) { t.Error("a failed preflight still downloaded") },
			plan:  func(plan *ZIPPlan) { plan.DownloadROMs = false },
			cause: "no selected output files",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newArchiveTransfer(t, tc.cdn)
			if tc.plan != nil {
				tc.plan(&f.plan)
			}
			logs := captureLogs(t)
			worker := f.start()
			waitFor(t, func() bool { return worker.loadState() != zipDLDownloading })
			if state := worker.loadState(); state != zipDLError {
				t.Fatalf("state = %v, want an error", state)
			}
			waitFor(t, func() bool { return hasLogLine(logs.String(), "[WARN]", "zip-download", tc.cause) })
		})
	}
}

func hasLogLine(logs string, parts ...string) bool {
	for _, line := range strings.Split(logs, "\n") {
		found := true
		for _, part := range parts {
			if !strings.Contains(line, part) {
				found = false
				break
			}
		}
		if found {
			return true
		}
	}
	return false
}

// The failed step stays in the log; the screen shows only the cause, which
// starts a sentence.
func TestArchiveDownloadErrorShowsTheCauseWithoutTheStep(t *testing.T) {
	f := newArchiveTransfer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	logs := captureLogs(t)
	worker := f.start()
	waitFor(t, func() bool { return worker.loadState() != zipDLDownloading })
	model := worker.CatSnapshot()
	if model.State != appui.DownloadProgressError || model.Detail == "" || strings.Contains(model.Detail, "download ZIP") {
		t.Fatalf("snapshot = %v %q, want the cause without the step", model.State, model.Detail)
	}
	waitFor(t, func() bool { return hasLogLine(logs.String(), "[WARN]", "download ZIP: "+model.Detail) })
}

// rateLimitingCDN answers HTTP 429 to its first `limited` requests, then
// serves an archive with the fixture plan's one GBC ROM.
func rateLimitingCDN(t *testing.T, limited int32, retryAfter string, hits *atomic.Int32) http.HandlerFunc {
	t.Helper()
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	entry, err := archive.Create("game.gbc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(bytes.Repeat([]byte("G"), 32)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	body := buf.Bytes()
	return func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= limited {
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		http.ServeContent(w, r, "game.zip", time.Time{}, bytes.NewReader(body))
	}
}

func partialFiles(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".itchio-*.part"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// A CDN 429 is not replayed, because the signed URL can expire during the
// cooldown. Like a single-file download, the archive waits the cooldown out,
// resolves a fresh URL once and streams again.
func TestArchiveDownloadRecoversFromOneCDN429WithAFreshURL(t *testing.T) {
	var hits atomic.Int32
	f := newArchiveTransfer(t, rateLimitingCDN(t, 1, "1", &hits))
	worker := f.start()
	waitFor(t, func() bool {
		state := worker.loadState()
		return state != zipDLDownloading && state != zipDLExtracting
	})
	if model := worker.CatSnapshot(); model.State != appui.DownloadProgressDone {
		t.Fatalf("snapshot = %v %q, want a completed download", model.State, model.Detail)
	}
	if resolves, requests := f.resolves.Load(), hits.Load(); resolves != 2 || requests != 2 {
		t.Fatalf("resolves %d, CDN requests %d; want one fresh URL after the 429", resolves, requests)
	}
	if roms, err := filepath.Glob(filepath.Join(f.romDir, "*.gbc")); err != nil || len(roms) != 1 {
		t.Fatalf("extracted ROMs = %v, %v", roms, err)
	}
	waitFor(t, func() bool { return len(partialFiles(t, f.romDir)) == 0 })
}

func TestArchiveDownloadStopsAfterASecondCDN429(t *testing.T) {
	var hits atomic.Int32
	f := newArchiveTransfer(t, rateLimitingCDN(t, 2, "1", &hits))
	worker := f.start()
	waitFor(t, func() bool { return worker.loadState() != zipDLDownloading })
	model := worker.CatSnapshot()
	if model.State != appui.DownloadProgressError || model.Detail != "itch.io is limiting requests. Wait a minute, then try again." {
		t.Fatalf("snapshot = %v %q, want the rate-limit sentence", model.State, model.Detail)
	}
	if resolves, requests := f.resolves.Load(), hits.Load(); resolves != 2 || requests != 2 {
		t.Fatalf("resolves %d, CDN requests %d; want one fresh URL, then the second 429 ends it", resolves, requests)
	}
	waitFor(t, func() bool { return len(partialFiles(t, f.romDir)) == 0 })
}

func TestArchiveDownloadCancelWhileWaitingToResolveAFreshURL(t *testing.T) {
	var hits atomic.Int32
	f := newArchiveTransfer(t, rateLimitingCDN(t, 1, "60", &hits))
	logs := captureLogs(t)
	worker := f.start()
	waitFor(t, func() bool { return strings.Contains(logs.String(), "resolving a fresh URL after its cooldown") })
	start := time.Now()
	worker.CatCancel()
	waitFor(t, func() bool { return worker.loadState() != zipDLDownloading })
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancel took %v; it waited out the cooldown", elapsed)
	}
	assertArchiveCancelled(t, worker, f.romDir)
	if resolves, requests := f.resolves.Load(), hits.Load(); resolves != 1 || requests != 1 {
		t.Fatalf("resolves %d, CDN requests %d; want no fresh URL after the cancel", resolves, requests)
	}
}
