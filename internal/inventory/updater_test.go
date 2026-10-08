package inventory_test

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func configureUpdaterPaths(t *testing.T, root string) {
	t.Helper()
	systems := make(map[string]string)
	images := make(map[string]string)
	for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"} {
		systems[id] = filepath.Join(root, "unused", id)
		images[id] = filepath.Join(root, "Images", id)
	}
	systems["GB"] = root
	if err := roms.ConfigurePaths(roms.PathConfig{
		SystemDirs: systems, ImageDirs: images, SourceID: "primary", PrimaryRoot: root,
		MusicRoot: filepath.Join(root, "Music"), StatesRoot: filepath.Join(root, "States"),
	}); err != nil {
		t.Fatal(err)
	}
}

// minimalPNG returns the bytes of a 1x1 white PNG.
func minimalPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// laterLaunch makes the next launch happen more than six hours after the
// last check, so its automatic check covers the entry again.
func laterLaunch(inv *inventory.Inventory, gameURL string) {
	inv.Entries[gameURL].UpdateCheckedAt = time.Now().Add(-7 * time.Hour)
}

func TestUpdateService_RepairsMissingCoverArt(t *testing.T) {
	pngData := minimalPNG()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.png" {
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngData)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	if err := os.WriteFile(romPath, []byte("ROM"), 0644); err != nil {
		t.Fatal(err)
	}

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	// Use srv.URL+"/game" as gameURL; FetchUploads will 404 but cover art runs first.
	gameURL := srv.URL + "/game"
	inv.Add(gameURL,
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	if err := inv.Save(invPath); err != nil {
		t.Fatal(err)
	}

	client := itchio.NewClientWithBase(srv.URL)
	svc := inventory.NewUpdateService(inv, invPath, client, nil)

	done := make(chan struct{})
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	if _, err := os.Stat(artPath); err != nil {
		t.Errorf("cover art not created at %s: %v", artPath, err)
	}
}

func TestUpdateService_SkipsCoverArtIfPresent(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cover.png" {
			callCount++
			w.Header().Set("Content-Type", "image/png")
			w.Write(minimalPNG())
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)

	// Pre-create the cover art so it already exists.
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	// IsFree: false → checkPaidGame; no FetchUploads call, avoids file-listing HTTP traffic.
	gameURL := srv.URL + "/game"
	inv.Add(gameURL,
		inventory.Entry{Title: "G", IsFree: false, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	done := make(chan struct{})
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	if callCount != 0 {
		t.Errorf("cover art HTTP GET called %d times, want 0 (art already present)", callCount)
	}
	entry, _ := inv.Lookup(gameURL)
	if len(entry.Files) != 1 || entry.Files[0].ArtworkPath != artPath ||
		entry.Files[0].ArtworkHash == "" || entry.Files[0].ArtworkCreated {
		t.Fatalf("existing user artwork metadata = %+v", entry.Files)
	}
}

func TestUpdateServiceRetainsMatchingAppArtworkOwnership(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "offline", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	artPath := inventory.CanonicalArtworkPath(romPath)
	art := minimalPNG()
	for path, data := range map[string][]byte{romPath: []byte("ROM"), artPath: art} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(art))
	gameURL := srv.URL + "/game"
	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add(gameURL, inventory.Entry{Title: "G", IsFree: false, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, ArtworkPath: artPath,
			ArtworkHash: hash, ArtworkCreated: true})
	client := itchio.NewClientWithBase(srv.URL)
	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	entry, _ := inv.Lookup(gameURL)
	file := entry.Files[0]
	if !file.ArtworkCreated || file.ArtworkHash != hash || file.ArtworkPath != artPath {
		t.Fatalf("app-owned artwork metadata changed: %+v", file)
	}
}

