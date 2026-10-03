//go:build !headless

package ui

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func TestArchiveMarkdownPreflightAndExtraction(t *testing.T) {
	sega := make([]byte, 512)
	copy(sega[0x100:], "SEGA GENESIS    ")
	for _, test := range []struct {
		name  string
		files map[string][]byte
		roms  int
	}{
		{"mixed", map[string][]byte{"game/sonic.md": sega, "game/README.md": []byte("# About\n"), "README.MD": []byte("# Controls\n"), "game/game.gbc": []byte("gbc")}, 2},
		{"readme", map[string][]byte{"README.md": []byte("# About\n")}, 0},
		{"rom", map[string][]byte{"game/sonic.md": sega}, 1},
	} {
		for _, format := range []string{"zip", "7z"} {
			t.Run(test.name+"/"+format, func(t *testing.T) {
				data := zipOf(t, test.files)
				if format == "7z" {
					var err error
					data, err = os.ReadFile("../../testdata/archive-markdown-" + test.name + ".7z")
					if err != nil {
						t.Fatal(err)
					}
				}
				worker, gbDir := runArchive(t, "bundle."+format, data, &settings.Config{}, false)
				if test.roms == 0 {
					if worker.CatSnapshot().State != appui.DownloadProgressError {
						t.Fatal("README-only archive installed as a ROM")
					}
				} else if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
					t.Fatalf("archive = %+v", snapshot)
				}
				if len(worker.extracted) != test.roms {
					t.Fatalf("extracted = %v, want %d ROMs", worker.extracted, test.roms)
				}
				mdFiles := filesIn(t, filepath.Join(filepath.Dir(gbDir), "MD"))
				if test.roms == 0 {
					if len(mdFiles) != 0 {
						t.Fatalf("Markdown installed: %v", keys(mdFiles))
					}
				} else if len(mdFiles) != 1 || !bytes.Equal([]byte(mdFiles["sonic.md"]), sega) {
					t.Fatalf("MD files = %v, want only the intact sonic.md ROM", keys(mdFiles))
				}
			})
		}
	}
}
