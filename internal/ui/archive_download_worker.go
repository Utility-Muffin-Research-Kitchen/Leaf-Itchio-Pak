//go:build !headless

package ui

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bodgit/sevenzip"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type zipDLState int32

const (
	zipDLDownloading zipDLState = iota
	zipDLExtracting
	zipDLDone
	zipDLError
)

// ArchiveDownloadWorker downloads a ZIP to a temp path, extracts ROM and music files
// to their respective destinations, and records all extracted files in inventory.
type ArchiveDownloadWorker struct {
	client  *itchio.Client
	cfg     *settings.Config
	game    itchio.Game
	detail  *itchio.GameDetail
	plan    ZIPPlan
	inv     *inventory.Inventory
	invPath string

	state      zipDLState
	downloaded int64
	total      int64
	extracted  []string
	skipped    []string
	// names holds every path this extraction has written, so a later entry
	// (possibly re-classified by magic bytes) never replaces an earlier one.
	names *roms.NameReservations
	// keepNames holds the ROM destinations whose unified names would meet
	// another file of this archive; they keep their original names.
	keepNames *roms.NameReservations
	// romPaths maps an archive entry to where it is extracted, chosen by
	// planROMNames so no entry lands on another game's file.
	romPaths map[string]string
	// cueTracks holds the lower-case names of the files the archive's
	// chosen .cue sheets reference; nil when the archive installs none.
	cueTracks map[string]bool
	// musicNames maps an archive entry path to its slash-separated path
	// inside the game's Music folder, set by planMusicNames so same-named
	// tracks from different folders both survive.
	musicNames     map[string]string
	musicFailed    bool
	err            error
	inhibitBlocked atomic.Bool
}

func (s *ArchiveDownloadWorker) loadState() zipDLState {
	return zipDLState(atomic.LoadInt32((*int32)(&s.state)))
}
func (s *ArchiveDownloadWorker) storeState(st zipDLState) {
	atomic.StoreInt32((*int32)(&s.state), int32(st))
}

func NewArchiveDownloadWorker(
	client *itchio.Client, cfg *settings.Config,
	game itchio.Game, detail *itchio.GameDetail, plan ZIPPlan,
	inv *inventory.Inventory, invPath string,
) *ArchiveDownloadWorker {
	s := &ArchiveDownloadWorker{
		client: client, cfg: cfg,
		game: game, detail: detail, plan: plan.Seal(),
		inv: inv, invPath: invPath,
	}
	go s.run(false)
	return s
}

