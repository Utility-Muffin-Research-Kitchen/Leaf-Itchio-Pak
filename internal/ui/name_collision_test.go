//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bodgit/sevenzip"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// freeFileServer answers the web resolver (POST /file/{id}) with a CDN URL
// and serves each file's bytes from /cdn/{id}.
func freeFileServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/file/"):
			fmt.Fprintf(w, `{"url":%q}`, srv.URL+"/cdn/"+strings.TrimPrefix(r.URL.Path, "/file/"))
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			id := strings.TrimPrefix(r.URL.Path, "/cdn/")
			http.ServeContent(w, r, id, time.Time{}, bytes.NewReader(files[id]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func freeUpload(srv *httptest.Server, id, filename string) roms.Upload {
	return roms.Upload{Filename: filename, URL: srv.URL + "/file/" + id + "?key=eyJpZCI6NDJ9.sig&csrf=csrf"}
}

func gbROM(tag string) []byte {
	data := make([]byte, 0x200)
	copy(data[0x104:], []byte{0xCE, 0xED, 0x66, 0x66, 0xCC, 0x0D, 0x00, 0x0B})
	copy(data[0x150:], tag)
	return data
}

func collisionInventory(t *testing.T) (*inventory.Inventory, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inventory.json")
	inv, err := inventory.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return inv, path
}

func filesIn(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = string(data)
	}
	return out
}

var collisionGame = itchio.Game{Title: "Leafbound", URL: "https://dev.itch.io/leafbound", IsFree: true}

// Two discs of one game would both be renamed to the game title; the second
// must not replace the first (upstream 4915dde).
func TestMultiDownloadKeepsEveryFileWhenUnifiedNamesCollide(t *testing.T) {
	primary, _ := transactionPaths(t)
	srv := freeFileServer(t, map[string][]byte{"1": []byte("DISC-ONE"), "2": []byte("DISC-TWO")})
	inv, invPath := collisionInventory(t)
	psDir := filepath.Join(primary, "Roms", "PS")
	downloads := []romDownload{
		{Upload: freeUpload(srv, "1", "leafbound-disc1.chd"), DestPath: filepath.Join(psDir, "leafbound-disc1.chd")},
		{Upload: freeUpload(srv, "2", "leafbound-disc2.chd"), DestPath: filepath.Join(psDir, "leafbound-disc2.chd")},
	}
	worker := NewMultiDownloadWorker(itchio.NewClientWithBase(srv.URL), &settings.Config{UnifiedNaming: true},
		collisionGame, &itchio.GameDetail{}, downloads, inv, invPath)
	waitForWorker(t, func() bool { return worker.loadState() != multiDLDownloading })
	if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
		t.Fatalf("multi download = %+v", snapshot)
	}

	got := filesIn(t, psDir)
	contents := make([]string, 0, len(got))
	for _, data := range got {
		contents = append(contents, data)
	}
	sort.Strings(contents)
	if len(contents) != 2 || contents[0] != "DISC-ONE" || contents[1] != "DISC-TWO" {
		t.Fatalf("PS folder = %v, want both discs", got)
	}
	if worker.finalPaths[0] == worker.finalPaths[1] {
		t.Fatalf("both files recorded at %s", worker.finalPaths[0])
	}
}

// A single unified name is still applied when nothing else claims it.
func TestMultiDownloadStillUnifiesANonCollidingName(t *testing.T) {
	primary, _ := transactionPaths(t)
	srv := freeFileServer(t, map[string][]byte{"1": gbROM("GB"), "2": []byte("NES")})
	inv, invPath := collisionInventory(t)
	downloads := []romDownload{
		{Upload: freeUpload(srv, "1", "lb.gb"), DestPath: filepath.Join(primary, "Roms", "GB", "lb.gb")},
		{Upload: freeUpload(srv, "2", "lb.nes"), DestPath: filepath.Join(primary, "Roms", "FC", "lb.nes")},
	}
	worker := NewMultiDownloadWorker(itchio.NewClientWithBase(srv.URL), &settings.Config{UnifiedNaming: true},
		collisionGame, &itchio.GameDetail{}, downloads, inv, invPath)
	waitForWorker(t, func() bool { return worker.loadState() != multiDLDownloading })
	if filepath.Base(worker.finalPaths[0]) != "Leafbound.gb" || filepath.Base(worker.finalPaths[1]) != "Leafbound.nes" {
		t.Fatalf("final paths = %q, want both unified", worker.finalPaths)
	}
}

