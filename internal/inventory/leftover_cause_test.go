package inventory_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// F28: a file an install leaves behind is either from an older version of its
// upload or an earlier copy of the same version that the install wrote
// somewhere else. The log says which, and so does Manage.

const (
	causeGame = "https://dev.itch.io/moss-garden"
	copyA     = "/leaf/Roms/PICO8/Moss Garden/"
	copyB     = "/leaf/Roms/PICO8/Moss Garden 2/"
)

// installAt records a Pico-8 set in dir the way the archive worker does and
// commits the install.
func installAt(inv *inventory.Inventory, dir, fingerprint string) {
	files := []inventory.DownloadedFile{
		member(dir+"main.p8", "moss.zip", "game/main.p8", time.Now(), fingerprint),
		member(dir+"world2/main.p8", "moss.zip", "game/world2/main.p8", time.Now(), fingerprint),
		member(dir+"lib.lua", "moss.zip", "game/lib.lua", time.Now(), fingerprint),
	}
	written := make([]string, 0, len(files))
	for _, file := range files {
		file.UploadID = "9"
		inv.Add(causeGame, inventory.Entry{Title: "Moss Garden"}, file)
		written = append(written, file.DestPath)
	}
	inv.CommitUploadInstall(causeGame, inventory.UploadInstall{
		UploadID: "9", Filename: "moss.zip", Fingerprint: fingerprint, Written: written,
	})
}

func leftOverLogLines(logs *artworkLogs) []string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	var lines []string
	for _, line := range strings.Split(logs.buf.String(), "\n") {
		if strings.Contains(line, "inventory:") && (strings.Contains(line, "left over") || strings.Contains(line, "earlier copy")) {
			// Drop the timestamp and level.
			lines = append(lines, line[strings.Index(line, "inventory:"):])
		}
	}
	return lines
}

func TestCommitUploadInstallSaysWhyItLeftAFileBehind(t *testing.T) {
	cases := []struct {
		name string
		// install1 and install2 are the two installs: the folder and the
		// upload's fingerprint.
		dir1, fp1, dir2, fp2 string
		want                 []string
	}{
		{"an older version of the upload, in the same folder", copyA, "upd:1/10", copyA, "upd:2/12", nil},
		{"an older version of the upload, installed into another folder", copyA, "upd:1/10", copyB, "upd:2/12", []string{
			"inventory: lib.lua is left over from an older version of moss.zip",
			"inventory: main.p8 is left over from an older version of moss.zip",
			"inventory: main.p8 is left over from an older version of moss.zip",
		}},
		{"the same upload installed again into another folder", copyA, "upd:1/10", copyB, "upd:1/10", []string{
			"inventory: lib.lua is an earlier copy of moss.zip; the new install is in /leaf/Roms/PICO8/Moss Garden 2",
			"inventory: main.p8 is an earlier copy of moss.zip; the new install is in /leaf/Roms/PICO8/Moss Garden 2",
			"inventory: main.p8 is an earlier copy of moss.zip; the new install is in /leaf/Roms/PICO8/Moss Garden 2",
		}},
		{"the same upload installed again on the other card", copyA, "upd:1/10", "/secondary/Roms/PICO8/Moss Garden/", "upd:1/10", []string{
			"inventory: lib.lua is an earlier copy of moss.zip; the new install is in /secondary/Roms/PICO8/Moss Garden",
			"inventory: main.p8 is an earlier copy of moss.zip; the new install is in /secondary/Roms/PICO8/Moss Garden",
			"inventory: main.p8 is an earlier copy of moss.zip; the new install is in /secondary/Roms/PICO8/Moss Garden",
		}},
		{"the same upload installed again, fingerprints unknown", copyA, "", copyB, "", []string{
			"inventory: lib.lua is an earlier copy of moss.zip; the new install is in /leaf/Roms/PICO8/Moss Garden 2",
			"inventory: main.p8 is an earlier copy of moss.zip; the new install is in /leaf/Roms/PICO8/Moss Garden 2",
			"inventory: main.p8 is an earlier copy of moss.zip; the new install is in /leaf/Roms/PICO8/Moss Garden 2",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureArtworkLogs(t)
			inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
			installAt(inv, tc.dir1, tc.fp1)
			installAt(inv, tc.dir2, tc.fp2)

			got := leftOverLogLines(logs)
			sortStrings(got)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("log:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// The folder an old file sits in tells a layout change from a second copy: a
// track an older version of the app put in the album folder, when the new
// install files the album's tracks in subfolders, is left over from an older
// version, not an earlier copy.
func TestCommitUploadInstallKeepsTheOlderVersionMessageForARelayout(t *testing.T) {
	logs := captureArtworkLogs(t)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	const album = "/leaf/Music/Leafbound/"
	old := p8(album+"01 Theme.ogg", "ost.zip")
	old.UploadFingerprint = "upd:1/10"
	inv.Add(causeGame, inventory.Entry{Title: "Leafbound"}, old)
	current := p8(album+"cd1/01 Theme.ogg", "ost.zip")
	current.UploadFingerprint = "upd:1/10"
	inv.Add(causeGame, inventory.Entry{Title: "Leafbound"}, current)
	inv.CommitUploadInstall(causeGame, inventory.UploadInstall{
		Filename: "ost.zip", Fingerprint: "upd:1/10", Written: []string{current.DestPath},
	})

	got := leftOverLogLines(logs)
	if want := "inventory: 01 Theme.ogg is left over from an older version of ost.zip"; len(got) != 1 || got[0] != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	entry, _ := inv.Lookup(causeGame)
	for _, file := range entry.Files {
		if file.LeftOver && entry.LeftOverIsEarlierCopy(file) {
			t.Fatalf("%s is described as an earlier copy", file.Filename)
		}
	}
}

// Manage asks the entry which of its left-over files are earlier copies.
func TestLeftOverIsEarlierCopy(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	installAt(inv, copyA, "upd:1/10")
	installAt(inv, copyB, "upd:1/10")
	entry, _ := inv.Lookup(causeGame)
	var flagged, copies int
	for _, file := range entry.Files {
		if !file.LeftOver {
			if entry.LeftOverIsEarlierCopy(file) {
				t.Fatalf("%s is current but described as an earlier copy", file.DestPath)
			}
			continue
		}
		flagged++
		if entry.LeftOverIsEarlierCopy(file) {
			copies++
		}
	}
	if flagged != 3 || copies != 3 {
		t.Fatalf("%d left-over files, %d of them earlier copies; want 3 and 3", flagged, copies)
	}

	// An update that follows the copy makes the files of that update's
	// predecessor older versions, whichever folder holds them.
	installAt(inv, copyB, "upd:2/12")
	entry, _ = inv.Lookup(causeGame)
	for _, file := range entry.Files {
		if file.LeftOver && entry.LeftOverIsEarlierCopy(file) {
			t.Fatalf("%s (version %s) is described as an earlier copy beside version upd:2/12", file.DestPath, file.UploadFingerprint)
		}
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
