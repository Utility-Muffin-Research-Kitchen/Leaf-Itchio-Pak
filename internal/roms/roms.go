package roms

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// ROMExt returns the effective ROM extension for filename.
// For Pico-8 cartridges with the compound extension .p8.png it returns ".p8.png"
// rather than the ".png" that filepath.Ext would return.
func ROMExt(filename string) string {
	if strings.HasSuffix(strings.ToLower(filename), ".p8.png") {
		return ".p8.png"
	}
	return filepath.Ext(filename)
}

// TrimROMExt returns filename without its ROM extension, keeping the case of
// the rest of the name. ROMExt reports ".p8.png" in lower case whatever the
// case of the name, so strings.TrimSuffix(name, ROMExt(name)) leaves
// "GAME.P8.PNG" whole; this cuts by length, which is right for any case.
func TrimROMExt(filename string) string {
	ext := ROMExt(filename)
	if cut := len(filename) - len(ext); cut >= 0 && strings.EqualFold(filename[cut:], ext) {
		return filename[:cut]
	}
	return filename
}

type Upload struct {
	UploadFingerprint string // metadata captured when this version was selected
	Filename          string
	URL               string // web resolver URL; empty for API uploads
	UploadID          string // itch.io upload ID; recorded with each installed file
	NeedsFormat       bool   // true if the user must choose a supported format
	// DesktopOrWeb marks a build for a computer, phone or browser, from the
	// API listing's upload type and traits. Such a file is never chosen
	// automatically and is listed last.
	DesktopOrWeb bool
	// Install is set for uploads listed through the itch.io API and is the
	// only test for an API download: free API downloads have no purchase ID.
	// nil means the anonymous web flow.
	Install *InstallSession
	// Listing is the list this upload was chosen from, shared by every
	// upload of that list; nil when the list is unknown. The install uses it
	// to tell an update, whose old upload the list no longer offers, from
	// another build the list still offers, and seeds update checks with it.
	Listing *UploadListing
}

// UploadListing records what one upload list offered.
type UploadListing struct {
	API     bool // listed by api.itch.io; false for the web download page
	Uploads []ListedUpload
}

// ListedUpload is one entry of an UploadListing.
type ListedUpload struct {
	Filename, DisplayName, UploadID, Fingerprint string
	// DesktopOrWebOnly marks an upload that only ships desktop or web
	// builds; a new one never raises an update badge.
	DesktopOrWebOnly bool
	// Soundtrack marks an upload itch.io lists as a soundtrack.
	Soundtrack bool
}

// ViaAPI reports whether the upload downloads through the itch.io API.
func (upload Upload) ViaAPI() bool { return upload.Install != nil }

var psxLaunchExts = map[string]bool{
	".cbn": true, ".chd": true, ".cue": true, ".img": true, ".iso": true,
	".mdf": true, ".pbp": true, ".toc": true, ".m3u": true,
}

func IsPSXLaunchExt(ext string) bool { return psxLaunchExts[strings.ToLower(ext)] }

func IsPSXSupportExt(ext string) bool { return strings.EqualFold(ext, ".bin") }

func IsPSXExt(ext string) bool { return IsPSXLaunchExt(ext) || IsPSXSupportExt(ext) }

// IsSupportedUploadExt reports whether a filename can be routed without a
// manual format choice. BIN is accepted as PSX companion data; a bin-only
// install remains harmless because Leaf indexes descriptors/images, not BIN.
func IsSupportedUploadExt(ext string) bool {
	ext = strings.ToLower(ext)
	switch ext {
	case ".gb", ".gbc", ".gba", ".nes", ".md", ".gen", ".smd",
		".p8", ".p8.png", ".zip", ".7z":
		return true
	default:
		return IsPSXExt(ext)
	}
}

func ScoreUpload(filename string) int {
	switch strings.ToLower(ROMExt(filename)) {
	case ".gbc", ".p8.png":
		return 2
	case ".gb", ".gba", ".nes", ".md", ".gen", ".smd", ".p8",
		".cbn", ".chd", ".cue", ".img", ".iso", ".mdf", ".pbp", ".toc", ".m3u":
		return 1
	default:
		return 0
	}
}

type PathConfig struct {
	SystemDirs  map[string]string
	ImageDirs   map[string]string
	SourceID    string
	PrimaryRoot string
	MusicRoot   string
	StatesRoot  string
	Sources     []SourcePathConfig
}

