package inventory

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// UpdateService checks each inventory entry for missing cover art, removed
// games, and new upstream files. It runs once at startup and re-runs each time
// TriggerNow is called.
type UpdateService struct {
	inv            *Inventory
	inventoryPath  string
	client         *itchio.Client
	notify         func()
	triggerCh      chan struct{} // buffered(1): absorbs duplicate triggers
	stopCh         chan struct{}
	stopOnce       sync.Once
	running        atomic.Bool
	sources        leaf.SourceList
	scanLibrary    func() (string, error)
	artworkChanged bool
}

func (s *UpdateService) SetSources(sources leaf.SourceList) {
	s.sources = append(leaf.SourceList(nil), sources...)
}

// SetLibraryScanRequester installs the Jawaka rescan hook used after startup
// artwork repair. It is configured before Start and remains optional in tests.
func (s *UpdateService) SetLibraryScanRequester(request func() (string, error)) {
	s.scanLibrary = request
}

// NewUpdateService constructs an UpdateService. notify (may be nil) is called
// after each runCheck completes; use it to push an SDL UserEvent from the
// caller without importing SDL here.
func NewUpdateService(inv *Inventory, inventoryPath string, client *itchio.Client, notify func()) *UpdateService {
	return &UpdateService{
		inv:           inv,
		inventoryPath: inventoryPath,
		client:        client,
		notify:        notify,
		triggerCh:     make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
	}
}

