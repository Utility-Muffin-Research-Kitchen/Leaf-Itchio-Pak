package inventory_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// A left-over mark says a later install of the upload replaced the file. When
// that replacement is deleted, the file is the only copy left, and nothing may
// go on offering it for deletion.

func leftOverPaths(t *testing.T, inv *inventory.Inventory) []string {
	t.Helper()
	entry, ok := inv.Lookup(causeGame)
	if !ok {
		t.Fatal("no entry for the game")
	}
	var paths []string
	for _, file := range entry.Files {
		if file.LeftOver {
			paths = append(paths, file.DestPath)
		}
	}
	return paths
}

// The same upload installed into a second folder, then the second copy
// deleted: the first copy is no longer left over once nothing replaces it.
func TestRemovingTheReplacementClearsTheLeftOverMark(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	installAt(inv, copyA, "upd:1/10")
	secondCopy := installAt(inv, copyB, "upd:1/10")
	if got := len(leftOverPaths(t, inv)); got != 3 {
		t.Fatalf("%d files left over after the second install, want the first copy's 3", got)
	}

	// Deleting one file of the second copy frees only the file of the same
	// archive member: the rest of the first copy still has a replacement.
	for _, path := range secondCopy {
		if filepath.Base(path) == "main.p8" && filepath.Base(filepath.Dir(path)) == "Moss Garden 2" {
			inv.RemoveFile(causeGame, path)
		}
	}
	left := leftOverPaths(t, inv)
	if len(left) != 2 {
		t.Fatalf("left over after deleting the second copy's main.p8 = %v, want the other two files of the first copy", left)
	}
	for _, path := range left {
		if path == copyA+"main.p8" {
			t.Fatalf("%s is still left over; its only replacement was deleted", path)
		}
	}

	for _, path := range secondCopy {
		inv.RemoveFile(causeGame, path)
	}
	if left := leftOverPaths(t, inv); len(left) != 0 {
		t.Fatalf("left over after deleting the whole second copy = %v, want none", left)
	}
	entry, _ := inv.Lookup(causeGame)
	if len(entry.Files) != 3 {
		t.Fatalf("records = %v, want the first copy's 3 files", recordedPaths(t, inv))
	}
}

// Copies of an upload that are all left over (each superseded by a later one)
// become plain files together once the newest goes.
func TestRemovingTheNewestCopyClearsEveryOlderCopy(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	installAt(inv, copyA, "upd:1/10")
	installAt(inv, copyB, "upd:1/10")
	third := installAt(inv, "/secondary/Roms/PICO8/Moss Garden/", "upd:1/10")
	if got := len(leftOverPaths(t, inv)); got != 6 {
		t.Fatalf("%d files left over, want the first two copies' 6", got)
	}
	for _, path := range third {
		inv.RemoveFile(causeGame, path)
	}
	if left := leftOverPaths(t, inv); len(left) != 0 {
		t.Fatalf("left over after deleting the newest copy = %v, want none", left)
	}
}

// The check at launch removes the records of files that are gone from the
// card, which is how a copy deleted outside the app goes. It frees the first
// copy the same way.
func TestVerifyAndCleanClearsTheLeftOverMarkOfALostReplacement(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "Moss Garden")+"/", filepath.Join(dir, "Moss Garden 2")+"/"
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	written := append(installAt(inv, first, "upd:1/10"), installAt(inv, second, "upd:1/10")...)
	for _, path := range written {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("cart"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(leftOverPaths(t, inv)); got != 3 {
		t.Fatalf("%d files left over, want the first copy's 3", got)
	}
	if removed := inv.VerifyAndClean(filepath.Join(dir, "inventory.json")); removed != 0 {
		t.Fatalf("removed %d records of files that exist", removed)
	}
	if got := len(leftOverPaths(t, inv)); got != 3 {
		t.Fatalf("%d files left over after a check that found nothing missing, want 3", got)
	}

	if err := os.RemoveAll(filepath.Join(dir, "Moss Garden 2")); err != nil {
		t.Fatal(err)
	}
	if removed := inv.VerifyAndClean(filepath.Join(dir, "inventory.json")); removed != 3 {
		t.Fatalf("removed %d records, want the second copy's 3", removed)
	}
	if left := leftOverPaths(t, inv); len(left) != 0 {
		t.Fatalf("left over after the second copy vanished = %v, want none", left)
	}
}

// The ordinary case does not change: while the file that replaced an older one
// is there, the older one stays left over, and deleting other files, another
// upload's files or a left-over file itself does not touch the marks.
func TestLeftOverMarksStayWhileTheirReplacementExists(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	installAt(inv, copyA, "upd:1/10")
	installAt(inv, copyB, "upd:2/12") // an update of the same upload, into another folder
	want := len(leftOverPaths(t, inv))
	if want != 3 {
		t.Fatalf("%d files left over after the update, want the first version's 3", want)
	}

	// A file of another upload of the game, current, and then deleted.
	other := member("/leaf/Roms/GB/other.gb", "other.zip", "other.gb", time.Now(), "upd:1/5")
	inv.Add(causeGame, inventory.Entry{Title: "Moss Garden"}, other)
	inv.RemoveFile(causeGame, other.DestPath)
	// A left-over file deleted by itself (Delete left-over files).
	inv.RemoveFile(causeGame, copyA+"lib.lua")

	left := leftOverPaths(t, inv)
	if len(left) != 2 {
		t.Fatalf("left over = %v, want the first version's two files that remain", left)
	}
	entry, _ := inv.Lookup(causeGame)
	for _, file := range entry.Files {
		if file.DestPath != copyA+"main.p8" && file.DestPath != copyA+"world2/main.p8" {
			if file.LeftOver {
				t.Fatalf("%s is flagged left over", file.DestPath)
			}
		}
	}
}
