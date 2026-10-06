package inventory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

const (
	ContentKindROM     = "rom"
	ContentKindMusic   = "music"
	ContentKindArtwork = "artwork"

	FileTypeROM   = ContentKindROM
	FileTypeMusic = ContentKindMusic
	FileTypeM3U   = "m3u" // legacy UI subtype; inventory content_kind remains ROM

	SchemaVersion = 2
)

var ErrUnsupportedSchema = errors.New("unsupported inventory schema")

// romFileExt returns the effective file extension for a ROM filename, treating
// ".p8.png" as a single compound extension rather than just ".png".
// filepath.Ext alone would give ".png" for "game.p8.png", producing a wrong
// stem ("game.p8") that fails upstream filename matching in the update service.
//
// Implementation uses zero-allocation byte-level comparison. The previous
// strings.ToLower approach allocated a full string copy on every call; this
// function is invoked from HasPendingUpdates which is called per-visible-row
// per-frame, making the allocation cost significant (~945K objects/session).
func romFileExt(filename string) string {
	const p8png = ".p8.png"
	if len(filename) >= len(p8png) {
		s := filename[len(filename)-len(p8png):]
		if s[0] == '.' &&
			(s[1] == 'p' || s[1] == 'P') &&
			s[2] == '8' &&
			s[3] == '.' &&
			(s[4] == 'p' || s[4] == 'P') &&
			(s[5] == 'n' || s[5] == 'N') &&
			(s[6] == 'g' || s[6] == 'G') {
			return p8png
		}
	}
	return filepath.Ext(filename)
}

type DownloadedFile struct {
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
	ContentKind       string    `json:"content_kind"`
	SourceID          string    `json:"source_id,omitempty"`
	RelativePath      string    `json:"relative_path,omitempty"`
	CanonicalSystem   string    `json:"canonical_system,omitempty"`
	OriginalUpload    string    `json:"original_upload,omitempty"`
	InstalledName     string    `json:"installed_name,omitempty"`
	UploadID          string    `json:"upload_id,omitempty"`
	UploadFingerprint string    `json:"upload_fingerprint,omitempty"`
	PurchaseID        string    `json:"purchase_id,omitempty"`
	ContentHash       string    `json:"content_hash,omitempty"`
	ArtworkPath       string    `json:"artwork_path,omitempty"`
	ArtworkHash       string    `json:"artwork_hash,omitempty"`
	ArtworkCreated    bool      `json:"artwork_created,omitempty"`

	// Legacy compatibility fields remain available to the existing UI while its
	// callers move to source-relative Leaf paths during later port phases.
	Filename      string    `json:"filename,omitempty"`
	DestPath      string    `json:"dest_path,omitempty"`
	DownloadedAt  time.Time `json:"downloaded_at,omitempty"`
	UnifiedName   bool      `json:"unified_name,omitempty"`
	FileType      string    `json:"file_type,omitempty"`
	SourceArchive string    `json:"source_archive,omitempty"`
}

