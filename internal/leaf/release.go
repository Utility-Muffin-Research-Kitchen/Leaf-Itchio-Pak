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
// rejected, and so is a version or release_id that does not fit its 64- or
// 128-byte buffer.
const (
	releaseJSONMaxBytes = 64 * 1024
	versionMaxBytes     = 63
	releaseIDMaxBytes   = 127
)

// Release is the installed Leaf release recorded in release.json.
type Release struct {
	Version   string
	ReleaseID string
}

// String formats the release as Jawaka's About screen does: the version,
// with the release id added only when it says something the version does
// not. The release id alone stands in when the version is missing.
func (r Release) String() string {
	switch {
	case r.Version == "":
		return r.ReleaseID
	case r.ReleaseID == "" || r.ReleaseID == r.Version:
		return r.Version
	default:
		return r.Version + " (" + r.ReleaseID + ")"
	}
}

// InstalledRelease reads release.json from Leaf's launcher control-state
// directory ($UMRK_INTERNAL_DATA_PATH), the record Jawaka reads for its own
// About screen. It returns an error when the file is missing, unreadable or
// malformed, or names neither a usable version nor a usable release_id.
func InstalledRelease() (Release, error) {
	return readRelease(internalDataPath(os.LookupEnv))
}

func readRelease(stateDir string) (Release, error) {
	if stateDir == "" {
		return Release{}, errors.New("Leaf control-state directory is not set")
	}
	file, err := os.Open(filepath.Join(stateDir, "release.json"))
	if err != nil {
		return Release{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, releaseJSONMaxBytes+1))
	if err != nil {
		return Release{}, err
	}
	if len(data) > releaseJSONMaxBytes {
		return Release{}, fmt.Errorf("release.json exceeds %d bytes", releaseJSONMaxBytes)
	}
	// Like Jawaka, a field that is not a string reads as empty.
	var raw struct {
		Version   any `json:"version"`
		ReleaseID any `json:"release_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Release{}, fmt.Errorf("parse release.json: %w", err)
	}
	version, _ := raw.Version.(string)
	releaseID, _ := raw.ReleaseID.(string)
	if len(version) > versionMaxBytes || len(releaseID) > releaseIDMaxBytes {
		return Release{}, errors.New("release.json has an over-long version or release_id")
	}
	release := Release{Version: displayableID(version), ReleaseID: displayableID(releaseID)}
	if release.Version == "" && release.ReleaseID == "" {
		return Release{}, errors.New("release.json has no usable version or release_id")
	}
	return release, nil
}

// displayableID trims value and drops it when it contains control
// characters, which would break the About line and the log.
func displayableID(value string) string {
	value = strings.TrimSpace(value)
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return ""
	}
	return value
}
