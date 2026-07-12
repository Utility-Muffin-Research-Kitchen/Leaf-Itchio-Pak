package roms_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func TestRemoteArchiveErrorDoesNotExposeSignedURL(t *testing.T) {
	const signature = "phase94-archive-signature-8c2e"
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/archive.7z?X-Amz-Signature=" + signature
	srv.Close()

	_, err := roms.InspectRemote7z(&http.Client{}, url)
	if err == nil {
		t.Fatal("InspectRemote7z unexpectedly succeeded")
	}
	for _, forbidden := range []string{signature, url, srv.URL} {
		if strings.Contains(err.Error(), forbidden) {
			t.Errorf("archive error leaked %q: %v", forbidden, err)
		}
	}
}
