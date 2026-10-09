package itchio_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func coverServer(t *testing.T, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if status != http.StatusOK {
			http.Error(w, "no cover", status)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(minimalPNG())
	}))
	t.Cleanup(srv.Close)
	return srv, requests
}

// F26: one fetch saves a game's cover under the name of each ROM, downloading
// it once.
func TestCoverFetchDownloadsOnceForSeveralImages(t *testing.T) {
	srv, requests := coverServer(t, http.StatusOK)
	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	imageDir := configureArtworkRoot(t, dir)
	fetch := c.NewCoverFetch(srv.URL + "/cover.png")

	var results []itchio.ArtworkResult
	for _, rom := range []string{"one.gb", "two.gb", "three.gb"} {
		result, err := fetch.EnsureCoverArt(filepath.Join(dir, rom))
		if err != nil {
			t.Fatalf("%s: %v", rom, err)
		}
		results = append(results, result)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("cover requests = %d, want 1", got)
	}
	for index, stem := range []string{"one", "two", "three"} {
		want := filepath.Join(imageDir, stem+".png")
		if results[index].Path != want || !results[index].Created || results[index].SHA256 != results[0].SHA256 {
			t.Fatalf("result %d = %+v, want a new %s with the first one's content", index, results[index], want)
		}
		if _, err := os.Stat(want); err != nil {
			t.Fatal(err)
		}
	}
}

// F26: a fetch downloads nothing while every image it is asked for exists, and
// never replaces one.
func TestCoverFetchLeavesExistingImagesAlone(t *testing.T) {
	srv, requests := coverServer(t, http.StatusOK)
	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	imageDir := configureArtworkRoot(t, dir)
	mine := filepath.Join(imageDir, "one.png")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	fetch := c.NewCoverFetch(srv.URL + "/cover.png")

	result, err := fetch.EnsureCoverArt(filepath.Join(dir, "one.gb"))
	if err != nil || result.Created || result.Path != mine {
		t.Fatalf("existing image: %+v, %v", result, err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("cover requests = %d while the only image asked for existed", got)
	}
	if _, err := fetch.EnsureCoverArt(filepath.Join(dir, "two.gb")); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(mine); string(data) != "mine" {
		t.Fatalf("existing image = %q", data)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("cover requests = %d, want 1", got)
	}
}

// F26: a cover that cannot be fetched is asked for once, and every image that
// needed it reports the failure.
func TestCoverFetchAsksForAFailingCoverOnce(t *testing.T) {
	srv, requests := coverServer(t, http.StatusNotFound)
	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	configureArtworkRoot(t, dir)
	fetch := c.NewCoverFetch(srv.URL + "/cover.png")

	for _, rom := range []string{"one.gb", "two.gb"} {
		if _, err := fetch.EnsureCoverArt(filepath.Join(dir, rom)); err == nil {
			t.Fatalf("%s: no error for a missing cover", rom)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("cover requests = %d, want 1", got)
	}
}
