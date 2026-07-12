//go:build !headless

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type multiDLState int32

const (
	multiDLDownloading multiDLState = iota
	multiDLDone
	multiDLError
	multiDLCancelled
)

// romDownload pairs an upload with its resolved destination path.
type romDownload struct {
	Upload   roms.Upload
	DestPath string
}

// MultiDownloadWorker downloads a list of ROM files sequentially,
// placing each in its respective folder and tracking all in the inventory.
type MultiDownloadWorker struct {
	client    *itchio.Client
	cfg       *settings.Config
	game      itchio.Game
	detail    *itchio.GameDetail
	downloads []romDownload
	inv       *inventory.Inventory
	invPath   string

	state          int32 // multiDLState, accessed atomically
	currentIdx     int32 // index of the file currently being downloaded, atomic
	dlProgress     int64 // bytes downloaded for current file, atomic
	dlTotal        int64 // total bytes for current file, atomic
	err            error
	finalPaths     []string // resolved dest path for each completed download
	inhibitBlocked atomic.Bool
	cancelMu       sync.Mutex
	cancel         context.CancelFunc
}

func NewMultiDownloadWorker(
	client *itchio.Client, cfg *settings.Config,
	game itchio.Game, detail *itchio.GameDetail,
	downloads []romDownload,
	inv *inventory.Inventory, invPath string,
) *MultiDownloadWorker {
	s := &MultiDownloadWorker{
		client: client, cfg: cfg, game: game, detail: detail,
		downloads:  downloads,
		inv:        inv,
		invPath:    invPath,
		finalPaths: make([]string, len(downloads)),
	}
	s.startDownloads(false)
	return s
}

func (s *MultiDownloadWorker) loadState() multiDLState {
	return multiDLState(atomic.LoadInt32(&s.state))
}

func (s *MultiDownloadWorker) startDownloads(allowUninhibited bool) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	s.cancel = cancel
	s.cancelMu.Unlock()
	go s.runDownloads(ctx, allowUninhibited)
}

func (s *MultiDownloadWorker) runDownloads(ctx context.Context, allowUninhibited bool) {
	defer func() {
		s.cancelMu.Lock()
		s.cancel = nil
		s.cancelMu.Unlock()
	}()
	lease, guardErr := leaf.BeginOperation(ctx, "batch download", allowUninhibited)
	if guardErr != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			atomic.StoreInt32(&s.state, int32(multiDLCancelled))
			return
		}
		s.err = fmt.Errorf("%w. Press A to continue without suspend protection or B to cancel", guardErr)
		s.inhibitBlocked.Store(true)
		atomic.StoreInt32(&s.state, int32(multiDLError))
		return
	}
	defer lease.Release()
	if !lease.Protected {
		logger.Warn("multi-download: continuing without Jawaka suspend protection by user request")
	}
	s.inhibitBlocked.Store(false)

	for i, dl := range s.downloads {
		atomic.StoreInt32(&s.currentIdx, int32(i))
		atomic.StoreInt64(&s.dlProgress, 0)
		atomic.StoreInt64(&s.dlTotal, 0)

		progress := func(downloaded, total int64) {
			atomic.StoreInt64(&s.dlProgress, downloaded)
			atomic.StoreInt64(&s.dlTotal, total)
		}

		isAuth := dl.Upload.DownloadKeyID != ""
		logger.Info("multi-download: [%d/%d] starting %s → %s auth=%v",
			i+1, len(s.downloads), dl.Upload.Filename, dl.DestPath, isAuth)

		var err error
		if isAuth {
			err = s.client.DownloadAuthUploadContext(ctx, s.cfg.APIKey, dl.Upload.UploadID, dl.Upload.DownloadKeyID, dl.DestPath, progress)
		} else {
			itchUpload := itchio.Upload{Filename: dl.Upload.Filename, URL: dl.Upload.URL}
			err = s.client.DownloadFreeContext(ctx, itchUpload, dl.DestPath, progress)
		}

		if err != nil {
			if errors.Is(err, context.Canceled) {
				logger.Info("multi-download: cancelled at file %d/%d", i+1, len(s.downloads))
				atomic.StoreInt32(&s.state, int32(multiDLCancelled))
				return
			}
			logger.Error("multi-download: [%d/%d] failed %s: %v", i+1, len(s.downloads), dl.Upload.Filename, err)
			s.err = err
			atomic.StoreInt32(&s.state, int32(multiDLError))
			return
		}

		logger.Info("multi-download: [%d/%d] complete %s", i+1, len(s.downloads), dl.Upload.Filename)

		finalDest := dl.DestPath
		unifiedName := false
		if s.cfg.UnifiedNaming && roms.SupportsUnifiedNaming(dl.Upload.Filename) {
			entry, entryExists := s.inv.Lookup(s.game.URL)
			disabled := entryExists && entry.UnifiedNamingDisabled
			if !disabled {
				newDest, didRename := roms.ResolveUnifiedDest(dl.DestPath, s.game.Title, true)
				if didRename {
					if renameErr := os.Rename(dl.DestPath, newDest); renameErr != nil {
						logger.Warn("unified-naming: rename failed: %v", renameErr)
					} else {
						logger.Info("unified-naming: renamed %q → %q", filepath.Base(dl.DestPath), filepath.Base(newDest))
						finalDest = newDest
						unifiedName = true
					}
				} else {
					unifiedName = true
				}
			}
		}

		if roms.IsPSXSupportExt(roms.ROMExt(dl.Upload.Filename)) {
			// BIN tracks are companion data and do not own launcher artwork.
		} else if roms.ROMExt(dl.Upload.Filename) == ".p8.png" {
			if artErr := itchio.CopyCoverArt(finalDest); artErr != nil {
				logger.Warn("cover-art: game=%q: %v", s.game.Title, artErr)
			}
		} else if artErr := s.client.DownloadCoverArt(s.game.CoverURL, finalDest); artErr != nil {
			logger.Warn("cover-art: game=%q: %v", s.game.Title, artErr)
		}

		s.finalPaths[i] = finalDest
		s.inv.Add(s.game.URL, inventory.Entry{
			GameURL:  s.game.URL,
			Title:    s.game.Title,
			Author:   s.game.Author,
			CoverURL: s.game.CoverURL,
			IsFree:   s.game.IsFree,
		}, inventory.DownloadedFile{
			Filename:     dl.Upload.Filename,
			DestPath:     finalDest,
			DownloadedAt: time.Now(),
			UnifiedName:  unifiedName,
		})
		if saveErr := s.inv.Save(s.invPath); saveErr != nil {
			logger.Warn("inventory: save failed: %v", saveErr)
		} else {
			logger.Info("inventory: recorded game=%q file=%s unified=%v", s.game.Title, filepath.Base(finalDest), unifiedName)
		}
	}

	atomic.StoreInt32(&s.state, int32(multiDLDone))
}

func (s *MultiDownloadWorker) Cancel() {
	s.cancelMu.Lock()
	cancel := s.cancel
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// IsBusy implements BusyChecker. Returns true while downloads are in flight.
func (s *MultiDownloadWorker) IsBusy() bool {
	return s.loadState() == multiDLDownloading
}
