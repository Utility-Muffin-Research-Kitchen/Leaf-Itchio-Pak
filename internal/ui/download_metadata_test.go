//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func TestDownloadWorkersRetainSelectedUploadVersion(t *testing.T) {
	primary, _ := transactionPaths(t)
	f := newInstallAPI(t, `{"uploads":[{"id":1,"filename":"cart.gb","build_id":8},{"id":2,"filename":"cart.gbc","md5_hash":"abc"}]}`,
		map[string][]byte{"1": []byte("GB-ROM"), "2": []byte("GBC-ROM")})
	flow := f.flow(t, &settings.Config{ROMLocation: "auto"})
	listing := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil {
		t.Fatal(listing.err)
	}
	first := NewDirectDownloadWorker(flow.client, flow.cfg, flow.game, flow.detail, listing.uploads[0],
		filepath.Join(primary, "Roms", "GB", "cart.gb"), flow.inv, filepath.Join(t.TempDir(), "inventory.json"))
	waitFor(t, func() bool { return first.loadState() != dlDownloading })
	if first.CatSnapshot().State != appui.DownloadProgressDone {
		t.Fatalf("direct download failed: %+v", first.CatSnapshot())
	}
	second := NewMultiDownloadWorker(flow.client, flow.cfg, flow.game, flow.detail,
		[]romDownload{{Upload: listing.uploads[1], DestPath: filepath.Join(primary, "Roms", "GBC", "cart.gbc")}},
		flow.inv, filepath.Join(t.TempDir(), "inventory.json"))
	waitFor(t, func() bool { return second.loadState() != multiDLDownloading })
	if second.CatSnapshot().State != appui.DownloadProgressDone {
		t.Fatalf("multi download failed: %+v", second.CatSnapshot())
	}
	entry, _ := flow.inv.Lookup(flow.game.URL)
	if entry.GameID != "42" || len(entry.Files) != 2 {
		t.Fatalf("installed entry = %+v", entry)
	}
	for _, file := range entry.Files {
		want := map[string]string{"1": "build:8", "2": "md5:abc"}[file.UploadID]
		if want == "" || file.UploadFingerprint != want || file.OriginalUpload != file.Filename {
			t.Fatalf("installed upload metadata = %+v", file)
		}
	}
}

