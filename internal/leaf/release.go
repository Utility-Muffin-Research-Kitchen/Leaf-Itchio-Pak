package leaf

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Limits mirror Jawaka's jw_installed_release_read
// (internal/platform/leaf_version.{c,h}): a release.json above 64 KiB is
// rejected, and so is a release_id that does not fit its 128-byte buffer.
const (
	releaseJSONMaxBytes = 64 * 1024
	releaseIDMaxBytes   = 127
)

// InstalledReleaseID returns release_id from release.json in Leaf's
// launcher control-state directory ($UMRK_INTERNAL_DATA_PATH), the record
// Jawaka reads for its own About screen. It returns an error when the file
// is missing, unreadable or malformed, or has no usable release_id.
func InstalledReleaseID() (string, error) {
	return readReleaseID(internalDataPath(os.LookupEnv))
}

func readReleaseID(stateDir string) (string, error) {
	if stateDir == "" {
		return "", errors.New("Leaf control-state directory is not set")
	}
	file, err := os.Open(filepath.Join(stateDir, "release.json"))
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, releaseJSONMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > releaseJSONMaxBytes {
		return "", fmt.Errorf("release.json exceeds %d bytes", releaseJSONMaxBytes)
	}
	var release struct {
		ReleaseID *string `json:"release_id"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("parse release.json: %w", err)
	}
	if release.ReleaseID == nil {
		return "", errors.New("release.json has no release_id")
	}
	id := strings.TrimSpace(*release.ReleaseID)
	if id == "" || len(id) > releaseIDMaxBytes || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return "", errors.New("release.json has an unusable release_id")
	}
	return id, nil
}