func (s *ArchiveDownloadWorker) run(allowUninhibited bool) {
	lease, guardErr := leaf.BeginOperation(context.Background(), "archive download", allowUninhibited)
	if guardErr != nil {
		s.err = fmt.Errorf("%w. Press A to continue without suspend protection or B to cancel", guardErr)
		s.inhibitBlocked.Store(true)
		s.storeState(zipDLError)
		return
	}
	defer lease.Release()
	if !lease.Protected {
		logger.Warn("zip-download: continuing without Jawaka suspend protection by user request")
	}
	s.inhibitBlocked.Store(false)
	s.names, s.keepNames, s.romPaths = &roms.NameReservations{}, nil, nil

	tempDir, err := s.plan.preflight(s.cfg, s.plan.Manifest)
	if err != nil {
		s.err = fmt.Errorf("archive destination preflight: %w", err)
		s.storeState(zipDLError)
		return
	}
	tmp, err := os.CreateTemp(tempDir, ".itchio-archive-*.part")
	if err != nil {
		logger.Error("zip-download: create temp file: %v", err)
		s.err = fmt.Errorf("create temp file: %w", err)
		s.storeState(zipDLError)
		return
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		s.err = fmt.Errorf("close temp file: %w", err)
		s.storeState(zipDLError)
		return
	}
	if err := os.Remove(tmpPath); err != nil {
		s.err = fmt.Errorf("prepare temp file: %w", err)
		s.storeState(zipDLError)
		return
	}
	defer os.Remove(tmpPath)

	// Re-resolve CDN URL immediately before the download so a stale URL from
	// the inspect step (which may have run minutes ago) does not cause a 403.
	cdnURL := s.plan.CDNURL
	if s.plan.Upload.ViaAPI() {
		// Same install session as the inspection that produced this plan.
		fresh, rerr := s.client.ResolveUploadURLContext(context.Background(), s.cfg.Credential(), s.plan.Upload.UploadID, s.plan.Upload.Install)
		if rerr != nil {
			logger.Warn("zip-download: re-resolve auth URL failed (%v), using cached URL", rerr)
		} else {
			cdnURL = fresh
		}
	} else {
		itchUpload := itchio.Upload{Filename: s.plan.Upload.Filename, URL: s.plan.Upload.URL}
		fresh, rerr := s.client.ResolveFreeURL(itchUpload)
		if rerr != nil {
			logger.Warn("zip-download: re-resolve free URL failed (%v), using cached URL", rerr)
		} else {
			cdnURL = fresh
		}
	}

	progress := func(dl, total int64) {
		atomic.StoreInt64(&s.downloaded, dl)
		atomic.StoreInt64(&s.total, total)
	}
	logger.Info("zip-download: streaming %s → %s", s.plan.Upload.Filename, tmpPath)
	if err := s.client.DownloadURL(cdnURL, tmpPath, progress); err != nil {
		s.err = fmt.Errorf("download ZIP: %w", err)
		s.storeState(zipDLError)
		return
	}

	s.storeState(zipDLExtracting)

	// 7z archives are extracted via sevenzip; everything else uses archive/zip.
	if strings.ToLower(filepath.Ext(s.plan.Upload.Filename)) == ".7z" {
		s.run7z(tmpPath)
		return
	}

	r, err := zip.OpenReader(tmpPath)
	if err != nil {
		logger.Error("zip-download: open ZIP: %v", err)
		s.err = fmt.Errorf("open ZIP: %w", err)
		s.storeState(zipDLError)
		return
	}
	defer r.Close()
	entries := zipEntries(r.File)
	actualManifest, err := classifyArchive(entries)
	if err == nil {
		_, err = s.plan.preflight(s.cfg, actualManifest)
	}
	if err != nil {
		s.err = fmt.Errorf("downloaded archive preflight: %w", err)
		s.storeState(zipDLError)
		return
	}

	// Pico-8 multi-file: path-preserving extraction to game subdirectory.
	if s.plan.Pico8GameDir != "" {
		now := time.Now()
		s.extractPico8ZIP(&r.Reader, now)
		// Cover art and .m3u launcher for multi-file Pico-8 games.
		if len(s.extracted) > 0 {
			gameDir := strings.TrimSuffix(s.plan.Pico8GameDir, "/")

			// Cover art: artRef is <gameDir>.p8 so the canonical image uses the
			// directory name as its Jawaka-visible ROM stem.
			artRef := gameDir + ".p8"
			artwork := ensureROMArtwork(s.client, s.inv, s.game, artRef)
			if artwork.Path != "" {
				for _, dest := range s.extracted {
					s.inv.SetArtwork(s.game.URL, dest, artwork.Path, artwork.SHA256, artwork.Created)
				}
			}

			// .m3u launcher: collect .p8/.p8.png files, sort naturally, write
			// <safe>.m3u inside the game directory so the emulator loads all carts.
			safe := roms.SanitiseFilename(s.game.Title, "")
			if safe == "" {
				safe = "Unknown"
			}
			var p8Files []string
			for _, dest := range s.extracted {
				ext := strings.ToLower(roms.ROMExt(filepath.Base(dest)))
				if ext == ".p8" || ext == ".p8.png" {
					p8Files = append(p8Files, filepath.Base(dest))
				}
			}
			if len(p8Files) > 1 {
				sort.Slice(p8Files, func(i, j int) bool { return naturalLess(p8Files[i], p8Files[j]) })
				m3uPath := filepath.Join(gameDir, safe+".m3u")
				if s.ownedByAnotherGame(m3uPath) {
					logger.Warn("zip-download: pico8 m3u: another game's file is already saved as %s", filepath.Base(m3uPath))
				} else if err := os.WriteFile(m3uPath, []byte(strings.Join(p8Files, "\n")+"\n"), 0644); err != nil {
					logger.Warn("zip-download: pico8 m3u write: %v", err)
				} else {
					logger.Info("zip-download: pico8 m3u written %s (%d carts)", m3uPath, len(p8Files))
					s.extracted = append(s.extracted, m3uPath)
					file := inventory.DownloadedFile{
						UploadID:      s.plan.Upload.UploadID,
						Filename:      filepath.Base(m3uPath),
						DestPath:      m3uPath,
						DownloadedAt:  now,
						FileType:      inventory.FileTypeM3U,
						SourceArchive: s.plan.Upload.Filename,
					}
					applyArtwork(&file, artwork)
					s.inv.Add(s.game.URL, inventory.Entry{
						GameURL: s.game.URL, Title: s.game.Title,
						Author: s.game.Author, CoverURL: s.game.CoverURL, IsFree: s.game.IsFree,
					}, file)
				}
			}
		}
		if len(s.extracted) == 0 {
			logger.Error("zip-download: pico8: no files extracted (skipped=%d)", len(s.skipped))
			s.err = fmt.Errorf("no Pico-8 files could be extracted from ZIP")
			s.storeState(zipDLError)
			return
		}
		if err := s.inv.Save(s.invPath); err != nil {
			logger.Warn("zip-download: save inventory: %v", err)
		}
		s.recordLeftOvers()
		logger.Info("zip-download: pico8 done, extracted %d file(s)", len(s.extracted))
		s.storeState(zipDLDone)
		return
	}

	s.installEntries(entries, "zip-download")

	if err := s.inv.Save(s.invPath); err != nil {
		logger.Warn("zip-download: save inventory: %v", err)
	}

	if len(s.extracted) == 0 {
		logger.Error("zip-download: no files extracted (skipped=%d)", len(s.skipped))
		s.err = fmt.Errorf("no files could be extracted from ZIP")
		s.storeState(zipDLError)
		return
	}
	s.recordLeftOvers()
	logger.Info("zip-download: done, extracted %d file(s)", len(s.extracted))
	s.storeState(zipDLDone)
}

// run7z handles extraction for 7z archives using the same plan logic as run().
func (s *ArchiveDownloadWorker) run7z(tmpPath string) {
	r, err := sevenzip.OpenReader(tmpPath)
	if err != nil {
		logger.Error("7z-download: open archive: %v", err)
		s.err = fmt.Errorf("open 7z: %w", err)
		s.storeState(zipDLError)
		return
	}
	defer r.Close()
	entries := sevenZipEntries(r.File)
	actualManifest, err := classifyArchive(entries)
	if err == nil {
		_, err = s.plan.preflight(s.cfg, actualManifest)
	}
	if err != nil {
		s.err = fmt.Errorf("downloaded archive preflight: %w", err)
		s.storeState(zipDLError)
		return
	}

	if s.plan.Pico8GameDir != "" {
		now := time.Now()
		s.extractPico8_7z(r, now)
		if err := s.inv.Save(s.invPath); err != nil {
			logger.Warn("7z-download: save inventory: %v", err)
		}
		if len(s.extracted) == 0 {
			logger.Error("7z-download: pico8: no files extracted (skipped=%d)", len(s.skipped))
			s.err = fmt.Errorf("no Pico-8 files could be extracted from 7z")
			s.storeState(zipDLError)
			return
		}
		s.recordLeftOvers()
		logger.Info("7z-download: pico8 done, extracted %d file(s)", len(s.extracted))
		s.storeState(zipDLDone)
		return
	}

	s.installEntries(entries, "7z-download")

	if err := s.inv.Save(s.invPath); err != nil {
		logger.Warn("7z-download: save inventory: %v", err)
	}
	if len(s.extracted) == 0 {
		logger.Error("7z-download: no files extracted (skipped=%d)", len(s.skipped))
		s.err = fmt.Errorf("no files could be extracted from 7z")
		s.storeState(zipDLError)
		return
	}
	s.recordLeftOvers()
	logger.Info("7z-download: done, extracted %d file(s)", len(s.extracted))
	s.storeState(zipDLDone)
}

// manifestFromZIP classifies a downloaded ZIP's members; see classifyArchive.
func manifestFromZIP(files []*zip.File) (roms.ZIPManifest, error) {
	return classifyArchive(zipEntries(files))
}

// manifestFrom7z classifies a downloaded 7z's members; see classifyArchive.
func manifestFrom7z(files []*sevenzip.File) (roms.ZIPManifest, error) {
	return classifyArchive(sevenZipEntries(files))
}

