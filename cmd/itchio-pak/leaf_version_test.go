package main

import (
	"os"
	"path/filepath"
	"testing"
)

// clearLeafVersionEnv pins every variable readLeafVersion consults, so the
// developer's shell cannot leak a version into the test.
func clearLeafVersionEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"LEAF_VERSION", "UMRK_RELEASE_ID", "UMRK_INTERNAL_DATA_PATH"} {
		t.Setenv(name, "")
	}
	t.Setenv("PLATFORM", "mlp1")
	t.Setenv("SDCARD_PATH", t.TempDir())
}

func writeReleaseJSON(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadLeafVersionFallsBackToInstalledRelease(t *testing.T) {
	clearLeafVersionEnv(t)
	internal := t.TempDir()
	t.Setenv("UMRK_INTERNAL_DATA_PATH", internal)
	writeReleaseJSON(t, internal, `{"schema": 1, "version": "v0.13.0-beta.1", "release_id": "v0.13.0-beta.1"}`)

	if got := readLeafVersion(); got != "v0.13.0-beta.1" {
		t.Fatalf("readLeafVersion() = %q, want the release_id from release.json", got)
	}
}

func TestReadLeafVersionUsesDefaultInternalDataPath(t *testing.T) {
	clearLeafVersionEnv(t)
	card := t.TempDir()
	t.Setenv("SDCARD_PATH", card)
	writeReleaseJSON(t, filepath.Join(card, ".umrk", "mlp1"), `{"release_id": "v0.13.0"}`)

	if got := readLeafVersion(); got != "v0.13.0" {
		t.Fatalf("readLeafVersion() = %q, want release.json under $SDCARD_PATH/.umrk/$PLATFORM", got)
	}
}

func TestReadLeafVersionPrefersEnvironment(t *testing.T) {
	for _, name := range []string{"LEAF_VERSION", "UMRK_RELEASE_ID"} {
		t.Run(name, func(t *testing.T) {
			clearLeafVersionEnv(t)
			internal := t.TempDir()
			t.Setenv("UMRK_INTERNAL_DATA_PATH", internal)
			writeReleaseJSON(t, internal, `{"release_id": "v0.13.0-beta.1"}`)
			t.Setenv(name, " v0.14.0 ")

			if got := readLeafVersion(); got != "v0.14.0" {
				t.Fatalf("readLeafVersion() = %q, want %s to win over release.json", got, name)
			}
		})
	}
}

func TestReadLeafVersionUnknownWithoutUsableRelease(t *testing.T) {
	tests := map[string]string{
		"missing file":   "",
		"malformed JSON": `{"release_id":`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			clearLeafVersionEnv(t)
			internal := t.TempDir()
			t.Setenv("UMRK_INTERNAL_DATA_PATH", internal)
			if body != "" {
				writeReleaseJSON(t, internal, body)
			}

			if got := readLeafVersion(); got != "unknown" {
				t.Fatalf("readLeafVersion() = %q, want unknown", got)
			}
		})
	}
}

// Jawaka's About shows release.json's version and adds the release id only
// when it differs; the id alone stands in when the version is missing.
func TestReadLeafVersionShowsVersionThenDifferingReleaseID(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"same", `{"version": "v0.13.0", "release_id": "v0.13.0"}`, "v0.13.0"},
		{"different", `{"version": "v0.13.0-dev", "release_id": "2026-07-20-gabc1234"}`, "v0.13.0-dev (2026-07-20-gabc1234)"},
		{"version missing", `{"release_id": "2026-07-20-gabc1234"}`, "2026-07-20-gabc1234"},
		{"release_id missing", `{"version": "v0.13.0"}`, "v0.13.0"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearLeafVersionEnv(t)
			internal := t.TempDir()
			t.Setenv("UMRK_INTERNAL_DATA_PATH", internal)
			writeReleaseJSON(t, internal, test.body)

			if got := readLeafVersion(); got != test.want {
				t.Fatalf("readLeafVersion() = %q, want %q", got, test.want)
			}
		})
	}
}
