//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bodgit/sevenzip"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// pico8ArtSet7z is the 7z form of pico8ArtSetEntries, written by libarchive
// (bsdtar 3.7.4), since Go has no 7z writer:
//
//	bsdtar --format 7zip -cf set.7z game/main.p8 game/world2/main.p8 game/level2.p8 game/boss.p8.png game/lib.lua
const pico8ArtSet7z = "N3q8ryccAAOMnrURDAEAAAAAAAAiAAAAAAAAAE0MGV0AOBpIjiRWEBs71esuHjUZ+x1v86lyn5RdxaoqPdZcpZbFz1gAoQjPdU4qjIEy8QC7fW7MMbGGSYaX/ZVx74I0nqNRQH//7HwAAAAAgTMHrg/SdAR9QMCQ00PE4fnosgBZKTaGZ4tOqFH6rDGqdKhfE4NQI1lAHvZ+WacgdyB+p0+gUSLcHKN8jYOzlZALNhXmZspJQ+K44mJ1qbDl1CHsD+QuQMbFa7g+ZA7O4nbCv3aBdbc8arZxwkZeVeP8oFONX9qsqhFdg2F0HjB4ZBRDAz4HkQPm0EnwPOY+gLO+yKAI+DOLbCescpxVtrJoCAK6MmTbuMs1eyVd1172iQgo02KR8zUOp//+gOQAFwZKAQmAwgAHCwEAASMDAQEFXQAAgAAMgXoKAV9ay2IAAA=="

// pico8ArtSetEntries is a multi-file Pico-8 game with two carts that share a
// name in different folders, one more text cart, a compiled cart and a Lua
// file. Jawaka looks a cart's art up by the cart's own name, whatever its
// folder: main.png serves both main.p8 carts.
var pico8ArtSetEntries = map[string]string{
	"game/main.p8":        "pico-8 cartridge // MAIN\n",
	"game/world2/main.p8": "pico-8 cartridge // WORLD2\n",
	"game/level2.p8":      "pico-8 cartridge // LEVEL2\n",
	"game/boss.p8.png":    "PNG CART BOSS",
	"game/lib.lua":        "-- lib\n",
}

func pico8ArtSetArchives(t *testing.T) map[string][]byte {
	t.Helper()
	entries := make(map[string][]byte, len(pico8ArtSetEntries))
	for name, content := range pico8ArtSetEntries {
		entries[name] = []byte(content)
	}
	return map[string][]byte{"moss.zip": zipOf(t, entries), "moss.7z": decode7z(t, pico8ArtSet7z)}
}

