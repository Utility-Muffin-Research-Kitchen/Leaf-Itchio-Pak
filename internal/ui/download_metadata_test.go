//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
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
	f := newInstallAPI(t, `{"uploads":[{"id":5,"filename":"release.zip","build_id":9}]}`, nil)
	flow := f.flow(t, &settings.Config{ROMLocation: "auto"})
	listing := flow.fetchForKey(itchio.OwnedKey{ID: 7})
	if listing.err != nil {
		t.Fatal(listing.err)
	}
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
	worker := &ArchiveDownloadWorker{client: flow.client, cfg: flow.cfg, game: flow.game, detail: flow.detail, inv: flow.inv,
		plan: ZIPPlan{Upload: listing.uploads[0], ROMDirs: map[string]string{".gb": filepath.Join(primary, "Roms", "GB")}, MusicDir: filepath.Join(primary, "Music")}}
	for _, file := range reader.File {
		if filepath.Ext(file.Name) == ".gb" {
			_, err = worker.extractROM(file, file.Name, time.Now())
		} else {
			_, err = worker.extractMusic(file, file.Name, time.Now())
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	entry, _ := flow.inv.Lookup(flow.game.URL)
	if entry.GameID != "42" || len(entry.Files) != 2 {
		t.Fatalf("archive entry = %+v", entry)
	}
	for _, file := range entry.Files {
		if file.UploadID != "5" || file.UploadFingerprint != "build:9" || file.OriginalUpload != "release.zip" {
			t.Fatalf("extracted upload identity = %+v", file)
		}
	}
}
