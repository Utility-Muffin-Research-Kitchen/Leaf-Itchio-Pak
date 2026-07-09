package itchio_test

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

func captureDebugLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)
	logger.SetLevel(logger.LevelDebug)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
		logger.SetLevel(logger.LevelInfo)
	})
	return &buf
}

func TestSignedURLsAndAPIKeysAreAbsentFromDebugLogs(t *testing.T) {
	const (
		signature = "signed-query-secret-7e4f"
		apiKey    = "itch-api-secret-91ab"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/download"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"url": "https://cdn.example.invalid/game.gbc?X-Amz-Signature=" + signature,
			})
		default:
			_, _ = w.Write([]byte("header-bytes"))
		}
	}))
	defer srv.Close()

	buf := captureDebugLog(t)
	client := itchio.NewClientWithBase(srv.URL)
	if _, err := client.FetchFileHeader(srv.URL+"/game.gbc?X-Amz-Signature="+signature, 6); err != nil {
		t.Fatalf("FetchFileHeader: %v", err)
	}
	if _, err := client.ResolveAuthURL(apiKey, "55", "77"); err != nil {
		t.Fatalf("ResolveAuthURL: %v", err)
	}

	out := buf.String()
	for _, secret := range []string{signature, apiKey, "X-Amz-Signature"} {
		if strings.Contains(out, secret) {
			t.Errorf("debug log contains sensitive URL material %q:\n%s", secret, out)
		}
	}
	if !strings.Contains(out, "header fetch: read") {
		t.Errorf("expected non-sensitive range-probe diagnostic, got:\n%s", out)
	}
}

func TestResolverErrorBodyCannotLeakSignedURLCredentials(t *testing.T) {
	const signature = "resolver-error-signature-3c81"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed https://cdn.example/game.zip?X-Amz-Signature="+signature, http.StatusBadGateway)
	}))
	defer srv.Close()

	buf := captureDebugLog(t)
	client := itchio.NewClientWithBase(srv.URL)
	_, err := client.ResolveFreeURL(itchio.Upload{
		Filename: "game.zip",
		URL:      srv.URL + "/file/99?key=eyJpZCI6NDJ9.sig&csrf=private-csrf",
	})
	if err == nil {
		t.Fatal("ResolveFreeURL returned nil for HTTP 502")
	}
	if strings.Contains(buf.String(), signature) {
		t.Errorf("resolver response leaked its signed credential:\n%s", buf.String())
	}
}
