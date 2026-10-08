package itchio

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpdateMetadataDecodingAndFingerprintPreference(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/games/42/uploads" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"uploads":[{"id":7,"filename":"cart.gb","display_name":"Game Boy","type":"default","build_id":12,"md5_hash":"abc","updated_at":"2026-10-01T10:15:20Z","size":4096}]}`)
	}))
	defer srv.Close()
	uploads, err := NewClientWithBase(srv.URL).FetchUploadsForKey("TOKEN", "42", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(uploads) != 1 {
		t.Fatalf("uploads = %+v", uploads)
	}
	u := uploads[0]
	if u.DisplayName != "Game Boy" || u.Type != "default" || u.Fingerprint() != "build:12" {
		t.Fatalf("decoded metadata = %+v", u)
	}
	u.BuildID = 0
	if u.Fingerprint() != "md5:abc" {
		t.Fatal("checksum fingerprint not preferred over timestamp")
	}
	u.MD5 = ""
	if u.Fingerprint() != "upd:2026-10-01T10:15:20Z/4096" {
		t.Fatalf("timestamp fingerprint = %s", u.Fingerprint())
	}
	u.UpdatedAt = time.Time{}
	if u.Fingerprint() != "" {
		t.Fatal("size alone is not a reliable content fingerprint")
	}
}

func TestPageUploadNamesArePublicAndBoundedToUploadNodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/game" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected public request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `<strong class="name">not-an-upload.gb</strong><div class="upload"><strong class="name" title="cart.gb">Title</strong></div><div class="upload"><strong class="name">cart.gb</strong></div><div class="upload"><strong class="name">cover.png</strong></div><div class="upload"><strong class="name">cart.nes</strong></div><div class="upload"><strong class="name">cart.md</strong></div>`)
	}))
	defer srv.Close()
	client := NewClientWithBase(srv.URL)
	client.SetAuthToken("TOKEN")
	names, err := client.FetchPageUploadNames(srv.URL + "/game")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 || names[0] != "cart.gb" || names[1] != "cart.nes" || names[2] != "cart.md" {
		t.Fatalf("public upload names = %v", names)
	}
}

func TestUploadMetadataMalformedCollectionIsNotAnEmptyList(t *testing.T) {
	for _, body := range []string{`{"uploads":"unavailable"}`, `{"uploads":{"error":"unavailable"}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		_, err := NewClientWithBase(srv.URL).FetchUploadsForKey("TOKEN", "42", "")
		srv.Close()
		if err == nil {
			t.Fatalf("malformed response looked like an authoritative empty list: %s", body)
		}
	}
}

func TestMalformedUploadTimestampStillLists(t *testing.T) {
	// The upload list also drives signed-in downloads, so one bad timestamp
	// must not hide every file. The timestamp is only a weak fingerprint.
	for _, tc := range []struct {
		name, value, fingerprint string
	}{
		{"garbage", `"garbage"`, ""},
		{"number", `12345`, ""},
		{"object", `{"at":"2026-10-01"}`, ""},
		{"null", `null`, ""},
		{"rfc3339", `"2026-10-01T10:15:20Z"`, "upd:2026-10-01T10:15:20Z/4"},
		{"space separated", `"2026-10-01 10:15:20"`, "upd:2026-10-01T10:15:20Z/4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"uploads":[{"id":7,"filename":"cart.gb","size":4,"updated_at":%s},{"id":8,"filename":"two.gbc"}]}`, tc.value)
			}))
			defer srv.Close()
			uploads, err := NewClientWithBase(srv.URL).FetchUploadsForKey("TOKEN", "42", "")
			if err != nil {
				t.Fatalf("listing failed: %v", err)
			}
			if len(uploads) != 2 || uploads[0].UploadID != "7" || uploads[0].Fingerprint() != tc.fingerprint {
				t.Fatalf("uploads = %+v, want both with fingerprint %q", uploads, tc.fingerprint)
			}
		})
	}
}
