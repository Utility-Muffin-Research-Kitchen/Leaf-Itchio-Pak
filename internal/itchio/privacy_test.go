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
	if strings.Contains(buf.String(), signature) || strings.Contains(err.Error(), signature) {
		t.Errorf("resolver response leaked its signed credential:\n%s", buf.String())
	}
}

func TestCredentialBearingRequestURLsStayOutOfReturnedErrors(t *testing.T) {
	const (
		apiKey     = "phase94-error-api-3fa8"
		downloadID = "phase94-download-id-52ce"
		signature  = "phase94-error-signature-1bd7"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	base := srv.URL
	srv.Close()

	client := itchio.NewClientWithBase(base)
	_, headerErr := client.FetchFileHeader(base+"/game.zip?X-Amz-Signature="+signature, 8)
	_, uploadsErr := client.FetchUploadsForKey(apiKey, "7", downloadID)
	for name, err := range map[string]error{"signed header": headerErr, "owned uploads": uploadsErr} {
		if err == nil {
			t.Fatalf("%s request unexpectedly succeeded", name)
		}
		for _, forbidden := range []string{apiKey, downloadID, signature, base} {
			if strings.Contains(err.Error(), forbidden) {
				t.Errorf("%s error leaked %q: %v", name, forbidden, err)
			}
		}
	}
}

func TestAPIValidationDoesNotLogAccountOrCredentialMaterial(t *testing.T) {
	const (
		apiKey   = "phase94-validation-api-4da2"
		username = "phase94-private-account"
		cookie   = "phase94-profile-cookie-83bc"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Errorf("Authorization header = %q", got)
		}
		switch r.URL.Path {
		case "/profile":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: cookie})
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]string{"username": username}})
		case "/profile/owned-keys":
			_, _ = w.Write([]byte(`{"owned_keys":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	buf := captureDebugLog(t)
	logger.RegisterSecret(apiKey, "[API-KEY]")
	t.Cleanup(func() { logger.RemoveSecret("[API-KEY]") })
	client := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL)
	gotUsername, _, err := client.ValidateAPIKey(apiKey)
	if err != nil {
		t.Fatal(err)
	}
	if gotUsername != username {
		t.Fatalf("returned username = %q, want %q", gotUsername, username)
	}
	out := buf.String()
	for _, forbidden := range []string{apiKey, username, cookie, "Authorization: Bearer"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("validation log leaked %q:\n%s", forbidden, out)
		}
	}
	if !strings.Contains(out, "authenticated itch.io account") {
		t.Errorf("sanitized authentication diagnostic missing:\n%s", out)
	}
}
