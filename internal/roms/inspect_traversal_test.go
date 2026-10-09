package roms_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// traversal7z is a Pico-8 set written by libarchive (bsdtar 3.7.4 with -P and
// -s renames, since Go has no 7z writer): main.p8, level2.p8, lib.lua and four
// carts named "../../../../evil.p8", "game/../../evil2.p8", "/tmp/abs-evil.p8"
// and "..\\..\\evil3.p8".
const traversal7z = "N3q8ryccAANtYmaTMwEAAAAAAAAiAAAAAAAAAEgpcb0AOBpIjiRWEBs71esuHjUZ+x1v86lyn5RdxaoqPdZYuTI/ewHG7pkH+gso8Vgcw5IT/bgIwUJi5D//+NWAAAAAgTMHrg/R1LcIoJCgd7D+k4fhqh8nai1cYm6bIz1o/20LOA7ifzb76hPspiXSIlYzSkgiwqUYkaCL+I82E6Q7qfIzdOv+jrybByADmRsgqaPMRYKGnZ/YXHV7tENWHcyPn91n35gs3BTWLVYBn5IXoodMJ/wPnRJhCwwKgRUG1Pn0RZiNfqWvl/++lv+Y628rb37ZYwxpU/G6DZWoRbEahJ3j8+aKIjtHy6J+SyAA46uupbp+t+NJmiMZCw+1Giu0OK1Bw45tQ2D9LKUCD6r2G9zRoNhu5SJddHbBRLPMrI9yaS2/CMoNEjPYeevQ//92B4AAFwY+AQmA9QAHCwEAASMDAQEFXQAAgAAMgeUKAS8ePAwAAA=="

// The remote inspection reads an archive's member names and sizes and writes
// no member: its only file is a temporary copy of the archive, under a name of
// its own, removed afterwards. Names with "../", an absolute path or
// backslashes reach nothing on disk.
func TestInspectionWritesNoMemberOfAnArchive(t *testing.T) {
	zipData := buildTestZIP(t, map[string]string{
		"main.p8": "pico-8 cartridge // MAIN\n", "level2.p8": "pico-8 cartridge // LEVEL2\n", "lib.lua": "-- lib\n",
		"../../../../evil.p8": "pico-8 cartridge // EVIL\n", "game/../../evil2.p8": "pico-8 cartridge // EVIL2\n",
		"/tmp/abs-evil.p8": "pico-8 cartridge // ABS\n", "..\\..\\evil3.p8": "pico-8 cartridge // EVIL3\n",
	})
	sevenData, err := base64.StdEncoding.DecodeString(traversal7z)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		range_ bool
		run    func(*http.Client, string) (roms.ZIPManifest, error)
	}{
		{"zip by ranges", zipData, true, func(c *http.Client, url string) (roms.ZIPManifest, error) {
			return roms.InspectRemoteZIP(c, url, nil)
		}},
		{"zip by a full download", zipData, false, func(c *http.Client, url string) (roms.ZIPManifest, error) {
			return roms.InspectRemoteZIP(c, url, nil)
		}},
		{"7z", sevenData, true, roms.InspectRemote7z},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.range_ {
					http.ServeContent(w, r, "set", time.Time{}, bytes.NewReader(tc.data))
					return
				}
				w.Write(tc.data)
			}))
			defer srv.Close()

			manifest, err := tc.run(srv.Client(), srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			if len(manifest.Entries) != 7 {
				t.Fatalf("manifest = %+v, want 7 members", manifest.Entries)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				t.Errorf("inspection left %s in the temporary folder", entry.Name())
			}
			if _, err := os.Stat(strings.TrimSuffix(tmp, "/") + "/../evil.p8"); err == nil {
				t.Error("a member was written beside the temporary folder")
			}
		})
	}
}
