//go:build !headless

package ui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/leaf"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
)

type ArchiveLimits struct {
	MaxEntries          int
	MaxFileBytes        uint64
	MaxTotalBytes       uint64
	MaxCompressionRatio uint64
}

var DefaultArchiveLimits = ArchiveLimits{
	MaxEntries: 4096, MaxFileBytes: (4 << 30) - 1,
	MaxTotalBytes: 16 << 30, MaxCompressionRatio: 200,
}

type PlannedFile struct {
	Upload            roms.Upload
	SourceID          string
	SourceRoot        string
	ContentRoot       string
	CanonicalSystem   string
	FinalPath         string
	RelativePath      string
	TempDir           string
	TempPattern       string
	ExpectedBytes     int64
	ExpectedSHA256    string
	ArtworkPath       string
	InventoryMutation inventory.DownloadedFile
}

type DownloadTransaction struct {
	GameURL       string
	GameID        string
	PurchaseID    string
	Files         []PlannedFile
	RequiredBytes int64
	ArchiveLimits ArchiveLimits
}

func (plan *CatDownloadPlan) Seal(game itchio.Game, detail *itchio.GameDetail) (*CatDownloadPlan, error) {
	if plan == nil {
		return nil, nil
	}
	sealed := &CatDownloadPlan{
		Kind: plan.Kind, Uploads: append([]roms.Upload(nil), plan.Uploads...),
		DestPaths:   append([]string(nil), plan.DestPaths...),
		LogicalExts: append([]string(nil), plan.LogicalExts...),
	}
	if sealed.Kind != CatDownloadPlanDirect && sealed.Kind != CatDownloadPlanMulti {
		return sealed, nil
	}
	if len(sealed.Uploads) == 0 || len(sealed.Uploads) != len(sealed.DestPaths) {
		return nil, fmt.Errorf("download transaction has inconsistent file destinations")
	}
	transaction := DownloadTransaction{GameURL: game.URL, RequiredBytes: -1, ArchiveLimits: DefaultArchiveLimits}
	if detail != nil {
		transaction.GameID = detail.GameID
	}
	names := &roms.NameReservations{}
	for index, upload := range sealed.Uploads {
		dest := filepath.Clean(sealed.DestPaths[index])
		if !names.Claim(dest) {
			return nil, duplicateDestinationError(dest)
		}
		identity, sourceRoot, err := validatePlannedPath(dest)
		if err != nil {
			return nil, err
		}
		contentRoot := roms.SourceSystemDir(identity.SourceID, identity.CanonicalSystem)
		planned := PlannedFile{
			Upload: upload, SourceID: identity.SourceID, SourceRoot: sourceRoot,
			ContentRoot: contentRoot, CanonicalSystem: identity.CanonicalSystem,
			FinalPath: dest, RelativePath: identity.RelativePath, TempDir: filepath.Dir(dest),
			TempPattern:   filepath.Join(filepath.Dir(dest), ".itchio-download-*.part"),
			ExpectedBytes: -1, ArtworkPath: inventory.CanonicalArtworkPath(dest),
			InventoryMutation: inventory.DownloadedFile{
				UploadID: upload.UploadID, UploadFingerprint: upload.UploadFingerprint,
				OriginalUpload: upload.Filename, InstalledName: filepath.Base(dest),
				SourceID: identity.SourceID, RelativePath: identity.RelativePath,
				CanonicalSystem: identity.CanonicalSystem, DestPath: dest,
			},
		}
		if roms.IsPSXSupportExt(roms.ROMExt(dest)) {
			planned.ArtworkPath = ""
		}
		transaction.Files = append(transaction.Files, planned)
		if transaction.PurchaseID == "" && upload.ViaAPI() {
			transaction.PurchaseID = upload.Install.DownloadKeyID()
		}
	}
	sealed.Transaction = transaction
	return sealed, nil
}

// duplicateDestinationError stops a download whose files would share a name.
func duplicateDestinationError(dest string) error {
	return screentext.New(fmt.Sprintf("Two files in this download would be saved as %s.", filepath.Base(dest)))
}

