package itchio_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

const v2Key = "v2-api-secret-5be1"

// fakeAPI is an offline api.itch.io plus a separate CDN host.
type fakeAPI struct {
	t   *testing.T
	api *httptest.Server
	cdn *httptest.Server

	mu            sync.Mutex
	sessionForms  []string // download_key_id sent to each session create
	resolveUUIDs  []string // uuid sent to each resolve
	resolveKeys   []string // download_key_id sent to each resolve
	cdnAuth       []string // Authorization seen by the CDN
	sessionStatus int      // 0 = 201 with a UUID
	blockSession  chan struct{}
	requestURLs   []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{t: t}
	f.cdn = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.cdnAuth = append(f.cdnAuth, r.Header.Get("Authorization"))
		f.requestURLs = append(f.requestURLs, r.URL.String())
		f.mu.Unlock()
		w.Write([]byte("ROMDATA"))
	}))
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requestURLs = append(f.requestURLs, r.URL.String())
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+v2Key {
			http.Error(w, "missing bearer", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/games/42/download-sessions":
			r.ParseForm()
			f.mu.Lock()
			f.sessionForms = append(f.sessionForms, r.PostForm.Get("download_key_id"))
			status, block := f.sessionStatus, f.blockSession
			f.mu.Unlock()
			if block != nil {
				<-block
			}
			if status != 0 {
				w.WriteHeader(status)
				return
			}
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"uuid":"install-uuid-7f3a"}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/uploads/") && strings.HasSuffix(r.URL.Path, "/download"):
			f.mu.Lock()
			f.resolveUUIDs = append(f.resolveUUIDs, r.URL.Query().Get("uuid"))
			f.resolveKeys = append(f.resolveKeys, r.URL.Query().Get("download_key_id"))
			f.mu.Unlock()
			http.Redirect(w, r, f.cdn.URL+"/r2/upload.gbc?X-Amz-Signature=signed-8d2c", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { f.api.Close(); f.cdn.Close() })
	return f
}

func (f *fakeAPI) client() *itchio.Client {
	return itchio.NewClientWithBaseAndButler(f.api.URL, f.api.URL)
}

func (f *fakeAPI) snapshot() (sessions, uuids, keys, cdnAuth, urls []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sessionForms...), append([]string(nil), f.resolveUUIDs...),
		append([]string(nil), f.resolveKeys...), append([]string(nil), f.cdnAuth...),
		append([]string(nil), f.requestURLs...)
}

func TestResolveReadsRedirectWithoutFetchingTheCDN(t *testing.T) {
	f := newFakeAPI(t)
	session := roms.NewInstallSession("42", "777")
	cdnURL, err := f.client().ResolveUploadURLContext(context.Background(), v2Key, "9", session)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cdnURL, f.cdn.URL+"/r2/upload.gbc") {
		t.Fatalf("cdnURL = %q", cdnURL)
	}
	if _, _, _, cdnAuth, _ := f.snapshot(); len(cdnAuth) != 0 {
		t.Fatalf("resolver fetched the CDN itself (%d request(s))", len(cdnAuth))
	}
}

func TestDownloadSendsTheKeyOnlyToTheAPIOrigin(t *testing.T) {
	f := newFakeAPI(t)
	dest := filepath.Join(t.TempDir(), "game.gbc")
	session := roms.NewInstallSession("42", "777")
	if err := f.client().DownloadUploadContext(context.Background(), v2Key, "9", session, dest, nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dest); string(data) != "ROMDATA" {
		t.Fatalf("downloaded %q", data)
	}
	_, _, _, cdnAuth, urls := f.snapshot()
	if len(cdnAuth) != 1 || cdnAuth[0] != "" {
		t.Fatalf("CDN Authorization headers = %q, want one empty", cdnAuth)
	}
	for _, requestURL := range urls {
		if strings.Contains(requestURL, v2Key) {
			t.Fatalf("API key appeared in request URL %q", requestURL)
		}
	}
}

