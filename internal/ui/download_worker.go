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

type dlState int32

const (
	dlDownloading dlState = iota
	dlDone
	dlError
	dlCancelled
)

func (s *DirectDownloadWorker) loadState() dlState {
	return dlState(atomic.LoadInt32((*int32)(&s.state)))
}
func (s *DirectDownloadWorker) storeState(st dlState) {
	atomic.StoreInt32((*int32)(&s.state), int32(st))
}

type DirectDownloadWorker struct {
	client         *itchio.Client
	cfg            *settings.Config
	game           itchio.Game
	detail         *itchio.GameDetail
	upload         roms.Upload
	state          dlState
	downloaded     int64
	total          int64
	dest           string
	err            error
	inv            *inventory.Inventory
	inventoryPath  string
	start          func(bool)
	inhibitBlocked atomic.Bool
	cancelMu       sync.Mutex
	cancel         context.CancelFunc
}

func NewDirectDownloadWorker(client *itchio.Client, cfg *settings.Config, game itchio.Game, detail *itchio.GameDetail, upload roms.Upload, dest string, inv *inventory.Inventory, inventoryPath string) *DirectDownloadWorker {
	s := &DirectDownloadWorker{
		client: client, cfg: cfg, game: game, detail: detail,
		upload: upload, dest: dest, state: dlDownloading,
		inv: inv, inventoryPath: inventoryPath,
	}

	s.start = func(allowUninhibited bool) {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelMu.Lock()
		s.cancel = cancel
		s.cancelMu.Unlock()
		go func() {
			defer func() {
				s.cancelMu.Lock()
				s.cancel = nil
				s.cancelMu.Unlock()
			}()
			lease, guardErr := leaf.BeginOperation(ctx, "download", allowUninhibited)
			if guardErr != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					s.storeState(dlCancelled)
					return
				}
				s.err = fmt.Errorf("%w. Press A to continue without suspend protection or B to cancel", guardErr)
				s.inhibitBlocked.Store(true)
				s.storeState(dlError)
				return
			}
			defer lease.Release()
			if !lease.Protected {
				logger.Warn("download: continuing without Jawaka suspend protection by user request")
			}
			s.inhibitBlocked.Store(false)
			if _, _, preflightErr := validatePlannedPath(dest); preflightErr != nil {
				s.err = fmt.Errorf("download destination changed before transfer: %w", preflightErr)
				s.storeState(dlError)
				return
			}
			progress := func(dl, total int64) {
				atomic.StoreInt64(&s.downloaded, dl)
				atomic.StoreInt64(&s.total, total)
			}

			isAuth := upload.DownloadKeyID != ""
			logger.Info("download: starting %q file=%s dest=%s auth=%v",
				game.Title, upload.Filename, dest, isAuth)

			var err error
			if isAuth {
				err = client.DownloadAuthUploadContext(ctx, cfg.APIKey, upload.UploadID, upload.DownloadKeyID, dest, progress)
			} else {
				itchUpload := itchio.Upload{Filename: upload.Filename, URL: upload.URL}
				err = client.DownloadFreeContext(ctx, itchUpload, dest, progress)
			}

			if err != nil {
				if errors.Is(err, context.Canceled) {
					logger.Info("download: cancelled file=%s", upload.Filename)
					s.storeState(dlCancelled)
					return
				}
				logger.Error("download: failed file=%s: %v", upload.Filename, err)
				s.err = err
				s.storeState(dlError)
			} else {
				logger.Info("download: complete file=%s", upload.Filename)

				// Apply unified naming if enabled for this game.
				finalDest := dest
				unifiedName := false
				if cfg.UnifiedNaming && roms.SupportsUnifiedNaming(upload.Filename) {
					entry, entryExists := inv.Lookup(game.URL)
					disabled := entryExists && entry.UnifiedNamingDisabled
					if !disabled {
						newDest, didRename := roms.ResolveUnifiedDest(dest, game.Title, true)
						if didRename {
							if renameErr := os.Rename(dest, newDest); renameErr != nil {
								logger.Warn("unified-naming: rename failed: %v", renameErr)
							} else {
								logger.Info("unified-naming: renamed %q → %q", filepath.Base(dest), filepath.Base(newDest))
								finalDest = newDest
								unifiedName = true
							}
						} else {
							unifiedName = true // name already correct
						}
					}
				}

				artwork := ensureROMArtwork(client, s.inv, game, finalDest)
				file := inventory.DownloadedFile{
					Filename:     upload.Filename,
					DestPath:     finalDest,
					DownloadedAt: time.Now(),
					UnifiedName:  unifiedName,
				}
				applyArtwork(&file, artwork)
				s.inv.Add(game.URL, inventory.Entry{
					GameURL:  game.URL,
					Title:    game.Title,
					Author:   game.Author,
					CoverURL: game.CoverURL,
					IsFree:   game.IsFree,
				}, file)
				if saveErr := s.inv.Save(s.inventoryPath); saveErr != nil {
					logger.Warn("inventory: save failed: %v", saveErr)
				} else {
					logger.Info("inventory: recorded game=%q file=%s unified=%v", game.Title, filepath.Base(finalDest), unifiedName)
				}
				s.dest = finalDest
				s.storeState(dlDone)
			}
		}()
	}
	s.start(false)

	return s
}

func (s *DirectDownloadWorker) Cancel() {
	s.cancelMu.Lock()
	cancel := s.cancel
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// IsBusy implements BusyChecker. Returns true while a download is in flight.
func (s *DirectDownloadWorker) IsBusy() bool {
	return s.loadState() == dlDownloading
}