// classifyArchive validates the downloaded headers before opening any
// member, then classifies each member once: by extension, or for names
// that do not decide it by its first bytes. The result is kept on the
// entries, and naming, preflight and extraction all read it, so no member
// is opened again to sniff it. In a solid 7z every open decodes everything
// before the member, so each repeated sniff was another decode of the
// archive. Members extraction never installs (macOS metadata) are not
// opened at all.
func classifyArchive(entries []archiveEntry) (roms.ZIPManifest, error) {
	manifest := roms.ZIPManifest{Entries: make([]roms.ZIPEntry, 0, len(entries))}
	for _, entry := range entries {
		if entry.isDir {
			continue
		}
		manifest.Entries = append(manifest.Entries, roms.ZIPEntry{
			Name: entry.name, Kind: roms.ClassifyEntry(entry.name),
			Size: entry.size, CompressedSize: entry.compressedSize,
		})
	}
	if err := ValidateArchiveManifest(manifest, DefaultArchiveLimits); err != nil {
		return roms.ZIPManifest{}, err
	}
	index := 0
	for position := range entries {
		entry := &entries[position]
		if entry.isDir {
			continue
		}
		entry.kind, entry.base = manifest.Entries[index].Kind, path.Base(entry.name)
		if entry.installable() {
			kind, name, err := classifyWithMagic(entry.name, entry.open)
			if err != nil {
				// A .md member that cannot be read could be a ROM.
				return roms.ZIPManifest{}, fmt.Errorf("read %s: %w", path.Base(entry.name), err)
			}
			entry.kind, entry.base = kind, path.Base(name)
			manifest.Entries[index].Kind, manifest.Entries[index].Name = kind, name
			if strings.EqualFold(roms.ROMExt(entry.base), ".cue") {
				entry.cueFiles = readCueFiles(entry.open)
			}
		}
		index++
	}
	return manifest, nil
}

// maxCueBytes bounds how much of a .cue sheet is read for its FILE lines.
const maxCueBytes = 64 << 10

// readCueFiles returns the base names of the files a .cue sheet references
// in its FILE lines, such as "Game (Track 1).bin".
func readCueFiles(open func() (io.ReadCloser, error)) []string {
	rc, err := open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxCueBytes))
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 5 || !strings.EqualFold(line[:5], "FILE ") {
			continue
		}
		rest := strings.TrimSpace(line[5:])
		name := ""
		if strings.HasPrefix(rest, "\"") {
			if end := strings.Index(rest[1:], "\""); end >= 0 {
				name = rest[1 : end+1]
			}
		} else if fields := strings.Fields(rest); len(fields) > 0 {
			name = fields[0]
		}
		if name != "" {
			files = append(files, path.Base(strings.ReplaceAll(name, "\\", "/")))
		}
	}
	return files
}

// extractPico8_7z extracts .p8, .p8.png, and .lua files from a 7z archive,
// preserving relative paths into s.plan.Pico8GameDir.
func (s *ArchiveDownloadWorker) extractPico8_7z(r *sevenzip.ReadCloser, now time.Time) {
	gameDir := strings.TrimSuffix(s.plan.Pico8GameDir, "/")
	var relevantPaths []string
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if roms.IsInMacOSMetaDir(f.Name) {
			continue
		}
		name := filepath.ToSlash(strings.ReplaceAll(f.Name, "\\", "/"))
		base := filepath.Base(name)
		if strings.HasPrefix(base, "._") {
			continue
		}
		lower := strings.ToLower(base)
		ext := strings.ToLower(roms.ROMExt(base))
		if ext == ".p8" || ext == ".p8.png" || strings.HasSuffix(lower, ".lua") {
			relevantPaths = append(relevantPaths, name)
		}
	}
	prefix := commonPathPrefix(relevantPaths)

	p8PNGCount := 0
	for _, p := range relevantPaths {
		if strings.ToLower(roms.ROMExt(filepath.Base(p))) == ".p8.png" {
			p8PNGCount++
		}
	}
	unifyP8PNG := p8PNGCount == 1 && s.cfg.UnifiedNaming
	if unifyP8PNG {
		if inv, ok := s.inv.Lookup(s.game.URL); ok && inv.UnifiedNamingDisabled {
			unifyP8PNG = false
		}
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if roms.IsInMacOSMetaDir(f.Name) {
			continue
		}
		name := filepath.ToSlash(strings.ReplaceAll(f.Name, "\\", "/"))
		base := filepath.Base(name)
		if strings.HasPrefix(base, "._") {
			continue
		}
		lower := strings.ToLower(base)
		ext := strings.ToLower(roms.ROMExt(base))
		if ext != ".p8" && ext != ".p8.png" && !strings.HasSuffix(lower, ".lua") {
			continue
		}
		relPath := strings.TrimPrefix(name, prefix)
		dest := filepath.Join(gameDir, filepath.FromSlash(relPath))
		if s.ownedByAnotherGame(dest) {
			logger.Warn("7z-download: pico8 %s: another game's file is already saved there", base)
			s.skipped = append(s.skipped, base)
			continue
		}
		if !s.names.Claim(dest) {
			logger.Warn("7z-download: pico8 %s: another file from this archive is already saved there", base)
			s.skipped = append(s.skipped, base)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			s.skipped = append(s.skipped, base)
			continue
		}
		if err := extractEntry(f.Open, f.FileInfo().Size(), dest); err != nil {
			logger.Warn("7z-download: pico8 extract %s: %v", base, err)
			s.skipped = append(s.skipped, base)
			continue
		}

		finalDest := dest
		unifiedName := false
		if ext == ".p8.png" && unifyP8PNG {
			finalDest, unifiedName = s.unifyArchiveROM(dest, "7z-download: pico8")
		}

		logger.Info("7z-download: pico8 extracted %s → %s", base, finalDest)
		s.extracted = append(s.extracted, finalDest)
		s.inv.Add(s.game.URL, inventory.Entry{
			GameURL: s.game.URL, Title: s.game.Title,
			Author: s.game.Author, CoverURL: s.game.CoverURL, IsFree: s.game.IsFree,
		}, inventory.DownloadedFile{
			UploadID:      s.plan.Upload.UploadID,
			Filename:      filepath.Base(finalDest),
			DestPath:      finalDest,
			DownloadedAt:  now,
			FileType:      inventory.FileTypeROM,
			UnifiedName:   unifiedName,
			SourceArchive: s.plan.Upload.Filename,
		})
	}
}

