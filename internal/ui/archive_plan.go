//go:build !headless

package ui

import (
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// ZIPPlan carries inspected archive contents and the user's source-aware
// extraction choices into the Cat-owned transfer worker.
type ZIPPlan struct {
	Upload   roms.Upload
	CDNURL   string
	Manifest roms.ZIPManifest

	DownloadROMs  bool
	DownloadMusic bool
	Pico8GameDir  string
	SelectedROMs  map[string]string
	ROMDirs       map[string]string
	MusicDir      string
}