func validatePlannedPath(path string) (roms.PathIdentity, string, error) {
	identity, ok := roms.DescribeDestination(path)
	if !ok || identity.SourceID == "" || identity.RelativePath == "" {
		return roms.PathIdentity{}, "", fmt.Errorf("download destination is outside configured Leaf sources")
	}
	root := filepath.Clean(roms.SourceRoot(identity.SourceID))
	if root == "." || root == "" {
		return roms.PathIdentity{}, "", fmt.Errorf("download source %q is not configured", identity.SourceID)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return roms.PathIdentity{}, "", screentext.Wrap(fmt.Errorf("download source %q is not mounted", identity.SourceID),
			"The SD card for this download isn't available. Insert it, then try again.")
	}
	if _, err := leaf.RelativeWithin(root, path); err != nil {
		return roms.PathIdentity{}, "", fmt.Errorf("download destination escapes source %q: %w", identity.SourceID, err)
	}
	contentRoot := roms.SourceSystemDir(identity.SourceID, identity.CanonicalSystem)
	if contentRoot == "" {
		return roms.PathIdentity{}, "", fmt.Errorf("download destination is outside a canonical Leaf system")
	}
	if err := catDestinationDirectorySafe(contentRoot, filepath.Dir(path), false); err != nil {
		return roms.PathIdentity{}, "", fmt.Errorf("download destination is unsafe: %w", err)
	}
	if err := leaf.RequireFreeSpace(filepath.Dir(path), 1); err != nil {
		return roms.PathIdentity{}, "", err
	}
	return identity, root, nil
}

func validateArchiveDirectory(dir string) error {
	marker := filepath.Join(filepath.Clean(dir), ".itchio-preflight")
	identity, ok := roms.DescribeDestination(marker)
	if !ok || identity.SourceID == "" {
		return fmt.Errorf("archive destination is outside configured Leaf sources")
	}
	root := roms.SourceSystemDir(identity.SourceID, identity.CanonicalSystem)
	if root == "" {
		musicRoot := roms.SourceMusicRoot(identity.SourceID)
		if musicRoot == "" {
			return fmt.Errorf("archive destination is outside a canonical content root")
		}
		if _, err := leaf.RelativeWithin(musicRoot, dir); err != nil {
			return fmt.Errorf("archive destination is outside a canonical content root")
		}
		root = musicRoot
	}
	if err := catDestinationDirectorySafe(root, dir, true); err != nil {
		return fmt.Errorf("archive destination is unsafe: %w", err)
	}
	return nil
}

const archiveTooLarge = "This archive is too large to install."

func ValidateArchiveManifest(manifest roms.ZIPManifest, limits ArchiveLimits) error {
	if limits.MaxEntries > 0 && len(manifest.Entries) > limits.MaxEntries {
		return screentext.Wrap(fmt.Errorf("archive has %d entries; limit is %d", len(manifest.Entries), limits.MaxEntries),
			"This archive has more files than Leaf can install at once.")
	}
	var total uint64
	for _, entry := range manifest.Entries {
		if limits.MaxFileBytes > 0 && entry.Size > limits.MaxFileBytes {
			return screentext.Wrap(fmt.Errorf("archive entry %q exceeds the FAT32-safe file limit", entry.Name),
				"A file in this archive is larger than 4 GB, too large for the SD card.")
		}
		if ^uint64(0)-total < entry.Size {
			return screentext.Wrap(fmt.Errorf("archive uncompressed size overflows"), archiveTooLarge)
		}
		total += entry.Size
		if limits.MaxTotalBytes > 0 && total > limits.MaxTotalBytes {
			return screentext.Wrap(fmt.Errorf("archive expands beyond the %d-byte transaction limit", limits.MaxTotalBytes),
				archiveTooLarge)
		}
		if limits.MaxCompressionRatio > 0 && entry.CompressedSize > 0 {
			quotient := entry.Size / entry.CompressedSize
			remainder := entry.Size % entry.CompressedSize
			if quotient > limits.MaxCompressionRatio || quotient == limits.MaxCompressionRatio && remainder != 0 {
				return screentext.Wrap(fmt.Errorf("archive entry %q exceeds the compression-ratio limit", entry.Name),
					"This archive looks unsafe to unpack, so it wasn't installed.")
			}
		}
	}
	return nil
}