// coverPNG is a cover image no cart's bytes can be mistaken for.
func coverPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		img.Set(x, x, color.RGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// artSetServer serves the free-file resolver and /cdn/{id} like freeFileServer
// and counts requests for the game's cover at /cdn/cover.
func artSetServer(t *testing.T, files map[string][]byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	covers := &atomic.Int32{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/file/"):
			fmt.Fprintf(w, `{"url":%q}`, srv.URL+"/cdn/"+strings.TrimPrefix(r.URL.Path, "/file/"))
		case strings.HasPrefix(r.URL.Path, "/cdn/"):
			id := strings.TrimPrefix(r.URL.Path, "/cdn/")
			if id == "cover" {
				covers.Add(1)
			}
			http.ServeContent(w, r, id, time.Time{}, bytes.NewReader(files[id]))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, covers
}

// artSetFixture is a Pico-8 set install on a Leaf card layout, with a server
// that counts cover downloads.
type artSetFixture struct {
	t       *testing.T
	sources leaf.SourceList
	catalog *leaf.Catalog
	inv     *inventory.Inventory
	invPath string
	game    itchio.Game
	srv     *httptest.Server
	covers  *atomic.Int32
	files   map[string][]byte // what the server serves at /cdn/{id}
	archive []byte
	name    string
}

func newArtSetFixture(t *testing.T, name string, archive []byte) *artSetFixture {
	t.Helper()
	sources, catalog, _ := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv, invPath := collisionInventory(t)
	files := map[string][]byte{"9": archive, "cover": coverPNG(t)}
	srv, covers := artSetServer(t, files)
	game := itchio.Game{Title: "Moss Garden", URL: "https://dev.itch.io/moss-garden", IsFree: true, CoverURL: srv.URL + "/cdn/cover"}
	return &artSetFixture{t: t, sources: sources, catalog: catalog, inv: inv, invPath: invPath,
		game: game, srv: srv, covers: covers, files: files, archive: archive, name: name}
}

func (f *artSetFixture) gameDir() string {
	return filepath.Join(f.sources[0].RomsPath, "PICO8", "Moss Garden")
}

// image is where Jawaka looks for the art of a cart called stem, wherever the
// cart sits in the Pico-8 folder: Images/PICO8/<name without extension>.png.
func (f *artSetFixture) image(stem string) string {
	return filepath.Join(f.sources[0].Root, "Images", "PICO8", stem+".png")
}

func (f *artSetFixture) install() *ArchiveDownloadWorker {
	f.t.Helper()
	return f.installInto(f.gameDir(), "", f.archive)
}

// installInto installs data as the set into gameDir, as an upload with the
// given fingerprint.
func (f *artSetFixture) installInto(gameDir, fingerprint string, data []byte) *ArchiveDownloadWorker {
	f.t.Helper()
	f.files["9"] = data
	var manifest roms.ZIPManifest
	if strings.HasSuffix(f.name, ".7z") {
		reader, err := sevenzip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			f.t.Fatal(err)
		}
		if manifest, err = manifestFrom7z(reader.File); err != nil {
			f.t.Fatal(err)
		}
	} else {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			f.t.Fatal(err)
		}
		if manifest, err = manifestFromZIP(reader.File); err != nil {
			f.t.Fatal(err)
		}
	}
	plan := ZIPPlan{
		Upload: freeUpload(f.srv, "9", f.name), CDNURL: f.srv.URL + "/cdn/9", Manifest: manifest, DownloadROMs: true,
		Pico8GameDir: gameDir + string(filepath.Separator),
	}
	plan.Upload.UploadFingerprint = fingerprint
	worker := NewArchiveDownloadWorker(itchio.NewClientWithBase(f.srv.URL), &settings.Config{}, f.game, &itchio.GameDetail{}, plan, f.inv, f.invPath)
	waitForWorker(f.t, func() bool { state := worker.loadState(); return state == zipDLDone || state == zipDLError })
	if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone || len(worker.skipped) != 0 {
		f.t.Fatalf("%s: archive = %+v, skipped %v", f.name, snapshot, worker.skipped)
	}
	return worker
}

