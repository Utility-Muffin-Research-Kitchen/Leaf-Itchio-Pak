package itchio_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

// minimalJPEG encodes a 1×1 white pixel as a JPEG.
func minimalJPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}

// minimalPNG encodes a 1×1 white pixel as a PNG.
func minimalPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// minimalGIF encodes a 1×1 white pixel as a GIF.
func minimalGIF() []byte {
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), []color.Color{color.White})
	var buf bytes.Buffer
	_ = gif.Encode(&buf, img, nil)
	return buf.Bytes()
}

// TestDownloadCoverArtEmptyURL verifies that an empty coverURL is a no-op
// and does not create the .media directory.
func TestDownloadCoverArtEmptyURL(t *testing.T) {
	c := itchio.NewClientWithBase("http://localhost")
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.gbc")

	if err := c.DownloadCoverArt("", romPath); err != nil {
		t.Fatalf("expected nil error for empty URL, got: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".media")); !os.IsNotExist(statErr) {
		t.Error(".media dir must not be created when coverURL is empty")
	}
}

// TestDownloadCoverArtHTTP404 verifies that a non-200 response returns an error.
func TestDownloadCoverArtHTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.gbc")

	if err := c.DownloadCoverArt(srv.URL+"/cover.png", romPath); err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention 404, got: %v", err)
	}
}

// TestDownloadCoverArtSuccess verifies a JPEG source is saved as .png with correct name.
func TestDownloadCoverArtSuccess(t *testing.T) {
	imgBytes := minimalJPEG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(imgBytes)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	romPath := filepath.Join(dir, "Wario Land II.gbc")

	result, err := c.EnsureCoverArt(srv.URL+"/cover.png", romPath)
	if err != nil {
		t.Fatalf("DownloadCoverArt: %v", err)
	}

	artPath := filepath.Join(dir, ".media", "Wario Land II.png")
	fi, err := os.Stat(artPath)
	if os.IsNotExist(err) {
		t.Fatalf("expected art file at %s, not found", artPath)
	}
	if fi.Size() == 0 {
		t.Errorf("art file at %s is empty", artPath)
	}
	if !result.Created || result.Path != artPath || len(result.SHA256) != 64 {
		t.Fatalf("created artwork metadata = %#v", result)
	}
}

func TestEnsureCoverArtPreservesExistingUserArtwork(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write(minimalPNG())
	}))
	defer srv.Close()
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.gbc")
	artPath := filepath.Join(dir, ".media", "game.png")
	if err := os.MkdirAll(filepath.Dir(artPath), 0o755); err != nil {
		t.Fatal(err)
	}
	userArt := []byte("user-owned-art")
	if err := os.WriteFile(artPath, userArt, 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := itchio.NewClientWithBase(srv.URL).EnsureCoverArt(srv.URL+"/cover.png", romPath)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("existing user artwork triggered %d network request(s)", requests)
	}
	if result.Created || result.Path != artPath {
		t.Fatalf("existing artwork metadata = %#v", result)
	}
	wantHash := fmt.Sprintf("%x", sha256.Sum256(userArt))
	if result.SHA256 != wantHash {
		t.Fatalf("existing artwork hash = %q, want %q", result.SHA256, wantHash)
	}
	if got, err := os.ReadFile(artPath); err != nil || !bytes.Equal(got, userArt) {
		t.Fatalf("user artwork changed: %q, %v", got, err)
	}
}

// TestDownloadCoverArtGIFConvertedToPNG verifies that a GIF source is decoded
// and re-encoded as PNG (the only format NextUI displays for cover art).
func TestDownloadCoverArtGIFConvertedToPNG(t *testing.T) {
	gifBytes := minimalGIF()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		w.Write(gifBytes)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	romPath := filepath.Join(dir, "Opossum Country.gbc")

	if err := c.DownloadCoverArt(srv.URL+"/cover.gif", romPath); err != nil {
		t.Fatalf("DownloadCoverArt (gif): %v", err)
	}

	// Must be saved as .png, not .gif
	artPath := filepath.Join(dir, ".media", "Opossum Country.png")
	fi, err := os.Stat(artPath)
	if os.IsNotExist(err) {
		t.Fatalf("expected .png at %s (gif should be converted), not found", artPath)
	}
	if fi.Size() == 0 {
		t.Errorf("art file at %s is empty", artPath)
	}
	if _, gifErr := os.Stat(filepath.Join(dir, ".media", "Opossum Country.gif")); !os.IsNotExist(gifErr) {
		t.Errorf(".gif file must not be saved; only .png should exist")
	}
}

