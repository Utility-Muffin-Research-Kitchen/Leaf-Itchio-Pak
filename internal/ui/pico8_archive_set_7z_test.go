//go:build !headless

package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// F22: a set from a 7z archive installs the carts and Lua files a ZIP does,
// recorded the same way, and the records alone tell it is a set. The ZIP path
// also writes a playlist; see the comment on extractPico8 for why the 7z
// path does not.
func TestPico8SetFromA7zInstallsTheSameFilesAsFromAZip(t *testing.T) {
	type installed struct {
		files   map[string]string
		records map[string]string // file path under the game folder -> member
		inSet   map[string]bool   // file path under the game folder -> InPico8Set
	}
	run := func(name string, data []byte) installed {
		primary, _ := transactionPaths(t)
		inv, invPath := collisionInventory(t)
		gameDir := filepath.Join(primary, "Roms", "PICO8", "Moss Garden")
		runArchiveFor(t, primary, ownerGame, inv, invPath, name, data, &settings.Config{},
			func(plan *ZIPPlan, _ string) { plan.Pico8GameDir = gameDir + string(filepath.Separator) })
		got := installed{files: map[string]string{}, records: map[string]string{}, inSet: map[string]bool{}}
		for path, content := range treeOf(t, gameDir) {
			if !isPlaylist(path) {
				got.files[path] = content
			}
		}
		entry, _ := inv.Lookup(ownerGame.URL)
		for _, file := range entry.Files {
			rel, _ := filepath.Rel(gameDir, file.DestPath)
			if !isPlaylist(rel) {
				got.records[filepath.ToSlash(rel)] = file.SourceMember + "|" + file.SourceArchive
				got.inSet[filepath.ToSlash(rel)] = entry.InPico8Set(file)
			}
		}
		return got
	}
	archives := pico8SetArchives(t)
	zipSet, sevenSet := run("moss.zip", archives["moss.zip"]), run("moss.7z", archives["moss.7z"])
	if fmt.Sprint(zipSet.files) != fmt.Sprint(sevenSet.files) {
		t.Fatalf("7z installed %v, ZIP installed %v", sevenSet.files, zipSet.files)
	}
	if len(sevenSet.files) != 3 || len(sevenSet.records) != 3 {
		t.Fatalf("7z set = %v, records %v, want three files with a record each", sevenSet.files, sevenSet.records)
	}
	for path, member := range zipSet.records {
		// Only the archive's name differs.
		if want := strings.Replace(member, "moss.zip", "moss.7z", 1); sevenSet.records[path] != want {
			t.Fatalf("%s is recorded as %q from the 7z archive, want %q", path, sevenSet.records[path], want)
		}
	}
	// The records tell the 7z set from single carts as they do the ZIP's,
	// although the 7z set has no playlist: a Lua file, and the cart that has
	// the Lua file beside it, are files of a set.
	if fmt.Sprint(zipSet.inSet) != fmt.Sprint(sevenSet.inSet) {
		t.Fatalf("InPico8Set for the 7z set = %v, for the ZIP set %v", sevenSet.inSet, zipSet.inSet)
	}
	for _, path := range []string{"lib.lua", "main.p8"} {
		if !sevenSet.inSet[path] {
			t.Fatalf("%s of the 7z set is not recognised as a file of a Pico-8 set", path)
		}
	}
}