// unifyArchiveROM renames an extracted file to the game title unless that
// name belongs to another file of this archive, in which case the file keeps
// its original name. It returns the final path and whether that path is the
// unified name. Every rename site shares it (upstream c346eb0).
func (s *ArchiveDownloadWorker) unifyArchiveROM(dest, logPrefix string) (string, bool) {
	if s.keepNames != nil && s.keepNames.Holds(dest) {
		logger.Info("%s: keeping %q; its unified name belongs to another file of this archive", logPrefix, filepath.Base(dest))
		return dest, false
	}
	newDest, unified := s.namer().unifiedName(dest)
	if roms.SameFAT32Path(newDest, dest) {
		return dest, unified
	}
	if err := os.Rename(dest, newDest); err != nil {
		logger.Warn("%s: unified rename: %v", logPrefix, err)
		return dest, false
	}
	s.names.Release(dest)
	s.names.Claim(newDest)
	return newDest, unified
}

// namer picks names for this archive's files: an earlier install of the same
// archive for this game may be replaced, nothing else.
func (s *ArchiveDownloadWorker) namer() *installNamer {
	return newInstallNamer(s.inv, s.game, s.plan.Upload.Filename, s.names).withListing(s.plan.Upload)
}

// ownedByAnotherGame reports whether dest holds another game's file or one
// the app does not know. A Pico-8 game extracts by relative path into its
// own folder, where its files cannot be renamed apart, so files from any
// upload of the same game are replaced there.
func (s *ArchiveDownloadWorker) ownedByAnotherGame(dest string) bool {
	namer := s.namer()
	namer.anyUpload = true
	return !namer.replaceable(dest)
}

// ownedElsewhere reports whether dest holds a file this archive may not
// replace: another game's, another build's, or one the app does not know.
func (s *ArchiveDownloadWorker) ownedElsewhere(dest string) bool {
	return !s.namer().replaceable(dest)
}

// romDest is where an archive ROM entry with baseName (after magic-byte
// classification) is extracted before any unified rename.
func (s *ArchiveDownloadWorker) romDest(baseName string) string {
	ext := strings.ToLower(roms.ROMExt(baseName))
	destDir := s.plan.ROMDirs[ext]
	if destDir == "" {
		destDir = roms.DestinationDir(ext)
	}
	stem := strings.TrimSuffix(baseName, roms.ROMExt(baseName))
	safeName := roms.SanitiseFilename(stem, ext)
	if safeName == "" {
		safeName = baseName
	}
	return archiveOutputPath(destDir, safeName)
}

// planCueTracks records which files the .cue sheets this archive installs
// reference. When there is one, only those .bin tracks install: a BIOS
// image such as openbios.bin shipped next to the game is not a track.
func (s *ArchiveDownloadWorker) planCueTracks(entries []archiveEntry) {
	s.cueTracks = nil
	for _, entry := range entries {
		if !entry.installable() || !strings.EqualFold(roms.ROMExt(entry.base), ".cue") ||
			!s.shouldExtractROM(entry.classifiedName()) {
			continue
		}
		if s.cueTracks == nil {
			s.cueTracks = map[string]bool{}
		}
		for _, file := range entry.cueFiles {
			s.cueTracks[strings.ToLower(file)] = true
		}
	}
}

// unusedTrack reports whether entry is a .bin that no installed .cue uses.
func (s *ArchiveDownloadWorker) unusedTrack(entry archiveEntry) bool {
	return s.cueTracks != nil && roms.IsPSXSupportExt(roms.ROMExt(entry.base)) &&
		!s.cueTracks[strings.ToLower(entry.base)]
}

// plannedROMDest is where planROMNames decided an entry is extracted.
func (s *ArchiveDownloadWorker) plannedROMDest(entryName, baseName string) string {
	if dest, ok := s.romPaths[strings.ReplaceAll(entryName, "\\", "/")]; ok {
		return dest
	}
	return s.romDest(baseName)
}

// archiveEntry is one ZIP or 7z member. classifyArchive sets kind and base
// once; every later pass reads them instead of opening the member again.
type archiveEntry struct {
	name           string // path inside the archive, with forward slashes
	isDir          bool
	size           uint64
	compressedSize uint64
	open           func() (io.ReadCloser, error)

	kind roms.FileKind
	base string // file name, with the extension its first bytes confirm
	// cueFiles lists the base names a .cue member references.
	cueFiles []string
}

// classifiedName is the entry's path with the extension classification
// gave it. The ROM chooser and the preflight plan list members by it.
func (entry archiveEntry) classifiedName() string {
	return path.Join(path.Dir(entry.name), entry.base)
}

// installable reports whether extraction looks at the entry at all.
// Directories and macOS metadata never install.
func (entry archiveEntry) installable() bool {
	return !entry.isDir && !roms.IsInMacOSMetaDir(entry.name) && !strings.HasPrefix(path.Base(entry.name), "._")
}

func zipEntries(files []*zip.File) []archiveEntry {
	entries := make([]archiveEntry, 0, len(files))
	for _, file := range files {
		entries = append(entries, archiveEntry{
			name: strings.ReplaceAll(file.Name, "\\", "/"), isDir: file.FileInfo().IsDir(),
			size: file.UncompressedSize64, compressedSize: file.CompressedSize64, open: file.Open,
		})
	}
	return entries
}

func sevenZipEntries(files []*sevenzip.File) []archiveEntry {
	entries := make([]archiveEntry, 0, len(files))
	for _, file := range files {
		entries = append(entries, archiveEntry{
			name: strings.ReplaceAll(file.Name, "\\", "/"), isDir: file.FileInfo().IsDir(),
			size: file.UncompressedSize, open: file.Open,
		})
	}
	return entries
}