func TestUpdateServiceMigratesOwnedMediaArtworkAndRequestsScan(t *testing.T) {
	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	oldArt := filepath.Join(dir, ".media", "game.png")
	art := minimalPNG()
	for path, data := range map[string][]byte{romPath: []byte("ROM"), oldArt: art} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(art))
	gameURL := "https://example.invalid/game"
	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add(gameURL, inventory.Entry{Title: "G", CoverURL: "https://example.invalid/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, ArtworkPath: oldArt,
			ArtworkHash: hash, ArtworkCreated: true})
	scans := 0
	svc := inventory.NewUpdateService(inv, invPath, itchio.NewClientWithBase("http://127.0.0.1:1"), nil)
	svc.SetSources(leaf.SourceList{{ID: "primary", Root: dir, Primary: true}})
	svc.SetLibraryScanRequester(func() (string, error) {
		scans++
		return "scan-library queued", nil
	})
	done := make(chan struct{})
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	expected := inventory.CanonicalArtworkPath(romPath)
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("canonical artwork was not created: %v", err)
	}
	if _, err := os.Stat(oldArt); !os.IsNotExist(err) {
		t.Fatalf("old app-owned artwork remains: %v", err)
	}
	entry, _ := inv.Lookup(gameURL)
	if len(entry.Files) != 1 || entry.Files[0].ArtworkPath != expected ||
		entry.Files[0].ArtworkHash != hash || !entry.Files[0].ArtworkCreated {
		t.Fatalf("migrated inventory metadata = %+v", entry.Files)
	}
	if scans != 1 {
		t.Fatalf("library scan requests = %d, want 1", scans)
	}
}

// freeGameServer exposes public upload names without starting any download.
func freeGameServer(t *testing.T, status int, filenames []string) *httptest.Server {
	t.Helper()
	return publicGameServer(t, status, &filenames)
}

func freeGameServerDynamic(t *testing.T, filenames *[]string) *httptest.Server {
	t.Helper()
	return publicGameServer(t, http.StatusOK, filenames)
}

func publicGameServer(t *testing.T, status int, filenames *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/game" {
			t.Errorf("background check started an unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if status != http.StatusOK {
			http.Error(w, "not found", status)
			return
		}
		for _, name := range *filenames {
			fmt.Fprintf(w, `<div class="upload"><strong class="name" title="%s">%s</strong></div>`, name, name)
		}
	}))
}

func TestUpdateService_Marks404AsRemoved(t *testing.T) {
	srv := freeGameServer(t, http.StatusNotFound, nil)
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	// Pre-create cover art so repair is skipped.
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add(srv.URL+"/game",
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)
	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	if !inv.IsRemoved(srv.URL + "/game") {
		t.Error("IsRemoved: want true after 404 from upstream")
	}
}

func TestUpdateService_DiffAddsNewFile(t *testing.T) {
	// First check: only game.gb is available. Second check: game-v2.gb is added.
	filenames := []string{"game.gb"}
	srv := freeGameServerDynamic(t, &filenames)
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add(srv.URL+"/game",
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)

	// First check: establishes baseline (game.gb only, IsNew=false for all).
	done1 := make(chan struct{})
	svc1 := inventory.NewUpdateService(inv, invPath, client, nil)
	svc1.Start(func() { close(done1) })
	<-done1
	svc1.Stop()

	if inv.HasPendingUpdates(srv.URL + "/game") {
		t.Error("HasPendingUpdates: want false after first check (no genuinely new files yet)")
	}

	// Developer publishes game-v2.gb.
	filenames = append(filenames, "game-v2.gb")
	laterLaunch(inv, srv.URL+"/game")

	// Second check: game-v2.gb appears and is flagged as genuinely new.
	done2 := make(chan struct{})
	svc2 := inventory.NewUpdateService(inv, invPath, client, nil)
	svc2.Start(func() { close(done2) })
	<-done2
	svc2.Stop()

	if !inv.HasPendingUpdates(srv.URL + "/game") {
		t.Error("HasPendingUpdates: want true after new file detected upstream on second check")
	}
}

