//go:build !headless

package ui

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
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

func (plan ZIPPlan) Seal() ZIPPlan {
	sealed := plan
	sealed.Manifest.Entries = append([]roms.ZIPEntry(nil), plan.Manifest.Entries...)
	sealed.SelectedROMs = cloneStringMap(plan.SelectedROMs)
	sealed.ROMDirs = cloneStringMap(plan.ROMDirs)
	return sealed
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func (plan ZIPPlan) shouldExtractROM(name string) bool {
	if !plan.DownloadROMs {
		return false
	}
	if len(plan.SelectedROMs) == 0 {
		return true
	}
	ext := strings.ToLower(roms.ROMExt(name))
	chosen, ok := plan.SelectedROMs[ext]
	if !ok {
		return true
	}
	return chosen == name || chosen == filepath.Base(name)
}

func (plan ZIPPlan) extractionRequirements(cfg *settings.Config, manifest roms.ZIPManifest) (map[string]int64, error) {
	required := make(map[string]int64)
	for _, entry := range manifest.Entries {
		dir := ""
		if plan.Pico8GameDir != "" {
			ext := strings.ToLower(roms.ROMExt(entry.Name))
			if ext == ".p8" || ext == ".p8.png" || strings.HasSuffix(strings.ToLower(entry.Name), ".lua") {
				dir = plan.Pico8GameDir
			}
		} else if (entry.Kind == roms.KindROM || entry.Kind == roms.KindROMSupport) && plan.shouldExtractROM(entry.Name) {
			ext := strings.ToLower(roms.ROMExt(entry.Name))
			dir = plan.ROMDirs[ext]
			if dir == "" {
				dir = roms.DestinationDir(ext)
			}
		} else if entry.Kind == roms.KindMusic && plan.DownloadMusic {
			dir = plan.MusicDir
		}
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if entry.Size > math.MaxInt64 || required[dir] > math.MaxInt64-int64(entry.Size) {
			return nil, fmt.Errorf("archive extraction size overflows for %s", filepath.Base(filepath.Clean(dir)))
		}
		required[dir] += int64(entry.Size)
	}
	if len(required) == 0 {
		return nil, fmt.Errorf("archive transaction has no selected output files")
	}
	return required, nil
}

func (plan ZIPPlan) preflight(cfg *settings.Config, manifest roms.ZIPManifest) (string, error) {
	if err := ValidateArchiveManifest(manifest, DefaultArchiveLimits); err != nil {
		return "", err
	}
	required, err := plan.extractionRequirements(cfg, manifest)
	if err != nil {
		return "", err
	}
	dirs := make([]string, 0, len(required))
	for dir := range required {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	requiredBySource := make(map[string]int64)
	representativeDir := make(map[string]string)
	for _, dir := range dirs {
		if err := validateArchiveDirectory(dir); err != nil {
			return "", err
		}
		identity, ok := roms.DescribeDestination(filepath.Join(dir, ".itchio-preflight"))
		if !ok || identity.SourceID == "" {
			return "", fmt.Errorf("archive destination source changed during preflight")
		}
		if requiredBySource[identity.SourceID] > math.MaxInt64-required[dir] {
			return "", fmt.Errorf("archive extraction size overflows for source %s", identity.SourceID)
		}
		requiredBySource[identity.SourceID] += required[dir]
		if representativeDir[identity.SourceID] == "" {
			representativeDir[identity.SourceID] = dir
		}
	}
	for sourceID, bytes := range requiredBySource {
		if err := leaf.RequireFreeSpace(representativeDir[sourceID], bytes); err != nil {
			return "", fmt.Errorf("archive storage preflight for source %s: %w", sourceID, err)
		}
	}
	return dirs[0], nil
}
