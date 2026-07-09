package roms_test

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// sevenZipFixture contains nested/game.gbc, soundtrack/disc-1/track01.flac,
// archives/inner.zip, and a __MACOSX resource fork. It was created with
// libarchive and is embedded so the test has no external 7z tool dependency.
const sevenZipFixture = "N3q8ryccAAPFgRUMIAEAAAAAAAAiAAAAAAAAAIUSmK8AKRPF0WOsv7p0QEut8eus3aArp3HlqBgWJy/i///3VAAAAACBMweuD9As9LyfP0dBZcXYr+QLlwpy9hlh2A091ZWAn+mJNCTxdmSsNObau2RNF4ZU66EcW/b3JDkTqBGfj49xRdRhAkKgYDOXnmzBaYhKFmoKAAKM/2yEj2zL8p5cs04dkfNG3G/93esjY9JmM3wm2ZlTmMVkLphzoZPw6MvPhl8Oou0R7ADmValyLoUvcy++/dmMuzRi2+Vql2tLwmAej5MqbxGOQJYKys7c+qSuqoRUBhYMPlWDn8F3jIqPCKEFLVmtQmJZEyE8xcdjic+bD+D5vuVt4kYc6bQYzItspPCsgNTbqrUMOG6XPuSxbyVdYeGdTbJX//OnWGAXBiIBCYD+AAcLAQABIwMBAQVdAACAAAyCGwoBf4Rc2QAA"

func TestInspectRemote7zClassifiesNestedContent(t *testing.T) {
	archive, err := base64.StdEncoding.DecodeString(sevenZipFixture)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	manifest, err := roms.InspectRemote7z(srv.Client(), srv.URL+"/bundle.7z")
	if err != nil {
		t.Fatalf("InspectRemote7z: %v", err)
	}
	if manifest.ROMCount() != 1 {
		t.Errorf("ROMCount = %d, want 1", manifest.ROMCount())
	}
	if manifest.MusicCount() != 1 {
		t.Errorf("MusicCount = %d, want 1", manifest.MusicCount())
	}
	if !manifest.HasOtherFiles() {
		t.Error("nested ZIP should remain KindOther")
	}
	if len(manifest.Entries) != 3 {
		t.Errorf("entry count = %d, want 3 (__MACOSX resource fork excluded)", len(manifest.Entries))
	}
}
