package inventory_test

import (
	"bytes"
	"crypto/sha256"
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

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

type artworkLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *artworkLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

// lines returns the cover-art log lines, debug included.
func (l *artworkLogs) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, line := range strings.Split(l.buf.String(), "\n") {
		if strings.Contains(line, "cover-art:") {
			out = append(out, line)
		}
	}
	return out
}

func (l *artworkLogs) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Reset()
}

func captureArtworkLogs(t *testing.T) *artworkLogs {
	t.Helper()
	capture := &artworkLogs{}
	log.SetOutput(capture)
	logger.SetLevel(logger.LevelDebug)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		logger.SetLevel(logger.LevelInfo)
	})
	return capture
}

// artworkPassFixture is one installed GB ROM with launcher art on disk and a
// server that counts cover downloads.
type artworkPassFixture struct {
	dir, romPath, artPath, invPath, gameURL, coverURL string
	art                                               []byte
	covers                                            *atomic.Int32
	client                                            *itchio.Client
}

func newArtworkPassFixture(t *testing.T) artworkPassFixture {
	t.Helper()
	covers := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.png" {
			covers.Add(1)
			w.Header().Set("Content-Type", "image/png")
			w.Write(minimalPNG())
			return
		}
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	t.Cleanup(func() {
		// Later tests expect the paths TestMain set.
		if err := configureInventoryTestPaths(); err != nil {
			t.Error(err)
		}
	})
	fixture := artworkPassFixture{
		dir:      dir,
		romPath:  filepath.Join(dir, "game.gb"),
		invPath:  filepath.Join(dir, "inventory.json"),
		gameURL:  srv.URL + "/game",
		coverURL: srv.URL + "/cover.png",
		art:      minimalPNG(),
		covers:   covers,
		client:   itchio.NewClientWithBase(srv.URL),
	}
	fixture.artPath = inventory.CanonicalArtworkPath(fixture.romPath)
	for path, data := range map[string][]byte{fixture.romPath: []byte("ROM"), fixture.artPath: fixture.art} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (f artworkPassFixture) inventory(file inventory.DownloadedFile) *inventory.Inventory {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	file.Filename, file.DestPath = "game.gb", f.romPath
	inv.Add(f.gameURL, inventory.Entry{Title: "G", CoverURL: f.coverURL}, file)
	return inv
}

// runLaunchCheck runs one launch check, as at every app start.
func (f artworkPassFixture) runLaunchCheck(t *testing.T, inv *inventory.Inventory) {
	t.Helper()
	done := make(chan struct{}, 1)
	svc := inventory.NewUpdateService(inv, f.invPath, f.client, nil)
	svc.Start(func() {
		select {
		case done <- struct{}{}:
		default:
		}
	})
	<-done
	svc.Stop()
}