func TestUpdateService_DiffPrunesVanishedFile(t *testing.T) {
	// Game page now only has game.gb — game-v2.gb has vanished.
	srv := freeGameServer(t, http.StatusOK, []string{"game.gb"})
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add(srv.URL+"/game",
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	// Seed a previously-known file that has now vanished.
	inv.SetUpstreamFiles(srv.URL+"/game", []inventory.UpstreamFile{
		{Filename: "game.gb", UploadID: "100", SeenAt: time.Now().Add(-time.Hour)},
		{Filename: "game-v2.gb", UploadID: "101", SeenAt: time.Now().Add(-time.Hour)},
	})
	laterLaunch(inv, srv.URL+"/game")
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)
	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	e, _ := inv.Lookup(srv.URL + "/game")
	for _, u := range e.KnownUpstreamFiles {
		if u.Filename == "game-v2.gb" {
			t.Error("vanished file game-v2.gb should have been pruned from KnownUpstreamFiles")
		}
	}
}

// runFreeGameCheck seeds one downloaded free game and runs a launch check
// against srv. prior, when set, seeds an earlier check's upload list.
func runFreeGameCheck(t *testing.T, srv *httptest.Server, prior []string, removed bool) (*inventory.Inventory, string) {
	t.Helper()
	return runFreeGameCheckFor(t, srv, inventory.DownloadedFile{Filename: "game.gb"}, prior, removed)
}

// runFreeGameCheckFor is runFreeGameCheck for a game whose one downloaded
// file is installed (Filename and SourceArchive set by the caller). The
// seeded check is aged, so the launch check covers the entry again.
func runFreeGameCheckFor(t *testing.T, srv *httptest.Server, installed inventory.DownloadedFile, prior []string, removed bool) (*inventory.Inventory, string) {
	t.Helper()
	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := srv.URL + "/game"
	installed.DestPath, installed.DownloadedAt = romPath, time.Now()
	inv.Add(gameURL, inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"}, installed)
	if prior != nil {
		files := make([]inventory.UpstreamFile, 0, len(prior))
		for index, name := range prior {
			files = append(files, inventory.UpstreamFile{Filename: name, UploadID: fmt.Sprint(100 + index), SeenAt: time.Now().Add(-time.Hour)})
		}
		inv.SetUpstreamFiles(gameURL, files)
		laterLaunch(inv, gameURL)
	}
	if removed {
		inv.MarkRemoved(gameURL)
	}
	inv.Save(invPath)

	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, itchio.NewClientWithBase(srv.URL), nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()
	return inv, gameURL
}

// runSignedInCheckFor installs one file of game 42 and runs two checks
// signed in, with a download key for the game, so each upload list is
// complete (decision D8). first is the upload list JSON of the first check,
// which sets the baseline; then is the list the second check sees.
func runSignedInCheckFor(t *testing.T, installed inventory.DownloadedFile, first, then string) (*inventory.Inventory, string) {
	t.Helper()
	var mu sync.Mutex
	uploads := first
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile/owned-keys":
			fmt.Fprint(w, `{"owned_keys":[{"id":123,"game_id":42,"purchase_id":456}],"per_page":100}`)
		case "/games/42/uploads":
			mu.Lock()
			defer mu.Unlock()
			fmt.Fprint(w, uploads)
		default:
			t.Errorf("signed-in check made an unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := srv.URL + "/game"
	installed.DestPath, installed.DownloadedAt = romPath, time.Now()
	inv.Add(gameURL, inventory.Entry{GameID: "42", Title: "G"}, installed)
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	done := make(chan struct{}, 2)
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	svc.Start(func() { done <- struct{}{} })
	<-done
	if inv.IsRemoved(gameURL) || inv.HasPendingUpdates(gameURL) {
		t.Fatalf("the first check changed the game's state: removed=%v pending=%+v", inv.IsRemoved(gameURL), inv.PendingUpdateFiles(gameURL))
	}
	mu.Lock()
	uploads = then
	mu.Unlock()
	svc.CheckAllNow()
	<-done
	svc.Stop()
	return inv, gameURL
}

// A new version replacing the downloaded upload is an update, not a removal
// (upstream 79539ff).
func TestUpdateService_SupersededUploadIsAnUpdateNotARemoval(t *testing.T) {
	srv := freeGameServer(t, http.StatusOK, []string{"game-v2.gb"})
	defer srv.Close()

	inv, gameURL := runFreeGameCheck(t, srv, []string{"game.gb"}, false)
	if inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: a superseded upload marked the game removed")
	}
	if !inv.HasPendingUpdates(gameURL) {
		t.Error("HasPendingUpdates: the replacement upload was not offered as an update")
	}
}

func TestUpdateService_SupersededUploadClearsAStaleRemoval(t *testing.T) {
	srv := freeGameServer(t, http.StatusOK, []string{"game-v2.gb"})
	defer srv.Close()

	inv, gameURL := runFreeGameCheck(t, srv, []string{"game.gb"}, true)
	if inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: a reachable game offering downloads kept a stale removal")
	}
}

