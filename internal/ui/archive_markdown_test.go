//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
	"github.com/bodgit/sevenzip"
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

func TestArchiveUppercaseMDChoiceInstallsSelectedROM(t *testing.T) {
	sega := make([]byte, 512)
	copy(sega[0x100:], "SEGA GENESIS    ")
	for _, format := range []string{"zip", "7z"} {
		t.Run(format, func(t *testing.T) {
			data := zipOf(t, map[string][]byte{"SONIC.MD": sega, "ALT.MD": sega, "README.md": []byte("# About\n")})
			if format == "7z" {
				var err error
				data, err = os.ReadFile("../../testdata/archive-markdown-uppercase.7z")
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
			flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "auto", MusicDownload: "off"}, manifest)
			flow.prepareInitialAction()
			if action := flow.TakeAction(); action != CatArchiveChooseContents {
				t.Fatalf("action = %v", action)
			}
			model := appui.NewDownloadSelectModel("Leafbound")
			flow.PrepareChoices(model)
			flow.Choose(model)
			selected := flow.plan.SelectedROMs[".md"]
			worker, gbDir := runArchive(t, "bundle."+format, data, &settings.Config{}, false, func(plan *ZIPPlan, _ string) {
				plan.SelectedROMs = flow.plan.SelectedROMs
			})
			if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
				t.Fatalf("selected %q: %+v", selected, snapshot)
			}
			got := filesIn(t, filepath.Join(filepath.Dir(gbDir), "MD"))
			if len(got) != 1 {
				t.Fatalf("selected %q, installed %v", selected, keys(got))
			}
			for filename, content := range got {
				if !strings.EqualFold(filename, selected) {
					t.Fatalf("selected %q, installed %q", selected, filename)
				}
				if !bytes.Equal([]byte(content), sega) {
					t.Fatal("selected ROM content changed")
				}
			}
			if selected != "SONIC.MD" && selected != "ALT.MD" {
				t.Fatalf("picker changed archive entry identity: %q", selected)
			}
		})
	}
}

type archiveReadCounter struct {
	io.ReaderAt
	reads int
}

func (r *archiveReadCounter) ReadAt(p []byte, offset int64) (int, error) {
	r.reads++
	return r.ReaderAt.ReadAt(p, offset)
}

func TestDownloadedArchiveLimitsCheckedBeforeReadingMembers(t *testing.T) {
	for _, format := range []string{"zip", "7z"} {
		t.Run(format, func(t *testing.T) {
			data := zipOf(t, map[string][]byte{"README.md": []byte("# About\n"), "other.gbc": []byte("gbc")})
			if format == "7z" {
				var err error
				data, err = os.ReadFile("../../testdata/archive-markdown-mixed.7z")
				if err != nil {
					t.Fatal(err)
				}
			}
			counter := &archiveReadCounter{ReaderAt: bytes.NewReader(data)}
			var manifestErr error
			if format == "7z" {
				r, err := sevenzip.NewReader(counter, int64(len(data)))
				if err != nil {
					t.Fatal(err)
				}
				r.File[len(r.File)-1].UncompressedSize = DefaultArchiveLimits.MaxFileBytes + 1
				counter.reads = 0
				_, manifestErr = manifestFrom7z(r.File)
			} else {
				r, err := zip.NewReader(counter, int64(len(data)))
				if err != nil {
					t.Fatal(err)
				}
				r.File[len(r.File)-1].UncompressedSize64 = DefaultArchiveLimits.MaxFileBytes + 1
				counter.reads = 0
				_, manifestErr = manifestFromZIP(r.File)
			}
			if counter.reads != 0 {
				t.Fatalf("read archive member data %d times before rejecting oversized header", counter.reads)
			}
			if manifestErr == nil {
				t.Fatal("oversized archive header was accepted")
			}
		})
	}
}
