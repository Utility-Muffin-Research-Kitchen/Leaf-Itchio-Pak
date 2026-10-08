//go:build !headless

package ui

import (
	"path/filepath"
	"sort"
	"sync/atomic"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
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

// libraryTitleGroups sends one title group per system. Jawaka adds each
// match's scanned name when one group matches several games: that tells the
// discs of one set apart, but a game with a GB and a GBA build would show
// "Title — Title" twice. Files of one system stay in one group.
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
	systems := make(map[string]string)
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
		if _, duplicate := systems[file.DestPath]; duplicate {
			continue
		}
		systems[file.DestPath] = titleGroupSystem(file)
		paths = append(paths, file.DestPath)
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	var groups []leaf.LibraryTitleGroup
	groupIndex := make(map[string]int)
	for _, path := range paths {
		index, ok := groupIndex[systems[path]]
		if !ok {
			index = len(groups)
			groupIndex[systems[path]] = index
			groups = append(groups, leaf.LibraryTitleGroup{
				Provider: itchioLibraryTitleProvider,
				Title:    title,
			})
		}
		groups[index].ROMPaths = append(groups[index].ROMPaths, path)
	}
	return groups
}

// titleGroupSystem is the library system a ROM is listed under: its canonical
// system, or its folder when it sits outside every system folder.
func titleGroupSystem(file inventory.DownloadedFile) string {
	if identity, ok := roms.DescribeDestination(file.DestPath); ok && identity.CanonicalSystem != "" {
		return identity.CanonicalSystem
	}
	if file.CanonicalSystem != "" {
		return file.CanonicalSystem
	}
	return filepath.Dir(file.DestPath)
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

// inhibitBlockedError is a download's failure to get suspend protection
// from Jawaka. The screen asks whether to continue without it; the log
// keeps Jawaka's own error.
func inhibitBlockedError(guardErr error) error {
	return screentext.Wrap(guardErr, "Jawaka is unavailable, so Leaf can't prevent suspend during this download. "+
		"Press A to download without that protection, or B to cancel.")
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
		model.Detail = screentext.FromError(s.err)
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
		model.Detail = screentext.FromError(s.err)
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
	state := s.loadState()
	model := appui.DownloadProgressModel{
		State: appui.DownloadProgressRunning, Title: s.game.Title, Filename: s.plan.Upload.Filename,
		Downloaded: atomic.LoadInt64(&s.downloaded), Total: atomic.LoadInt64(&s.total), FileCount: 1,
		Locked: state != zipDLDownloading,
	}
	switch state {
	case zipDLExtracting:
		model.Filename = "Extracting " + s.plan.Upload.Filename
	case zipDLDone:
		model.State = appui.DownloadProgressDone
		model.SavedPaths = append([]string(nil), s.extracted...)
		model.Skipped = append([]string(nil), s.skipped...)
	case zipDLError:
		model.State = appui.DownloadProgressError
		model.Detail = screentext.FromError(s.err)
		if s.inhibitBlocked.Load() {
			model.State = appui.DownloadProgressInhibitBlocked
		}
	case zipDLCancelled:
		model.State = appui.DownloadProgressCancelled
		model.Detail = "No files were installed."
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

// CatCancel stops the archive transfer and removes its partial file. Once
// extraction starts it is a no-op: extraction cannot safely stop halfway
// through a file set, so the progress screen stays locked until it completes.
func (s *ArchiveDownloadWorker) CatCancel() {
	if s.loadState() == zipDLDownloading {
		s.cancel()
	}
}