// Files whose planned names differ only by case are one file on FAT32.
func TestSealRejectsDestinationsThatCollideOnFAT32(t *testing.T) {
	primary, _ := transactionPaths(t)
	psDir := filepath.Join(primary, "Roms", "PS")
	plan := &CatDownloadPlan{
		Kind:      CatDownloadPlanMulti,
		Uploads:   []roms.Upload{{Filename: "Disc.chd"}, {Filename: "disc.chd"}},
		DestPaths: []string{filepath.Join(psDir, "Disc.chd"), filepath.Join(psDir, "disc.chd")},
	}
	if _, err := plan.Seal(collisionGame, nil); err == nil {
		t.Fatal("sealed two files that FAT32 stores under one name")
	}
}

func zipOf(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	archive := zip.NewWriter(&buf)
	for _, name := range names {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		writer.Write(entries[name])
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// 7z fixtures written by libarchive (bsdtar 3.7.4) from tar streams, since
// Go has no 7z writer: roms holds extra.dat and leafbound.gb, both Game Boy
// ROMs by magic bytes; pico8 holds game/Main.p8 and game/main.p8.
const (
	romsCollision7z  = "N3q8ryccAANGV7fcpAAAAAAAAAAhAAAAAAAAANkgO+8AAG/hDutf1sspZKiVUd+BbxHGletl3gM9OqokyyQYLEzeePaYQf//3bpmAAAAgTMHrg/QxEs8nz9HQVjW/gJqJajogIWPWMVE3SzZDFIuHPsZcOQJGagjBn9nBIroED71KRvcFOJukPbKfTTMPwOu8V6jMuLNO5cX80WvixpsxBwG9WbwHwDPb3y8MBY9vguYFuVCYq24SANJEUuIiD//+hRYABcGLAEJeAAHCwEAASMDAQEFXQAAgAAMgIIKATQV3uAAAA=="
	pico8Collision7z = "N3q8ryccAAN9pfObnAAAAAAAAAAhAAAAAAAAAFsmNk8AOBpIjiRWEBs71esuHjUZ+x1v86lymVg+RSz+vs3mBy3B/4tUPoTqa5j//BMgAAAAgTMHrg/Q8bH8nz9HQVjW/gJqJajogIWADK+n6253tq7m0K7KU1F7K+ZOgXQXHUlzggrBC+t2m0IBwBHIO1lno/I5ZWhqw6tt5GZL+fuSZKwDmmjq50HWh+IOaJ8ANOkY0gZuWwj//6ldwAAXBi8BCW0ABwsBAAEjAwEBBV0AAIAADICGCgHbSeSkAAA="
)

func decode7z(t *testing.T, fixture string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// runArchive extracts data (a .zip or .7z named filename) through the real
// worker. pico8 selects the path-preserving Pico-8 extraction.
func runArchive(t *testing.T, filename string, data []byte, cfg *settings.Config, pico8 bool, configure ...func(plan *ZIPPlan, primary string)) (*ArchiveDownloadWorker, string) {
	t.Helper()
	primary, _ := transactionPaths(t)
	srv := freeFileServer(t, map[string][]byte{"9": data})
	var manifest roms.ZIPManifest
	if strings.HasSuffix(filename, ".7z") {
		reader, err := sevenzip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		manifest = manifestFrom7z(reader.File)
	} else {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		manifest = manifestFromZIP(reader.File)
	}
	inv, invPath := collisionInventory(t)
	plan := ZIPPlan{
		Upload: freeUpload(srv, "9", filename), CDNURL: srv.URL + "/cdn/9",
		Manifest: manifest, DownloadROMs: true,
	}
	if pico8 {
		plan.Pico8GameDir = filepath.Join(primary, "Roms", "PICO8", "Leafbound") + string(filepath.Separator)
	}
	for _, apply := range configure {
		apply(&plan, primary)
	}
	worker := NewArchiveDownloadWorker(itchio.NewClientWithBase(srv.URL), cfg, collisionGame, &itchio.GameDetail{}, plan, inv, invPath)
	waitForWorker(t, func() bool { state := worker.loadState(); return state == zipDLDone || state == zipDLError })
	if pico8 {
		return worker, filepath.Join(primary, "Roms", "PICO8", "Leafbound")
	}
	return worker, filepath.Join(primary, "Roms", "GB")
}

// Extraction re-classifies entries by magic bytes after the inspection-time
// duplicate check, so two entries can reach one unified name (upstream
// c346eb0).
func TestArchiveKeepsEveryROMWhenMagicDetectionMakesNamesCollide(t *testing.T) {
	for name, data := range map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{"leafbound.gb": gbROM("MAIN"), "extra.dat": gbROM("EXTRA")}),
		"leafbound.7z":  decode7z(t, romsCollision7z),
	} {
		worker, gbDir := runArchive(t, name, data, &settings.Config{UnifiedNaming: true}, false)
		if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
			t.Fatalf("%s: archive = %+v", name, snapshot)
		}
		got := filesIn(t, gbDir)
		var tags []string
		for _, data := range got {
			tags = append(tags, strings.Trim(data[0x150:0x160], "\x00"))
		}
		sort.Strings(tags)
		if len(tags) != 2 || tags[0] != "EXTRA" || tags[1] != "MAIN" {
			t.Fatalf("%s: GB folder = %v, want both ROMs", name, keys(got))
		}
	}
}

