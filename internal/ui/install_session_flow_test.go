//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

const sessionTestKey = "session-flow-key-3e9d"

// installAPI is an offline api.itch.io for game 42 plus a separate CDN.
type installAPI struct {
	api, cdn *httptest.Server
	files    map[string][]byte // upload ID -> CDN body

	mu       sync.Mutex
	creates  []string // download_key_id per session create
	resolves []string // uuid per resolve
	cdnAuth  []string
}

func newInstallAPI(t *testing.T, uploads string, files map[string][]byte) *installAPI {
	t.Helper()
	f := &installAPI{files: files}
	f.cdn = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.cdnAuth = append(f.cdnAuth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/r2/")
		http.ServeContent(w, r, id, time.Time{}, bytes.NewReader(f.files[id]))
	}))
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sessionTestKey {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/games/42/uploads":
			fmt.Fprint(w, uploads)
		case r.Method == http.MethodPost && r.URL.Path == "/games/42/download-sessions":
			r.ParseForm()
			f.mu.Lock()
			f.creates = append(f.creates, r.PostForm.Get("download_key_id"))
			uuid := fmt.Sprintf("install-%d", len(f.creates))
			f.mu.Unlock()
			fmt.Fprintf(w, `{"uuid":%q}`, uuid)
		case strings.HasPrefix(r.URL.Path, "/uploads/"):
			f.mu.Lock()
			f.resolves = append(f.resolves, r.URL.Query().Get("uuid"))
			f.mu.Unlock()
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/uploads/"), "/download")
			http.Redirect(w, r, f.cdn.URL+"/r2/"+id, http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { f.api.Close(); f.cdn.Close() })
	return f
}

func (f *installAPI) counts() (creates, resolves, cdnAuth []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.creates...), append([]string(nil), f.resolves...), append([]string(nil), f.cdnAuth...)
}