func TestArchiveWorkerRetainsOriginalUploadForROMAndMusic(t *testing.T) {
	primary, _ := transactionPaths(t)
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, name := range []string{"cart.gb", "theme.mp3"} {
		file, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		file.Write([]byte("file contents"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatal(err)
	}
	manifest := roms.ZIPManifest{}
	for _, file := range reader.File {
		manifest.Entries = append(manifest.Entries, roms.ZIPEntry{
			Name: file.Name, Kind: roms.ClassifyEntry(file.Name),
			Size: file.UncompressedSize64, CompressedSize: file.CompressedSize64,
		})
	}
	f := newInstallAPI(t, `{"uploads":[{"id":5,"filename":"release.zip","build_id":9}]}`,
		map[string][]byte{"5": data.Bytes()})
	flow := f.flow(t, &settings.Config{ROMLocation: "auto"})
	listing := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil {
		t.Fatal(listing.err)
	}
	plan := ZIPPlan{Upload: listing.uploads[0], Manifest: manifest, DownloadROMs: true, DownloadMusic: true,
		ROMDirs: map[string]string{".gb": filepath.Join(primary, "Roms", "GB")}, MusicDir: filepath.Join(primary, "Music")}
	worker := NewArchiveDownloadWorker(flow.client, flow.cfg, flow.game, flow.detail, plan, flow.inv,
		filepath.Join(t.TempDir(), "inventory.json"))
	waitFor(t, func() bool { return worker.loadState() == zipDLDone || worker.loadState() == zipDLError })
	if state := worker.CatSnapshot(); state.State != appui.DownloadProgressDone {
		t.Fatalf("archive download failed: %+v", state)
	}
	entry, _ := flow.inv.Lookup(flow.game.URL)
	if entry.GameID != "42" || len(entry.Files) != 2 {
		t.Fatalf("archive entry = %+v", entry)
	}
	for _, file := range entry.Files {
		if content, err := os.ReadFile(file.DestPath); err != nil || string(content) != "file contents" {
			t.Fatalf("extracted contents for %s: %q, %v", file.Filename, content, err)
		}
		if file.UploadID != "5" || file.UploadFingerprint != "build:9" || file.OriginalUpload != "release.zip" {
			t.Fatalf("extracted upload identity = %+v", file)
		}
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, content := range files {
		file, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		file.Write([]byte(content))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func zipManifest(t *testing.T, data []byte) roms.ZIPManifest {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	manifest := roms.ZIPManifest{}
	for _, file := range reader.File {
		manifest.Entries = append(manifest.Entries, roms.ZIPEntry{
			Name: file.Name, Kind: roms.ClassifyEntry(file.Name),
			Size: file.UncompressedSize64, CompressedSize: file.CompressedSize64,
		})
	}
	return manifest
}

func TestArchiveReinstallAcknowledgesUploadWithRenamedMember(t *testing.T) {
	primary, _ := transactionPaths(t)
	v1 := zipBytes(t, map[string]string{"cart.gb": "rom v1", "01 Theme.mp3": "theme"})
	f := newInstallAPI(t, `{"uploads":[{"id":5,"filename":"release.zip","build_id":1}]}`, map[string][]byte{"5": v1})
	flow := f.flow(t, &settings.Config{ROMLocation: "auto"})
	listing := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil {
		t.Fatal(listing.err)
	}
	install := func(upload roms.Upload, data []byte) {
		t.Helper()
		plan := ZIPPlan{Upload: upload, Manifest: zipManifest(t, data), DownloadROMs: true, DownloadMusic: true,
			ROMDirs: map[string]string{".gb": filepath.Join(primary, "Roms", "GB")}, MusicDir: filepath.Join(primary, "Music", "Leafbound")}
		worker := NewArchiveDownloadWorker(flow.client, flow.cfg, flow.game, flow.detail, plan, flow.inv,
			filepath.Join(t.TempDir(), "inventory.json"))
		waitFor(t, func() bool { return worker.loadState() == zipDLDone || worker.loadState() == zipDLError })
		if state := worker.CatSnapshot(); state.State != appui.DownloadProgressDone {
			t.Fatalf("archive download failed: %+v", state)
		}
	}
	install(listing.uploads[0], v1)
	url := flow.game.URL
	flow.inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, []inventory.UpstreamFile{{Filename: "release.zip", UploadID: "5", Fingerprint: "build:1"}})
	v2Listing := []inventory.UpstreamFile{{Filename: "release.zip", UploadID: "5", Fingerprint: "build:2"}}
	flow.inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, v2Listing)
	if !flow.inv.HasPendingUpdates(url) {
		t.Fatal("version 2 was not detected")
	}

	v2 := zipBytes(t, map[string]string{"cart.gb": "rom v2", "01 Main Theme.mp3": "theme"})
	f.files["5"] = v2
	upload := listing.uploads[0]
	upload.UploadFingerprint = "build:2"
	install(upload, v2)
	if flow.inv.HasPendingUpdates(url) {
		t.Fatalf("update pending after reinstall: %+v", flow.inv.PendingUpdateFiles(url))
	}
	flow.inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, v2Listing)
	if flow.inv.HasPendingUpdates(url) {
		t.Fatalf("re-check raised the update again: %+v", flow.inv.PendingUpdateFiles(url))
	}
	entry, _ := flow.inv.Lookup(url)
	oldTheme := filepath.Join(primary, "Music", "Leafbound", "01 Theme.mp3")
	if len(entry.LeftoverFiles) != 1 || entry.LeftoverFiles[0] != oldTheme {
		t.Fatalf("left over files = %v, want [%s]", entry.LeftoverFiles, oldTheme)
	}
	if _, err := os.Stat(oldTheme); err != nil {
		t.Fatalf("the old track was deleted: %v", err)
	}
}

func TestDirectReinstallAcknowledgesUploadRenamedUnderSameID(t *testing.T) {
	primary, _ := transactionPaths(t)
	f := newInstallAPI(t, `{"uploads":[{"id":7,"filename":"cart-v1.gb","build_id":1}]}`, map[string][]byte{"7": []byte("rom v1")})
	flow := f.flow(t, &settings.Config{ROMLocation: "auto"})
	listing := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil {
		t.Fatal(listing.err)
	}
	install := func(upload roms.Upload) {
		t.Helper()
		worker := NewDirectDownloadWorker(flow.client, flow.cfg, flow.game, flow.detail, upload,
			filepath.Join(primary, "Roms", "GB", upload.Filename), flow.inv, filepath.Join(t.TempDir(), "inventory.json"))
		waitFor(t, func() bool { return worker.loadState() != dlDownloading })
		if worker.CatSnapshot().State != appui.DownloadProgressDone {
			t.Fatalf("direct download failed: %+v", worker.CatSnapshot())
		}
	}
	install(listing.uploads[0])
	url := flow.game.URL
	flow.inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, []inventory.UpstreamFile{{Filename: "cart-v1.gb", UploadID: "7", Fingerprint: "build:1"}})
	v2Listing := []inventory.UpstreamFile{{Filename: "cart-v2.gb", UploadID: "7", Fingerprint: "build:2"}}
	flow.inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, v2Listing)
	if !flow.inv.HasPendingUpdates(url) {
		t.Fatal("version 2 was not detected")
	}
	f.files["7"] = []byte("rom v2")
	upload := listing.uploads[0]
	upload.Filename, upload.UploadFingerprint = "cart-v2.gb", "build:2"
	install(upload)
	flow.inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, v2Listing)
	if flow.inv.HasPendingUpdates(url) {
		t.Fatalf("update pending after reinstall and re-check: %+v", flow.inv.PendingUpdateFiles(url))
	}
	if entry, _ := flow.inv.Lookup(url); len(entry.LeftoverFiles) != 1 || filepath.Base(entry.LeftoverFiles[0]) != "cart-v1.gb" {
		t.Fatalf("left over files = %v, want the version 1 ROM", entry.LeftoverFiles)
	}
}