// Pico-8 extraction keeps relative paths; two that differ only by case are one
// file on FAT32, so the later entry is skipped instead of replacing the first.
func TestPico8ArchiveSkipsAPathThatCollidesOnFAT32(t *testing.T) {
	for name, data := range map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{
			"game/Main.p8": []byte("pico-8 cartridge // FIRST\n"), "game/main.p8": []byte("pico-8 cartridge // SECOND\n"),
		}),
		"leafbound.7z": decode7z(t, pico8Collision7z),
	} {
		worker, dir := runArchive(t, name, data, &settings.Config{UnifiedNaming: true}, true)
		got := filesIn(t, dir)
		if len(worker.extracted) != 1 || len(worker.skipped) != 1 || len(got) != 1 {
			t.Fatalf("%s: extracted %v skipped %v folder %v, want one kept and one skipped", name, worker.extracted, worker.skipped, keys(got))
		}
		for file, data := range got {
			if !strings.Contains(data, "FIRST") {
				t.Fatalf("%s: %s = %q, want the first entry intact", name, file, data)
			}
		}
	}
}

// Two entries that end up with the same original name after detection must
// not overwrite each other either; the later one is skipped safely.
func TestArchiveSkipsAnEntryWhoseOriginalNameIsAlsoTaken(t *testing.T) {
	data := zipOf(t, map[string][]byte{"game.gb": gbROM("FIRST"), "game.dat": gbROM("SECOND")})
	worker, gbDir := runArchive(t, "game.zip", data, &settings.Config{}, false)
	got := filesIn(t, gbDir)
	if len(got) != 1 || strings.Trim(got["game.gb"][0x150:0x160], "\x00") != "SECOND" && strings.Trim(got["game.gb"][0x150:0x160], "\x00") != "FIRST" {
		t.Fatalf("GB folder = %v", keys(got))
	}
	if len(worker.skipped) != 1 || len(worker.extracted) != 1 {
		t.Fatalf("extracted %v skipped %v, want one of each", worker.extracted, worker.skipped)
	}
}

// Music fixtures written by libarchive (bsdtar 3.7.4) from tar streams:
// folders holds Soundtrack/cd1/01 Theme.ogg and Soundtrack/cd2/01 Theme.ogg;
// caseOnly holds Soundtrack/Theme.ogg (FIRST) and Soundtrack/theme.ogg.
const (
	musicFolders7z  = "N3q8ryccAANuPe61lwAAAAAAAAAhAAAAAAAAACt5JycAIZECIUoi6IpeKwPOP21IPB///xB0AAAAAIEzB64Pz4CuDA/r6p4BDWIDjdNMQj8OeVdVR7yHOmV6Y0mMnUnmPOagPWa+qKF4BtlCogzr0slWp58fkB8fdFFmu4Ew4wXzqcTS+FBtAEchJBWib08dK8KQ89DZA8CNSE9v/l2nix59RuUvHfu4rwYkxK0+x0oHZ//+FweAFwYYAQl/AAcLAQABIwMBAQVdAACAAAyAwgoBFloAJAAA"
	musicCaseOnly7z = "N3q8ryccAAM0/+SxjAAAAAAAAAAhAAAAAAAAAPfYmGYAIxJGitO8BlFA2/nZcAdE//6bQAAAAIEzB64Pz0tvjAfIQ4CDgVv/rHbPeD8OahwBsRDpkth/XU5hU1AGKndPgpJ+grT8LZPeXoqZNu5CdtjLH9v6nXmBvbPn0vhEcTivzZAeSi4Ty5fT/2qpJFoJdF2V8uMP/mYQ7SitTepA0zgNhSlaZ///amIAABcGFQEJdwAHCwEAASMDAQEFXQAAgAAMgKYKAaIjWZgAAA=="
)

