package roms

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/bodgit/sevenzip"
)

// InspectRemote7z downloads the 7z archive at cdnURL to a temporary file,
// reads its directory, and returns a classified ZIPManifest.
// Unlike ZIP, 7z cannot be inspected via HTTP Range requests, so a full
// download is always required.
func InspectRemote7z(client *http.Client, cdnURL string) (ZIPManifest, error) {
	tmp, err := os.CreateTemp("", "itchio-inspect-*.7z")
	if err != nil {
		return ZIPManifest{}, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)
	logger.Debug("7z-inspect: downloading to temp=%s", tmpPath)

	resp, err := client.Get(cdnURL)
	if err != nil {
		return ZIPManifest{}, remoteRequestError("download 7z", err)
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return ZIPManifest{}, fmt.Errorf("open temp: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return ZIPManifest{}, fmt.Errorf("write temp: %w", err)
	}
	f.Close()

	r, err := sevenzip.OpenReader(tmpPath)
	if err != nil {
		return ZIPManifest{}, fmt.Errorf("sevenzip.OpenReader: %w", err)
	}
	defer r.Close()

	logger.Debug("7z-inspect: read %d entries", len(r.File))
	return manifestFrom7zReader(r)
}

// manifestFrom7zReader classifies a 7z's members. A ".md" member that
// cannot be read fails the inspection instead of passing as Markdown.
func manifestFrom7zReader(r *sevenzip.ReadCloser) (ZIPManifest, error) {
	var m ZIPManifest
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		// Normalise path separators (Windows 7z archives may use backslashes).
		fullPath := strings.ReplaceAll(f.Name, "\\", "/")
		if IsInMacOSMetaDir(fullPath) {
			continue
		}
		name := filepath.Base(fullPath)
		if strings.HasPrefix(name, "._") {
			continue // macOS resource-fork stub outside __MACOSX/
		}
		// Classify by extension, or by the first bytes when the name does not
		// decide it (see ClassifyArchiveMember).
		kind, name, err := ClassifyArchiveMember(name, f.Open)
		if err != nil {
			return ZIPManifest{}, fmt.Errorf("read %s: %w", name, err)
		}

		m.Entries = append(m.Entries, ZIPEntry{
			Name: name,
			Kind: kind,
			Size: f.FileHeader.UncompressedSize,
		})
	}
	return m, nil
}