// Start launches the background goroutine and runs the first check immediately.
// onDone is called after the first check completes (for tests; may be nil).
func (s *UpdateService) Start(onDone func()) {
	go func() {
		s.running.Store(true)
		s.runCheck()
		s.running.Store(false)
		if onDone != nil {
			onDone()
		}
		if s.notify != nil {
			s.notify()
		}
		for {
			select {
			case <-s.triggerCh:
				s.running.Store(true)
				s.runCheck()
				s.running.Store(false)
				if onDone != nil {
					onDone()
				}
				if s.notify != nil {
					s.notify()
				}
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop signals the goroutine to exit. Idempotent — safe to call multiple times.
func (s *UpdateService) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// TriggerNow queues a re-check. Non-blocking; a pending check absorbs the signal.
func (s *UpdateService) TriggerNow() {
	select {
	case s.triggerCh <- struct{}{}:
		logger.Info("update-svc: manual check triggered")
	default:
		logger.Debug("update-svc: trigger ignored (check already queued)")
	}
}

// IsRunning reports whether runCheck is currently executing.
func (s *UpdateService) IsRunning() bool {
	return s.running.Load()
}

// LatestCheckedAt delegates to the inventory's LatestCheckedAt.
func (s *UpdateService) LatestCheckedAt() time.Time {
	return s.inv.LatestCheckedAt()
}

func (s *UpdateService) runCheck() {
	s.artworkChanged = false
	s.inv.VerifyAndCleanWithSources(s.inventoryPath, s.sources)

	urls := s.inv.AllURLs()
	token, generation := s.client.AuthSnapshot()
	gameIDs := make(map[string]string, len(urls))
	canCheck := true
	skipped := make(map[string]bool)
	var ids []string
	for _, gameURL := range urls {
		s.repairCoverArt(gameURL)
		entry, ok := s.inv.Lookup(gameURL)
		if !ok || token == "" {
			continue
		}
		id := entry.GameID
		if id == "" {
			// A stable stored ID avoids fetching data.json on every launch.
			data, err := s.client.FetchGameData(gameURL)
			if err != nil && !isGameRemoved(err) {
				logger.Warn("update-svc: game metadata unavailable: %v", err)
				skipped[gameURL] = true
				if errors.Is(err, itchio.ErrRateLimited) {
					canCheck = false
					break
				}
			} else if err == nil && data.ID > 0 {
				id = strconv.FormatInt(data.ID, 10)
				s.inv.mu.Lock()
				if current := s.inv.Entries[gameURL]; current != nil {
					current.GameID = id
					current.IsFree = data.Pricing() != itchio.PricingPaid
				}
				s.inv.mu.Unlock()
			}
		}
		if id != "" {
			gameIDs[gameURL] = id
			ids = append(ids, id)
		}
	}

	// Include free games too: a developer may have started charging since the
	// download. Ownership keys are held only for this check and never saved.
	var keys map[string]string
	if canCheck && token != "" && len(ids) > 0 {
		var err error
		keys, err = s.client.OwnedKeysForGames(token, ids)
		if err != nil {
			logger.Warn("update-svc: ownership lookup unavailable: %v", err)
			if errors.Is(err, itchio.ErrNoAccess) {
				token = ""
			} else {
				canCheck = false
			}
		}
	}
	logger.Info("update-svc: checking %d inventory entries (signed in: %v)", len(urls), token != "")
	pending := make(map[string]upstreamResult, len(urls))
	for _, gameURL := range urls {
		if !canCheck || s.client.AuthGeneration() != generation {
			break
		}
		if skipped[gameURL] {
			continue
		}
		entry, exists := s.inv.Lookup(gameURL)
		if !exists {
			continue
		}
		result, err := s.checkGame(gameURL, gameIDs[gameURL], token, keys[gameIDs[gameURL]])
		if err == nil {
			result.installed = entry.Files
			pending[gameURL] = result
		} else {
			logger.Warn("update-svc: check failed for %s: %v", gameURL, err)
			if errors.Is(err, itchio.ErrRateLimited) {
				break
			}
		}
	}
	// Discard results even after A -> B -> A account changes; comparing tokens
	// alone would allow the first account's stale scan to publish.
	staleInstall := false
	applied := s.client.ApplyIfAuthGeneration(generation, func() {
		s.inv.mu.Lock()
		defer s.inv.mu.Unlock()
		for gameURL, result := range pending {
			entry := s.inv.Entries[gameURL]
			if entry == nil {
				continue
			}
			// A foreground install can finish while this listing is in flight.
			// Do not compare its newer version against an older server snapshot.
			if !slices.Equal(entry.Files, result.installed) {
				staleInstall = true
				continue
			}
			if result.removed {
				if entry.GameRemovedAt.IsZero() {
					entry.GameRemovedAt = time.Now()
				}
			} else {
				entry.GameRemovedAt, entry.RemovalDismissedAt = time.Time{}, time.Time{}
			}
			if result.files != nil {
				s.inv.setUpstreamFilesLocked(entry, result.source, result.files)
			} else {
				entry.UpdateCheckedAt = time.Now()
			}
		}
	})
	if !applied || staleInstall {
		logger.Debug("update-svc: check changed during a sign-in or install; scheduling fresh metadata")
		s.TriggerNow()
	}
	if err := s.inv.Save(s.inventoryPath); err != nil {
		logger.Error("update-svc: save: %v", err)
	}
	if s.artworkChanged && s.scanLibrary != nil {
		if message, err := s.scanLibrary(); err != nil {
			logger.Warn("update-svc: artwork repaired but library rescan failed: %v", err)
		} else {
			logger.Info("update-svc: artwork repaired; Leaf library rescan requested: %s", message)
		}
	}

	logger.Info("update-svc: check complete")
}

// repairCoverArt keeps Leaf's source-local artwork and ownership metadata.
func (s *UpdateService) repairCoverArt(gameURL string) {
	entry, ok := s.inv.Lookup(gameURL)
	if !ok {
		return
	}
	coverURL, files := entry.CoverURL, entry.Files

	// 1. Cover art repair.
	for _, f := range files {
		if f.ContentKind == ContentKindMusic || f.FileType == FileTypeMusic ||
			roms.IsPSXSupportExt(roms.ROMExt(f.DestPath)) {
			continue
		}
		result, migrated := s.migrateOwnedArtwork(f)
		var err error
		if !migrated {
			result, err = s.client.EnsureCoverArt(coverURL, f.DestPath)
		}
		if err != nil {
			logger.Error("update-svc: cover art repair failed for %s: %v", f.Filename, err)
			continue
		}
		if result.Path != "" {
			created := result.Created
			if !created && f.ArtworkCreated && filepath.Clean(f.ArtworkPath) == filepath.Clean(result.Path) &&
				(f.ArtworkHash == "" || f.ArtworkHash == result.SHA256) {
				created = true
			}
			s.inv.SetArtwork(gameURL, f.DestPath, result.Path, result.SHA256, created)
			if result.Created {
				s.artworkChanged = true
			}
		}
	}

}

func (s *UpdateService) migrateOwnedArtwork(file DownloadedFile) (itchio.ArtworkResult, bool) {
	expected := CanonicalArtworkPath(file.DestPath)
	oldPath := filepath.Clean(file.ArtworkPath)
	if !file.ArtworkCreated || file.ArtworkHash == "" || expected == "" || file.ArtworkPath == "" ||
		oldPath == filepath.Clean(expected) || filepath.Base(filepath.Dir(oldPath)) != ".media" {
		return itchio.ArtworkResult{}, false
	}
	identity, ok := roms.DescribeDestination(file.DestPath)
	if !ok {
		return itchio.ArtworkResult{}, false
	}
	source, ok := s.sources.ByID(identity.SourceID)
	if !ok || !source.Available() {
		return itchio.ArtworkResult{}, false
	}
	if _, err := leaf.RelativeWithin(source.Root, oldPath); err != nil {
		return itchio.ArtworkResult{}, false
	}
	if _, err := leaf.RelativeWithin(source.Root, expected); err != nil {
		return itchio.ArtworkResult{}, false
	}
	info, err := os.Lstat(oldPath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return itchio.ArtworkResult{}, false
	}
	fileHandle, err := os.Open(oldPath)
	if err != nil {
		return itchio.ArtworkResult{}, false
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, fileHandle)
	closeErr := fileHandle.Close()
	actualHash := fmt.Sprintf("%x", hash.Sum(nil))
	if copyErr != nil || closeErr != nil || actualHash != file.ArtworkHash {
		return itchio.ArtworkResult{}, false
	}
	if _, err := os.Lstat(expected); err == nil || !os.IsNotExist(err) {
		return itchio.ArtworkResult{}, false
	}
	if err := os.MkdirAll(filepath.Dir(expected), 0o755); err != nil {
		return itchio.ArtworkResult{}, false
	}
	if err := os.Rename(oldPath, expected); err != nil {
		return itchio.ArtworkResult{}, false
	}
	_ = os.Remove(filepath.Dir(oldPath))
	logger.Info("update-svc: moved app-owned artwork to canonical Leaf image root: %s", expected)
	return itchio.ArtworkResult{Path: expected, SHA256: actualHash, Created: true}, true
}

// isGameRemoved reports whether err indicates a 404 or 410 HTTP response.
func isGameRemoved(err error) bool {
	return errors.Is(err, itchio.ErrGameRemoved)
}

type upstreamResult struct {
	installed []DownloadedFile
	source    string
	files     []UpstreamFile
	removed   bool
}

// checkGame lists metadata only. No update path creates a download session,
// resolves a CDN URL, or starts the browser download_url handshake.
func (s *UpdateService) checkGame(gameURL, gameID, token, key string) (upstreamResult, error) {
	if token != "" && gameID != "" {
		uploads, err := s.client.FetchUploadsForKey(token, gameID, key)
		if err == nil {
			files := make([]UpstreamFile, 0, len(uploads))
			for _, upload := range uploads {
				if upload.Type == "html" {
					continue
				}
				files = append(files, UpstreamFile{Filename: upload.Filename, DisplayName: upload.DisplayName,
					UploadID: upload.UploadID, Fingerprint: upload.Fingerprint()})
			}
			if len(files) > 0 || key != "" {
				// A complete list you can access with no downloadable files
				// confirms removal; a replaced upload with others present
				// does not.
				return upstreamResult{source: SourceAPI, files: files, removed: len(files) == 0}, nil
			}
			// Without a download key, an empty list may only mean you cannot
			// access a paid game, including one that started charging after
			// a free install, so the inventory's free flag cannot vouch for it.
			logger.Debug("update-svc: empty upload list without a download key, checking public page")
		} else if !errors.Is(err, itchio.ErrNoAccess) && !isGameRemoved(err) {
			return upstreamResult{}, err
		} else {
			logger.Debug("update-svc: API access unavailable, checking public page: %v", err)
		}
	}
	names, err := s.client.FetchPageUploadNames(gameURL)
	if err != nil {
		if isGameRemoved(err) {
			return upstreamResult{removed: true}, nil
		}
		return upstreamResult{}, err
	}
	if len(names) == 0 {
		// Public pages may hide paid uploads. Reachability alone cannot prove
		// removal, content freshness, or the disappearance of a known upload.
		return upstreamResult{}, nil
	}
	files := make([]UpstreamFile, 0, len(names))
	for _, name := range names {
		files = append(files, UpstreamFile{Filename: name})
	}
	return upstreamResult{source: SourcePage, files: files}, nil
}
