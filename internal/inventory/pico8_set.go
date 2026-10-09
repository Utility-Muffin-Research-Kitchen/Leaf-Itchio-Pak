package inventory

import (
	"path/filepath"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// pico8Role is what a file is to a Pico-8 game.
type pico8Role int

const (
	pico8None     pico8Role = iota // not a file of a Pico-8 game
	pico8Cart                      // .p8 or .p8.png
	pico8Lua                       // a .lua file a cart includes
	pico8Playlist                  // the .m3u that lists a game's carts
)

func pico8RoleOf(path string) pico8Role {
	switch strings.ToLower(roms.ROMExt(filepath.Base(path))) {
	case ".p8", ".p8.png":
		return pico8Cart
	case ".lua":
		return pico8Lua
	case ".m3u":
		return pico8Playlist
	}
	return pico8None
}

// InPico8Set reports whether file belongs to a Pico-8 game that was installed
// as a set of files: carts, Lua files and the playlist the app writes for
// several carts. The set refers to itself by file name. The playlist lists
// the carts, a cart includes Lua files and loads other carts by name, so
// renaming any of them can stop the game from loading, and nothing the app
// records says which names are referred to.
//
// The records alone decide it, so installs from before this check count
// too. A file is in a set when it is
//
//   - a .lua file: nothing installs one except a set;
//   - a cart or the playlist with a .lua file or the playlist of the same
//     game in the same folder; or
//   - a cart with another cart of the same game in the same folder that came
//     from the same archive. Only a set puts two carts of one archive in one
//     folder. This catches a set whose playlist was not written because
//     another game's file was in the way.
//
// Folder depth is not used. A multi-file install does go into a folder named
// after the game, but a single cart can sit in a folder too, one you picked
// as its destination inside the Pico-8 folder, and that cart can be renamed
// safely. A single cart has no sibling of the kinds above: two single-cart
// uploads of one game share a folder but not an archive, and a cart you
// downloaded on its own has no archive at all.
func (e Entry) InPico8Set(file DownloadedFile) bool {
	role := pico8RoleOf(file.DestPath)
	if role == pico8None {
		return false
	}
	if role == pico8Lua {
		return true
	}
	dir := filepath.Dir(file.DestPath)
	for _, other := range e.Files {
		// FAT32 ignores letter case.
		if other.DestPath == file.DestPath || !strings.EqualFold(filepath.Dir(other.DestPath), dir) {
			continue
		}
		switch otherRole := pico8RoleOf(other.DestPath); {
		case otherRole == pico8None:
		case role == pico8Playlist:
			// A playlist with another playlist beside it is a PlayStation
			// game's; it is the set's when a cart or Lua file is there.
			if otherRole != pico8Playlist {
				return true
			}
		case otherRole != pico8Cart:
			return true
		case file.SourceArchive != "" && other.SourceArchive == file.SourceArchive:
			return true
		}
	}
	return false
}
