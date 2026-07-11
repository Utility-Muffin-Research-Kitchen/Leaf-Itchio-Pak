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
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/renderer"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
	"github.com/veandco/go-sdl2/sdl"
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

// MultiROMDownloadScreen downloads a list of ROM files sequentially,
// placing each in its respective folder and tracking all in the inventory.
type MultiROMDownloadScreen struct {
	client    *itchio.Client
	cfg       *settings.Config
	game      itchio.Game
	detail    *itchio.GameDetail
	downloads []romDownload
	inv       *inventory.Inventory
	invPath   string
	prev      Screen

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

func NewMultiROMDownloadScreen(
	client *itchio.Client, cfg *settings.Config,
	game itchio.Game, detail *itchio.GameDetail,
	downloads []romDownload,
	inv *inventory.Inventory, invPath string,
	prev Screen,
) *MultiROMDownloadScreen {
	s := &MultiROMDownloadScreen{
		client: client, cfg: cfg, game: game, detail: detail,
		downloads:  downloads,
		inv:        inv,
		invPath:    invPath,
		prev:       prev,
		finalPaths: make([]string, len(downloads)),
	}
	s.startDownloads(false)
	return s
}

func (s *MultiROMDownloadScreen) loadState() multiDLState {
	return multiDLState(atomic.LoadInt32(&s.state))
}

func (s *MultiROMDownloadScreen) startDownloads(allowUninhibited bool) {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMu.Lock()
	s.cancel = cancel
	s.cancelMu.Unlock()
	go s.runDownloads(ctx, allowUninhibited)
}

func (s *MultiROMDownloadScreen) runDownloads(ctx context.Context, allowUninhibited bool) {
	defer func() { sdl.PushEvent(&sdl.UserEvent{Type: sdl.USEREVENT}) }()
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
		sdl.PushEvent(&sdl.UserEvent{Type: sdl.USEREVENT})

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
		if s.cfg.UnifiedNaming {
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

		if roms.ROMExt(dl.Upload.Filename) == ".p8.png" {
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

func (s *MultiROMDownloadScreen) Cancel() {
	s.cancelMu.Lock()
	cancel := s.cancel
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *MultiROMDownloadScreen) NeedsRedraw() bool         { return true }
func (s *MultiROMDownloadScreen) HasPendingAnimation() bool { return false }

func (s *MultiROMDownloadScreen) Draw(r *renderer.Renderer) {
	bg := r.Theme.Background
	r.Clear(bg[0], bg[1], bg[2])

	footerH := int32(52)
	_, fontH := r.TextSize("Ag")
	_, smallFH := r.SmallTextSize("Ag")
	headerH := fontH + smallFH + 16
	hdr := r.Theme.HeaderBG
	ac := r.Theme.Accent
	r.DrawRect(0, 0, r.W, headerH, hdr[0], hdr[1], hdr[2])
	r.DrawRect(0, headerH, r.W, 2, ac[0], ac[1], ac[2])
	mt := r.Theme.MainText
	r.DrawText(truncateToWidth(r, s.game.Title, r.W-24), 12, 8, mt[0], mt[1], mt[2])
	ht := r.Theme.HintText
	r.DrawSmallText("by "+s.game.Author, 12, 8+fontH+4, ht[0], ht[1], ht[2])

	contentH := r.H - headerH - footerH
	mid := headerH + contentH/2
	st := s.loadState()

	switch st {
	case multiDLDownloading:
		idx := int(atomic.LoadInt32(&s.currentIdx))
		dl := atomic.LoadInt64(&s.dlProgress)
		tot := atomic.LoadInt64(&s.dlTotal)
		label := fmt.Sprintf("File %d of %d", idx+1, len(s.downloads))
		r.DrawSmallTextCentered(label, 0, mid-fontH-smallFH-14, r.W, 140, 140, 140)
		if idx < len(s.downloads) {
			name := truncateSmallToWidth(r, s.downloads[idx].Upload.Filename, r.W-40)
			r.DrawSmallTextCentered(name, 0, mid-fontH-6, r.W, ht[0], ht[1], ht[2])
		}
		barW := r.W - 80
		r.DrawRect(40, mid-10, barW, 20, 60, 60, 60)
		if tot > 0 {
			filled := int32(float64(barW) * float64(dl) / float64(tot))
			r.DrawRect(40, mid-10, filled, 20, 80, 200, 80)
			r.DrawText(fmt.Sprintf("%d%%  (%s / %s)", dl*100/tot, humanBytes(dl), humanBytes(tot)),
				40, mid+18, mt[0], mt[1], mt[2])
		} else if dl > 0 {
			r.DrawRect(40, mid-10, barW/3, 20, 80, 200, 80)
			r.DrawText(humanBytes(dl)+" downloaded", 40, mid+18, mt[0], mt[1], mt[2])
		}

	case multiDLDone:
		r.DrawTextCentered(fmt.Sprintf("%d ROM(s) downloaded!", len(s.downloads)), 0, mid-fontH-8, r.W, 80, 200, 80)
		lineY := mid + 8
		for _, p := range s.finalPaths {
			if p == "" || lineY+smallFH > r.H-footerH-4 {
				break
			}
			label := truncateSmallToWidth(r, filepath.Base(p), r.W-40)
			r.DrawSmallTextCentered(label, 0, lineY, r.W, ht[0], ht[1], ht[2])
			lineY += smallFH + 4
		}

	case multiDLError:
		y := headerH + 10 + 8
		r.DrawText("Download failed:", 20, y, 200, 60, 60)
		y += fontH + 6
		r.DrawWrappedText(s.err.Error(), 20, y, r.W-40, fontH+4, 200, 100, 100)
	case multiDLCancelled:
		r.DrawTextCentered("Download cancelled", 0, mid-fontH/2, r.W, ht[0], ht[1], ht[2])
	}

	ftrY := r.DrawFooterBar(footerH)
	if st == multiDLError && s.inhibitBlocked.Load() {
		r.DrawFooterHints([]renderer.FooterHint{
			{Kind: renderer.BadgeCircle, Label: "A", Text: "Continue"},
			{Kind: renderer.BadgeCircle, Label: "B", Text: "Cancel"},
		}, ftrY)
		r.Present()
		return
	}
	switch st {
	case multiDLDownloading:
		r.DrawFooterHints([]renderer.FooterHint{{Kind: renderer.BadgeCircle, Label: "B", Text: "Cancel"}}, ftrY)
	default:
		r.DrawFooterHints([]renderer.FooterHint{
			{Kind: renderer.BadgePill, Label: "A/B", Text: "Back"},
		}, ftrY)
	}
	r.Present()
}

func (s *MultiROMDownloadScreen) HandleEvent(e sdl.Event) Screen {
	if s.loadState() == multiDLDownloading {
		switch ev := e.(type) {
		case *sdl.KeyboardEvent:
			if ev.Type == sdl.KEYDOWN && ev.Keysym.Sym == sdl.K_ESCAPE {
				s.Cancel()
			}
		case *sdl.ControllerButtonEvent:
			if ev.Type == sdl.CONTROLLERBUTTONDOWN && ev.Button == sdl.CONTROLLER_BUTTON_A {
				s.Cancel()
			}
		}
		return s
	}
	switch ev := e.(type) {
	case *sdl.KeyboardEvent:
		if ev.Type == sdl.KEYDOWN {
			if s.loadState() == multiDLError && s.inhibitBlocked.Load() {
				switch ev.Keysym.Sym {
				case sdl.K_RETURN:
					atomic.StoreInt32(&s.state, int32(multiDLDownloading))
					s.startDownloads(true)
					return s
				case sdl.K_ESCAPE:
					return s.prev
				}
			}
			switch ev.Keysym.Sym {
			case sdl.K_ESCAPE, sdl.K_RETURN:
				return s.prev
			}
		}
	case *sdl.ControllerButtonEvent:
		if ev.Type == sdl.CONTROLLERBUTTONDOWN {
			if s.loadState() == multiDLError && s.inhibitBlocked.Load() {
				switch ev.Button {
				case sdl.CONTROLLER_BUTTON_B:
					atomic.StoreInt32(&s.state, int32(multiDLDownloading))
					s.startDownloads(true)
					return s
				case sdl.CONTROLLER_BUTTON_A:
					return s.prev
				}
			}
			switch ev.Button {
			case sdl.CONTROLLER_BUTTON_A, sdl.CONTROLLER_BUTTON_B:
				return s.prev
			}
		}
	}
	return s
}

// IsBusy implements BusyChecker. Returns true while downloads are in flight.
func (s *MultiROMDownloadScreen) IsBusy() bool {
	return s.loadState() == multiDLDownloading
}
