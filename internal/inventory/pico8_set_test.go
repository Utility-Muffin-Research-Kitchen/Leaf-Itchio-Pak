package inventory_test

import (
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// p8 is a record of a file the app wrote at path from archive.
func p8(path, archive string) inventory.DownloadedFile {
	return inventory.DownloadedFile{
		Filename: filepath.Base(path), DestPath: path, SourceArchive: archive, OriginalUpload: archive,
	}
}

// F19: the files of a Pico-8 game that came in several files are told apart
// from single carts by the records alone.
func TestInPico8Set(t *testing.T) {
	const root = "/leaf/Roms/PICO8"
	game := root + "/Moss Garden/"
	cases := []struct {
		name  string
		files []inventory.DownloadedFile
		// want maps the base name of each file to whether it is in a set.
		want map[string]bool
	}{
		{"carts, Lua and a playlist", []inventory.DownloadedFile{
			p8(game+"main.p8", "moss.zip"), p8(game+"level2.p8", "moss.zip"),
			p8(game+"lib.lua", "moss.zip"), p8(game+"Moss Garden.m3u", "moss.zip"),
		}, map[string]bool{"main.p8": true, "level2.p8": true, "lib.lua": true, "Moss Garden.m3u": true}},
		{"one cart and Lua", []inventory.DownloadedFile{
			p8(game+"main.p8", "moss.zip"), p8(game+"lib.lua", "moss.zip"),
		}, map[string]bool{"main.p8": true, "lib.lua": true}},
		{"a Lua file on its own", []inventory.DownloadedFile{p8(game+"lib.lua", "moss.zip")},
			map[string]bool{"lib.lua": true}},
		{"carts of one archive in one folder, no playlist written", []inventory.DownloadedFile{
			p8(game+"a.p8", "moss.zip"), p8(game+"b.p8.png", "moss.zip"),
		}, map[string]bool{"a.p8": true, "b.p8.png": true}},
		{"extensions in capitals", []inventory.DownloadedFile{
			p8(game+"MAIN.P8.PNG", "moss.zip"), p8(game+"LIB.LUA", "moss.zip"),
		}, map[string]bool{"MAIN.P8.PNG": true, "LIB.LUA": true}},
		{"folder named in another letter case", []inventory.DownloadedFile{
			p8(game+"main.p8", "moss.zip"), p8("/leaf/Roms/PICO8/MOSS GARDEN/lib.lua", "moss.zip"),
		}, map[string]bool{"main.p8": true, "lib.lua": true}},
		{"a cart of another upload beside the Lua files of this one", []inventory.DownloadedFile{
			p8(game+"main.p8", "moss-carts.zip"), p8(game+"lib.lua", "moss-web.zip"),
		}, map[string]bool{"main.p8": true, "lib.lua": true}},

		{"a single cart in the Pico-8 folder", []inventory.DownloadedFile{p8(root+"/cart.p8.png", "moss.zip")},
			map[string]bool{"cart.p8.png": false}},
		{"a single cart downloaded on its own", []inventory.DownloadedFile{p8(root+"/cart.p8", "")},
			map[string]bool{"cart.p8": false}},
		{"single carts of two uploads in the Pico-8 folder", []inventory.DownloadedFile{
			p8(root+"/web.p8.png", "moss-web.zip"), p8(root+"/native.p8", "moss-native.zip"),
		}, map[string]bool{"web.p8.png": false, "native.p8": false}},
		{"single carts downloaded on their own into a folder you picked", []inventory.DownloadedFile{
			p8(root+"/Platformers/jump.p8", ""), p8(root+"/Platformers/run.p8.png", ""),
		}, map[string]bool{"jump.p8": false, "run.p8.png": false}},
		{"a single cart in a folder you picked", []inventory.DownloadedFile{p8(root+"/Platformers/jump.p8", "moss.zip")},
			map[string]bool{"jump.p8": false}},
		{"carts in different folders", []inventory.DownloadedFile{
			p8(root+"/Platformers/a.p8", "moss.zip"), p8(root+"/Puzzles/b.p8", "moss.zip"),
		}, map[string]bool{"a.p8": false, "b.p8": false}},
		{"a playlist on its own", []inventory.DownloadedFile{p8(game+"Moss Garden.m3u", "moss.zip")},
			map[string]bool{"Moss Garden.m3u": false}},
		{"PlayStation playlists", []inventory.DownloadedFile{
			p8("/leaf/Roms/PSX/disc.m3u", "moss.zip"), p8("/leaf/Roms/PSX/other.m3u", "moss.zip"),
		}, map[string]bool{"disc.m3u": false, "other.m3u": false}},
		{"a Game Boy ROM beside Lua files", []inventory.DownloadedFile{
			p8("/leaf/Roms/GB/moss.gb", "moss.zip"), p8("/leaf/Roms/GB/lib.lua", "moss.zip"),
		}, map[string]bool{"moss.gb": false, "lib.lua": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := inventory.Entry{Title: "Moss Garden", Files: tc.files}
			for _, file := range tc.files {
				name := filepath.Base(file.DestPath)
				want, ok := tc.want[name]
				if !ok {
					t.Fatalf("no expectation for %s", name)
				}
				if got := entry.InPico8Set(file); got != want {
					t.Errorf("InPico8Set(%s) = %v, want %v", file.DestPath, got, want)
				}
			}
		})
	}
}