// A public page that loads but lists no downloads does not mark the game
// removed: it can hide paid uploads, and a signed-out page check never marks
// it (decisions D5 and D8). A missing page or a complete API listing does.
func TestUpdateService_ReachablePageWithoutDownloadsIsNotARemoval(t *testing.T) {
	srv := freeGameServer(t, http.StatusOK, nil)
	defer srv.Close()

	inv, gameURL := runFreeGameCheck(t, srv, []string{"game.gb"}, false)
	if inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: a public page without downloads marked the game removed")
	}
}

// A deleted upload is superseded only by a new upload of the same kind, so
// other platforms' builds still listed are no update. Signed in, a complete
// listing that no longer has the installed upload and offers nothing of its
// kind proves the removal (review finding R18-3, decision D8).
func TestUpdateService_DeletedUploadWithoutAReplacementIsARemoval(t *testing.T) {
	installed := inventory.DownloadedFile{Filename: "game.gb", OriginalUpload: "game.gb", UploadID: "1"}
	inv, gameURL := runSignedInCheckFor(t, installed,
		`{"uploads":[{"id":1,"filename":"game.gb"},{"id":2,"filename":"game_win.zip","traits":["p_windows"]},
			{"id":3,"filename":"game_mac.zip","traits":["p_osx"]}]}`,
		`{"uploads":[{"id":2,"filename":"game_win.zip","traits":["p_windows"]},{"id":3,"filename":"game_mac.zip","traits":["p_osx"]}]}`)
	if !inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: the downloaded upload is gone and nothing replaces it")
	}
	if inv.HasPendingUpdates(gameURL) {
		t.Error("HasPendingUpdates: other platforms' builds were offered as an update")
	}
}

// The signed-out counterpart: the public page cannot prove the deletion, as it
// can hide paid uploads, so the game is not marked removed (decision D8), and
// the other platforms' builds are still no update (review finding R18-3).
func TestUpdateService_PublicPageCannotProveADeletedUpload(t *testing.T) {
	srv := freeGameServer(t, http.StatusOK, []string{"game_win.zip", "game_mac.zip"})
	defer srv.Close()

	inv, gameURL := runFreeGameCheck(t, srv, []string{"game.gb", "game_win.zip", "game_mac.zip"}, false)
	if inv.HasPendingUpdates(gameURL) {
		t.Error("HasPendingUpdates: other platforms' builds were offered as an update")
	}
	if inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: a public page that omits the upload marked the game removed")
	}
}

// The replacement is flagged even when the first check after the download
// already sees only the new upload: the install seeds the listing it was
// chosen from, so that check compares instead of starting a baseline
// (review finding R25-3).
func TestUpdateService_SupersededUploadIsAnUpdateOnTheFirstCheck(t *testing.T) {
	srv := freeGameServer(t, http.StatusOK, []string{"game-v2.gb"})
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := srv.URL + "/game"
	inv.Add(gameURL, inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", OriginalUpload: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	inv.CommitUploadInstall(gameURL, inventory.UploadInstall{Filename: "game.gb", Written: []string{romPath},
		Listing: []inventory.UpstreamFile{{Filename: "game.gb"}}, ListingSource: inventory.SourcePage})
	laterLaunch(inv, gameURL)
	inv.Save(invPath)

	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, itchio.NewClientWithBase(srv.URL), nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()
	if inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: a superseded upload marked the game removed")
	}
	if !inv.HasPendingUpdates(gameURL) {
		t.Error("HasPendingUpdates: the replacement upload was not offered as an update")
	}
}

