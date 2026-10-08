//go:build !headless

package ui

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

// F7: a reinstall that finds artwork in place says whose it is, quietly when
// it is the app's own.
func TestEnsureROMArtworkSaysWhoseArtworkItKeeps(t *testing.T) {
	sources, catalog, _ := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	romPath := filepath.Join(sources[0].RomsPath, "GBC", "Game.gbc")
	artPath := inventory.CanonicalArtworkPath(romPath)
	art := []byte("art")
	for path, data := range map[string][]byte{romPath: []byte("rom"), artPath: art} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	game := itchio.Game{Title: "Game", URL: "https://example.invalid/game", CoverURL: "https://example.invalid/cover.png"}
	client := itchio.NewClientWithBase("http://127.0.0.1:1")
	hash := fmt.Sprintf("%x", sha256.Sum256(art))

	for _, tc := range []struct {
		name    string
		file    inventory.DownloadedFile
		created bool
		line    string
	}{
		{"app", inventory.DownloadedFile{ArtworkPath: artPath, ArtworkHash: hash, ArtworkCreated: true},
			true, "[DEBUG] cover-art: keeping app artwork "},
		{"user", inventory.DownloadedFile{}, false, "[INFO]  cover-art: keeping user artwork "},
		{"replaced", inventory.DownloadedFile{ArtworkPath: artPath, ArtworkHash: "older", ArtworkCreated: true},
			false, "[INFO]  cover-art: keeping user artwork "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t)
			inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
			tc.file.Filename, tc.file.DestPath = "Game.gbc", romPath
			inv.Add(game.URL, inventory.Entry{Title: game.Title, CoverURL: game.CoverURL}, tc.file)

			result := ensureROMArtwork(client, inv, game, romPath)

			if result.Path != artPath || result.SHA256 != hash || result.Created != tc.created {
				t.Fatalf("artwork = %+v, want created %v", result, tc.created)
			}
			var lines []string
			for _, line := range strings.Split(logs.String(), "\n") {
				if strings.Contains(line, "cover-art:") {
					lines = append(lines, line)
				}
			}
			if len(lines) != 1 || !strings.Contains(lines[0], tc.line) {
				t.Fatalf("cover-art log:\n%s\nwant one line with %q", strings.Join(lines, "\n"), tc.line)
			}
			if got, err := os.ReadFile(artPath); err != nil || string(got) != string(art) {
				t.Fatalf("artwork changed: %q, %v", got, err)
			}
		})
	}
}
