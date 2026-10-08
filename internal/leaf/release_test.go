package leaf

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadReleaseID(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema": 1, "version": "v0.13.0-beta.1", "release_id": "v0.13.0-beta.1"}`
	if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readReleaseID(dir)
	if err != nil || got != "v0.13.0-beta.1" {
		t.Fatalf("readReleaseID() = %q, %v; want v0.13.0-beta.1", got, err)
	}
}

func TestReadReleaseIDMissingFile(t *testing.T) {
	if _, err := readReleaseID(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("readReleaseID() error = %v, want fs.ErrNotExist", err)
	}
	if _, err := readReleaseID(""); err == nil {
		t.Fatal("readReleaseID(\"\") succeeded, want an error")
	}
}

func TestReadReleaseIDRejectsUnusableFiles(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":     `{"release_id":`,
		"not an object":      `["v0.13.0"]`,
		"no release_id":      `{"schema": 1, "version": "v0.13.0"}`,
		"non-string":         `{"release_id": 13}`,
		"blank":              `{"release_id": "  "}`,
		"control characters": `{"release_id": "v0.13.0\nforged log line"}`,
		"over Jawaka's cap":  `{"release_id": "` + strings.Repeat("a", releaseIDMaxBytes+1) + `"}`,
		"oversized file":     `{"release_id": "v0.13.0", "pad": "` + strings.Repeat(" ", releaseJSONMaxBytes) + `"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got, err := readReleaseID(dir); err == nil {
				t.Fatalf("readReleaseID() = %q, want an error", got)
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