// TestDownloadCoverArtPNGRoundTrip verifies PNG source is saved as PNG.
func TestDownloadCoverArtPNGRoundTrip(t *testing.T) {
	pngBytes := minimalPNG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngBytes)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.gbc")

	if err := c.DownloadCoverArt(srv.URL+"/cover.png", romPath); err != nil {
		t.Fatalf("DownloadCoverArt (png): %v", err)
	}

	artPath := filepath.Join(dir, ".media", "game.png")
	if _, err := os.Stat(artPath); os.IsNotExist(err) {
		t.Fatalf("expected .png at %s (png should be converted), not found", artPath)
	}
}

// TestDownloadCoverArtFullStemPreserved verifies that bracket/paren tags in the
// ROM filename are kept in the cover art name. NextUI looks up art by the full
// ROM stem (including [v1.2] etc.), even though it strips them for display.
func TestDownloadCoverArtFullStemPreserved(t *testing.T) {
	imgBytes := minimalJPEG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(imgBytes)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	romPath := filepath.Join(dir, "Kero Kero Cowboy [v1.2].gbc")

	if err := c.DownloadCoverArt(srv.URL+"/cover.png", romPath); err != nil {
		t.Fatalf("DownloadCoverArt: %v", err)
	}

	// Full stem must be preserved: "Kero Kero Cowboy [v1.2].png"
	artPath := filepath.Join(dir, ".media", "Kero Kero Cowboy [v1.2].png")
	if _, err := os.Stat(artPath); os.IsNotExist(err) {
		t.Fatalf("expected art at %q (full stem), not found", artPath)
	}
	// Stripped name must NOT exist
	if _, err := os.Stat(filepath.Join(dir, ".media", "Kero Kero Cowboy.png")); !os.IsNotExist(err) {
		t.Errorf("stripped filename should not exist; full stem must be preserved")
	}
}

// animatedGIFFirstFrameBlack returns a 2-frame animated GIF where frame 1 is
// entirely black and frame 2 composites a red pixel at (0,0). Calling
// image.Decode on this GIF yields only the first (black) frame; correct
// handling must composite all frames so pixel (0,0) is red in the output PNG.
func animatedGIFFirstFrameBlack() []byte {
	palette := color.Palette{color.Black, color.RGBA{R: 255, A: 255}}

	frame1 := image.NewPaletted(image.Rect(0, 0, 2, 1), palette)
	frame1.SetColorIndex(0, 0, 0) // black
	frame1.SetColorIndex(1, 0, 0) // black

	frame2 := image.NewPaletted(image.Rect(0, 0, 2, 1), palette)
	frame2.SetColorIndex(0, 0, 1) // red
	frame2.SetColorIndex(1, 0, 0) // black

	g := &gif.GIF{
		Image:    []*image.Paletted{frame1, frame2},
		Delay:    []int{0, 0},
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
	}
	var buf bytes.Buffer
	_ = gif.EncodeAll(&buf, g)
	return buf.Bytes()
}