// musicOnly extracts only the soundtrack into Music/Leafbound.
func musicOnly(plan *ZIPPlan, primary string) {
	plan.DownloadROMs = false
	plan.DownloadMusic = true
	plan.MusicDir = filepath.Join(primary, "Music", "Leafbound") + string(filepath.Separator)
}

func musicDir(worker *ArchiveDownloadWorker) string { return filepath.Clean(worker.plan.MusicDir) }

// Tracks with one name in different archive folders land in one flat Music
// folder; both must survive under distinguishable names.
func TestArchiveMusicKeepsSameNamedTracksFromDifferentFolders(t *testing.T) {
	for name, data := range map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{
			"Soundtrack/cd1/01 Theme.ogg": []byte("CD1-THEME"), "Soundtrack/cd2/01 Theme.ogg": []byte("CD2-THEME"),
		}),
		"leafbound.7z": decode7z(t, musicFolders7z),
	} {
		worker, _ := runArchive(t, name, data, &settings.Config{UnifiedNaming: true}, false, musicOnly)
		if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
			t.Fatalf("%s: archive = %+v", name, snapshot)
		}
		got := filesIn(t, musicDir(worker))
		if len(got) != 2 || got["cd1 - 01 Theme.ogg"] != "CD1-THEME" || got["cd2 - 01 Theme.ogg"] != "CD2-THEME" {
			t.Fatalf("%s: Music folder = %v, want both tracks named by their folders", name, got)
		}
		entry, _ := worker.inv.Lookup(collisionGame.URL)
		recorded := map[string]bool{}
		for _, file := range entry.Files {
			if file.FileType != inventory.FileTypeMusic {
				t.Fatalf("%s: %s recorded as %q, want music", name, file.Filename, file.FileType)
			}
			recorded[file.DestPath] = true
		}
		if len(recorded) != 2 {
			t.Fatalf("%s: inventory = %+v, want both tracks", name, entry.Files)
		}
	}
}

// A single track keeps its plain name; only colliding tracks gain a prefix.
func TestArchiveMusicKeepsPlainNamesWithoutACollision(t *testing.T) {
	data := zipOf(t, map[string][]byte{"Soundtrack/cd1/01 Theme.ogg": []byte("A"), "Soundtrack/cd2/02 Boss.ogg": []byte("B")})
	worker, _ := runArchive(t, "leafbound.zip", data, &settings.Config{}, false, musicOnly)
	if got := filesIn(t, musicDir(worker)); len(got) != 2 || got["01 Theme.ogg"] != "A" || got["02 Boss.ogg"] != "B" {
		t.Fatalf("Music folder = %v", got)
	}
}

// Names that differ only by case are one file on FAT32 and have no folder to
// tell them apart: the later track is skipped, never written over the first.
func TestArchiveMusicSkipsACaseOnlyDuplicate(t *testing.T) {
	for name, data := range map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{"Soundtrack/Theme.ogg": []byte("FIRST"), "Soundtrack/theme.ogg": []byte("SECOND")}),
		"leafbound.7z":  decode7z(t, musicCaseOnly7z),
	} {
		worker, _ := runArchive(t, name, data, &settings.Config{}, false, musicOnly)
		got := filesIn(t, musicDir(worker))
		if len(worker.extracted) != 1 || len(worker.skipped) != 1 || len(got) != 1 {
			t.Fatalf("%s: extracted %v skipped %v folder %v, want one kept and one skipped", name, worker.extracted, worker.skipped, got)
		}
		for file, data := range got {
			if data != "FIRST" {
				t.Fatalf("%s: %s = %q, want the first track intact", name, file, data)
			}
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func waitForWorker(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