// installEntries extracts the classified ROM and music entries of a
// non-Pico-8 archive.
func (s *ArchiveDownloadWorker) installEntries(entries []archiveEntry, logPrefix string) {
	s.planCueTracks(entries)
	s.planROMNames(entries)
	s.planMusicNames(entries)
	now := time.Now()
	for _, entry := range entries {
		if !entry.installable() {
			continue
		}
		switch entry.kind {
		case roms.KindROM, roms.KindROMSupport:
			if !s.shouldExtractROM(entry.classifiedName()) {
				continue
			}
			if s.unusedTrack(entry) {
				logger.Info("%s: not installing %s; no .cue in this archive uses it", logPrefix, entry.base)
				continue
			}
			dest, err := s.extractROMFromOpener(entry.open, int64(entry.size), entry.name, entry.base, now)
			if err != nil {
				logger.Warn("%s: ROM %s: %v", logPrefix, entry.base, err)
				s.skipped = append(s.skipped, entry.base)
				continue
			}
			s.extracted = append(s.extracted, dest)
		case roms.KindMusic:
			if !s.plan.DownloadMusic || s.plan.MusicDir == "" {
				continue
			}
			dest, err := s.extractMusicFromOpener(entry.open, int64(entry.size), entry.name, entry.base, now)
			if err != nil {
				logger.Warn("%s: music %s: %v", logPrefix, entry.base, err)
				s.skipped = append(s.skipped, entry.base)
				continue
			}
			s.extracted = append(s.extracted, dest)
		}
	}
}

// planROMNames classifies every ROM entry this extraction will write, the
// same way extraction does (including magic bytes), and records which of
// them must keep their original names because unified naming would give two
// files of the archive one name. Deciding up front keeps both files whatever
// order the entries come in.
func (s *ArchiveDownloadWorker) planROMNames(entries []archiveEntry) {
	var dests []string
	s.romPaths = map[string]string{}
	// Entries that FAT32 would store under one name share one plan, so the
	// later one is still skipped when it is written.
	owned := map[string]string{}
	for _, entry := range entries {
		name := entry.name
		if !entry.installable() {
			continue
		}
		if (entry.kind == roms.KindROM || entry.kind == roms.KindROMSupport) && s.shouldExtractROM(entry.classifiedName()) &&
			!s.unusedTrack(entry) {
			natural := s.romDest(entry.base)
			key := strings.ToLower(filepath.Clean(natural))
			dest, planned := owned[key]
			if !planned {
				var err error
				if dest, err = newInstallNamer(s.inv, s.game, s.plan.Upload.Filename, nil).withListing(s.plan.Upload).ownName(natural); err != nil {
					dest = natural
				} else if !roms.SameFAT32Path(dest, natural) {
					logger.Info("archive: %s belongs to another game or upload; saving as %s", filepath.Base(natural), filepath.Base(dest))
				}
				owned[key] = dest
			}
			s.romPaths[name] = dest
			dests = append(dests, dest)
		}
	}
	s.keepNames = &roms.NameReservations{}
	if !s.cfg.UnifiedNaming {
		return
	}
	for index, keep := range roms.UnifiedCollisions(dests, s.game.Title) {
		if keep {
			s.keepNames.Claim(dests[index])
		}
	}
}

// extractROMFromOpener is like extractROM but takes an opener func instead of *zip.File.
// Used by run7z so the same inventory/naming logic applies to 7z entries.
func (s *ArchiveDownloadWorker) extractROMFromOpener(open func() (io.ReadCloser, error), size int64, entryName, baseName string, now time.Time) (string, error) {
	ext := strings.ToLower(roms.ROMExt(baseName))
	dest := s.plannedROMDest(entryName, baseName)
	destDir := filepath.Dir(dest)

	// Skip when an identical ROM already exists.
	if existing := s.findIdenticalFromOpener(open, size, ext); existing != "" {
		logger.Info("7z-download: ROM %s: identical file at %s, skipping", baseName, existing)
		s.backfillSourceArchive(existing)
		return existing, nil
	}

	if s.ownedElsewhere(dest) {
		return "", fmt.Errorf("another game's file is already saved as %s", filepath.Base(dest))
	}
	if !s.names.Claim(dest) {
		return "", fmt.Errorf("another file from this archive is already saved as %s", filepath.Base(dest))
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return "", fmt.Errorf("mkdirall %s: %w", destDir, err)
	}
	if err := extractEntry(open, size, dest); err != nil {
		return "", err
	}

	finalDest := dest
	unifiedName := false
	if s.cfg.UnifiedNaming && roms.SupportsUnifiedNaming(baseName) {
		entry, entryExists := s.inv.Lookup(s.game.URL)
		disabled := entryExists && entry.UnifiedNamingDisabled
		if !disabled {
			finalDest, unifiedName = s.unifyArchiveROM(dest, "7z-download")
		}
	}
	logger.Info("7z-download: ROM extracted → %s (unified=%v)", finalDest, unifiedName)
	artwork := ensureROMArtwork(s.client, s.inv, s.game, finalDest)
	file := inventory.DownloadedFile{
		UploadID:      s.plan.Upload.UploadID,
		Filename:      filepath.Base(finalDest),
		DestPath:      finalDest,
		DownloadedAt:  now,
		FileType:      inventory.FileTypeROM,
		UnifiedName:   unifiedName,
		SourceArchive: s.plan.Upload.Filename,
	}
	applyArtwork(&file, artwork)
	s.inv.Add(s.game.URL, inventory.Entry{
		GameURL: s.game.URL, Title: s.game.Title,
		Author: s.game.Author, CoverURL: s.game.CoverURL, IsFree: s.game.IsFree,
	}, file)
	return finalDest, nil
}

// musicFileName is a track's file name in the game's Music folder.
func musicFileName(baseName string) string {
	ext := filepath.Ext(baseName)
	if safeName := roms.SanitiseFilename(strings.TrimSuffix(baseName, ext), ext); safeName != "" {
		return safeName
	}
	return baseName
}

