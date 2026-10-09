//go:build !headless

package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// pico8Set7z is the 7z form of pico8SetEntries, written by libarchive
// (bsdtar 3.7.4), since Go has no 7z writer:
//
//	bsdtar --format 7zip -cf set.7z game/main.p8 game/world2/main.p8 game/lib.lua
const pico8Set7z = "N3q8ryccAAMAhzOmzwAAAAAAAAAiAAAAAAAAAK59fIoAOBpIjiRWEBs71esuHjUZ+x1v86lyn5RdxaoqPdZcpZbFz1fzMLW+bUcuOSKFOLT//yp0AAAAAIEzB64P0VujJKCQoHew/pOH4aofJ2otT4VUNUXMokOg+mAdY33wrYiJ3ecn6hGE/SRuLcf/QT63RIzofedO5Fbgbnlh4HyqPiVvp22I6U0uoLhaZ2YE6zS7yHmNKSOVXLR3ARVtRy0K0h8sJ+jO73KA7zRNCashhBbGaFriUVc6XpXswQ6NimHWtswrl6PUyJDT/vU26wAXBjYBCYCZAAcLAQABIwMBAQVdAACAAAyA9woBl5RHdwAA"

// pico8SetEntries is a multi-file Pico-8 game with two carts that share a
// name in different folders, and a Lua file.
var pico8SetEntries = map[string]string{
	"game/main.p8":        "pico-8 cartridge // MAIN\n",
	"game/world2/main.p8": "pico-8 cartridge // WORLD2\n",
	"game/lib.lua":        "-- lib\n",
}

func pico8SetArchives(t *testing.T) map[string][]byte {
	t.Helper()
	entries := make(map[string][]byte, len(pico8SetEntries))
	for name, content := range pico8SetEntries {
		entries[name] = []byte(content)
	}
	return map[string][]byte{"moss.zip": zipOf(t, entries), "moss.7z": decode7z(t, pico8Set7z)}
}

// treeOf lists the files under dir by slash-separated path, with contents.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func isPlaylist(path string) bool { return strings.EqualFold(filepath.Ext(path), ".m3u") }

// F23: two carts of one archive that share a name each keep a record, for a
// ZIP and a 7z archive, and a reinstall or the next launch's inventory check
// leaves one record per file.
func TestPico8ArchiveKeepsARecordForEachSameNamedCart(t *testing.T) {
	for name, data := range pico8SetArchives(t) {
		primary, _ := transactionPaths(t)
		inv, invPath := collisionInventory(t)
		gameDir := filepath.Join(primary, "Roms", "PICO8", "Moss Garden")
		inGameDir := func(plan *ZIPPlan, _ string) { plan.Pico8GameDir = gameDir + string(filepath.Separator) }

		var perInstall int
		for install := 1; install <= 2; install++ {
			worker := runArchiveFor(t, primary, ownerGame, inv, invPath, name, data, &settings.Config{}, inGameDir)
			if len(worker.skipped) != 0 {
				t.Fatalf("%s install %d skipped %v", name, install, worker.skipped)
			}
			tree := treeOf(t, gameDir)
			for member, content := range map[string]string{"main.p8": pico8SetEntries["game/main.p8"],
				"world2/main.p8": pico8SetEntries["game/world2/main.p8"], "lib.lua": pico8SetEntries["game/lib.lua"]} {
				if tree[member] != content {
					t.Fatalf("%s install %d: %s = %q, want %q", name, install, member, tree[member], content)
				}
			}
			// The ZIP path also writes a playlist; the records count one per file.
			entry, ok := inv.Lookup(ownerGame.URL)
			if !ok || len(entry.Files) != len(tree) {
				t.Fatalf("%s install %d: %d records for %d files: %v", name, install, len(entry.Files), len(tree), entryPaths(t, inv, ownerGame.URL))
			}
			if install == 1 {
				perInstall = len(entry.Files)
			}
			if len(entry.Files) != perInstall {
				t.Fatalf("%s: %d records after the reinstall, %d after the first install", name, len(entry.Files), perInstall)
			}
			seen := map[string]bool{}
			for _, file := range entry.Files {
				key := strings.ToLower(file.DestPath)
				if seen[key] {
					t.Fatalf("%s install %d: two records for %s", name, install, file.DestPath)
				}
				seen[key] = true
				if file.LeftOver {
					t.Fatalf("%s install %d: %s is flagged left over", name, install, file.DestPath)
				}
			}
		}

		inv.VerifyAndClean(invPath)
		after, _ := inv.Lookup(ownerGame.URL)
		if len(after.Files) != perInstall {
			t.Fatalf("%s: the inventory check left %d records, want %d: %v", name, len(after.Files), perInstall, entryPaths(t, inv, ownerGame.URL))
		}
	}
}