func TestInstallSessionIsCreatedOnceAndSharedByEveryResolution(t *testing.T) {
	f := newFakeAPI(t)
	client := f.client()
	session := roms.NewInstallSession("42", "777")
	var wg sync.WaitGroup
	for index := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.ResolveUploadURLContext(context.Background(), v2Key, fmt.Sprint(index), session); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	sessions, uuids, keys, _, _ := f.snapshot()
	if len(sessions) != 1 || sessions[0] != "777" {
		t.Fatalf("session creates = %q, want one with the purchase", sessions)
	}
	for index := range uuids {
		if uuids[index] != "install-uuid-7f3a" || keys[index] != "777" {
			t.Fatalf("resolution %d sent uuid %q key %q", index, uuids[index], keys[index])
		}
	}

	// A new install gets its own session.
	if _, err := client.ResolveUploadURLContext(context.Background(), v2Key, "1", roms.NewInstallSession("42", "777")); err != nil {
		t.Fatal(err)
	}
	if sessions, _, _, _, _ := f.snapshot(); len(sessions) != 2 {
		t.Fatalf("session creates = %d, want a second for the new install", len(sessions))
	}
}

func TestFreeAPIDownloadHasNoPurchaseID(t *testing.T) {
	f := newFakeAPI(t)
	if _, err := f.client().ResolveUploadURLContext(context.Background(), v2Key, "9", roms.NewInstallSession("42", "")); err != nil {
		t.Fatal(err)
	}
	sessions, uuids, keys, _, urls := f.snapshot()
	if len(sessions) != 1 || sessions[0] != "" || uuids[0] == "" || keys[0] != "" {
		t.Fatalf("sessions %q uuids %q keys %q", sessions, uuids, keys)
	}
	for _, requestURL := range urls {
		if strings.Contains(requestURL, "download_key_id") {
			t.Fatalf("free download sent a purchase ID: %s", requestURL)
		}
	}
}

func TestFailedSessionCreationDegradesToUngroupedDownloads(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusTooManyRequests} {
		f := newFakeAPI(t)
		f.sessionStatus = status
		client := f.client()
		session := roms.NewInstallSession("42", "777")
		for range 3 {
			if _, err := client.ResolveUploadURLContext(context.Background(), v2Key, "9", session); err != nil {
				t.Fatalf("HTTP %d session: %v", status, err)
			}
		}
		sessions, uuids, _, _, _ := f.snapshot()
		if len(sessions) != 1 {
			t.Fatalf("HTTP %d: %d session attempts, want exactly one", status, len(sessions))
		}
		for _, uuid := range uuids {
			if uuid != "" {
				t.Fatalf("HTTP %d: resolve sent uuid %q after a failed create", status, uuid)
			}
		}
	}
}