// musicFolder turns archive folder components into a safe subfolder path,
// dropping empty, dot and parent components.
func musicFolder(dirs []string) string {
	var parts []string
	for _, dir := range dirs {
		part := roms.SanitiseFilename(dir, "")
		if part == "" || strings.HasPrefix(part, ".") {
			continue
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

// planMusicNames places every soundtrack track this extraction will write,
// as a slash-separated path inside the game's Music folder. Tracks go in by
// name. When tracks in different archive folders share a name
// ("cd1/01 Theme.ogg", "cd2/01 Theme.ogg"), every track of those folders
// keeps its folder as a subfolder ("cd1/01 Theme.ogg", "cd1/02 Battle.ogg"),
// so each disc stays together and Disco Boy, which sorts by full path,
// plays it in order. Leading folders all of them share ("Soundtrack/") are
// dropped. Deciding up front keeps the layout independent of entry order.
func (s *ArchiveDownloadWorker) planMusicNames(entries []archiveEntry) {
	s.musicNames = map[string]string{}
	if !s.plan.DownloadMusic || s.plan.MusicDir == "" {
		return
	}
	type track struct {
		entry, dir, name string
	}
	var tracks []*track
	byName := map[string][]*track{}
	for _, entry := range entries {
		if !entry.installable() || entry.kind != roms.KindMusic {
			continue
		}
		t := &track{entry: entry.name, dir: path.Dir(entry.name), name: musicFileName(entry.base)}
		tracks = append(tracks, t)
		key := strings.ToLower(t.name)
		byName[key] = append(byName[key], t)
	}
	// Folders holding a track whose name a track in another folder has too.
	colliding := map[string]bool{}
	for _, group := range byName {
		for _, t := range group[1:] {
			if t.dir != group[0].dir {
				for _, member := range group {
					colliding[member.dir] = true
				}
				break
			}
		}
	}
	var shared []string
	first := true
	for dir := range colliding {
		var parts []string
		if dir != "." {
			parts = strings.Split(dir, "/")
		}
		if first {
			shared, first = parts, false
			continue
		}
		if len(parts) < len(shared) {
			shared = shared[:len(parts)]
		}
		for index := range shared {
			if !strings.EqualFold(parts[index], shared[index]) {
				shared = shared[:index]
				break
			}
		}
	}
	// One reservation pass over every final name: a track that would still
	// meet an earlier one (names or folders differing only in case) is
	// numbered, "Theme (2).ogg", instead of skipped.
	taken := map[string]bool{}
	for _, t := range tracks {
		rel := t.name
		if colliding[t.dir] && t.dir != "." {
			if folder := musicFolder(strings.Split(t.dir, "/")[len(shared):]); folder != "" {
				rel = folder + "/" + t.name
			}
		}
		if taken[strings.ToLower(rel)] {
			folder, base := path.Split(rel)
			ext := filepath.Ext(base)
			for attempt := 2; attempt <= maxNameAttempts; attempt++ {
				candidate := fmt.Sprintf("%s%s (%d)%s", folder, strings.TrimSuffix(base, ext), attempt, ext)
				if !taken[strings.ToLower(candidate)] {
					rel = candidate
					break
				}
			}
		}
		taken[strings.ToLower(rel)] = true
		s.musicNames[t.entry] = rel
	}
}

// musicDest reserves the Music folder path for an archive entry. It fails,
// writing nothing, when another file of this archive already holds that
// name, so a track is skipped rather than written over another.
func (s *ArchiveDownloadWorker) musicDest(entryName, baseName string) (string, error) {
	rel, ok := s.musicNames[strings.ReplaceAll(entryName, "\\", "/")]
	if !ok {
		rel = musicFileName(baseName)
	}
	planned := archiveOutputPath(s.plan.MusicDir, filepath.FromSlash(rel))
	if _, err := leaf.RelativeWithin(s.plan.MusicDir, planned); err != nil {
		return "", fmt.Errorf("track %s escapes the Music folder", rel)
	}
	dest, err := s.ownMusicPath(planned)
	if err != nil {
		return "", err
	}
	if !s.names.Claim(dest) {
		return "", fmt.Errorf("another file from this archive is already saved as %s", rel)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		s.musicFailed = true
		return "", fmt.Errorf("mkdirall music dir %s: %w", filepath.Dir(dest), err)
	}
	return dest, nil
}

// musicRecordName is the inventory name of a track: its path inside the
// Music folder, so tracks of one name in different subfolders stay apart.
func (s *ArchiveDownloadWorker) musicRecordName(dest string) string {
	if rel, err := filepath.Rel(filepath.Clean(s.plan.MusicDir), dest); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(dest)
}

// extractMusicFromOpener is like extractMusic but takes an opener func.
func (s *ArchiveDownloadWorker) extractMusicFromOpener(open func() (io.ReadCloser, error), size int64, entryName, baseName string, now time.Time) (string, error) {
	dest, err := s.musicDest(entryName, baseName)
	if err != nil {
		return "", err
	}
	if err := extractEntry(open, size, dest); err != nil {
		return "", err
	}
	s.inv.Add(s.game.URL, inventory.Entry{
		GameURL: s.game.URL, Title: s.game.Title,
		Author: s.game.Author, CoverURL: s.game.CoverURL, IsFree: s.game.IsFree,
	}, inventory.DownloadedFile{
		UploadID:      s.plan.Upload.UploadID,
		Filename:      s.musicRecordName(dest),
		DestPath:      dest,
		DownloadedAt:  now,
		FileType:      inventory.FileTypeMusic,
		SourceArchive: s.plan.Upload.Filename,
	})
	return dest, nil
}

// ownMusicPath keeps a track off a file this game does not own, naming it
// "<Title> - <track>" instead. Music records do not say which upload they
// came from, so any track of this game may be replaced.
func (s *ArchiveDownloadWorker) ownMusicPath(dest string) (string, error) {
	// Reservations are left to musicDest, so a track that meets another
	// track of this archive is skipped rather than renamed.
	namer := newInstallNamer(s.inv, s.game, s.plan.Upload.Filename, nil)
	namer.anyUpload = true
	path, err := namer.ownName(dest)
	if err == nil && !roms.SameFAT32Path(path, dest) {
		logger.Info("archive: %s belongs to another game; saving as %s", filepath.Base(dest), filepath.Base(path))
	}
	return path, err
}

// recordLeftOvers runs once this archive is installed. Every file the
// inventory records from this archive, of the kinds this install wrote,
// that the install did not write or keep is listed as left over from an
// older version, for example a track an older version named differently.
// Manage offers those files for deletion; nothing is deleted here.
func (s *ArchiveDownloadWorker) recordLeftOvers() {
	var kinds []string
	if s.plan.DownloadROMs {
		kinds = append(kinds, inventory.ContentKindROM)
	}
	if s.plan.DownloadMusic && s.plan.MusicDir != "" && !s.musicFailed {
		s.adoptUnattributedMusic()
		kinds = append(kinds, inventory.ContentKindMusic)
	}
	for _, kind := range kinds {
		for _, file := range s.inv.MarkLeftOver(s.game.URL, s.plan.Upload.Filename, kind, s.extracted) {
			logger.Info("archive: %s is left over from an older version of %s", filepath.Base(file.DestPath), s.plan.Upload.Filename)
		}
	}
	if len(kinds) > 0 {
		if err := s.inv.Save(s.invPath); err != nil {
			logger.Warn("archive: save left-over files: %v", err)
		}
	}
}

// adoptUnattributedMusic attributes this game's tracks that older versions
// recorded without their archive to this archive, when they sit in the
// Music folder it installs to. Their archive is unknown otherwise, and the
// game's soundtrack folder is the best evidence of where they came from.
func (s *ArchiveDownloadWorker) adoptUnattributedMusic() {
	entry, ok := s.inv.Lookup(s.game.URL)
	if !ok {
		return
	}
	for _, file := range entry.Files {
		if managedContentKind(file) != inventory.ContentKindMusic || file.SourceArchive != "" {
			continue
		}
		if _, err := leaf.RelativeWithin(s.plan.MusicDir, file.DestPath); err != nil {
			continue
		}
		file.SourceArchive = s.plan.Upload.Filename
		s.inv.UpdateFile(s.game.URL, file.DestPath, file)
	}
}

// backfillSourceArchive patches SourceArchive into an existing inventory entry
// whose DestPath matches. Called when extraction is skipped because an identical
// file already exists — pre-fix entries have SourceArchive="" which causes the
// update service to incorrectly mark the game as removed.
func (s *ArchiveDownloadWorker) backfillSourceArchive(destPath string) {
	if s.plan.Upload.Filename == "" {
		return
	}
	entry, ok := s.inv.Lookup(s.game.URL)
	if !ok {
		return
	}
	for _, f := range entry.Files {
		if f.DestPath == destPath && f.SourceArchive == "" {
			f.SourceArchive = s.plan.Upload.Filename
			s.inv.UpdateFile(s.game.URL, destPath, f)
			logger.Debug("zip-download: backfilled SourceArchive=%q for %s",
				s.plan.Upload.Filename, filepath.Base(destPath))
			return
		}
	}
}

// findIdenticalFromOpener checks the inventory for a ROM matching the given
// opener's content. Used by the 7z extraction path.
func (s *ArchiveDownloadWorker) findIdenticalFromOpener(open func() (io.ReadCloser, error), size int64, ext string) string {
	entry, ok := s.inv.Lookup(s.game.URL)
	if !ok {
		return ""
	}
	// Hash the entry only when a same-sized ROM of its type exists: in a
	// solid 7z reading it decodes everything before it.
	wantHash := ""
	for _, df := range entry.Files {
		if df.FileType != inventory.FileTypeROM {
			continue
		}
		if strings.ToLower(roms.ROMExt(df.DestPath)) != ext {
			continue
		}
		fi, err := os.Stat(df.DestPath)
		if err != nil || fi.Size() != size {
			continue
		}
		if wantHash == "" {
			if wantHash, err = entryMD5(open); err != nil {
				logger.Warn("archive: identical-file check for %s: %v", filepath.Base(df.DestPath), err)
				return ""
			}
		}
		if hash, err := fileMD5(df.DestPath); err == nil && hash == wantHash {
			return df.DestPath
		}
	}
	return ""
}

// classifyWithMagic classifies an archive member by name and, when the
// name does not decide it, by its first bytes; see roms.ClassifyArchiveMember.
// This handles members whose name uses a generic extension, such as ".bin"
// for a Mega Drive ROM, and ".md", which is Markdown or a Mega Drive ROM.
func classifyWithMagic(name string, open func() (io.ReadCloser, error)) (roms.FileKind, string, error) {
	return roms.ClassifyArchiveMember(name, open)
}

// shouldExtractROM reports whether the ROM member with the classified name
// is one the user chose (or no choice applied to its type).
func (s *ArchiveDownloadWorker) shouldExtractROM(name string) bool {
	return s.plan.shouldExtractROM(name)
}

// extractPico8ZIP extracts all .p8, .p8.png, and .lua files from r into
// s.plan.Pico8GameDir, preserving relative paths from the ZIP after stripping
// any common top-level wrapper directory. Support files (.lua) required by
// Pico-8 carts are extracted alongside the cartridges.
func (s *ArchiveDownloadWorker) extractPico8ZIP(r *zip.Reader, now time.Time) {
	gameDir := strings.TrimSuffix(s.plan.Pico8GameDir, "/")

	// Collect all relevant file paths to determine the common prefix to strip.
	var relevantPaths []string
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if roms.IsInMacOSMetaDir(f.Name) {
			continue
		}
		name := filepath.ToSlash(f.Name)
		base := filepath.Base(name)
		if strings.HasPrefix(base, "._") {
			continue
		}
		lower := strings.ToLower(base)
		ext := strings.ToLower(roms.ROMExt(base))
		if ext == ".p8" || ext == ".p8.png" || strings.HasSuffix(lower, ".lua") {
			relevantPaths = append(relevantPaths, name)
		}
	}
	prefix := commonPathPrefix(relevantPaths)
	logger.Debug("zip-download: pico8 strip-prefix=%q game-dir=%s", prefix, gameDir)

	// Apply unified naming to the .p8.png only when it is the sole compiled
	// cart in the ZIP. Multiple .p8.png files indicate a genuine multi-cart
	// game where per-file names are meaningful and must not be collapsed.
	p8PNGCount := 0
	for _, p := range relevantPaths {
		if strings.ToLower(roms.ROMExt(filepath.Base(p))) == ".p8.png" {
			p8PNGCount++
		}
	}
	unifyP8PNG := p8PNGCount == 1 && s.cfg.UnifiedNaming
	if unifyP8PNG {
		if inv, ok := s.inv.Lookup(s.game.URL); ok && inv.UnifiedNamingDisabled {
			unifyP8PNG = false
		}
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if roms.IsInMacOSMetaDir(f.Name) {
			continue
		}
		name := filepath.ToSlash(f.Name)
		base := filepath.Base(name)
		if strings.HasPrefix(base, "._") {
			continue
		}
		lower := strings.ToLower(base)
		ext := strings.ToLower(roms.ROMExt(base))
		isP8 := ext == ".p8" || ext == ".p8.png"
		isLua := strings.HasSuffix(lower, ".lua")
		if !isP8 && !isLua {
			continue
		}

		relPath := strings.TrimPrefix(name, prefix)
		dest := filepath.Join(gameDir, filepath.FromSlash(relPath))

		if s.ownedByAnotherGame(dest) {
			logger.Warn("zip-download: pico8 %s: another game's file is already saved there", base)
			s.skipped = append(s.skipped, base)
			continue
		}
		if !s.names.Claim(dest) {
			logger.Warn("zip-download: pico8 %s: another file from this archive is already saved there", base)
			s.skipped = append(s.skipped, base)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			logger.Warn("zip-download: pico8 mkdir %s: %v", filepath.Dir(dest), err)
			s.skipped = append(s.skipped, base)
			continue
		}
		if err := extractZIPEntry(f, dest); err != nil {
			logger.Warn("zip-download: pico8 extract %s: %v", base, err)
			s.skipped = append(s.skipped, base)
			continue
		}

		finalDest := dest
		unifiedName := false
		if ext == ".p8.png" && unifyP8PNG {
			finalDest, unifiedName = s.unifyArchiveROM(dest, "zip-download: pico8")
		}

		logger.Info("zip-download: pico8 extracted %s → %s", base, finalDest)
		s.extracted = append(s.extracted, finalDest)

		s.inv.Add(s.game.URL, inventory.Entry{
			GameURL: s.game.URL, Title: s.game.Title,
			Author: s.game.Author, CoverURL: s.game.CoverURL, IsFree: s.game.IsFree,
		}, inventory.DownloadedFile{
			UploadID:      s.plan.Upload.UploadID,
			Filename:      filepath.Base(finalDest),
			DestPath:      finalDest,
			DownloadedAt:  now,
			FileType:      inventory.FileTypeROM,
			UnifiedName:   unifiedName,
			SourceArchive: s.plan.Upload.Filename,
		})
	}
}

// entryMD5 reads the uncompressed content via open() and returns its MD5 hex digest.
func entryMD5(open func() (io.ReadCloser, error)) (string, error) {
	rc, err := open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := md5.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// fileMD5 returns the MD5 hex digest of the file at path.
func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// extractEntry copies the content returned by open() to dest on disk.
// Used for both ZIP and 7z entries.
func extractEntry(open func() (io.ReadCloser, error), expectedSize int64, dest string) error {
	if err := leaf.RequireFreeSpace(filepath.Dir(dest), expectedSize); err != nil {
		return fmt.Errorf("extract storage preflight: %w", err)
	}
	rc, err := open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.CreateTemp(filepath.Dir(dest), ".itchio-extract-*.part")
	if err != nil {
		return err
	}
	tmpPath := out.Name()
	committed := false
	defer func() {
		_ = out.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	written, err := io.Copy(out, rc)
	if err != nil {
		return err
	}
	if expectedSize > 0 && written != expectedSize {
		return fmt.Errorf("extracted size %d does not match expected %d", written, expectedSize)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Chmod(0o644); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return err
	}
	committed = true
	return nil
}

// extractZIPEntry is a convenience wrapper around extractEntry for zip.File.
func extractZIPEntry(f *zip.File, dest string) error {
	return extractEntry(f.Open, int64(f.UncompressedSize64), dest)
}

// commonPathPrefix returns the longest common directory path shared by all
// paths, including the trailing slash. Returns "" when paths share no common
// parent directory (files at the ZIP root, or in entirely different subtrees).
//
// Unlike the former commonPathPrefix which stopped after the first component,
// this strips ALL shared leading directory levels so that packaging structures
// like "GameName/pico8/cart.p8" are fully unwrapped:
//
//	["game/pico8/a.p8", "game/pico8/b.p8"]  → "game/pico8/"
//	["game/cart_a/a.p8", "game/cart_b/b.p8"] → "game/"
//	["a.p8", "b.p8"]                         → ""
func commonPathPrefix(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	dirParts := func(p string) []string {
		p = filepath.ToSlash(p)
		idx := strings.LastIndex(p, "/")
		if idx < 0 {
			return nil
		}
		return strings.Split(p[:idx], "/")
	}
	parts := dirParts(paths[0])
	if len(parts) == 0 {
		return ""
	}
	for _, p := range paths[1:] {
		other := dirParts(p)
		n := len(parts)
		if len(other) < n {
			n = len(other)
		}
		for i := 0; i < n; i++ {
			if parts[i] != other[i] {
				n = i
				break
			}
		}
		parts = parts[:n]
		if len(parts) == 0 {
			return ""
		}
	}
	return strings.Join(parts, "/") + "/"
}

// IsBusy implements BusyChecker. Returns true while download or extraction is in flight.
func (s *ArchiveDownloadWorker) IsBusy() bool {
	st := s.loadState()
	return st == zipDLDownloading || st == zipDLExtracting
}

// naturalLess compares two strings using natural sort order so that numeric
// substrings are compared as integers (poom_9.p8 < poom_10.p8).
func naturalLess(a, b string) bool {
	for len(a) > 0 && len(b) > 0 {
		// Consume matching non-digit prefix.
		i := 0
		for i < len(a) && i < len(b) && (a[i] < '0' || a[i] > '9') && (b[i] < '0' || b[i] > '9') {
			if a[i] != b[i] {
				return a[i] < b[i]
			}
			i++
		}
		a, b = a[i:], b[i:]
		if len(a) == 0 || len(b) == 0 {
			break
		}
		// One or both strings are at a digit run.
		aIsDigit := a[0] >= '0' && a[0] <= '9'
		bIsDigit := b[0] >= '0' && b[0] <= '9'
		if !aIsDigit || !bIsDigit {
			return a[0] < b[0]
		}
		// Parse both numeric runs.
		ai, bi := 0, 0
		for ai < len(a) && a[ai] >= '0' && a[ai] <= '9' {
			ai++
		}
		for bi < len(b) && b[bi] >= '0' && b[bi] <= '9' {
			bi++
		}
		na, _ := strconv.Atoi(a[:ai])
		nb, _ := strconv.Atoi(b[:bi])
		if na != nb {
			return na < nb
		}
		a, b = a[ai:], b[bi:]
	}
	return len(a) < len(b)
}
