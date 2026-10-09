//go:build !headless

package ui

import (
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func ensureROMArtwork(client *itchio.Client, inv *inventory.Inventory, game itchio.Game, romPath string) itchio.ArtworkResult {
	return ensureROMArtworkFrom(client.NewCoverFetch(game.CoverURL), inv, game, romPath)
}

// ensureROMArtworkFrom is ensureROMArtwork for one of several ROMs of a game
// that share a CoverFetch, so the cover is downloaded once for all of them.
func ensureROMArtworkFrom(cover *itchio.CoverFetch, inv *inventory.Inventory, game itchio.Game, romPath string) itchio.ArtworkResult {
	if roms.IsPSXSupportExt(roms.ROMExt(romPath)) {
		return itchio.ArtworkResult{}
	}
	var (
		result itchio.ArtworkResult
		err    error
	)
	if strings.EqualFold(roms.ROMExt(romPath), ".p8.png") {
		result, err = itchio.EnsureCopiedCoverArt(romPath)
	} else {
		result, err = cover.EnsureCoverArt(romPath)
	}
	if err != nil {
		logger.Warn("cover-art: game=%q: %v", game.Title, err)
		return itchio.ArtworkResult{}
	}
	if !result.Created && result.Path != "" {
		var files []inventory.DownloadedFile
		if inv != nil {
			if entry, ok := inv.Lookup(game.URL); ok {
				files = entry.Files
			}
		}
		result = inventory.KeepExistingArtwork(result, files)
	}
	return result
}

func applyArtwork(file *inventory.DownloadedFile, artwork itchio.ArtworkResult) {
	if file == nil || artwork.Path == "" {
		return
	}
	file.ArtworkPath = artwork.Path
	file.ArtworkHash = artwork.SHA256
	file.ArtworkCreated = artwork.Created
}
