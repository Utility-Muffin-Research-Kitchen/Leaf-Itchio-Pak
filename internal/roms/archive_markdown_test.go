package roms_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// The 7z fixtures were written by libarchive from tar streams (tar --format=7zip
// -cf archive.7z @-), with the same entries as the ZIPs below. Tests need no 7z tool.
func TestInspectArchiveDistinguishesMarkdownFromMegaDrive(t *testing.T) {
	sega := make([]byte, 512)
	copy(sega[0x100:], "SEGA GENESIS    ")
	for _, test := range []struct {
		name  string
		files map[string]string
		roms  int
	}{
		{"mixed", map[string]string{"game/sonic.md": string(sega), "game/README.md": "# About\n", "README.MD": "# Controls\n", "game/game.gbc": "gbc"}, 2},
		{"readme", map[string]string{"README.md": "# About\n"}, 0},
		{"rom", map[string]string{"game/sonic.md": string(sega)}, 1},
	} {
		for _, format := range []string{"zip", "7z"} {
			t.Run(test.name+"/"+format, func(t *testing.T) {
				data := buildTestZIP(t, test.files)
				if format == "7z" {
					var err error
					data, err = os.ReadFile("../../testdata/archive-markdown-" + test.name + ".7z")
					if err != nil {
						t.Fatal(err)
					}
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.ServeContent(w, r, "game."+format, time.Time{}, bytes.NewReader(data))
				}))
				defer srv.Close()
				var manifest roms.ZIPManifest
				var err error
				if format == "7z" {
					manifest, err = roms.InspectRemote7z(srv.Client(), srv.URL)
				} else {
					manifest, err = roms.InspectRemoteZIP(srv.Client(), srv.URL, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				if manifest.ROMCount() != test.roms || len(manifest.Entries) != len(test.files) {
					t.Fatalf("manifest = %+v, want %d ROMs and %d total entries", manifest, test.roms, len(test.files))
				}
				for _, entry := range manifest.Entries {
					if entry.Name == "README.md" || entry.Name == "README.MD" {
						if entry.Kind != roms.KindOther {
							t.Errorf("%s classified as %v", entry.Name, entry.Kind)
						}
					}
				}
			})
		}
	}
}
