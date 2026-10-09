//go:build !headless

package ui

import (
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// playlistOf returns the one .m3u in a game folder tree and its text.
func playlistOf(t *testing.T, dir string) (string, string) {
	t.Helper()
	var found []string
	for name := range treeOf(t, dir) {
		if isPlaylist(name) {
			found = append(found, name)
		}
	}
	if len(found) != 1 {
		t.Fatalf("playlists in %s = %v, want one", dir, found)
	}
	return found[0], treeOf(t, dir)[found[0]]
}

// F25: the ZIP path's playlist lists each cart by its path relative to the
// playlist, with forward slashes, as an .m3u file does. Listing bare names
// showed nested carts as main.p8 twice.
func TestPico8PlaylistListsCartsRelativeToItself(t *testing.T) {
	cart := func(name string) []byte { return []byte("pico-8 cartridge // " + name + "\n") }
	cases := []struct {
		name    string
		members map[string][]byte
		want    string
	}{
		{"carts in subfolders", map[string][]byte{
			"game/main.p8": cart("MAIN"), "game/world2/main.p8": cart("WORLD2"), "game/lib.lua": []byte("-- lib\n"),
		}, "main.p8\nworld2/main.p8\n"},
		{"carts two folders down, PNG and text carts", map[string][]byte{
			"game/main.p8": cart("MAIN"), "game/world2/zone3/boss.p8": cart("BOSS"), "game/world2/cover.p8.png": []byte("PNG CART"),
		}, "main.p8\nworld2/cover.p8.png\nworld2/zone3/boss.p8\n"},
		{"carts that sort by number", map[string][]byte{
			"level10.p8": cart("10"), "level9.p8": cart("9"), "level2.p8": cart("2"),
		}, "level2.p8\nlevel9.p8\nlevel10.p8\n"},
		{"carts in the playlist's own folder", map[string][]byte{
			"a.p8": cart("A"), "b.p8.png": []byte("PNG CART"),
		}, "a.p8\nb.p8.png\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			primary, _ := transactionPaths(t)
			inv, invPath := collisionInventory(t)
			gameDir := filepath.Join(primary, "Roms", "PICO8", "Moss Garden")
			runArchiveFor(t, primary, ownerGame, inv, invPath, "moss.zip", zipOf(t, tc.members), &settings.Config{},
				func(plan *ZIPPlan, _ string) { plan.Pico8GameDir = gameDir + string(filepath.Separator) })

			name, text := playlistOf(t, gameDir)
			if text != tc.want {
				t.Fatalf("%s = %q, want %q", name, text, tc.want)
			}
			// The playlist is recorded as before: one record, an M3U file of
			// the archive, at its path.
			entry, _ := inv.Lookup(ownerGame.URL)
			records := 0
			for _, file := range entry.Files {
				if !isPlaylist(file.DestPath) {
					continue
				}
				records++
				if file.DestPath != filepath.Join(gameDir, name) || file.Filename != name ||
					file.FileType != inventory.FileTypeM3U || file.SourceArchive != "moss.zip" {
					t.Fatalf("playlist record = %+v", file)
				}
			}
			if records != 1 {
				t.Fatalf("playlist records = %d, want 1", records)
			}
		})
	}
}