func (f *installAPI) flow(t *testing.T, cfg *settings.Config) *CatDownloadFlow {
	t.Helper()
	inv, err := inventory.Load(filepath.Join(t.TempDir(), "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.AuthToken = sessionTestKey
	return &CatDownloadFlow{
		client: itchio.NewClientWithBase(f.api.URL), cfg: cfg,
		game:   itchio.Game{Title: "Leafbound", URL: "https://dev.itch.io/leafbound"},
		detail: &itchio.GameDetail{GameID: "42"}, inv: inv,
		updates: make(chan catDownloadUpdate, 2),
	}
}

func TestEachPurchaseSelectionStartsOneInstall(t *testing.T) {
	f := newInstallAPI(t, `{"uploads":[{"id":1,"filename":"a.gb"},{"id":2,"filename":"b.gbc"}]}`, nil)
	flow := f.flow(t, &settings.Config{})

	first := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	second := flow.fetchForKey(itchio.OwnedKey{ID: 8})
	if first.err != nil || second.err != nil || len(first.uploads) != 2 {
		t.Fatalf("listings = %+v / %+v", first, second)
	}
	install := first.uploads[0].Install
	if install == nil || first.uploads[1].Install != install || install.DownloadKeyID() != "7" {
		t.Fatal("uploads of one listing do not share one install for purchase 7")
	}
	if second.uploads[0].Install == install || second.uploads[0].Install.DownloadKeyID() != "8" {
		t.Fatal("a second purchase selection reused the first install")
	}
	if (roms.Upload{Filename: "web.gb", URL: "https://dev.itch.io/leafbound/file/1"}).ViaAPI() {
		t.Fatal("a web upload was treated as an API download")
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestProbeAndEveryFileShareOneInstallSession(t *testing.T) {
	primary, _ := transactionPaths(t)
	gbROM := append(make([]byte, 0x104), bytes.Repeat([]byte{0xCE, 0xED}, 24)...)
	f := newInstallAPI(t, `{"uploads":[{"id":1,"filename":"a.gb"},{"id":2,"filename":"b.gbc"}]}`,
		map[string][]byte{"1": gbROM, "2": []byte("GBC-ROM")})
	flow := f.flow(t, &settings.Config{ROMLocation: "auto"})

	listing := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil {
		t.Fatal(listing.err)
	}
	flow.detect(listing.uploads[0]) // a format probe before the download
	if probe := <-flow.updates; probe.err != nil {
		t.Fatal(probe.err)
	}

	downloads := []romDownload{
		{Upload: listing.uploads[0], DestPath: filepath.Join(primary, "Roms", "GB", "a.gb")},
		{Upload: listing.uploads[1], DestPath: filepath.Join(primary, "Roms", "GBC", "b.gbc")},
	}
	worker := NewMultiDownloadWorker(flow.client, flow.cfg, flow.game, flow.detail, downloads, flow.inv,
		filepath.Join(t.TempDir(), "inventory.json"))
	waitFor(t, func() bool { return worker.loadState() != multiDLDownloading })
	if state := worker.CatSnapshot(); state.State != appui.DownloadProgressDone {
		t.Fatalf("multi download = %+v", state)
	}

	creates, resolves, cdnAuth := f.counts()
	if len(creates) != 1 || creates[0] != "7" {
		t.Fatalf("session creates = %q, want one for purchase 7", creates)
	}
	if len(resolves) != 3 {
		t.Fatalf("resolves = %q, want the probe and both files", resolves)
	}
	for _, uuid := range resolves {
		if uuid != "install-1" {
			t.Fatalf("resolves = %q, want every one in install-1", resolves)
		}
	}
	for _, auth := range cdnAuth {
		if auth != "" {
			t.Fatal("the API key reached the CDN")
		}
	}
}

func singleROMZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	entry, err := archive.Create("game.gba")
	if err != nil {
		t.Fatal(err)
	}
	entry.Write(bytes.Repeat([]byte("GBA"), 64))
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Archive inspection, the direct plan it produces, and that plan's download
// all belong to one install; the plan keeps the upload's own source rather
// than the inspected, soon-expiring CDN URL.
func TestArchiveInspectionAndItsDownloadShareOneInstall(t *testing.T) {
	transactionPaths(t)
	f := newInstallAPI(t, `{"uploads":[{"id":5,"filename":"game.zip"}]}`, map[string][]byte{"5": singleROMZip(t)})
	downloadFlow := f.flow(t, &settings.Config{ROMLocation: "auto"})
	listing := downloadFlow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil || len(listing.uploads) != 1 {
		t.Fatalf("listing = %+v", listing)
	}
	upload := listing.uploads[0]

	archive := NewCatArchiveFlow(downloadFlow.client, downloadFlow.cfg, downloadFlow.game, upload, downloadFlow.inv, nil)
	model := archive.Snapshot()
	waitFor(t, func() bool { return archive.Sync(&model) })
	if model.State == appui.DownloadProgressError {
		t.Fatalf("inspection failed: %s", model.Detail)
	}
	if action := archive.TakeAction(); action != CatArchiveStartDirect {
		t.Fatalf("action = %v, want a direct single-ROM ZIP download", action)
	}
	plan := archive.TakeDirectPlan()
	if plan == nil || plan.Uploads[0].Install != upload.Install || plan.Uploads[0].URL != "" {
		t.Fatalf("direct plan upload = %+v, want the same install and no CDN URL", plan.Uploads[0])
	}
	sealed, err := plan.Seal(downloadFlow.game, downloadFlow.detail)
	if err != nil {
		t.Fatal(err)
	}
	file := sealed.Transaction.Files[0]
	worker := NewDirectDownloadWorker(downloadFlow.client, downloadFlow.cfg, downloadFlow.game, downloadFlow.detail,
		file.Upload, file.FinalPath, downloadFlow.inv, filepath.Join(t.TempDir(), "inventory.json"))
	waitFor(t, func() bool { return worker.loadState() != dlDownloading })
	if state := worker.CatSnapshot(); state.State != appui.DownloadProgressDone {
		t.Fatalf("direct download = %+v", state)
	}
	if _, err := os.Stat(file.FinalPath); err != nil {
		t.Fatal(err)
	}

	creates, resolves, _ := f.counts()
	if len(creates) != 1 || len(resolves) != 2 || resolves[0] != "install-1" || resolves[1] != "install-1" {
		t.Fatalf("creates %q resolves %q, want inspection and download in one install", creates, resolves)
	}
}

func TestZIPPlanSealKeepsTheInstall(t *testing.T) {
	install := roms.NewInstallSession("42", "7")
	sealed := ZIPPlan{Upload: roms.Upload{Filename: "game.zip", UploadID: "5", Install: install}}.Seal()
	if sealed.Upload.Install != install {
		t.Fatal("sealing the archive plan dropped its install session")
	}
}
