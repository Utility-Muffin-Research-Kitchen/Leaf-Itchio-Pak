//go:build !headless

package ui

import (
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func ensureROMArtwork(client *itchio.Client, inv *inventory.Inventory, game itchio.Game, romPath string) itchio.ArtworkResult {
	if roms.IsPSXSupportExt(roms.ROMExt(romPath)) {
		return itchio.ArtworkResult{}
	}
	var (
		result itchio.ArtworkResult
		err    error
	)
	if roms.ROMExt(romPath) == ".p8.png" {
		result, err = itchio.EnsureCopiedCoverArt(romPath)
	} else {
		result, err = client.EnsureCoverArt(game.CoverURL, romPath)
	}
	if err != nil {
		logger.Warn("cover-art: game=%q: %v", game.Title, err)
		return itchio.ArtworkResult{}
	}
	if !result.Created && result.Path != "" && inv != nil {
		if entry, ok := inv.Lookup(game.URL); ok {
			for _, file := range entry.Files {
				if file.ArtworkCreated && file.ArtworkPath == result.Path &&
					(file.ArtworkHash == "" || file.ArtworkHash == result.SHA256) {
					result.Created = true
					break
				}
			}
		}
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