type SourcePathConfig struct {
	SourceID, Root, MusicRoot, StatesRoot string
	SystemDirs, ImageDirs                 map[string]string
}

var pathConfig atomic.Pointer[PathConfig]

func withTrailingSlash(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Clean(path) + string(filepath.Separator)
}

// ConfigurePaths installs the source-local paths resolved from Leaf's runtime
// environment and canonical systems catalog. It must run before UI workers.
func ConfigurePaths(config PathConfig) error {
	required := []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"}
	copyConfig := &PathConfig{
		SystemDirs: make(map[string]string, len(config.SystemDirs)),
		ImageDirs:  make(map[string]string, len(config.ImageDirs)),
	}
	for _, id := range required {
		path := config.SystemDirs[id]
		if path == "" {
			return fmt.Errorf("missing Leaf destination for system %s", id)
		}
		copyConfig.SystemDirs[id] = withTrailingSlash(path)
		imagePath := config.ImageDirs[id]
		if imagePath == "" {
			return fmt.Errorf("missing Leaf image destination for system %s", id)
		}
		copyConfig.ImageDirs[id] = withTrailingSlash(imagePath)
	}
	if config.MusicRoot == "" {
		return fmt.Errorf("missing Leaf music root")
	}
	if config.PrimaryRoot == "" {
		return fmt.Errorf("missing Leaf primary root")
	}
	if config.SourceID == "" {
		return fmt.Errorf("missing Leaf source id")
	}
	copyConfig.SourceID = config.SourceID
	if config.StatesRoot == "" {
		return fmt.Errorf("missing Leaf states root")
	}
	copyConfig.PrimaryRoot = withTrailingSlash(config.PrimaryRoot)
	copyConfig.MusicRoot = withTrailingSlash(config.MusicRoot)
	copyConfig.StatesRoot = withTrailingSlash(config.StatesRoot)
	if len(config.Sources) == 0 {
		config.Sources = []SourcePathConfig{{
			SourceID: config.SourceID, Root: config.PrimaryRoot, MusicRoot: config.MusicRoot,
			StatesRoot: config.StatesRoot, SystemDirs: config.SystemDirs, ImageDirs: config.ImageDirs,
		}}
	}
	seenSources := make(map[string]bool, len(config.Sources))
	for _, source := range config.Sources {
		if source.SourceID == "" || source.Root == "" || seenSources[source.SourceID] {
			return fmt.Errorf("invalid Leaf source path configuration %q", source.SourceID)
		}
		seenSources[source.SourceID] = true
		copySource := SourcePathConfig{
			SourceID: source.SourceID, Root: withTrailingSlash(source.Root),
			MusicRoot: withTrailingSlash(source.MusicRoot), StatesRoot: withTrailingSlash(source.StatesRoot),
			SystemDirs: make(map[string]string, len(source.SystemDirs)),
			ImageDirs:  make(map[string]string, len(source.ImageDirs)),
		}
		for _, id := range required {
			if source.SystemDirs[id] == "" {
				return fmt.Errorf("missing Leaf destination for source %s system %s", source.SourceID, id)
			}
			copySource.SystemDirs[id] = withTrailingSlash(source.SystemDirs[id])
			if source.ImageDirs[id] == "" {
				return fmt.Errorf("missing Leaf image destination for source %s system %s", source.SourceID, id)
			}
			copySource.ImageDirs[id] = withTrailingSlash(source.ImageDirs[id])
		}
		copyConfig.Sources = append(copyConfig.Sources, copySource)
	}
	pathConfig.Store(copyConfig)
	return nil
}

type PathIdentity struct {
	SourceID        string
	RelativePath    string
	CanonicalSystem string
}

// DescribeDestination converts a configured content-source path to stable
// inventory identity. Paths outside every source are rejected without guessing.
func DescribeDestination(path string) (PathIdentity, bool) {
	config := pathConfig.Load()
	if config == nil || path == "" {
		return PathIdentity{}, false
	}
	cleanPath := filepath.Clean(path)
	for _, source := range config.Sources {
		rel, err := filepath.Rel(filepath.Clean(source.Root), cleanPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		identity := PathIdentity{SourceID: source.SourceID, RelativePath: filepath.ToSlash(rel)}
		for id, dir := range source.SystemDirs {
			systemRel, systemErr := filepath.Rel(filepath.Clean(dir), cleanPath)
			if systemErr == nil && systemRel != ".." && !strings.HasPrefix(systemRel, ".."+string(filepath.Separator)) {
				identity.CanonicalSystem = id
				break
			}
		}
		return identity, true
	}
	return PathIdentity{}, false
}

func PrimaryRoot() string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	return config.PrimaryRoot
}

