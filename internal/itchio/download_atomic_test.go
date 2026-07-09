package itchio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamToFileFailurePreservesDestination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "64")
		_, _ = w.Write([]byte("truncated"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "game.gbc")
	const original = "existing-rom-must-survive"
	if err := os.WriteFile(dest, []byte(original), 0o644); err != nil {
		t.Fatalf("seed destination: %v", err)
	}

	client := NewClientWithBase(srv.URL)
	err := client.streamToFile(srv.URL+"/game.gbc?X-Amz-Signature=do-not-log", dest, nil)
	if err == nil {
		t.Fatal("streamToFile returned nil for a truncated response")
	}

	got, readErr := os.ReadFile(dest)
	if readErr != nil {
		t.Fatalf("read preserved destination: %v", readErr)
	}
	if string(got) != original {
		t.Fatalf("destination changed after failed download: got %q, want %q", got, original)
	}

	entries, readDirErr := os.ReadDir(dir)
	if readDirErr != nil {
		t.Fatalf("read destination directory: %v", readDirErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".itchio-download-") {
			t.Errorf("partial download was not removed: %s", entry.Name())
		}
	}
}

func TestStreamToFileCommitsCompleteResponse(t *testing.T) {
	const content = "complete-rom"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "12")
		_, _ = w.Write([]byte(content))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dest := filepath.Join(dir, "nested", "game.gbc")
	client := NewClientWithBase(srv.URL)
	if err := client.streamToFile(srv.URL+"/game.gbc", dest, nil); err != nil {
		t.Fatalf("streamToFile: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read committed destination: %v", err)
	}
	if string(got) != content {
		t.Fatalf("destination content = %q, want %q", got, content)
	}
}