type UpstreamFile struct {
	Filename    string    `json:"filename"`
	UploadID    string    `json:"upload_id"`
	SeenAt      time.Time `json:"seen_at"`
	IsNew       bool      `json:"is_new,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	Fingerprint string    `json:"fingerprint,omitempty"`
	Changed     bool      `json:"changed,omitempty"`
	// PreviousUploadIDs lists the IDs this file had before the developer
	// replaced it under the same name. A file installed from any of them
	// still counts as installed from this upload.
	PreviousUploadIDs []string `json:"previous_upload_ids,omitempty"`
	// DesktopOrWebOnly marks an upload that only ships desktop or web
	// builds. A new one never raises an update badge.
	DesktopOrWebOnly bool `json:"desktop_or_web_only,omitempty"`
}

const (
	SourcePage = "page"
	SourceAPI  = "api"
)

type Entry struct {
	GameID                string           `json:"game_id,omitempty"`
	GameURL               string           `json:"game_url"`
	Title                 string           `json:"title"`
	Author                string           `json:"author"`
	CoverURL              string           `json:"cover_url"`
	Files                 []DownloadedFile `json:"files"`
	VerifiedAt            time.Time        `json:"verified_at,omitempty"`
	IsFree                bool             `json:"is_free,omitempty"`
	KnownUpstreamFiles    []UpstreamFile   `json:"known_upstream_files,omitempty"`
	UpstreamSource        string           `json:"upstream_source,omitempty"`
	UpdateCheckedAt       time.Time        `json:"update_checked_at,omitempty"`
	UpdateDismissedAt     time.Time        `json:"update_dismissed_at,omitempty"`
	GameRemovedAt         time.Time        `json:"game_removed_at,omitempty"`
	RemovalDismissedAt    time.Time        `json:"removal_dismissed_at,omitempty"`
	UnifiedNamingDisabled bool             `json:"unified_naming_disabled,omitempty"`
	// AcknowledgedUploads maps an upload ID to the fingerprint of its last
	// complete install ("" for a download without one). Update checks compare
	// the listed version against it rather than against every file ever
	// recorded for the upload.
	AcknowledgedUploads map[string]string `json:"acknowledged_uploads,omitempty"`
	// LeftoverFiles holds the DestPaths of files recorded for an upload that
	// its latest complete reinstall did not write: left over from an older
	// version. They stay on disk and in Files until you remove them.
	LeftoverFiles []string `json:"leftover_files,omitempty"`
}

// UploadInstall describes one upload whose install finished.
type UploadInstall struct {
	UploadID    string
	Filename    string // the upload's filename; matches records without an ID
	Fingerprint string // "" for a download without version metadata
	// Written lists every DestPath this install wrote or found identical.
	Written []string
	// Replaces reports whether this install would have rewritten an older
	// file of the same upload had that file still been in it. A partial
	// install, such as one build picked from an archive, returns false for
	// files it did not choose. nil means the install replaces every file.
	Replaces func(DownloadedFile) bool
	// Listing is the list of uploads the install was chosen from, and
	// ListingSource where it came from (SourceAPI or SourcePage). When the
	// game has no update baseline from that source yet, the listing becomes
	// one, so the first background check compares instead of starting over.
	Listing       []UpstreamFile
	ListingSource string
}

type Inventory struct {
	mu      sync.Mutex
	Version int               `json:"version"`
	Entries map[string]*Entry `json:"entries"`
}

func emptyInventory() *Inventory {
	return &Inventory{Version: SchemaVersion, Entries: make(map[string]*Entry)}
}

func backupInventory(path, label string) (string, error) {
	base := path + "." + label + ".bak"
	backup := base
	for n := 1; ; n++ {
		if _, err := os.Stat(backup); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", fmt.Errorf("inspect inventory backup: %w", err)
		}
		backup = fmt.Sprintf("%s.%d", base, n)
	}
	if err := os.Rename(path, backup); err != nil {
		return "", fmt.Errorf("backup inventory: %w", err)
	}
	return backup, nil
}

// Load reads a current-schema inventory. Older, unknown, and malformed files
// are moved aside without partial interpretation so a fresh Leaf inventory can
// start safely.
func Load(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Debug("inventory: no file at %s, starting empty", path)
		} else {
			logger.Warn("inventory: read error at %s: %v, starting empty", path, err)
		}
		if os.IsNotExist(err) {
			return emptyInventory(), nil
		}
		return emptyInventory(), fmt.Errorf("read inventory: %w", err)
	}
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		backup, backupErr := backupInventory(path, "corrupt")
		if backupErr != nil {
			return emptyInventory(), fmt.Errorf("%w: malformed inventory (%v); %v", ErrUnsupportedSchema, err, backupErr)
		}
		logger.Warn("inventory: moved malformed file to %s: %v", backup, err)
		return emptyInventory(), fmt.Errorf("%w: malformed inventory backed up to %s", ErrUnsupportedSchema, backup)
	}
	if inv.Version != SchemaVersion {
		backup, backupErr := backupInventory(path, fmt.Sprintf("schema-%d", inv.Version))
		if backupErr != nil {
			return emptyInventory(), fmt.Errorf("%w: version %d; %v", ErrUnsupportedSchema, inv.Version, backupErr)
		}
		logger.Warn("inventory: moved schema %d file to %s", inv.Version, backup)
		return emptyInventory(), fmt.Errorf("%w: version %d backed up to %s", ErrUnsupportedSchema, inv.Version, backup)
	}
	if inv.Entries == nil {
		inv.Entries = make(map[string]*Entry)
	}
	logger.Debug("inventory: loaded %d entries from %s", len(inv.Entries), path)
	return &inv, nil
}

// Save writes the inventory to path atomically (write to .tmp then rename).
func (inv *Inventory) Save(path string) error {
	lease, err := leaf.BeginOperation(context.Background(), "inventory commit", false)
	if err != nil {
		return fmt.Errorf("protect inventory commit: %w", err)
	}
	defer lease.Release()
	inv.mu.Lock()
	inv.Version = SchemaVersion
	data, err := json.MarshalIndent(inv, "", "  ")
	count := len(inv.Entries)
	inv.mu.Unlock()
	if err != nil {
		return fmt.Errorf("marshal inventory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write inventory tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename inventory: %w", err)
	}
	logger.Debug("inventory: saved %d entries to %s", count, path)
	return nil
}

// Add upserts an entry and appends a file, deduplicating by DestPath.
func (inv *Inventory) Add(gameURL string, e Entry, file DownloadedFile) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.Version = SchemaVersion
	if file.ContentKind == "" {
		switch file.FileType {
		case FileTypeMusic:
			file.ContentKind = ContentKindMusic
		default:
			file.ContentKind = ContentKindROM
		}
	}
	if file.OriginalUpload == "" {
		file.OriginalUpload = file.Filename
	}
	if file.InstalledName == "" && file.DestPath != "" {
		file.InstalledName = filepath.Base(file.DestPath)
	}
	if file.UpdatedAt.IsZero() {
		file.UpdatedAt = file.DownloadedAt
	}
	if identity, ok := roms.DescribeDestination(file.DestPath); ok {
		if file.SourceID == "" {
			file.SourceID = identity.SourceID
		}
		if file.RelativePath == "" {
			file.RelativePath = identity.RelativePath
		}
		if file.CanonicalSystem == "" {
			file.CanonicalSystem = identity.CanonicalSystem
		}
	}
	existing, ok := inv.Entries[gameURL]
	if !ok {
		entry := &Entry{
			GameID:   e.GameID,
			GameURL:  gameURL,
			Title:    e.Title,
			Author:   e.Author,
			CoverURL: e.CoverURL,
			IsFree:   e.IsFree,
		}
		inv.Entries[gameURL] = entry
		existing = entry
	} else {
		if e.GameID != "" {
			existing.GameID = e.GameID
		}
		existing.Title = e.Title
		existing.Author = e.Author
		existing.CoverURL = e.CoverURL
	}
	replaced := false
	for i, f := range existing.Files {
		if f.DestPath == file.DestPath || f.Filename == file.Filename {
			if file.ArtworkPath == "" {
				file.ArtworkPath = f.ArtworkPath
				file.ArtworkHash = f.ArtworkHash
				file.ArtworkCreated = f.ArtworkCreated
			}
			existing.Files[i] = file
			replaced = true
			break
		}
	}
	if !replaced {
		existing.Files = append(existing.Files, file)
	}
	// Recording one file does not acknowledge an update: the install may
	// still fail. CommitUploadInstall does once the whole upload is in.
}

// CommitUploadInstall acknowledges one upload after every file of its install
// is recorded. It clears the upload's update unless a newer version was seen
// while the download ran, and records files of the same upload that the
// install did not write as left over from an older version. Those files are
// never deleted here.
func (inv *Inventory) CommitUploadInstall(gameURL string, install UploadInstall) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return
	}
	if install.ListingSource != "" && install.Listing != nil &&
		(e.UpdateCheckedAt.IsZero() || e.UpstreamSource != install.ListingSource) {
		inv.setUpstreamFilesLocked(e, install.ListingSource, install.Listing)
	}
	identity := UpstreamFile{Filename: install.Filename, UploadID: install.UploadID}
	if install.UploadID != "" {
		if e.AcknowledgedUploads == nil {
			e.AcknowledgedUploads = make(map[string]string)
		}
		e.AcknowledgedUploads[install.UploadID] = install.Fingerprint
	}
	for index := range e.KnownUpstreamFiles {
		upload := &e.KnownUpstreamFiles[index]
		if !sameUpload(identity, *upload) {
			continue
		}
		// An install that started before a newer version was listed does
		// not acknowledge that version.
		if newer, conclusive := fingerprintChanged(install.Fingerprint, upload.Fingerprint); !(newer && conclusive) {
			upload.Changed, upload.IsNew = false, false
		}
	}

	written := make(map[string]bool, len(install.Written))
	for _, path := range install.Written {
		written[filepath.Clean(path)] = true
	}
	leftover := make([]string, 0, len(e.LeftoverFiles))
	for _, path := range e.LeftoverFiles {
		if !written[filepath.Clean(path)] {
			leftover = append(leftover, path)
		}
	}
	for _, file := range e.Files {
		if file.DestPath == "" || written[filepath.Clean(file.DestPath)] || !fileMatchesUpload(file, identity) ||
			slices.Contains(leftover, file.DestPath) {
			continue
		}
		older, conclusive := fingerprintChanged(file.UploadFingerprint, install.Fingerprint)
		if (older && conclusive) || install.Replaces == nil || install.Replaces(file) {
			leftover = append(leftover, file.DestPath)
		}
	}
	e.LeftoverFiles = nil
	if len(leftover) > 0 {
		e.LeftoverFiles = leftover
	}
}

// sameUpload matches by upload ID when both sides have one, else by name.
func sameUpload(a, b UpstreamFile) bool {
	if a.UploadID != "" && b.UploadID != "" {
		return a.UploadID == b.UploadID
	}
	return fileMatchesUpload(DownloadedFile{OriginalUpload: a.Filename}, b)
}

// pruneLeftoverFilesLocked drops left over paths that no longer have a file
// record, after a removal or a clean-up of missing files.
func pruneLeftoverFilesLocked(e *Entry) {
	if len(e.LeftoverFiles) == 0 {
		return
	}
	kept := e.LeftoverFiles[:0]
	for _, path := range e.LeftoverFiles {
		if slices.ContainsFunc(e.Files, func(file DownloadedFile) bool { return file.DestPath == path }) {
			kept = append(kept, path)
		}
	}
	e.LeftoverFiles = nil
	if len(kept) > 0 {
		e.LeftoverFiles = kept
	}
}

// Remove deletes the entry for gameURL.
func (inv *Inventory) Remove(gameURL string) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	delete(inv.Entries, gameURL)
}

// Lookup returns a deep copy of the entry for gameURL.
func (inv *Inventory) Lookup(gameURL string) (Entry, bool) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return Entry{}, false
	}
	snap := *e
	snap.Files = append([]DownloadedFile(nil), e.Files...)
	snap.KnownUpstreamFiles = cloneUpstreamFiles(e.KnownUpstreamFiles)
	snap.AcknowledgedUploads = maps.Clone(e.AcknowledgedUploads)
	snap.LeftoverFiles = slices.Clone(e.LeftoverFiles)
	return snap, true
}

// ExistingDestPath returns the dest_path of an already-downloaded file matching
// the given upload filename, or "" if not found. Used to overwrite an existing
// download rather than creating a duplicate.
func (inv *Inventory) ExistingDestPath(gameURL, uploadFilename string) string {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return ""
	}
	for _, f := range e.Files {
		if f.Filename == uploadFilename {
			return f.DestPath
		}
	}
	return ""
}

// IsPresent reports whether gameURL has an inventory entry with at least one file.
// Assumes VerifyAndClean has already removed entries whose files are gone from disk.
func (inv *Inventory) IsPresent(gameURL string) bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return false
	}
	return len(e.Files) > 0
}

// RemoveFile removes the DownloadedFile with the given destPath from the entry for gameURL.
// Returns true when no files remain (the entry is also removed from the inventory).
func (inv *Inventory) RemoveFile(gameURL, destPath string) bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	entry, ok := inv.Entries[gameURL]
	if !ok {
		return true
	}
	var remaining []DownloadedFile
	for _, f := range entry.Files {
		if f.DestPath != destPath {
			remaining = append(remaining, f)
		}
	}
	entry.Files = remaining
	if len(entry.Files) == 0 {
		delete(inv.Entries, gameURL)
		return true
	}
	pruneLeftoverFilesLocked(entry)
	return false
}

// VerifyAndClean walks all entries, removes DownloadedFile rows whose DestPath no
// longer exists on disk, deduplicates rows with the same Filename (keeping the
// most recently downloaded), removes Entry values with no remaining files, saves
// if any changes were made, and returns the count of removed DownloadedFile rows.
func (inv *Inventory) VerifyAndClean(path string) int {
	return inv.verifyAndClean(path, nil)
}

// VerifyAndCleanWithSources keeps entries that live on a currently unavailable
// removable source. Absence of a card is not evidence that its files were
// deleted; those rows remain visible but immutable until the source returns.
func (inv *Inventory) VerifyAndCleanWithSources(path string, sources leaf.SourceList) int {
	return inv.verifyAndClean(path, sources)
}

// RepairArchiveRootROMs repairs files written by the pre-0.1.0 archive picker
// join bug. That bug could place an app-owned extracted ROM directly in a
// source's Roms directory when the selected canonical directory did not end in
// a path separator. Only archive-backed inventory rows in exactly that shape
// are eligible; user files and occupied canonical targets are left untouched.
func (inv *Inventory) RepairArchiveRootROMs(path string, sources leaf.SourceList) int {
	repaired := 0
	inv.mu.Lock()
	for _, entry := range inv.Entries {
		for index := range entry.Files {
			file := entry.Files[index]
			if file.ContentKind != ContentKindROM || file.SourceArchive == "" ||
				file.CanonicalSystem != "" || file.DestPath == "" {
				continue
			}
			identity, ok := roms.DescribeDestination(file.DestPath)
			if !ok || identity.SourceID == "" || identity.CanonicalSystem != "" ||
				filepath.ToSlash(filepath.Dir(identity.RelativePath)) != "Roms" {
				continue
			}
			source, ok := sources.ByID(identity.SourceID)
			if !ok || !source.Available() {
				continue
			}
			canonical, ok := leaf.CanonicalSystemForExtension(roms.ROMExt(file.DestPath))
			if !ok {
				continue
			}
			targetDir := roms.SourceSystemDir(identity.SourceID, canonical)
			if targetDir == "" {
				continue
			}
			target := filepath.Join(targetDir, filepath.Base(file.DestPath))
			if _, err := os.Stat(target); err == nil || !os.IsNotExist(err) {
				continue
			}
			info, err := os.Stat(file.DestPath)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				logger.Warn("inventory: create archive repair destination: %v", err)
				continue
			}
			if err := os.Rename(file.DestPath, target); err != nil {
				logger.Warn("inventory: move misplaced archive ROM: %v", err)
				continue
			}
			targetIdentity, ok := roms.DescribeDestination(target)
			if !ok || targetIdentity.CanonicalSystem != canonical {
				if rollbackErr := os.Rename(target, file.DestPath); rollbackErr != nil {
					logger.Error("inventory: archive repair rollback failed: %v", rollbackErr)
				}
				continue
			}
			file.DestPath = target
			file.RelativePath = targetIdentity.RelativePath
			file.CanonicalSystem = targetIdentity.CanonicalSystem
			file.InstalledName = filepath.Base(target)
			file.Filename = filepath.Base(target)
			entry.Files[index] = file
			repaired++
			logger.Info("inventory: repaired archive ROM destination %s", target)
		}
	}
	inv.mu.Unlock()
	if repaired > 0 {
		if err := inv.Save(path); err != nil {
			logger.Error("inventory: failed to save archive destination repairs: %v", err)
		}
	}
	return repaired
}

func (inv *Inventory) verifyAndClean(path string, sources leaf.SourceList) int {
	removed := 0
	changed := false
	inv.mu.Lock()
	for gameURL, entry := range inv.Entries {
		// Pass 1: drop files missing from disk.
		var present []DownloadedFile
		for _, f := range entry.Files {
			if sourceUnavailableForFile(f, sources) {
				present = append(present, f)
				continue
			}
			if _, err := os.Stat(f.DestPath); err == nil {
				present = append(present, f)
			} else {
				logger.Debug("inventory: removing stale file=%s", f.DestPath)
				removed++
				changed = true
			}
		}

		// Pass 2: deduplicate by Filename, keeping the most recently downloaded.
		best := make(map[string]DownloadedFile, len(present))
		for _, f := range present {
			if cur, ok := best[f.Filename]; !ok || f.DownloadedAt.After(cur.DownloadedAt) {
				best[f.Filename] = f
			}
		}
		if len(best) < len(present) {
			dropped := len(present) - len(best)
			logger.Debug("inventory: deduplicating %d file(s) for game=%q", dropped, entry.Title)
			removed += dropped
			changed = true
		}
		var kept []DownloadedFile
		for _, f := range best {
			kept = append(kept, f)
		}

		if len(kept) == 0 {
			logger.Debug("inventory: removing empty entry game=%q", entry.Title)
			delete(inv.Entries, gameURL)
		} else {
			entry.Files = kept
			entry.VerifiedAt = time.Now()
			pruneLeftoverFilesLocked(entry)
		}
	}
	inv.mu.Unlock()
	logger.Info("inventory: cleaned %d stale/duplicate file(s)", removed)
	if changed {
		if err := inv.Save(path); err != nil {
			logger.Error("inventory: failed to save after clean: %v", err)
		}
	}
	return removed
}

func sourceUnavailableForFile(file DownloadedFile, sources leaf.SourceList) bool {
	if len(sources) == 0 {
		return false
	}
	sourceID := file.SourceID
	if sourceID == "" {
		if identity, ok := roms.DescribeDestination(file.DestPath); ok {
			sourceID = identity.SourceID
		}
	}
	if sourceID == "" {
		return false
	}
	source, ok := sources.ByID(sourceID)
	return !ok || !source.Available()
}

// fileMatchesUpload prefers stable IDs; name fallback supports legacy records
// and public pages, including original archives and format-picker suffixes.
func fileMatchesUpload(file DownloadedFile, upload UpstreamFile) bool {
	if file.UploadID != "" && upload.UploadID != "" {
		return file.UploadID == upload.UploadID
	}
	for _, name := range []string{file.OriginalUpload, file.SourceArchive, file.Filename} {
		if name == "" {
			continue
		}
		stem := strings.TrimSuffix(name, romFileExt(name))
		if name == upload.Filename || stem == upload.Filename ||
			(upload.DisplayName != "" && (name == upload.DisplayName || stem == upload.DisplayName)) {
			return true
		}
	}
	return false
}

// fingerprintChanged compares the fingerprints of two listings of one upload.
// conclusive is false when they cannot be compared: one is missing, or only
// the form changed, as when itch.io lists an md5 for an upload first seen
// with a timestamp. A timestamp also moves on metadata-only edits, so two
// timestamp fingerprints differ only when the size differs; a build ID
// appearing where there was none means a pushed build.
func fingerprintChanged(old, current string) (changed, conclusive bool) {
	if old == "" || current == "" {
		return false, false
	}
	if old == current {
		return false, true
	}
	oldForm, oldValue, _ := strings.Cut(old, ":")
	currentForm, currentValue, _ := strings.Cut(current, ":")
	switch {
	case oldForm == "upd" && currentForm == "upd":
		return timestampFingerprintSize(oldValue) != timestampFingerprintSize(currentValue), true
	case oldForm == currentForm:
		return true, true
	case oldForm == "upd" && currentForm == "build":
		return true, true
	default:
		return false, false
	}
}

func timestampFingerprintSize(value string) string {
	if index := strings.LastIndexByte(value, '/'); index >= 0 {
		return value[index+1:]
	}
	return value
}

// HasPendingUpdates reports new uploads and known uploads whose content changed.
func (inv *Inventory) HasPendingUpdates(gameURL string) bool {
	return len(inv.PendingUpdateFiles(gameURL)) > 0
}

// PendingUpdateFiles returns a snapshot suitable for explaining update badges.
func (inv *Inventory) PendingUpdateFiles(gameURL string) []UpstreamFile {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return nil
	}
	var pending []UpstreamFile
	for _, upload := range e.KnownUpstreamFiles {
		if !upload.SeenAt.After(e.UpdateDismissedAt) {
			continue
		}
		installed := false
		for _, file := range e.Files {
			if fileInstalledFrom(file, upload) {
				installed = true
				break
			}
		}
		// A change matters only to an upload you installed; a new desktop or
		// web build never does.
		if (upload.Changed && installed) || (upload.IsNew && !installed && !upload.DesktopOrWebOnly) {
			pending = append(pending, cloneUpstreamFile(upload))
		}
	}
	return pending
}

// fileInstalledFrom also accepts a file installed from an upload ID that the
// developer has since replaced under the same name.
func fileInstalledFrom(file DownloadedFile, upload UpstreamFile) bool {
	if fileMatchesUpload(file, upload) {
		return true
	}
	return file.UploadID != "" && slices.Contains(upload.PreviousUploadIDs, file.UploadID)
}

func cloneUpstreamFile(file UpstreamFile) UpstreamFile {
	file.PreviousUploadIDs = slices.Clone(file.PreviousUploadIDs)
	return file
}

func cloneUpstreamFiles(files []UpstreamFile) []UpstreamFile {
	if files == nil {
		return nil
	}
	out := make([]UpstreamFile, len(files))
	for index, file := range files {
		out[index] = cloneUpstreamFile(file)
	}
	return out
}

// IsRemoved returns true when the game was detected as 404 upstream and the
// user has not yet dismissed the warning (or the warning reappeared after a
// subsequent removal).
func (inv *Inventory) IsRemoved(gameURL string) bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return false
	}
	return !e.GameRemovedAt.IsZero() &&
		(e.RemovalDismissedAt.IsZero() || e.GameRemovedAt.After(e.RemovalDismissedAt))
}

// DismissUpdate sets UpdateDismissedAt to now, suppressing [UP] for all
// upstream files seen before this moment.
func (inv *Inventory) DismissUpdate(gameURL string) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return
	}
	e.UpdateDismissedAt = time.Now()
}

// DismissRemoval sets RemovalDismissedAt to now, suppressing [!] until the
// game is re-detected as removed after reappearing upstream.
func (inv *Inventory) DismissRemoval(gameURL string) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return
	}
	e.RemovalDismissedAt = time.Now()
}

// MarkRemoved sets GameRemovedAt to now only on the first detection
// (idempotent: does nothing if GameRemovedAt is already set).
func (inv *Inventory) MarkRemoved(gameURL string) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok || !e.GameRemovedAt.IsZero() {
		return
	}
	e.GameRemovedAt = time.Now()
}

// MarkReachable clears GameRemovedAt and RemovalDismissedAt, returning the
// entry to a clean slate when a previously-removed game becomes reachable again.
func (inv *Inventory) MarkReachable(gameURL string) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return
	}
	e.GameRemovedAt = time.Time{}
	e.RemovalDismissedAt = time.Time{}
}

// SetUpstreamFiles records a public-page list; retained for callers that do
// not have API metadata.
func (inv *Inventory) SetUpstreamFiles(gameURL string, files []UpstreamFile) {
	inv.SetUpstreamFilesFrom(gameURL, SourcePage, files)
}

// SetUpstreamFilesFrom compares listings only within the same source. Legacy
// signed download pages, public pages and the API expose different files, so
// switching sources establishes a baseline without inventing updates.
func (inv *Inventory) SetUpstreamFilesFrom(gameURL, source string, files []UpstreamFile) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return
	}
	inv.setUpstreamFilesLocked(e, source, files)
}

func (inv *Inventory) setUpstreamFilesLocked(e *Entry, source string, files []UpstreamFile) {
	// The first listing, or the first from another source, is a baseline:
	// the sources expose different files and metadata. An upload matched by
	// ID is the same upload in either, so it is still compared.
	firstCheck := e.UpdateCheckedAt.IsZero()
	baseline := firstCheck || e.UpstreamSource != source
	byID := make(map[string]int, len(e.KnownUpstreamFiles))
	byName := make(map[string]int, len(e.KnownUpstreamFiles)*2)
	for index, file := range e.KnownUpstreamFiles {
		if file.UploadID != "" {
			byID[file.UploadID] = index
		}
		byName[file.Filename] = index
		if file.DisplayName != "" {
			byName[file.DisplayName] = index
		}
	}
	now := time.Now()
	files = cloneUpstreamFiles(files)
	listedIDs := make(map[string]bool, len(files))
	for _, file := range files {
		if file.UploadID != "" {
			listedIDs[file.UploadID] = true
		}
	}
	// A name match is a replacement only when the earlier upload is gone; a
	// second upload under a name that is still listed is a new upload.
	byNameIfGone := func(name string) (int, bool) {
		index, ok := byName[name]
		if ok && listedIDs[e.KnownUpstreamFiles[index].UploadID] {
			return 0, false
		}
		return index, ok
	}
	matched := make(map[int]bool, len(files))
	knownByID := make([]bool, len(files))
	for i := range files {
		file := &files[i]
		index, known := 0, false
		if file.UploadID != "" {
			index, known = byID[file.UploadID]
			knownByID[i] = known
		}
		if !known {
			index, known = byNameIfGone(file.Filename)
		}
		if !known && file.DisplayName != "" {
			index, known = byNameIfGone(file.DisplayName)
		}
		file.IsNew, file.Changed, file.SeenAt = false, false, now
		if known {
			prior := e.KnownUpstreamFiles[index]
			matched[index] = true
			file.IsNew, file.Changed, file.SeenAt = prior.IsNew, prior.Changed, prior.SeenAt
			file.PreviousUploadIDs = slices.Clone(prior.PreviousUploadIDs)
			if file.UploadID == "" {
				file.UploadID = prior.UploadID
			}
			compare := !firstCheck && (!baseline || knownByID[i])
			if file.Fingerprint == "" {
				file.Fingerprint = prior.Fingerprint
			} else if changed, _ := fingerprintChanged(prior.Fingerprint, file.Fingerprint); compare && changed {
				file.Changed, file.SeenAt = true, now
			}
			if file.UploadID != "" && prior.UploadID != "" && file.UploadID != prior.UploadID {
				// Upload IDs mean the same in every source, so a replacement
				// counts even on a baseline. Keep the replaced ID so its
				// installed files still match.
				if !slices.Contains(file.PreviousUploadIDs, prior.UploadID) {
					file.PreviousUploadIDs = append(file.PreviousUploadIDs, prior.UploadID)
				}
				file.Changed, file.SeenAt = true, now
			}
		} else if !baseline {
			file.IsNew = true
		}
		// The version of the last complete install also proves a change
		// before the first background baseline, and clears one it installed.
		if acknowledged, ok := e.AcknowledgedUploads[file.UploadID]; ok && file.UploadID != "" {
			if changed, conclusive := fingerprintChanged(acknowledged, file.Fingerprint); conclusive && !changed {
				file.IsNew, file.Changed = false, false
			} else if conclusive && source == SourceAPI && !file.Changed {
				file.Changed, file.SeenAt = true, now
			}
		} else if file.Fingerprint != "" {
			// Inventories from before per-upload acknowledgement: every
			// tracked file must carry the listed version.
			anyInstalled, allCurrent, hasOlderVersion := false, true, false
			for _, installed := range e.Files {
				if !fileMatchesUpload(installed, *file) || slices.Contains(e.LeftoverFiles, installed.DestPath) {
					continue
				}
				anyInstalled = true
				if changed, conclusive := fingerprintChanged(installed.UploadFingerprint, file.Fingerprint); changed || !conclusive {
					allCurrent = false
					hasOlderVersion = hasOlderVersion || changed
				}
			}
			if anyInstalled && allCurrent {
				file.IsNew, file.Changed = false, false
			} else if source == SourceAPI && hasOlderVersion && !file.Changed {
				file.Changed, file.SeenAt = true, now
			}
		}

	}
	if source == SourceAPI && baseline {
		markReplacementsOfMissingUploadsLocked(e, files, knownByID, now)
	}
	if source == SourcePage {
		// A public page can hide paid/API uploads. Omission cannot acknowledge
		// a known update; only an authoritative API list may prune it.
		for index, prior := range e.KnownUpstreamFiles {
			if !matched[index] && (prior.Changed || prior.IsNew) {
				files = append(files, prior)
			}
		}
	}
	e.KnownUpstreamFiles = files
	e.UpstreamSource = source
	e.UpdateCheckedAt = now
}

// markReplacementsOfMissingUploadsLocked handles an API baseline, which has
// no earlier listing to compare: an installed upload missing from the
// complete list was replaced, so an upload of the same kind that you have
// not installed and that was not known before is new.
func markReplacementsOfMissingUploadsLocked(e *Entry, files []UpstreamFile, knownByID []bool, now time.Time) {
	present := make(map[string]bool, len(files))
	for _, file := range files {
		present[file.UploadID] = true
		for _, id := range file.PreviousUploadIDs {
			present[id] = true
		}
	}
	missingKinds := make(map[string]bool)
	for _, installed := range e.Files {
		if installed.UploadID == "" || present[installed.UploadID] || slices.Contains(e.LeftoverFiles, installed.DestPath) {
			continue
		}
		name := installed.OriginalUpload
		if name == "" {
			name = installed.Filename
		}
		if kind := uploadKind(name); kind != "" {
			missingKinds[kind] = true
		}
	}
	if len(missingKinds) == 0 {
		return
	}
	for i := range files {
		file := &files[i]
		if knownByID[i] || file.UploadID == "" || !missingKinds[uploadKind(file.Filename)] {
			continue
		}
		installed := slices.ContainsFunc(e.Files, func(installed DownloadedFile) bool { return fileInstalledFrom(installed, *file) })
		if !installed && !file.IsNew {
			file.IsNew, file.SeenAt = true, now
		}
	}
}

// uploadKind groups uploads that can replace one another: the same system
// extension, or any archive for an archive.
func uploadKind(name string) string {
	ext := strings.ToLower(romFileExt(name))
	switch ext {
	case ".zip", ".7z", ".rar":
		return "archive"
	}
	return ext
}

// LatestCheckedAt returns the most recent UpdateCheckedAt across all entries,
// or the zero time if no checks have run.
func (inv *Inventory) LatestCheckedAt() time.Time {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	var latest time.Time
	for _, e := range inv.Entries {
		if e.UpdateCheckedAt.After(latest) {
			latest = e.UpdateCheckedAt
		}
	}
	return latest
}

// AllURLs returns the game URLs of all inventory entries.
func (inv *Inventory) AllURLs() []string {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	urls := make([]string, 0, len(inv.Entries))
	for url := range inv.Entries {
		urls = append(urls, url)
	}
	return urls
}

// CoverArtPath returns the source-local canonical Jawaka image path for a
// downloaded ROM, mirroring the naming convention used by
// itchio.DownloadCoverArt.
// Returns "" if either argument is empty.
func CoverArtPath(coverURL, romDestPath string) string {
	if coverURL == "" || romDestPath == "" {
		return ""
	}
	return CanonicalArtworkPath(romDestPath)
}

func CanonicalArtworkPath(romDestPath string) string {
	return roms.ArtworkPath(romDestPath)
}

// SetUnifiedNamingDisabled sets the per-game unified-naming opt-out flag.
func (inv *Inventory) SetUnifiedNamingDisabled(gameURL string, disabled bool) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return
	}
	e.UnifiedNamingDisabled = disabled
}

// UpdateFile replaces the DownloadedFile whose DestPath matches oldDestPath.
// Returns false if the game URL or file is not found.
func (inv *Inventory) UpdateFile(gameURL, oldDestPath string, file DownloadedFile) bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return false
	}
	for i, f := range e.Files {
		if f.DestPath == oldDestPath {
			e.Files[i] = file
			if index := slices.Index(e.LeftoverFiles, oldDestPath); index >= 0 {
				e.LeftoverFiles[index] = file.DestPath
			}
			return true
		}
	}
	return false
}

// SetArtwork records the exact launcher-art path, hash, and ownership for one
// managed file without changing its ROM/music identity.
func (inv *Inventory) SetArtwork(gameURL, destPath, artPath, artHash string, created bool) bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	e, ok := inv.Entries[gameURL]
	if !ok {
		return false
	}
	for index := range e.Files {
		if filepath.Clean(e.Files[index].DestPath) == filepath.Clean(destPath) {
			e.Files[index].ArtworkPath = artPath
			e.Files[index].ArtworkHash = artHash
			e.Files[index].ArtworkCreated = created
			return true
		}
	}
	return false
}

// ArtworkPathFor returns the recorded artwork path, falling back to the
// canonical path for inventories written before artwork metadata existed.
func ArtworkPathFor(coverURL string, file DownloadedFile) string {
	if file.ArtworkPath != "" {
		return file.ArtworkPath
	}
	return CoverArtPath(coverURL, file.DestPath)
}

// ArtworkReferencedOutside reports whether another managed file still owns the
// same artwork path after excluding a pending deletion set.
func (inv *Inventory) ArtworkReferencedOutside(artPath string, excluding []DownloadedFile) bool {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	excluded := make(map[string]bool, len(excluding))
	for _, file := range excluding {
		excluded[filepath.Clean(file.DestPath)] = true
	}
	for _, entry := range inv.Entries {
		for _, file := range entry.Files {
			if excluded[filepath.Clean(file.DestPath)] || !file.ArtworkCreated {
				continue
			}
			if filepath.Clean(ArtworkPathFor(entry.CoverURL, file)) == filepath.Clean(artPath) {
				return true
			}
		}
	}
	return false
}