// images lists the files in the Pico-8 image folder.
func (f *artSetFixture) images() []string {
	f.t.Helper()
	entries, err := os.ReadDir(filepath.Join(f.sources[0].Root, "Images", "PICO8"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		f.t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// records maps each recorded file, by its path below the game folder, to its record.
func (f *artSetFixture) records() map[string]inventory.DownloadedFile {
	f.t.Helper()
	entry, ok := f.inv.Lookup(f.game.URL)
	if !ok {
		f.t.Fatal("game missing from the inventory")
	}
	out := map[string]inventory.DownloadedFile{}
	for _, file := range entry.Files {
		rel, err := filepath.Rel(f.gameDir(), file.DestPath)
		if err != nil {
			f.t.Fatal(err)
		}
		out[filepath.ToSlash(rel)] = file
	}
	return out
}

// manage opens Manage and runs the delete behind the row whose label is label
// and whose detail contains detail ("" for any), or the action row of kind.
func (f *artSetFixture) manageDelete(match func(appui.ManageItem) bool) {
	f.t.Helper()
	flow, model, err := NewCatManageFlow(f.inv, f.invPath, f.game.URL, f.sources, f.catalog)
	if err != nil {
		f.t.Fatal(err)
	}
	for index, item := range model.Items {
		if match(item) {
			model.Cursor = index
			if _, _, err := flow.Activate(model); err != nil || model.State != appui.ManageConfirm {
				f.t.Fatalf("delete %q = state %v, %v", item.Label, model.State, err)
			}
			if _, err := flow.Confirm(model); err != nil {
				f.t.Fatal(err)
			}
			return
		}
	}
	f.t.Fatalf("no matching Manage row in %+v", model.Items)
}

func deleteAll(item appui.ManageItem) bool { return item.Kind == appui.ManageItemDeleteAll }

func deleteFile(label, detail string) func(appui.ManageItem) bool {
	return func(item appui.ManageItem) bool {
		return item.Kind == appui.ManageItemFile && item.Label == label && strings.Contains(item.Detail, detail)
	}
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// F26: every cart of a set gets launcher art where Jawaka looks for it. The
// game's cover is downloaded once. The carts of the set share Images/PICO8/,
// which Jawaka searches by the cart's own file name (without .p8 or .p8.png):
// the two main.p8 carts share main.png. A cart that is itself a PNG is its own
// art, as for a single .p8.png download. Nothing is saved for the Lua file or
// the playlist: the launcher lists no game for them.
func TestPico8SetCoverGoesWhereTheLauncherLooksForEachCart(t *testing.T) {
	for name, data := range pico8ArtSetArchives(t) {
		t.Run(name, func(t *testing.T) {
			f := newArtSetFixture(t, name, data)
			f.install()

			if got, want := fmt.Sprint(f.images()), "[boss.png level2.png main.png]"; got != want {
				t.Fatalf("%s: Images/PICO8 holds %s, want %s", name, got, want)
			}
			if got := f.covers.Load(); got != 1 {
				t.Fatalf("%s: %d cover downloads, want 1", name, got)
			}
			cover := fileHash(t, f.image("main"))
			if got := fileHash(t, f.image("level2")); got != cover {
				t.Fatalf("%s: level2.png is not the game's cover", name)
			}
			if got := readFile(t, f.image("boss")); got != "PNG CART BOSS" {
				t.Fatalf("%s: boss.png = %q, want the cart's own image", name, got)
			}

			records := f.records()
			for rel, image := range map[string]string{
				"main.p8": "main", "world2/main.p8": "main", "level2.p8": "level2", "boss.p8.png": "boss",
			} {
				file, ok := records[rel]
				if !ok {
					t.Fatalf("%s: no record for %s: %v", name, rel, records)
				}
				if file.ArtworkPath != f.image(image) || file.ArtworkHash != fileHash(t, f.image(image)) || !file.ArtworkCreated {
					t.Fatalf("%s: %s records art %q hash %q created %v, want %s as the app's", name, rel,
						file.ArtworkPath, file.ArtworkHash, file.ArtworkCreated, f.image(image))
				}
			}
			for rel, file := range records {
				if (rel == "lib.lua" || isPlaylist(rel)) && file.ArtworkPath != "" {
					t.Fatalf("%s: %s records art %q, want none", name, rel, file.ArtworkPath)
				}
			}
		})
	}
}

// F26: Manage's delete removes the images the app saved for the set. An image
// two carts share stays until the last of them goes.
func TestPico8SetDeleteRemovesTheArtItSaved(t *testing.T) {
	for name, data := range pico8ArtSetArchives(t) {
		t.Run(name, func(t *testing.T) {
			f := newArtSetFixture(t, name, data)
			f.install()

			f.manageDelete(deleteFile("main.p8", "Moss Garden/main.p8"))
			if _, err := os.Stat(f.image("main")); err != nil {
				t.Fatalf("%s: main.png went with one of the two carts that use it: %v", name, err)
			}
			f.manageDelete(deleteFile("level2.p8", ""))
			if _, err := os.Stat(f.image("level2")); !os.IsNotExist(err) {
				t.Fatalf("%s: level2.png outlived its cart: %v", name, err)
			}
			f.manageDelete(deleteFile("main.p8", "world2"))
			if got, want := fmt.Sprint(f.images()), "[boss.png]"; got != want {
				t.Fatalf("%s: Images/PICO8 holds %s after both main.p8 carts went, want %s", name, got, want)
			}
			f.manageDelete(deleteAll)
			if got := f.images(); len(got) != 0 {
				t.Fatalf("%s: Images/PICO8 holds %v after Delete all downloads", name, got)
			}
		})
	}
}

// F26: art you put in place is yours. The install keeps it, records it as
// yours, and a delete leaves it.
func TestPico8SetKeepsArtTheUserPutInPlace(t *testing.T) {
	for name, data := range pico8ArtSetArchives(t) {
		t.Run(name, func(t *testing.T) {
			f := newArtSetFixture(t, name, data)
			writeFiles(t, map[string]string{f.image("level2"): "my own level 2 art"})
			f.install()

			if got := readFile(t, f.image("level2")); got != "my own level 2 art" {
				t.Fatalf("%s: level2.png = %q, the install replaced it", name, got)
			}
			file := f.records()["level2.p8"]
			if file.ArtworkCreated || file.ArtworkPath != f.image("level2") {
				t.Fatalf("%s: level2.p8 records art %q created %v, want yours", name, file.ArtworkPath, file.ArtworkCreated)
			}
			if !f.records()["main.p8"].ArtworkCreated {
				t.Fatalf("%s: main.p8's art is not recorded as the app's", name)
			}

			f.manageDelete(deleteAll)
			if got := fmt.Sprint(f.images()); got != "[level2.png]" {
				t.Fatalf("%s: Images/PICO8 holds %s after the delete, want only your level2.png", name, got)
			}
			if got := readFile(t, f.image("level2")); got != "my own level 2 art" {
				t.Fatalf("%s: level2.png = %q after the delete", name, got)
			}
		})
	}
}

// F26: installing the set again keeps the images, their ownership and the
// number of cover downloads.
func TestPico8SetReinstallKeepsItsArt(t *testing.T) {
	for name, data := range pico8ArtSetArchives(t) {
		t.Run(name, func(t *testing.T) {
			f := newArtSetFixture(t, name, data)
			f.install()
			f.install()

			if got := f.covers.Load(); got != 1 {
				t.Fatalf("%s: %d cover downloads over two installs, want 1", name, got)
			}
			for rel, file := range f.records() {
				if rel != "lib.lua" && !isPlaylist(rel) && !file.ArtworkCreated {
					t.Fatalf("%s: %s lost the app's ownership of its art", name, rel)
				}
			}
			f.manageDelete(deleteAll)
			if got := f.images(); len(got) != 0 {
				t.Fatalf("%s: Images/PICO8 holds %v after Delete all downloads", name, got)
			}
		})
	}
}

// F26: an older version of the ZIP path saved one image named after the game's
// folder (Images/PICO8/Moss Garden.png), which matches no cart. A reinstall
// moves the carts' art to the names the launcher looks for and does not delete
// that image: the set's other files still record it as the app's, and Delete
// all downloads removes it.
func TestPico8SetReinstallLeavesTheOldFolderNamedImageToItsFiles(t *testing.T) {
	f := newArtSetFixture(t, "moss.zip", pico8ArtSetArchives(t)["moss.zip"])
	f.install()
	old := f.image("Moss Garden")
	writeFiles(t, map[string]string{old: "old cover"})
	for _, file := range f.records() {
		f.inv.SetArtwork(f.game.URL, file.DestPath, old, fileHash(t, old), true)
	}
	for _, stem := range []string{"main", "level2", "boss"} {
		if err := os.Remove(f.image(stem)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}

	f.install()

	for rel, image := range map[string]string{"main.p8": "main", "world2/main.p8": "main", "level2.p8": "level2", "boss.p8.png": "boss"} {
		if got := f.records()[rel].ArtworkPath; got != f.image(image) {
			t.Fatalf("%s records art %q after the reinstall, want %q", rel, got, f.image(image))
		}
	}
	if got := readFile(t, old); got != "old cover" {
		t.Fatalf("the old image = %q, want it left alone", got)
	}
	f.manageDelete(deleteAll)
	if got := f.images(); len(got) != 0 {
		t.Fatalf("Images/PICO8 holds %v after Delete all downloads", got)
	}
}