func TestCancellationDuringSessionCreationStopsTheOperation(t *testing.T) {
	f := newFakeAPI(t)
	f.blockSession = make(chan struct{})
	defer close(f.blockSession)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := f.client().ResolveUploadURLContext(ctx, v2Key, "9", roms.NewInstallSession("42", "777"))
		done <- err
	}()
	for {
		if sessions, _, _, _, _ := f.snapshot(); len(sessions) == 1 {
			break
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, uuids, _, _, _ := f.snapshot(); len(uuids) != 0 {
		t.Fatal("resolved an upload after cancellation")
	}
}

func TestResolverRejectsUnusableResponses(t *testing.T) {
	const signature = "signed-query-secret-a1b2"
	cases := map[string]http.HandlerFunc{
		"redirect without Location": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusFound) },
		"non-http location": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "ftp://cdn.example/file?X-Amz-Signature="+signature)
			w.WriteHeader(http.StatusFound)
		},
		"location with credentials": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "https://user:pw@cdn.example/file?X-Amz-Signature="+signature)
			w.WriteHeader(http.StatusFound)
		},
		"access denied": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"errors":["no access"]}`, http.StatusForbidden)
		},
		"errors body": func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"errors":["https://cdn.example/x?X-Amz-Signature=`+signature+`"]}`)
		},
		"malformed body": func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"url":`) },
	}
	for name, handler := range cases {
		srv := httptest.NewServer(handler)
		client := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL)
		_, err := client.ResolveUploadURLContext(context.Background(), v2Key, "9", roms.NewInstallSession("", "777"))
		srv.Close()
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		for _, secret := range []string{v2Key, signature, srv.URL} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: error leaked %q: %v", name, secret, err)
			}
		}
	}
}

func TestSessionUUIDAndKeyStayOutOfLogs(t *testing.T) {
	f := newFakeAPI(t)
	buf := captureDebugLog(t)
	dest := filepath.Join(t.TempDir(), "game.gbc")
	if err := f.client().DownloadUploadContext(context.Background(), v2Key, "9", roms.NewInstallSession("42", "777"), dest, nil); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"install-uuid-7f3a", v2Key, "signed-8d2c", "X-Amz-Signature"} {
		if strings.Contains(buf.String(), secret) {
			t.Errorf("log contains %q:\n%s", secret, buf.String())
		}
	}
}

func TestFetchUploadsHandlesEmptyCollectionShapes(t *testing.T) {
	for _, body := range []string{`{"uploads":{}}`, `{"uploads":null}`, `{}`, `{"uploads":[]}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, body) }))
		uploads, err := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL).FetchUploadsForKey(v2Key, "42", "")
		srv.Close()
		if err != nil || len(uploads) != 0 {
			t.Errorf("%s: uploads %v err %v", body, uploads, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"errors":["invalid key `+v2Key+`"]}`)
	}))
	defer srv.Close()
	if _, err := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL).FetchUploadsForKey(v2Key, "42", ""); err == nil || strings.Contains(err.Error(), v2Key) {
		t.Fatalf("errors body = %v, want a generic error", err)
	}
}

// ownedAPI serves a library of owned keys. When filter is true it applies
// game_ids like itch.io does; otherwise it ignores the filter.
type ownedAPI struct {
	filter    bool
	keys      []map[string]any
	fullScans atomic.Int32
	gameIDs   []string
	onPage    func()
	mu        sync.Mutex
}

func (o *ownedAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+v2Key {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/profile":
			fmt.Fprint(w, `{"user":{"username":"someone"}}`)
		case "/profile/owned-keys":
			if r.URL.Query().Has("game_id") {
				t.Errorf("sent game_id; itch.io takes game_ids")
			}
			filter := r.URL.Query().Get("game_ids")
			o.mu.Lock()
			o.gameIDs = append(o.gameIDs, filter)
			o.mu.Unlock()
			if filter == "" {
				o.fullScans.Add(1)
			}
			if o.onPage != nil {
				o.onPage()
			}
			var page []map[string]any
			for _, key := range o.keys {
				if !o.filter || filter == "" || fmt.Sprint(key["game_id"]) == filter {
					page = append(page, key)
				}
			}
			if len(page) == 0 {
				fmt.Fprint(w, `{"owned_keys":{}}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"per_page": 50, "owned_keys": page})
		default:
			http.NotFound(w, r)
		}
	})
}

func ownedLibrary() []map[string]any {
	key := func(id, game, purchase int) map[string]any {
		return map[string]any{"id": id, "game_id": game, "purchase_id": purchase, "created_at": "2026-01-02T03:04:05Z",
			"game": map[string]any{"id": game, "title": fmt.Sprint("Game ", game), "url": fmt.Sprintf("https://dev.itch.io/g%d", game)}}
	}
	// Game 42 is owned individually (purchase 1) and through a three-game
	// bundle (purchase 2).
	return []map[string]any{key(10, 42, 1), key(11, 42, 2), key(12, 43, 2), key(13, 44, 2), key(14, 45, 3)}
}

func bundleSizes(keys []itchio.OwnedKey) map[int64]int {
	sizes := map[int64]int{}
	for _, key := range keys {
		sizes[key.PurchaseID] = key.BundleSize
	}
	return sizes
}