// After you download the replacement, the old file stays on the card. It is
// still superseded, not removed, and nothing is new.
func TestUpdateService_DownloadedReplacementKeepsTheGameReachable(t *testing.T) {
	srv := freeGameServer(t, http.StatusOK, []string{"game-v2.gb"})
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := srv.URL + "/game"
	for _, name := range []string{"game.gb", "game-v2.gb"} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte("ROM"), 0644)
		art := inventory.CoverArtPath(srv.URL+"/cover.png", path)
		os.MkdirAll(filepath.Dir(art), 0755)
		os.WriteFile(art, minimalPNG(), 0644)
		inv.Add(gameURL, inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
			inventory.DownloadedFile{Filename: name, DestPath: path, DownloadedAt: time.Now()})
	}
	inv.SetUpstreamFiles(gameURL, []inventory.UpstreamFile{{Filename: "game-v2.gb", UploadID: "101", SeenAt: time.Now().Add(-time.Hour)}})
	laterLaunch(inv, gameURL)
	inv.Save(invPath)

	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, itchio.NewClientWithBase(srv.URL), nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()
	if inv.IsRemoved(gameURL) || inv.HasPendingUpdates(gameURL) {
		t.Errorf("removed=%v update=%v, want neither", inv.IsRemoved(gameURL), inv.HasPendingUpdates(gameURL))
	}
}

// An archive is replaced by an archive; a ROM of another kind does not
// replace it. When only the ROM is left, a complete listing signed in proves
// the archive removed; a signed-out page check never marks it (decision D8).
func TestUpdateService_ArchiveIsSupersededOnlyByAnArchive(t *testing.T) {
	t.Run("signed out", func(t *testing.T) {
		installed := inventory.DownloadedFile{Filename: "Game.gb", SourceArchive: "game-1.0.zip"}
		srv := freeGameServer(t, http.StatusOK, []string{"game-1.1.zip"})
		inv, gameURL := runFreeGameCheckFor(t, srv, installed, []string{"game-1.0.zip"}, false)
		srv.Close()
		if inv.IsRemoved(gameURL) || !inv.HasPendingUpdates(gameURL) {
			t.Errorf("new archive: removed=%v update=%v, want an update", inv.IsRemoved(gameURL), inv.HasPendingUpdates(gameURL))
		}

		// The page now lists only a ROM upload. It does not replace the
		// archive, so it is no update, and the page cannot prove a removal.
		srv = freeGameServer(t, http.StatusOK, []string{"game.nes"})
		inv, gameURL = runFreeGameCheckFor(t, srv, installed, []string{"game-1.0.zip", "game.nes"}, false)
		srv.Close()
		if inv.HasPendingUpdates(gameURL) {
			t.Error("HasPendingUpdates: an archive was treated as replaced by a ROM upload")
		}
		if inv.IsRemoved(gameURL) {
			t.Error("IsRemoved: a public page that omits the archive marked the game removed")
		}
	})
	t.Run("signed in", func(t *testing.T) {
		installed := inventory.DownloadedFile{Filename: "Game.gb", OriginalUpload: "game-1.0.zip",
			SourceArchive: "game-1.0.zip", UploadID: "1"}
		inv, gameURL := runSignedInCheckFor(t, installed,
			`{"uploads":[{"id":1,"filename":"game-1.0.zip"}]}`,
			`{"uploads":[{"id":4,"filename":"game-1.1.zip"}]}`)
		if inv.IsRemoved(gameURL) || !inv.HasPendingUpdates(gameURL) {
			t.Errorf("new archive: removed=%v update=%v, want an update", inv.IsRemoved(gameURL), inv.HasPendingUpdates(gameURL))
		}

		// The complete listing now has only a ROM upload. It does not
		// replace the archive, so it is no update, and the archive is gone.
		inv, gameURL = runSignedInCheckFor(t, installed,
			`{"uploads":[{"id":1,"filename":"game-1.0.zip"},{"id":2,"filename":"game.nes"}]}`,
			`{"uploads":[{"id":2,"filename":"game.nes"}]}`)
		if inv.HasPendingUpdates(gameURL) {
			t.Errorf("HasPendingUpdates: an archive was treated as replaced by a ROM upload: %+v", inv.PendingUpdateFiles(gameURL))
		}
		if !inv.IsRemoved(gameURL) {
			t.Error("IsRemoved: a complete listing without the archive or another archive kept the game")
		}
	})
}