func TestInstallSeedsUpdateChecksFromItsListing(t *testing.T) {
	primary, _ := transactionPaths(t)
	f := newInstallAPI(t, `{"uploads":[{"id":1,"filename":"cart.gb","build_id":1},{"id":2,"filename":"game-windows.zip","build_id":1},{"id":4,"filename":"web.zip","type":"html"}]}`,
		map[string][]byte{"1": []byte("rom")})
	flow := f.flow(t, &settings.Config{ROMLocation: "auto"})
	listing := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil {
		t.Fatal(listing.err)
	}
	worker := NewDirectDownloadWorker(flow.client, flow.cfg, flow.game, flow.detail, listing.uploads[0],
		filepath.Join(primary, "Roms", "GB", "cart.gb"), flow.inv, filepath.Join(t.TempDir(), "inventory.json"))
	waitFor(t, func() bool { return worker.loadState() != dlDownloading })
	if worker.CatSnapshot().State != appui.DownloadProgressDone {
		t.Fatalf("direct download failed: %+v", worker.CatSnapshot())
	}
	url := flow.game.URL
	entry, _ := flow.inv.Lookup(url)
	if entry.UpstreamSource != inventory.SourceAPI || len(entry.KnownUpstreamFiles) != 2 || flow.inv.HasPendingUpdates(url) {
		t.Fatalf("seeded entry = %+v, want the listing without its web build", entry)
	}
	// The developer replaces the ROM before the first background check.
	flow.inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, []inventory.UpstreamFile{
		{Filename: "cart-v2.gb", UploadID: "3", Fingerprint: "build:4"},
		{Filename: "game-windows.zip", UploadID: "2", Fingerprint: "build:1"},
	})
	if pending := flow.inv.PendingUpdateFiles(url); len(pending) != 1 || pending[0].UploadID != "3" {
		t.Fatalf("first check after the replacement = %+v, want the new ROM", pending)
	}
}

func TestWebListingSeedsPageBaseline(t *testing.T) {
	listing := webUploadListing([]itchio.Upload{{Filename: "cart.gb", UploadID: "1"}})
	files, source := installListing(roms.Upload{Filename: "cart.gb", UploadID: "1", Listing: listing})
	if source != inventory.SourcePage || len(files) != 1 || files[0].UploadID != "1" || files[0].Fingerprint != "" {
		t.Fatalf("web listing seed = %+v from %q", files, source)
	}
	if files, source := installListing(roms.Upload{Filename: "cart.gb"}); files != nil || source != "" {
		t.Fatalf("an upload without a listing seeded %+v from %q", files, source)
	}
}

func TestListingSeedMarksDesktopAndWebBuilds(t *testing.T) {
	listing := apiUploadListing([]itchio.Upload{
		{Filename: "cart.gb", UploadID: "1"},
		{Filename: "game-windows.zip", UploadID: "2", Traits: []string{"p_windows"}},
	})
	files, source := installListing(roms.Upload{Filename: "cart.gb", UploadID: "1", Listing: listing})
	if source != inventory.SourceAPI || len(files) != 2 || files[0].DesktopOrWebOnly || !files[1].DesktopOrWebOnly {
		t.Fatalf("seed = %+v from %q, want only the Windows build marked", files, source)
	}
}