func TestFetchOwnedKeysUsesGameIDsAndCachedBundleSizes(t *testing.T) {
	owned := &ownedAPI{filter: true, keys: ownedLibrary()}
	srv := httptest.NewServer(owned.handler(t))
	defer srv.Close()
	client := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL)

	if _, _, err := client.ValidateAPIKey(v2Key); err != nil {
		t.Fatal(err)
	}
	scansAfterValidation := owned.fullScans.Load()
	keys, err := client.FetchOwnedKeys(v2Key, "42")
	if err != nil {
		t.Fatal(err)
	}
	if got := bundleSizes(keys); len(keys) != 2 || got[1] != 1 || got[2] != 3 {
		t.Fatalf("bundle sizes = %v, want individual 1 and bundle 3", got)
	}
	if owned.fullScans.Load() != scansAfterValidation {
		t.Fatal("a filtered answer rescanned the library despite cached counts")
	}
	owned.mu.Lock()
	lastFilter := owned.gameIDs[len(owned.gameIDs)-1]
	owned.mu.Unlock()
	if lastFilter != "42" {
		t.Fatalf("game_ids = %q, want 42", lastFilter)
	}
}

func TestFetchOwnedKeysScansOnceOnACacheMiss(t *testing.T) {
	owned := &ownedAPI{filter: true, keys: ownedLibrary()}
	srv := httptest.NewServer(owned.handler(t))
	defer srv.Close()
	client := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL)

	keys, err := client.FetchOwnedKeys(v2Key, "42")
	if err != nil {
		t.Fatal(err)
	}
	if got := bundleSizes(keys); got[1] != 1 || got[2] != 3 {
		t.Fatalf("bundle sizes = %v", got)
	}
	if _, err := client.FetchOwnedKeys(v2Key, "42"); err != nil {
		t.Fatal(err)
	}
	if got := owned.fullScans.Load(); got != 1 {
		t.Fatalf("full scans = %d, want 1 then cached", got)
	}
}

func TestFetchOwnedKeysWorksWhenTheServerIgnoresTheFilter(t *testing.T) {
	owned := &ownedAPI{filter: false, keys: ownedLibrary()}
	srv := httptest.NewServer(owned.handler(t))
	defer srv.Close()
	keys, err := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL).FetchOwnedKeys(v2Key, "42")
	if err != nil {
		t.Fatal(err)
	}
	if got := bundleSizes(keys); len(keys) != 2 || got[1] != 1 || got[2] != 3 {
		t.Fatalf("bundle sizes = %v", got)
	}
	if owned.fullScans.Load() != 0 {
		t.Fatal("an unfiltered answer triggered another scan")
	}
}

func TestReplacingTheKeyDropsAccountDerivedCounts(t *testing.T) {
	owned := &ownedAPI{filter: true, keys: ownedLibrary()}
	srv := httptest.NewServer(owned.handler(t))
	defer srv.Close()
	client := itchio.NewClientWithBaseAndButler(srv.URL, srv.URL)

	if _, _, err := client.ValidateAPIKey(v2Key); err != nil {
		t.Fatal(err)
	}
	client.ResetAPIKeyState()
	before := owned.fullScans.Load()
	if _, err := client.FetchOwnedKeys(v2Key, "42"); err != nil {
		t.Fatal(err)
	}
	if owned.fullScans.Load() != before+1 {
		t.Fatal("counts cached under the previous key were reused")
	}
}

func TestValidationFromAnOlderKeyStoresNothing(t *testing.T) {
	owned := &ownedAPI{filter: true, keys: ownedLibrary()}
	var client *itchio.Client
	var once sync.Once
	owned.onPage = func() { once.Do(client.ResetAPIKeyState) } // key replaced mid-scan
	srv := httptest.NewServer(owned.handler(t))
	defer srv.Close()
	client = itchio.NewClientWithBaseAndButler(srv.URL, srv.URL)

	if _, _, err := client.ValidateAPIKey(v2Key); err != nil {
		t.Fatal(err)
	}
	owned.onPage = nil
	before := owned.fullScans.Load()
	if _, err := client.FetchOwnedKeys(v2Key, "42"); err != nil {
		t.Fatal(err)
	}
	if owned.fullScans.Load() != before+1 {
		t.Fatal("a validation that started under the old key seeded bundle sizes")
	}
}
