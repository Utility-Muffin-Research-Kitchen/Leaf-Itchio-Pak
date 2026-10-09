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

// automaticCheckInterval is how long a launch or an account change trusts an
// entry's last check. Update Inventory checks every entry regardless.
const automaticCheckInterval = 6 * time.Hour

// backgroundRequestInterval spaces the requests of one check, so a scan of
// a large inventory does not trip itch.io's rate limit, whose cooldown also
// delays the requests you make in the foreground.
var backgroundRequestInterval = time.Second

// UpdateService checks each inventory entry for missing cover art, removed
// games, and new upstream files. It runs once at startup, again for each
// TriggerNow or CheckAllNow, and waits while a download runs.
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

	mu        sync.Mutex
	queued    checkRequest // what the next run checks
	downloads int          // downloads running; checks wait for zero

	requestInterval time.Duration
	lastRequest     time.Time // owned by the running check
}

// checkRequest selects the entries one run checks.
type checkRequest struct {
	all       bool            // Update Inventory: every entry
	automatic bool            // launch or account change: entries not checked recently
	games     map[string]bool // re-check these games after an install
}

func (request checkRequest) empty() bool {
	return !request.all && !request.automatic && len(request.games) == 0
}

func (request *checkRequest) merge(other checkRequest) {
	request.all = request.all || other.all
	request.automatic = request.automatic || other.automatic
	for game := range other.games {
		if request.games == nil {
			request.games = make(map[string]bool)
		}
		request.games[game] = true
	}
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
		inv:             inv,
		inventoryPath:   inventoryPath,
		client:          client,
		notify:          notify,
		triggerCh:       make(chan struct{}, 1),
		stopCh:          make(chan struct{}),
		requestInterval: backgroundRequestInterval,
	}
}

// Start launches the background goroutine and runs the launch check
// immediately. onDone is called after each run (for tests; may be nil).
func (s *UpdateService) Start(onDone func()) {
	s.mu.Lock()
	s.queued.merge(checkRequest{automatic: true})
	s.mu.Unlock()
	go func() {
		run := func() {
			request := s.takeRequest()
			if request.empty() {
				return
			}
			s.running.Store(true)
			s.runCheck(request)
			s.running.Store(false)
			if onDone != nil {
				onDone()
			}
			if s.notify != nil {
				s.notify()
			}
		}
		run()
		for {
			select {
			case <-s.triggerCh:
				run()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *UpdateService) takeRequest() checkRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	request := s.queued
	s.queued = checkRequest{}
	return request
}

// request queues a check, which starts once no download is running.
func (s *UpdateService) request(request checkRequest) {
	s.mu.Lock()
	s.queued.merge(request)
	downloading := s.downloads > 0
	s.mu.Unlock()
	if !downloading {
		s.signal()
	}
}

func (s *UpdateService) signal() {
	select {
	case s.triggerCh <- struct{}{}:
	default:
		logger.Debug("update-svc: trigger ignored (check already queued)")
	}
}

// DownloadStarted pauses background checks until the returned function is
// called, once the download has finished, failed, or been cancelled. A check
// queued meanwhile runs then. Calling the function again does nothing.
func (s *UpdateService) DownloadStarted() (finished func()) {
	s.mu.Lock()
	s.downloads++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.downloads--
			resume := s.downloads == 0 && !s.queued.empty()
			s.mu.Unlock()
			if resume {
				s.signal()
			}
		})
	}
}

// deferIfDownloading queues request for after the running downloads and
// reports whether it did.
func (s *UpdateService) deferIfDownloading(request checkRequest) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.downloads == 0 {
		return false
	}
	s.queued.merge(request)
	return true
}

// pace waits until the next background request may start. It reports false
// when the service stops meanwhile.
func (s *UpdateService) pace() bool {
	if s.requestInterval > 0 && !s.lastRequest.IsZero() {
		if wait := time.Until(s.lastRequest.Add(s.requestInterval)); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-s.stopCh:
				timer.Stop()
				return false
			}
		}
	}
	s.lastRequest = time.Now()
	return true
}

// due reports whether request covers entry. An automatic check skips an
// entry checked in the last six hours, unless signing in or out since then
// made the other source available.
func (request checkRequest) due(gameURL string, entry Entry, signedIn bool, now time.Time) bool {
	if request.all || request.games[gameURL] {
		return true
	}
	if !request.automatic {
		return false
	}
	age := now.Sub(entry.UpdateCheckedAt)
	if entry.UpdateCheckedAt.IsZero() || age < 0 || age >= automaticCheckInterval {
		return true
	}
	if signedIn {
		return entry.GameID != "" && entry.UpstreamSource != SourceAPI
	}
	return entry.UpstreamSource == SourceAPI
}

// Stop signals the goroutine to exit. Idempotent — safe to call multiple times.
func (s *UpdateService) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// TriggerNow queues an automatic check, as after an account change: entries
// checked in the last six hours are skipped. Non-blocking.
func (s *UpdateService) TriggerNow() {
	logger.Info("update-svc: automatic check queued")
	s.request(checkRequest{automatic: true})
}