// keptArtworkLine matches the line for kept art at the fixture's (redacted)
// path.
func keptArtworkLine(line, level, owner string) bool {
	return strings.Contains(line, level) &&
		strings.Contains(line, "cover-art: keeping "+owner+" artwork ") &&
		strings.HasSuffix(line, filepath.Join("Images", "GB", "game.png"))
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// F7: artwork the app created, unchanged since, needs no work and no log
// line at every launch, sign-in change or Update Inventory.
func TestArtworkPassSkipsCurrentAppArtworkSilently(t *testing.T) {
	fixture := newArtworkPassFixture(t)
	logs := captureArtworkLogs(t)
	hash := sha256Hex(fixture.art)
	inv := fixture.inventory(inventory.DownloadedFile{ArtworkPath: fixture.artPath, ArtworkHash: hash, ArtworkCreated: true})

	fixture.runLaunchCheck(t, inv)

	if lines := logs.lines(); len(lines) != 0 {
		t.Fatalf("current app artwork logged:\n%s", strings.Join(lines, "\n"))
	}
	if n := fixture.covers.Load(); n != 0 {
		t.Fatalf("cover downloads = %d, want 0", n)
	}
	entry, _ := inv.Lookup(fixture.gameURL)
	if file := entry.Files[0]; !file.ArtworkCreated || file.ArtworkHash != hash || file.ArtworkPath != fixture.artPath {
		t.Fatalf("app artwork metadata changed: %+v", file)
	}
}

// User artwork the inventory already recorded is current too: the pass
// leaves it alone without saying so again.
func TestArtworkPassSkipsRecordedUserArtworkSilently(t *testing.T) {
	fixture := newArtworkPassFixture(t)
	logs := captureArtworkLogs(t)
	inv := fixture.inventory(inventory.DownloadedFile{})

	fixture.runLaunchCheck(t, inv)
	lines := logs.lines()
	if len(lines) != 1 || !keptArtworkLine(lines[0], "[INFO]", "user") {
		t.Fatalf("first pass over user artwork logged:\n%s", strings.Join(lines, "\n"))
	}
	entry, _ := inv.Lookup(fixture.gameURL)
	if file := entry.Files[0]; file.ArtworkCreated || file.ArtworkHash != sha256Hex(fixture.art) || file.ArtworkPath != fixture.artPath {
		t.Fatalf("user artwork metadata = %+v", file)
	}

	logs.reset()
	fixture.runLaunchCheck(t, inv)
	if lines := logs.lines(); len(lines) != 0 {
		t.Fatalf("recorded user artwork logged again:\n%s", strings.Join(lines, "\n"))
	}
	if got, err := os.ReadFile(fixture.artPath); err != nil || !bytes.Equal(got, fixture.art) {
		t.Fatalf("user artwork changed: %v", err)
	}
	if n := fixture.covers.Load(); n != 0 {
		t.Fatalf("cover downloads = %d, want 0", n)
	}
}

// App artwork recorded without a hash (an older inventory) is kept and said
// to be app artwork, at debug level only.
func TestArtworkPassKeepsUnhashedAppArtworkAtDebug(t *testing.T) {
	fixture := newArtworkPassFixture(t)
	logs := captureArtworkLogs(t)
	inv := fixture.inventory(inventory.DownloadedFile{ArtworkPath: fixture.artPath, ArtworkCreated: true})

	fixture.runLaunchCheck(t, inv)

	lines := logs.lines()
	if len(lines) != 1 || !keptArtworkLine(lines[0], "[DEBUG]", "app") {
		t.Fatalf("kept app artwork logged:\n%s", strings.Join(lines, "\n"))
	}
	entry, _ := inv.Lookup(fixture.gameURL)
	if file := entry.Files[0]; !file.ArtworkCreated || file.ArtworkHash != sha256Hex(fixture.art) {
		t.Fatalf("app artwork metadata = %+v", file)
	}
}

// Art the user put over the app's own is user artwork from then on: never
// overwritten, and no longer the app's to delete or rename.
func TestArtworkPassHandsReplacedAppArtworkToTheUser(t *testing.T) {
	fixture := newArtworkPassFixture(t)
	logs := captureArtworkLogs(t)
	inv := fixture.inventory(inventory.DownloadedFile{ArtworkPath: fixture.artPath,
		ArtworkHash: sha256Hex(fixture.art), ArtworkCreated: true})
	userArt := []byte("user-art")
	if err := os.WriteFile(fixture.artPath, userArt, 0o644); err != nil {
		t.Fatal(err)
	}

	fixture.runLaunchCheck(t, inv)

	lines := logs.lines()
	if len(lines) != 1 || !keptArtworkLine(lines[0], "[INFO]", "user") {
		t.Fatalf("replaced artwork logged:\n%s", strings.Join(lines, "\n"))
	}
	if got, err := os.ReadFile(fixture.artPath); err != nil || !bytes.Equal(got, userArt) {
		t.Fatalf("user artwork overwritten: %q, %v", got, err)
	}
	entry, _ := inv.Lookup(fixture.gameURL)
	if file := entry.Files[0]; file.ArtworkCreated || file.ArtworkHash != sha256Hex(userArt) {
		t.Fatalf("replaced artwork metadata = %+v", file)
	}
	if n := fixture.covers.Load(); n != 0 {
		t.Fatalf("cover downloads = %d, want 0", n)
	}
}

// Missing artwork is still repaired, recorded as the app's.
func TestArtworkPassRepairsDeletedAppArtwork(t *testing.T) {
	fixture := newArtworkPassFixture(t)
	captureArtworkLogs(t)
	inv := fixture.inventory(inventory.DownloadedFile{ArtworkPath: fixture.artPath,
		ArtworkHash: sha256Hex(fixture.art), ArtworkCreated: true})
	if err := os.Remove(fixture.artPath); err != nil {
		t.Fatal(err)
	}

	fixture.runLaunchCheck(t, inv)

	if n := fixture.covers.Load(); n != 1 {
		t.Fatalf("cover downloads = %d, want 1", n)
	}
	data, err := os.ReadFile(fixture.artPath)
	if err != nil {
		t.Fatalf("artwork not repaired: %v", err)
	}
	entry, _ := inv.Lookup(fixture.gameURL)
	if file := entry.Files[0]; !file.ArtworkCreated || file.ArtworkHash != sha256Hex(data) {
		t.Fatalf("repaired artwork metadata = %+v", file)
	}
}
