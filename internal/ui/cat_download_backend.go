//go:build !headless

package ui

import (
	"sync/atomic"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type CatDownloadBackend interface {
	CatSnapshot() appui.DownloadProgressModel
	CatContinueWithoutProtection()
	CatCancel()
}

func NewCatDirectDownloadBackend(client *itchio.Client, cfg *settings.Config,
	game itchio.Game, detail *itchio.GameDetail, upload roms.Upload, dest string,
	inv *inventory.Inventory, inventoryPath string) CatDownloadBackend {
	return NewDownloadScreen(client, cfg, game, detail, upload, dest, inv, inventoryPath, nil)
}

func NewCatMultiDownloadBackend(client *itchio.Client, cfg *settings.Config,
	game itchio.Game, detail *itchio.GameDetail, uploads []roms.Upload, destPaths []string,
	inv *inventory.Inventory, inventoryPath string) CatDownloadBackend {
	downloads := make([]romDownload, 0, len(uploads))
	for index, upload := range uploads {
		if index >= len(destPaths) {
			break
		}
		downloads = append(downloads, romDownload{Upload: upload, DestPath: destPaths[index]})
	}
	return NewMultiROMDownloadScreen(client, cfg, game, detail, downloads, inv, inventoryPath, nil)
}

func (s *DownloadScreen) CatSnapshot() appui.DownloadProgressModel {
	model := appui.DownloadProgressModel{
		State: appui.DownloadProgressRunning, Title: s.game.Title, Filename: s.upload.Filename,
		Downloaded: atomic.LoadInt64(&s.downloaded), Total: atomic.LoadInt64(&s.total), FileCount: 1,
	}
	switch s.loadState() {
	case dlDone:
		model.State = appui.DownloadProgressDone
		model.SavedPaths = []string{s.dest}
	case dlError:
		model.State = appui.DownloadProgressError
		if s.err != nil {
			model.Detail = s.err.Error()
		}
		if s.inhibitBlocked.Load() {
			model.State = appui.DownloadProgressInhibitBlocked
		}
	case dlCancelled:
		model.State = appui.DownloadProgressCancelled
		model.Detail = "No partial file was installed."
	}
	return model
}

func (s *DownloadScreen) CatCancel() { s.Cancel() }

func (s *DownloadScreen) CatContinueWithoutProtection() {
	if s.loadState() != dlError || !s.inhibitBlocked.Load() {
		return
	}
	s.storeState(dlDownloading)
	s.start(true)
}

func (s *MultiROMDownloadScreen) CatSnapshot() appui.DownloadProgressModel {
	index := int(atomic.LoadInt32(&s.currentIdx))
	model := appui.DownloadProgressModel{
		State: appui.DownloadProgressRunning, Title: s.game.Title,
		Downloaded: atomic.LoadInt64(&s.dlProgress), Total: atomic.LoadInt64(&s.dlTotal),
		FileIndex: index, FileCount: len(s.downloads),
	}
	if index >= 0 && index < len(s.downloads) {
		model.Filename = s.downloads[index].Upload.Filename
	}
	switch s.loadState() {
	case multiDLDone:
		model.State = appui.DownloadProgressDone
		model.SavedPaths = append([]string(nil), s.finalPaths...)
	case multiDLError:
		model.State = appui.DownloadProgressError
		if s.err != nil {
			model.Detail = s.err.Error()
		}
		if s.inhibitBlocked.Load() {
			model.State = appui.DownloadProgressInhibitBlocked
		}
	case multiDLCancelled:
		model.State = appui.DownloadProgressCancelled
		model.Detail = "No partial current file was installed; earlier completed files remain recorded."
	}
	return model
}

func (s *MultiROMDownloadScreen) CatContinueWithoutProtection() {
	if s.loadState() != multiDLError || !s.inhibitBlocked.Load() {
		return
	}
	atomic.StoreInt32(&s.state, int32(multiDLDownloading))
	s.startDownloads(true)
}

func (s *MultiROMDownloadScreen) CatCancel() { s.Cancel() }
