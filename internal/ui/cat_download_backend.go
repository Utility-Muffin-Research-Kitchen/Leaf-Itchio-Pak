//go:build !headless

package ui

import (
	"sort"
	"sync/atomic"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

type CatDownloadBackend interface {
	CatSnapshot() appui.DownloadProgressModel
	CatNeedsLibraryScan() bool
	CatLibraryTitleGroups() []leaf.LibraryTitleGroup
	CatContinueWithoutProtection()
	CatCancel()
}

const itchioLibraryTitleProvider = "org.umrk.itchio"

func libraryTitleGroups(inv *inventory.Inventory, gameURL, title string, savedPaths []string) []leaf.LibraryTitleGroup {
	if inv == nil || title == "" || len(savedPaths) == 0 {
		return nil
	}
	entry, ok := inv.Lookup(gameURL)
	if !ok {
		return nil
	}
	wanted := make(map[string]struct{}, len(savedPaths))
	for _, path := range savedPaths {
		if path != "" {
			wanted[path] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	paths := make([]string, 0, len(wanted))
	for _, file := range entry.Files {
		if _, ok := wanted[file.DestPath]; !ok {
			continue
		}
		isROM := file.ContentKind == inventory.ContentKindROM ||
			(file.ContentKind == "" && (file.FileType == inventory.FileTypeROM || file.FileType == inventory.FileTypeM3U))
		if !isROM || file.DestPath == "" {
			continue
		}
		if _, duplicate := seen[file.DestPath]; duplicate {
			continue
		}
		seen[file.DestPath] = struct{}{}
		paths = append(paths, file.DestPath)
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	return []leaf.LibraryTitleGroup{{
		Provider: itchioLibraryTitleProvider,
		Title:    title,
		ROMPaths: paths,
	}}
}

func NewCatDirectDownloadBackend(client *itchio.Client, cfg *settings.Config,
	game itchio.Game, detail *itchio.GameDetail, upload roms.Upload, dest string,
	inv *inventory.Inventory, inventoryPath string) CatDownloadBackend {
	return NewDirectDownloadWorker(client, cfg, game, detail, upload, dest, inv, inventoryPath)
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
	return NewMultiDownloadWorker(client, cfg, game, detail, downloads, inv, inventoryPath)
}

func NewCatArchiveDownloadBackend(client *itchio.Client, cfg *settings.Config,
	game itchio.Game, detail *itchio.GameDetail, plan ZIPPlan,
	inv *inventory.Inventory, inventoryPath string) CatDownloadBackend {
	return NewArchiveDownloadWorker(client, cfg, game, detail, plan, inv, inventoryPath)
}

func (s *DirectDownloadWorker) CatSnapshot() appui.DownloadProgressModel {
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

func (s *DirectDownloadWorker) CatCancel() { s.Cancel() }

func (s *DirectDownloadWorker) CatNeedsLibraryScan() bool { return true }

func (s *DirectDownloadWorker) CatLibraryTitleGroups() []leaf.LibraryTitleGroup {
	return libraryTitleGroups(s.inv, s.game.URL, s.game.Title, []string{s.dest})
}

func (s *DirectDownloadWorker) CatContinueWithoutProtection() {
	if s.loadState() != dlError || !s.inhibitBlocked.Load() {
		return
	}
	s.storeState(dlDownloading)
	s.start(true)
}

func (s *MultiDownloadWorker) CatSnapshot() appui.DownloadProgressModel {
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

func (s *MultiDownloadWorker) CatContinueWithoutProtection() {
	if s.loadState() != multiDLError || !s.inhibitBlocked.Load() {
		return
	}
	atomic.StoreInt32(&s.state, int32(multiDLDownloading))
	s.startDownloads(true)
}

func (s *MultiDownloadWorker) CatCancel() { s.Cancel() }

func (s *MultiDownloadWorker) CatNeedsLibraryScan() bool { return true }

func (s *MultiDownloadWorker) CatLibraryTitleGroups() []leaf.LibraryTitleGroup {
	return libraryTitleGroups(s.inv, s.game.URL, s.game.Title, s.finalPaths)
}

func (s *ArchiveDownloadWorker) CatSnapshot() appui.DownloadProgressModel {
	model := appui.DownloadProgressModel{
		State: appui.DownloadProgressRunning, Title: s.game.Title, Filename: s.plan.Upload.Filename,
		Downloaded: atomic.LoadInt64(&s.downloaded), Total: atomic.LoadInt64(&s.total), FileCount: 1, Locked: true,
	}
	switch s.loadState() {
	case zipDLExtracting:
		model.Filename = "Extracting " + s.plan.Upload.Filename
	case zipDLDone:
		model.State = appui.DownloadProgressDone
		model.SavedPaths = append([]string(nil), s.extracted...)
	case zipDLError:
		model.State = appui.DownloadProgressError
		if s.err != nil {
			model.Detail = s.err.Error()
		}
		if s.inhibitBlocked.Load() {
			model.State = appui.DownloadProgressInhibitBlocked
		}
	}
	return model
}

func (s *ArchiveDownloadWorker) CatContinueWithoutProtection() {
	if s.loadState() != zipDLError || !s.inhibitBlocked.Load() {
		return
	}
	s.storeState(zipDLDownloading)
	go s.run(true)
}

func (s *ArchiveDownloadWorker) CatNeedsLibraryScan() bool { return s.plan.DownloadROMs }

func (s *ArchiveDownloadWorker) CatLibraryTitleGroups() []leaf.LibraryTitleGroup {
	return libraryTitleGroups(s.inv, s.game.URL, s.game.Title, s.extracted)
}

// Archive extraction cannot safely stop halfway through a file set. Cancel is
// therefore a no-op while busy and the progress screen keeps the operation
// visible until its protected transaction completes.
func (s *ArchiveDownloadWorker) CatCancel() {}