func SourceRoot(sourceID string) string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	for _, source := range config.Sources {
		if source.SourceID == sourceID {
			return source.Root
		}
	}
	return ""
}

func SourceSystemDir(sourceID, systemID string) string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	for _, source := range config.Sources {
		if source.SourceID == sourceID {
			return source.SystemDirs[systemID]
		}
	}
	return ""
}

// SourceImageDir returns the canonical Jawaka image directory for one system
// on one configured Leaf source.
func SourceImageDir(sourceID, systemID string) string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	for _, source := range config.Sources {
		if source.SourceID == sourceID {
			return source.ImageDirs[systemID]
		}
	}
	return ""
}

// ArtworkPath resolves Jawaka's source-local Images/<system>/<ROM stem>.png
// path from a configured ROM destination. Paths outside canonical systems are
// rejected instead of guessed.
func ArtworkPath(romDestPath string) string {
	identity, ok := DescribeDestination(romDestPath)
	if !ok || identity.CanonicalSystem == "" {
		return ""
	}
	dir := SourceImageDir(identity.SourceID, identity.CanonicalSystem)
	if dir == "" {
		return ""
	}
	base := TrimROMExt(filepath.Base(romDestPath))
	return filepath.Join(dir, base+".png")
}

func SourceMusicRoot(sourceID string) string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	for _, source := range config.Sources {
		if source.SourceID == sourceID {
			return source.MusicRoot
		}
	}
	return ""
}

func MusicRoot() string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	return config.MusicRoot
}

func StatesRoot() string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	return config.StatesRoot
}

func SystemDir(id string) string {
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	return config.SystemDirs[id]
}

// Pico8ROMDir returns Leaf's single canonical PICO8 directory.
func Pico8ROMDir() string {
	return SystemDir("PICO8")
}

func DestinationDir(ext string) string {
	switch strings.ToLower(ext) {
	case ".gbc":
		return SystemDir("GBC")
	case ".gb":
		return SystemDir("GB")
	case ".gba":
		return SystemDir("GBA")
	case ".nes":
		return SystemDir("FC")
	case ".md", ".gen", ".smd":
		return SystemDir("MD")
	case ".p8", ".p8.png":
		return SystemDir("PICO8")
	case ".cbn", ".chd", ".cue", ".img", ".iso", ".mdf", ".pbp", ".toc", ".m3u", ".bin":
		return SystemDir("PS")
	case ".zip":
		return SystemDir("GBC")
	default:
		return ""
	}
}

// SupportsUnifiedNaming reports whether a ROM can be renamed without
// invalidating references inside a descriptor or playlist. PSX descriptor,
// playlist, companion, and raw-image formats conservatively retain their
// upload names; self-contained CHD and PBP images are safe to rename.
func SupportsUnifiedNaming(filename string) bool {
	switch strings.ToLower(ROMExt(filename)) {
	case ".cue", ".toc", ".m3u", ".bin", ".mdf", ".img", ".iso", ".cbn":
		return false
	default:
		return true
	}
}

func SelectBest(uploads []Upload) *Upload {
	var best *Upload
	bestScore := 0
	for i := range uploads {
		s := ScoreUpload(uploads[i].Filename)
		if s > bestScore {
			bestScore = s
			best = &uploads[i]
		}
	}
	return best
}

// MusicDestinationDir returns the target directory for a game's music files.
func MusicDestinationDir(gameTitle string) string {
	safe := SanitiseFilename(gameTitle, "")
	if safe == "" {
		safe = "Unknown"
	}
	config := pathConfig.Load()
	if config == nil {
		return ""
	}
	return config.MusicRoot + safe + "/"
}

// Pico8GameSubDir returns the subdirectory for a Pico-8 game that ships with
// multiple files (.p8/.p8.png/.lua). All game files are extracted here.
func Pico8GameSubDir(gameTitle string) string {
	safe := SanitiseFilename(gameTitle, "")
	if safe == "" {
		safe = "Unknown"
	}
	return Pico8ROMDir() + safe + "/"
}
