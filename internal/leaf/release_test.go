package leaf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRelease(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadRelease(t *testing.T) {
	tests := []struct {
		name, body string
		want       Release
	}{
		{"both", `{"schema": 1, "version": "v0.13.0-dev", "release_id": "2026-07-20-gabc1234"}`,
			Release{Version: "v0.13.0-dev", ReleaseID: "2026-07-20-gabc1234"}},
		{"version only", `{"version": " v0.13.0 "}`, Release{Version: "v0.13.0"}},
		{"release_id only", `{"release_id": "v0.13.0"}`, Release{ReleaseID: "v0.13.0"}},
		// Like Jawaka, a non-string field reads as empty rather than failing.
		{"non-string version", `{"version": 13, "release_id": "v0.13.0"}`, Release{ReleaseID: "v0.13.0"}},
		{"unusable version", `{"version": "v0.13\u0000", "release_id": "v0.13.0"}`, Release{ReleaseID: "v0.13.0"}},
		{"at Jawaka's caps", `{"version": "` + strings.Repeat("v", versionMaxBytes) + `", "release_id": "` + strings.Repeat("r", releaseIDMaxBytes) + `"}`,
			Release{Version: strings.Repeat("v", versionMaxBytes), ReleaseID: strings.Repeat("r", releaseIDMaxBytes)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := readRelease(writeRelease(t, test.body))
			if err != nil || got != test.want {
				t.Fatalf("readRelease() = %+v, %v; want %+v", got, err, test.want)
			}
		})
	}
}

func TestReleaseString(t *testing.T) {
	tests := []struct {
		release Release
		want    string
	}{
		{Release{Version: "v0.13.0", ReleaseID: "v0.13.0"}, "v0.13.0"},
		{Release{Version: "v0.13.0-dev", ReleaseID: "2026-07-20-gabc1234"}, "v0.13.0-dev (2026-07-20-gabc1234)"},
		{Release{ReleaseID: "2026-07-20-gabc1234"}, "2026-07-20-gabc1234"},
		{Release{Version: "v0.13.0"}, "v0.13.0"},
	}
	for _, test := range tests {
		if got := test.release.String(); got != test.want {
			t.Errorf("%+v.String() = %q, want %q", test.release, got, test.want)
		}
	}
}

func TestReadReleaseMissingFile(t *testing.T) {
	if _, err := readRelease(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readRelease() error = %v, want fs.ErrNotExist", err)
	}
	if _, err := readRelease(""); err == nil {
		t.Fatal("readRelease(\"\") succeeded, want an error")
	}
}

func TestReadReleaseRejectsUnusableFiles(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":     `{"release_id":`,
		"not an object":      `["v0.13.0"]`,
		"no identifiers":     `{"schema": 1}`,
		"non-strings":        `{"version": 13, "release_id": 13}`,
		"blank":              `{"version": "", "release_id": "  "}`,
		"control characters": `{"release_id": "v0.13.0\nforged log line"}`,
		// Jawaka drops the whole record when either string overflows its buffer.
		"release_id over cap": `{"version": "v0.13.0", "release_id": "` + strings.Repeat("a", releaseIDMaxBytes+1) + `"}`,
		"version over cap":    `{"version": "` + strings.Repeat("v", versionMaxBytes+1) + `", "release_id": "v0.13.0"}`,
		"oversized file":      `{"release_id": "v0.13.0", "pad": "` + strings.Repeat(" ", releaseJSONMaxBytes) + `"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := readRelease(writeRelease(t, body)); err == nil {
				t.Fatalf("readRelease() = %+v, want an error", got)
			}
		})
	}
}

func TestInternalDataPathDefaults(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"explicit", map[string]string{"UMRK_INTERNAL_DATA_PATH": "/cards/a/.umrk/mlp1/", "SDCARD_PATH": "/cards/b"}, "/cards/a/.umrk/mlp1"},
		{"from card root", map[string]string{"SDCARD_PATH": "/cards/b", "PLATFORM": "mlp1"}, "/cards/b/.umrk/mlp1"},
		{"device defaults", map[string]string{}, "/mnt/sdcard/.umrk/mlp1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := internalDataPath(mapEnv(test.env)); got != test.want {
				t.Fatalf("internalDataPath() = %q, want %q", got, test.want)
			}
		})
	}
}