// A server error or a blocked page leaves the removal state as it was.
func TestUpdateService_TransientFailuresPreserveRemovalState(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden} {
		for _, removed := range []bool{false, true} {
			srv := freeGameServer(t, status, nil)
			inv, gameURL := runFreeGameCheck(t, srv, []string{"game.gb"}, removed)
			srv.Close()
			if inv.IsRemoved(gameURL) != removed {
				t.Errorf("HTTP %d: IsRemoved = %v, want the prior %v", status, !removed, removed)
			}
		}
	}
}

// The downloaded upload replaced by a new one is an update, also when the
// game was marked removed before.
func TestUpdateService_KeepsReachableWhenDownloadedFileIsSuperseded(t *testing.T) {
	// Upstream now only has game-v2.gb; the originally downloaded game.gb is gone.
	srv := freeGameServer(t, http.StatusOK, []string{"game-v2.gb"})
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add(srv.URL+"/game",
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	inv.SetUpstreamFiles(srv.URL+"/game", []inventory.UpstreamFile{{Filename: "game.gb"}})
	laterLaunch(inv, srv.URL+"/game")
	inv.MarkRemoved(srv.URL + "/game")
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)
	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	if inv.IsRemoved(srv.URL + "/game") {
		t.Error("IsRemoved: want false when another upload supersedes the downloaded file")
	}
	if !inv.HasPendingUpdates(srv.URL + "/game") {
		t.Error("HasPendingUpdates: the replacement upload should remain an update")
	}
}

func TestUpdateService_ClearsRemovedWhenDownloadedFileReappearsInStore(t *testing.T) {
	// Upstream has game.gb again after it was previously removed.
	srv := freeGameServer(t, http.StatusOK, []string{"game.gb"})
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := srv.URL + "/game"
	inv.Add(gameURL,
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	// Seed a prior removal state.
	inv.MarkRemoved(gameURL)
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)
	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	if inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: want false when downloaded file has reappeared upstream")
	}
}

func TestUpdateService_DismissedUpdateDoesNotReappearOnRestart(t *testing.T) {
	// Start with only game.gb; game-v2.gb appears after first check.
	filenames := []string{"game.gb"}
	srv := freeGameServerDynamic(t, &filenames)
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "game.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := srv.URL + "/game"
	inv.Add(gameURL,
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "game.gb", DestPath: romPath, DownloadedAt: time.Now()})
	inv.Save(invPath)

	client := itchio.NewClientWithBase(srv.URL)

	// First check: baseline (game.gb only), no pending updates yet.
	done1 := make(chan struct{})
	svc1 := inventory.NewUpdateService(inv, invPath, client, nil)
	svc1.Start(func() { close(done1) })
	<-done1
	svc1.Stop()
	if inv.HasPendingUpdates(gameURL) {
		t.Fatal("HasPendingUpdates: want false after first check (no new files yet)")
	}

	// Developer publishes game-v2.gb.
	filenames = append(filenames, "game-v2.gb")
	laterLaunch(inv, gameURL)

	// Second check: game-v2.gb detected as genuinely new.
	done2 := make(chan struct{})
	svc2 := inventory.NewUpdateService(inv, invPath, client, nil)
	svc2.Start(func() { close(done2) })
	<-done2
	svc2.Stop()
	if !inv.HasPendingUpdates(gameURL) {
		t.Fatal("HasPendingUpdates: want true after new file detected on second check")
	}

	// User dismisses the update and we save to disk.
	inv.DismissUpdate(gameURL)
	if err := inv.Save(invPath); err != nil {
		t.Fatal(err)
	}
	if inv.HasPendingUpdates(gameURL) {
		t.Fatal("HasPendingUpdates: want false immediately after dismiss")
	}

	// Simulate app restart: reload inventory from disk, run third check.
	inv3, _ := inventory.Load(invPath)
	laterLaunch(inv3, gameURL)
	done3 := make(chan struct{})
	svc3 := inventory.NewUpdateService(inv3, invPath, client, nil)
	svc3.Start(func() { close(done3) })
	<-done3
	svc3.Stop()

	if inv3.HasPendingUpdates(gameURL) {
		t.Error("HasPendingUpdates: want false — dismissed update must not reappear after restart + re-check")
	}
}

