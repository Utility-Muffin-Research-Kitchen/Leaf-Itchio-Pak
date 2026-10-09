package inventory_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// pico8PassFixture is an installed Pico-8 set whose files carry no artwork
// yet, as a set from a 7z archive did before the install saved art, and a
// server that counts cover downloads.
type pico8PassFixture struct {
	dir, invPath, gameURL string
	covers                *atomic.Int32
	client                *itchio.Client
	inv                   *inventory.Inventory
}

func newPico8PassFixture(t *testing.T, files ...string) pico8PassFixture {
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
	// Every system gets its own Roms and Images folder, so no ROM path can
	// belong to a system whose folder is a parent of the others.
	systems, images := map[string]string{}, map[string]string{}
	for _, id := range []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"} {
		systems[id] = filepath.Join(dir, "Roms", id)
		images[id] = filepath.Join(dir, "Images", id)
	}
	configureTestPaths(t, roms.PathConfig{
		SystemDirs: systems, ImageDirs: images, SourceID: "primary", PrimaryRoot: dir,
		MusicRoot: filepath.Join(dir, "Music"), StatesRoot: filepath.Join(dir, "States"),
	})
	f := pico8PassFixture{
		dir: dir, invPath: filepath.Join(dir, "inventory.json"), gameURL: srv.URL + "/game",
		covers: covers, client: itchio.NewClientWithBase(srv.URL),
		inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)},
	}
	for _, rel := range files {
		path := filepath.Join(dir, "Roms", "PICO8", "Moss Garden", filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		f.inv.Add(f.gameURL, inventory.Entry{Title: "Moss Garden", CoverURL: srv.URL + "/cover.png"}, p8(path, "moss.zip"))
	}
	return f
}

func (f pico8PassFixture) runLaunchCheck(t *testing.T) {
	t.Helper()
	artworkPassFixture{invPath: f.invPath, client: f.client}.runLaunchCheck(t, f.inv)
}

func (f pico8PassFixture) images(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.dir, "Images", "PICO8"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// F26: the cover-art pass fetches a game's cover once for the several images
// it saves, as the install does.
func TestArtworkPassFetchesTheCoverOnceForSeveralImages(t *testing.T) {
	f := newPico8PassFixture(t, "main.p8", "level2.p8", "boss.p8")
	captureArtworkLogs(t)

	f.runLaunchCheck(t)

	if got := f.images(t); len(got) != 3 {
		t.Fatalf("images = %v, want main, level2 and boss", got)
	}
	if n := f.covers.Load(); n != 1 {
		t.Fatalf("cover downloads = %d, want 1", n)
	}
	entry, _ := f.inv.Lookup(f.gameURL)
	for _, file := range entry.Files {
		if want := inventory.CanonicalArtworkPath(file.DestPath); file.ArtworkPath != want || !file.ArtworkCreated || file.ArtworkHash == "" {
			t.Fatalf("%s records art %q created %v, want %s as the app's", filepath.Base(file.DestPath),
				file.ArtworkPath, file.ArtworkCreated, want)
		}
	}
}

// F26: the launcher lists no game for a set's Lua files or its playlist, so
// the pass saves no image for them. Their names would only add files that no
// cart looks for.
func TestArtworkPassSavesNoImageForASetsLuaFilesOrPlaylist(t *testing.T) {
	f := newPico8PassFixture(t, "main.p8", "world2/main.p8", "lib.lua", "Moss Garden.m3u")
	captureArtworkLogs(t)

	f.runLaunchCheck(t)

	if got := f.images(t); len(got) != 1 || got[0] != "main.png" {
		t.Fatalf("images = %v, want only main.png", got)
	}
	entry, _ := f.inv.Lookup(f.gameURL)
	for _, file := range entry.Files {
		name := filepath.Base(file.DestPath)
		if (name == "lib.lua" || name == "Moss Garden.m3u") && file.ArtworkPath != "" {
			t.Fatalf("%s records art %q", name, file.ArtworkPath)
		}
	}
}