// TestDownloadCoverArtAnimatedGIFFirstFrame verifies launcher artwork uses
// frame zero while the app's separate media cache retains GIF animation.
func TestDownloadCoverArtAnimatedGIFFirstFrame(t *testing.T) {
	gifBytes := animatedGIFFirstFrameBlack()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		w.Write(gifBytes)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	romPath := filepath.Join(dir, "Moon Escape.gb")

	if err := c.DownloadCoverArt(srv.URL+"/cover.gif", romPath); err != nil {
		t.Fatalf("DownloadCoverArt: %v", err)
	}

	artPath := filepath.Join(dir, ".media", "Moon Escape.png")
	f, err := os.Open(artPath)
	if err != nil {
		t.Fatalf("open saved PNG: %v", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode saved PNG: %v", err)
	}

	// Frame 2 places a red pixel at (0,0); launcher art deliberately keeps the
	// first black frame for deterministic frame-zero ownership semantics.
	red, _, _, _ := img.At(0, 0).RGBA()
	if red >= 0x8000 {
		t.Errorf("pixel (0,0) red channel = 0x%04x; launcher art did not keep frame zero", red)
	}
}

// TestCopyCoverArt verifies that CopyCoverArt copies the ROM file to the
// .media/ sibling directory with the correct art filename.
func TestCopyCoverArt(t *testing.T) {
	dir := t.TempDir()
	romPath := filepath.Join(dir, "game.p8.png")

	// Write a small PNG as the "ROM" (CopyCoverArt treats it as opaque bytes).
	if err := os.WriteFile(romPath, minimalPNG(), 0644); err != nil {
		t.Fatalf("write rom: %v", err)
	}

	result, err := itchio.EnsureCopiedCoverArt(romPath)
	if err != nil {
		t.Fatalf("CopyCoverArt: %v", err)
	}

	// coverArtBasename strips the last ext: "game.p8.png" → stem "game.p8"
	// → art path ".media/game.p8.png"
	artPath := filepath.Join(dir, ".media", "game.p8.png")
	if _, err := os.Stat(artPath); os.IsNotExist(err) {
		t.Fatalf("art file not created at %s", artPath)
	}

	got, err := os.ReadFile(artPath)
	if err != nil {
		t.Fatalf("read art: %v", err)
	}
	if !bytes.Equal(got, minimalPNG()) {
		t.Error("art file content does not match ROM content")
	}
	if !result.Created || result.Path != artPath || len(result.SHA256) != 64 {
		t.Fatalf("copied artwork metadata = %#v", result)
	}
}

// gifHighBrightnessLowVariance returns a 2-frame animated GIF where frame 1 is
// uniform gray (high brightness, zero colour variance) and frame 2 has one red
// and one blue pixel (lower summed brightness, high colour variance).
// DisposalNone means each frame is composited on top of the previous canvas;
// frame 2's canvas result is [red, blue] which has a much higher per-channel
// variance than frame 1's uniform gray.
func gifHighBrightnessLowVariance() []byte {
	palette := color.Palette{
		color.RGBA{R: 100, G: 100, B: 100, A: 255}, // 0 = gray
		color.RGBA{R: 255, G: 0, B: 0, A: 255},     // 1 = red
		color.RGBA{R: 0, G: 0, B: 255, A: 255},     // 2 = blue
	}

	frame1 := image.NewPaletted(image.Rect(0, 0, 2, 1), palette)
	frame1.SetColorIndex(0, 0, 0) // gray
	frame1.SetColorIndex(1, 0, 0) // gray

	frame2 := image.NewPaletted(image.Rect(0, 0, 2, 1), palette)
	frame2.SetColorIndex(0, 0, 1) // red
	frame2.SetColorIndex(1, 0, 2) // blue

	g := &gif.GIF{
		Image:    []*image.Paletted{frame1, frame2},
		Delay:    []int{10, 10},
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config:   image.Config{Width: 2, Height: 1},
	}
	var buf bytes.Buffer
	_ = gif.EncodeAll(&buf, g)
	return buf.Bytes()
}

// TestDownloadCoverArtDoesNotSelectLaterGIFFrame verifies a visually richer
// later frame does not replace frame zero in source-local launcher artwork.
func TestDownloadCoverArtDoesNotSelectLaterGIFFrame(t *testing.T) {
	gifBytes := gifHighBrightnessLowVariance()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/gif")
		w.Write(gifBytes)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	romPath := filepath.Join(dir, "Colour Test.gb")

	if err := c.DownloadCoverArt(srv.URL+"/cover.gif", romPath); err != nil {
		t.Fatalf("DownloadCoverArt: %v", err)
	}

	artPath := filepath.Join(dir, ".media", "Colour Test.png")
	f, err := os.Open(artPath)
	if err != nil {
		t.Fatalf("open saved PNG: %v", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode saved PNG: %v", err)
	}

	// Frame zero is gray (G≈25700); the later high-variance frame is red (G≈0).
	_, green, _, _ := img.At(0, 0).RGBA()
	if green <= 0x2000 {
		t.Errorf("pixel (0,0) green channel = 0x%04x; a later GIF frame replaced frame zero", green)
	}
}

// TestDownloadCoverArtOtherExtensionsPreserved verifies the app never deletes
// same-stem artwork it did not create.
func TestDownloadCoverArtOtherExtensionsPreserved(t *testing.T) {
	imgBytes := minimalJPEG()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(imgBytes)
	}))
	defer srv.Close()

	c := itchio.NewClientWithBase(srv.URL)
	dir := t.TempDir()
	mediaDir := filepath.Join(dir, ".media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Simulate a stale .gif left over from a previous download.
	staleGIF := filepath.Join(mediaDir, "Opossum Country.gif")
	if err := os.WriteFile(staleGIF, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	romPath := filepath.Join(dir, "Opossum Country.gbc")
	if err := c.DownloadCoverArt(srv.URL+"/cover.png", romPath); err != nil {
		t.Fatalf("DownloadCoverArt: %v", err)
	}

	// New .png must exist.
	if _, err := os.Stat(filepath.Join(mediaDir, "Opossum Country.png")); os.IsNotExist(err) {
		t.Fatalf("expected Opossum Country.png to exist after download")
	}
	// Existing alternate art is user-owned and must remain untouched.
	if got, err := os.ReadFile(staleGIF); err != nil || string(got) != "stale" {
		t.Errorf("existing Opossum Country.gif changed: %q, %v", got, err)
	}
}