func TestIsGameRemoved_SentinelUnwraps(t *testing.T) {
	wrapped := fmt.Errorf("outer: %w", itchio.ErrGameRemoved)
	if !errors.Is(wrapped, itchio.ErrGameRemoved) {
		t.Error("errors.Is should unwrap ErrGameRemoved through wrapping")
	}
	if errors.Is(fmt.Errorf("HTTP 404 plain text"), itchio.ErrGameRemoved) {
		t.Error("plain string error should NOT match ErrGameRemoved")
	}
}

// R20-2: a game downloaded through the API records the API filename, which
// may differ from the name on the web download page. The upload ID is the
// same, so the file is still offered upstream and the game is not removed.
func TestUpdateService_MatchesInstalledFileByUploadID(t *testing.T) {
	srv := freeGameServer(t, http.StatusOK, []string{"Web Name.gb"}) // upload ID 100
	defer srv.Close()

	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	romPath := filepath.Join(dir, "api-name.gb")
	os.WriteFile(romPath, []byte("ROM"), 0644)
	artPath := inventory.CoverArtPath(srv.URL+"/cover.png", romPath)
	os.MkdirAll(filepath.Dir(artPath), 0755)
	os.WriteFile(artPath, minimalPNG(), 0644)

	invPath := filepath.Join(dir, "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := srv.URL + "/game"
	inv.Add(gameURL,
		inventory.Entry{Title: "G", IsFree: true, CoverURL: srv.URL + "/cover.png"},
		inventory.DownloadedFile{Filename: "api-name.gb", UploadID: "100", DestPath: romPath, DownloadedAt: time.Now()})
	inv.Save(invPath)

	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, itchio.NewClientWithBase(srv.URL), nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()

	if inv.IsRemoved(gameURL) {
		t.Error("IsRemoved: the installed upload is still offered under another name")
	}
}

func TestUpdateServiceRepairsArtworkAfterMetadataRateLimit(t *testing.T) {
	pngData := minimalPNG()
	var mu sync.Mutex
	metadataRequests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/cover.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngData)
		case strings.HasSuffix(r.URL.Path, "/data.json"):
			mu.Lock()
			metadataRequests++
			mu.Unlock()
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	configureUpdaterPaths(t, dir)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	var roms []string
	for _, name := range []string{"one", "two", "three"} {
		rom := filepath.Join(dir, name+".gb")
		if err := os.WriteFile(rom, []byte("ROM"), 0o644); err != nil {
			t.Fatal(err)
		}
		roms = append(roms, rom)
		inv.Add(srv.URL+"/"+name, inventory.Entry{Title: name, CoverURL: srv.URL + "/cover.png"},
			inventory.DownloadedFile{Filename: name + ".gb", DestPath: rom})
	}
	invPath := filepath.Join(dir, "inventory.json")
	client := itchio.NewClientWithBase(srv.URL)
	client.HTTPClient().Transport = http.DefaultTransport // deterministic 429; transport retries have their own tests
	client.SetAuthToken("A")
	done := make(chan struct{})
	svc := inventory.NewUpdateService(inv, invPath, client, nil)
	svc.Start(func() { close(done) })
	<-done
	svc.Stop()
	for _, rom := range roms {
		if art := inventory.CanonicalArtworkPath(rom); art == "" {
			t.Fatalf("no artwork path for %s", rom)
		} else if _, err := os.Stat(art); err != nil {
			t.Errorf("artwork for %s was not repaired after a metadata rate limit: %v", filepath.Base(rom), err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if metadataRequests != 1 {
		t.Fatalf("made %d metadata requests, want the rest deferred after the first 429", metadataRequests)
	}
}
