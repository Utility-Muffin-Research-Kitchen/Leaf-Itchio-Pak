//go:build !headless

package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// maxNameAttempts bounds the " (2)", " (3)" search for a free name.
const maxNameAttempts = 999

// installNamer picks where one install writes its files. A path may be
// written when nothing is there, or when everything recorded there is this
// game's earlier copy from the same upload (a reinstall) or from an upload
// the page no longer offers (an update). A file another game installed,
// another build of this game the page still offers, or a file the app does
// not know about is never replaced; the new file gets a name of its own
// instead. Names are compared case-insensitively, as FAT32 does.
type installNamer struct {
	inv     *inventory.Inventory
	gameURL string
	title   string
	upload  string
	// uploadID and listing come from the list the user chose from; listing
	// is nil when that list is unknown, and then another upload's file is
	// never treated as superseded.
	uploadID string
	listing  *roms.UploadListing
	// anyUpload accepts every file of this game as replaceable. Music
	// records do not say which upload they came from.
	anyUpload bool
	// names holds the paths this operation has written or will write.
	names *roms.NameReservations
}

func newInstallNamer(inv *inventory.Inventory, game itchio.Game, upload string, names *roms.NameReservations) *installNamer {
	return &installNamer{inv: inv, gameURL: game.URL, title: game.Title, upload: upload, names: names}
}

// withListing records the chosen upload's ID and the listing it came from.
func (n *installNamer) withListing(upload roms.Upload) *installNamer {
	n.uploadID, n.listing = upload.UploadID, upload.Listing
	return n
}

// sameUpload reports whether file came from the upload being installed.
func (n *installNamer) sameUpload(file inventory.DownloadedFile) bool {
	return file.UploadName() == n.upload || file.UploadID != "" && file.UploadID == n.uploadID
}

// superseded reports whether the listing no longer offers file's upload, so
// installing this upload updates that build rather than adding another.
func (n *installNamer) superseded(file inventory.DownloadedFile) bool {
	if n.listing == nil {
		return false
	}
	name := file.UploadName()
	stem := strings.TrimSuffix(name, roms.ROMExt(name))
	for _, listed := range n.listing.Uploads {
		if file.UploadID != "" && listed.UploadID == file.UploadID || listed.Filename == name || listed.Filename == stem {
			return false
		}
	}
	return true
}

func (n *installNamer) owners(path string) []inventory.FileOwner {
	identity, ok := roms.DescribeDestination(path)
	if !ok || n.inv == nil {
		return nil
	}
	return n.inv.OwnerOf(identity.SourceID, identity.RelativePath)
}

// replaceable reports whether this install may write over path.
func (n *installNamer) replaceable(path string) bool {
	owners := n.owners(path)
	for _, owner := range owners {
		if owner.GameURL != n.gameURL {
			return false
		}
		if !n.anyUpload && !n.sameUpload(owner.File) && !n.superseded(owner.File) {
			return false
		}
	}
	if len(owners) > 0 {
		return true
	}
	_, exists := roms.ExistingFAT32Path(path)
	return !exists
}

// free reports whether path is neither held by this operation nor a file this
// install may not replace.
func (n *installNamer) free(path string) bool {
	return (n.names == nil || !n.names.Holds(path)) && n.replaceable(path)
}

// onlyThisGame reports whether every record at path belongs to this game.
func (n *installNamer) onlyThisGame(path string) bool {
	owners := n.owners(path)
	for _, owner := range owners {
		if owner.GameURL != n.gameURL {
			return false
		}
	}
	return len(owners) > 0
}

// realPath returns the existing file's own spelling of path, so replacing a
// file whose name differs only in case writes that file on every host.
func realPath(path string) string {
	if existing, ok := roms.ExistingFAT32Path(path); ok {
		return existing
	}
	return path
}

func (n *installNamer) uploadStem() string {
	if isArchive(n.upload) {
		return strings.TrimSuffix(n.upload, filepath.Ext(n.upload))
	}
	return strings.TrimSuffix(n.upload, roms.ROMExt(n.upload))
}

// ownName returns path when this install may write it. Otherwise it returns
// "<Title> - <name>" in the same folder, then " (2)", " (3)" if that is
// taken too. The next install of the same upload finds its own file there,
// so the name is stable across reinstalls.
func (n *installNamer) ownName(path string) (string, error) {
	if n.free(path) {
		return realPath(path), nil
	}
	base := filepath.Base(path)
	ext := roms.ROMExt(base)
	prefix := strings.TrimSuffix(base, ext)
	if title := roms.SanitiseFilename(n.title, ""); title != "" {
		prefix = title + " - " + prefix
	}
	return n.firstFree(filepath.Dir(path), prefix, ext, "")
}