// CheckAllNow queues a check of every entry, for Update Inventory.
// Non-blocking.
func (s *UpdateService) CheckAllNow() {
	logger.Info("update-svc: full check queued")
	s.request(checkRequest{all: true})
}

// IsRunning reports whether runCheck is currently executing.
func (s *UpdateService) IsRunning() bool {
	return s.running.Load()
}

// LatestCheckedAt delegates to the inventory's LatestCheckedAt.
func (s *UpdateService) LatestCheckedAt() time.Time {
	return s.inv.LatestCheckedAt()
}

func (s *UpdateService) runCheck(request checkRequest) {
	if s.deferIfDownloading(request) {
		logger.Info("update-svc: check waits for the running download")
		return
	}
	s.artworkChanged = false
	s.inv.VerifyAndCleanWithSources(s.inventoryPath, s.sources)

	urls := s.inv.AllURLs()
	token, generation := s.client.AuthSnapshot()
	now := time.Now()
	var dueURLs []string
	gameIDs := make(map[string]string, len(urls))
	canCheck := true
	skipped := make(map[string]bool)
	var ids []string
	for _, gameURL := range urls {
		s.repairCoverArt(gameURL)
		entry, ok := s.inv.Lookup(gameURL)
		if !ok || !request.due(gameURL, entry, token != "", now) {
			continue
		}
		dueURLs = append(dueURLs, gameURL)
		if token == "" {
			continue
		}
		id := entry.GameID
		if id == "" && !canCheck {
			// After a rate limit, look the ID up on the next run; artwork
			// repair still covers every entry.
			skipped[gameURL] = true
		} else if id == "" {
			// A stable stored ID avoids fetching data.json on every launch.
			if !s.pace() {
				return
			}
			data, err := s.client.FetchGameData(gameURL)
			if err != nil && !isGameRemoved(err) {
				logger.Warn("update-svc: game metadata unavailable: %v", err)
				skipped[gameURL] = true
				if errors.Is(err, itchio.ErrRateLimited) {
					canCheck = false
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
		if !s.pace() {
			return
		}
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
	logger.Info("update-svc: checking %d of %d inventory entries (signed in: %v)", len(dueURLs), len(urls), token != "")
	pending := make(map[string]upstreamResult, len(dueURLs))
	for index, gameURL := range dueURLs {
		if !canCheck || s.client.AuthGeneration() != generation {
			break
		}
		// A download that started meanwhile gets the connection; the rest
		// of this check runs after it.
		if s.deferIfDownloading(checkRequest{games: setOf(dueURLs[index:])}) {
			logger.Info("update-svc: %d entries wait for the running download", len(dueURLs)-index)
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
		if err == nil && result.source == SourceAPI && !result.complete {
			result.complete = s.keylessListingComplete(gameURL, entry, result.files)
		}
		if err == nil {
			result.installed = entry.Files
			pending[gameURL] = result
		} else {
			if errors.Is(err, errUpdateStopped) {
				return
			}
			logger.Warn("update-svc: check failed for %s: %v", gameURL, err)
			if errors.Is(err, itchio.ErrRateLimited) {
				break
			}
		}
	}
	// Discard results even after A -> B -> A account changes; comparing tokens
	// alone would allow the first account's stale scan to publish.
	staleInstall := make(map[string]bool)
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
				staleInstall[gameURL] = true
				continue
			}
			now := time.Now()
			if result.removed {
				markRemovedLocked(entry, RemovedByPage, now)
				continue
			}
			if result.files != nil {
				s.inv.setUpstreamFilesLocked(entry, result.source, result.files)
			} else {
				entry.UpdateCheckedAt = now
			}
			evidence := result.files
			if result.source == SourceAPI && result.files != nil {
				// The stored rows also carry replaced upload IDs.
				evidence = entry.KnownUpstreamFiles
				if result.complete {
					// R18-3: the upload that replaces an installed one that
					// is gone shows as an update.
					markReplacementsLocked(entry, entry.KnownUpstreamFiles, func(int) bool { return true }, now)
				}
			}
			applyRemovalEvidenceLocked(entry, result.source, result.complete, evidence, now)
		}
	})
	if !applied {
		logger.Debug("update-svc: account changed during the check; scheduling fresh metadata")
		s.request(request)
	} else if len(staleInstall) > 0 {
		// Re-check only the installed games, once their install is done.
		logger.Debug("update-svc: %d game(s) changed during an install; re-checking them", len(staleInstall))
		s.request(checkRequest{games: staleInstall})
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
	files := entry.Files
	// One cover download serves every image this pass writes for the game.
	cover := s.client.NewCoverFetch(entry.CoverURL)

	// 1. Cover art repair.
	for _, f := range files {
		if f.ContentKind == ContentKindMusic || f.FileType == FileTypeMusic ||
			roms.IsPSXSupportExt(roms.ROMExt(f.DestPath)) || entry.ListsNoGame(f) {
			continue
		}
		if artworkCurrent(f) {
			continue
		}
		result, migrated := s.migrateOwnedArtwork(f)
		var err error
		if !migrated {
			result, err = cover.EnsureCoverArt(f.DestPath)
		}
		if err != nil {
			logger.Error("update-svc: cover art repair failed for %s: %v", f.Filename, err)
			continue
		}
		if result.Path == "" {
			continue
		}
		written := result.Created
		if !written {
			result = KeepExistingArtwork(result, []DownloadedFile{f})
		}
		s.inv.SetArtwork(gameURL, f.DestPath, result.Path, result.SHA256, result.Created)
		if written {
			s.artworkChanged = true
		}
	}
}

// artworkCurrent reports whether a file's recorded launcher art is the
// canonical image and still has the recorded content, so the cover-art pass
// has nothing to do. The hash also notices art the user put over the app's
// own, which then becomes theirs.
func artworkCurrent(file DownloadedFile) bool {
	expected := CanonicalArtworkPath(file.DestPath)
	if expected == "" || file.ArtworkHash == "" ||
		filepath.Clean(file.ArtworkPath) != filepath.Clean(expected) {
		return false
	}
	hash, err := regularFileSHA256(expected)
	return err == nil && hash == file.ArtworkHash
}

// KeepExistingArtwork says whose launcher art EnsureCoverArt found in place
// and left alone. It is the app's when one of files records creating that
// image and its recorded hash, if any, still matches; otherwise it is the
// user's. The art stays either way, and only the app's own moves or goes with
// its ROM.
func KeepExistingArtwork(result itchio.ArtworkResult, files []DownloadedFile) itchio.ArtworkResult {
	for _, file := range files {
		if file.ArtworkCreated && filepath.Clean(file.ArtworkPath) == filepath.Clean(result.Path) &&
			(file.ArtworkHash == "" || file.ArtworkHash == result.SHA256) {
			result.Created = true
			logger.Debug("cover-art: keeping app artwork %s", result.Path)
			return result
		}
	}
	result.Created = false
	logger.Info("cover-art: keeping user artwork %s", result.Path)
	return result
}

// regularFileSHA256 hashes a regular file; symlinks and other kinds fail.
func regularFileSHA256(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
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
	actualHash, err := regularFileSHA256(oldPath)
	if err != nil || actualHash != file.ArtworkHash {
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

var errUpdateStopped = errors.New("update service stopped")

func setOf(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// keylessListingComplete reports whether an upload list fetched without a
// download key lists every upload, so it can prove a removal: only when the
// game is free now, not just when it was installed (decision D8). It uses the
// price data.json gave this session, and fetches it only when the list lacks
// an installed upload, which is rare. Unknown pricing makes the list
// incomplete: it can still clear a removal, never set one.
func (s *UpdateService) keylessListingComplete(gameURL string, entry Entry, files []UpstreamFile) bool {
	if data, ok := s.client.CachedPrice(gameURL); ok {
		return data.Pricing() != itchio.PricingPaid
	}
	preview := entry // a deep copy from Lookup
	s.inv.setUpstreamFilesLocked(&preview, SourceAPI, files)
	if len(missingUploadsLocked(&preview, preview.KnownUpstreamFiles)) == 0 {
		return false
	}
	if !s.pace() {
		return false
	}
	data, err := s.client.FetchGameData(gameURL)
	if err != nil {
		logger.Warn("update-svc: current pricing unavailable, not treating the upload list as complete: %v", err)
		return false
	}
	return data.Pricing() != itchio.PricingPaid
}

// isGameRemoved reports whether err indicates a 404 or 410 HTTP response.
func isGameRemoved(err error) bool {
	return errors.Is(err, itchio.ErrGameRemoved)
}

type upstreamResult struct {
	installed []DownloadedFile
	source    string
	files     []UpstreamFile
	// complete marks an API listing of every upload: fetched with the
	// account's download key, or for a game that is free now. Only such a
	// listing can prove that an installed upload is gone (decision D8).
	complete bool
	removed  bool // the game page itself is gone
}

// checkGame lists metadata only. No update path creates a download session,
// resolves a CDN URL, or starts the browser download_url handshake.
func (s *UpdateService) checkGame(gameURL, gameID, token, key string) (upstreamResult, error) {
	if token != "" && gameID != "" {
		if !s.pace() {
			return upstreamResult{}, errUpdateStopped
		}
		uploads, err := s.client.FetchUploadsForKey(token, gameID, key)
		if err == nil {
			files := make([]UpstreamFile, 0, len(uploads))
			for _, upload := range uploads {
				if upload.Type == "html" {
					continue
				}
				files = append(files, UpstreamFile{Filename: upload.Filename, DisplayName: upload.DisplayName,
					UploadID: upload.UploadID, Fingerprint: upload.Fingerprint(),
					DesktopOrWebOnly: upload.DesktopOrWebOnly(), Soundtrack: upload.Type == "soundtrack"})
			}
			if len(files) > 0 || key != "" {
				// The caller decides removal: a complete list that no longer
				// offers an installed upload, or anything of its kind, proves
				// it; a replaced upload shows as an update instead.
				return upstreamResult{source: SourceAPI, files: files, complete: key != ""}, nil
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
	if !s.pace() {
		return upstreamResult{}, errUpdateStopped
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