// unifiedName returns where unified naming moves current, and whether that
// name is title-based. The title name is used when this install may write
// it, which includes updating a build the page no longer offers. When it
// holds another build of this game that the page still offers, or the
// listing is unknown, the file is named "<Title> (<upload>)" so both builds
// stay. When it holds another game's or an unknown file, the next free
// "<Title> (2)" is used.
func (n *installNamer) unifiedName(current string) (string, bool) {
	target := roms.UnifiedTarget(current, n.title)
	if target == "" {
		return current, false
	}
	if roms.SameFAT32Path(target, current) {
		return current, true
	}
	if n.free(target) {
		for _, owner := range n.owners(target) {
			if !n.anyUpload && !n.sameUpload(owner.File) {
				logger.Info("unified-naming: %s updates %s, which the page no longer offers",
					n.upload, owner.File.UploadName())
				break
			}
		}
		return realPath(target), true
	}
	ext := roms.ROMExt(filepath.Base(current))
	prefix := strings.TrimSuffix(filepath.Base(target), ext)
	if n.onlyThisGame(target) {
		if named := roms.SanitiseFilename(fmt.Sprintf("%s (%s)", prefix, n.uploadStem()), ""); named != "" {
			prefix = named
		}
	}
	path, err := n.firstFree(filepath.Dir(current), prefix, ext, current)
	if err != nil {
		logger.Warn("unified-naming: keeping %q: %v", filepath.Base(current), err)
		return current, false
	}
	return path, true
}

// firstFree returns the first free "<prefix><ext>", "<prefix> (2)<ext>", ...
// in dir. current, the file being renamed, counts as free.
func (n *installNamer) firstFree(dir, prefix, ext, current string) (string, error) {
	for attempt := 1; attempt <= maxNameAttempts; attempt++ {
		name := prefix + ext
		if attempt > 1 {
			name = fmt.Sprintf("%s (%d)%s", prefix, attempt, ext)
		}
		candidate := filepath.Join(dir, name)
		if current != "" && roms.SameFAT32Path(candidate, current) {
			return current, nil
		}
		if n.free(candidate) {
			return realPath(candidate), nil
		}
	}
	return "", fmt.Errorf("no free name for %s%s", prefix, ext)
}

// installTarget is where one file of a download goes.
type installTarget struct {
	download string // the transfer writes here
	final    string // unified naming then moves it here
	unified  bool   // final is a title-based name
}

// planInstallTargets chooses every destination of a download before anything
// is written. Files never replace another game's or an unknown file, nor one
// another file of this download writes; files whose unified names would meet
// keep their own names.
func planInstallTargets(inv *inventory.Inventory, cfg *settings.Config, game itchio.Game, downloads []romDownload) ([]installTarget, error) {
	requested := &roms.NameReservations{}
	for _, dl := range downloads {
		if !requested.Claim(dl.DestPath) {
			return nil, duplicateDestinationError(dl.DestPath)
		}
	}
	names := &roms.NameReservations{}
	targets := make([]installTarget, len(downloads))
	paths := make([]string, len(downloads))
	for index, dl := range downloads {
		path, err := newInstallNamer(inv, game, dl.Upload.Filename, names).withListing(dl.Upload).ownName(dl.DestPath)
		if err != nil {
			return nil, err
		}
		if !roms.SameFAT32Path(path, dl.DestPath) {
			logger.Info("download: %s belongs to another game or upload; saving as %s",
				filepath.Base(dl.DestPath), filepath.Base(path))
		}
		names.Claim(path)
		targets[index] = installTarget{download: path, final: path}
		paths[index] = path
	}
	if !cfg.UnifiedNaming {
		return targets, nil
	}
	if entry, ok := inv.Lookup(game.URL); ok && entry.UnifiedNamingDisabled {
		return targets, nil
	}
	keepOriginal := roms.UnifiedCollisions(paths, game.Title)
	for index, dl := range downloads {
		if !roms.SupportsUnifiedNaming(dl.Upload.Filename) {
			continue
		}
		if keepOriginal[index] {
			logger.Info("unified-naming: keeping %q; another file in this download needs the same name", filepath.Base(paths[index]))
			continue
		}
		final, unified := newInstallNamer(inv, game, dl.Upload.Filename, names).withListing(dl.Upload).unifiedName(paths[index])
		if !roms.SameFAT32Path(final, paths[index]) {
			names.Claim(final)
		}
		targets[index].final, targets[index].unified = final, unified
	}
	return targets, nil
}

// applyInstallTarget moves a written file to its planned final name and
// returns where it ended up. A failed rename keeps the file where the
// transfer wrote it.
func applyInstallTarget(target installTarget) (string, bool) {
	if roms.SameFAT32Path(target.final, target.download) {
		return target.download, target.unified
	}
	if err := os.Rename(target.download, target.final); err != nil {
		logger.Warn("unified-naming: rename failed: %v", err)
		return target.download, false
	}
	logger.Info("unified-naming: renamed %q → %q", filepath.Base(target.download), filepath.Base(target.final))
	return target.final, target.unified
}
