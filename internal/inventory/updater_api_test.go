package inventory

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func updateTestInventory(t *testing.T, gameURL string) (*Inventory, string) {
	t.Helper()
	dir := t.TempDir()
	rom := filepath.Join(dir, "cart.gb")
	if err := os.WriteFile(rom, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	inv := emptyInventory()
	inv.Add(gameURL, Entry{GameID: "42", Title: "Paid game"}, DownloadedFile{Filename: "cart.gb", UploadID: "7", DestPath: rom})
	return inv, filepath.Join(dir, "inventory.json")
}

func TestUpdateAPIUsesMetadataForPaidSameNameReplacement(t *testing.T) {
	build, ownershipCalls, uploadCalls := 1, 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer TEST_TOKEN" {
			t.Errorf("unexpected update request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
			return
		}
		switch r.URL.Path {
		case "/profile/owned-keys":
			ownershipCalls++
			if r.URL.Query().Get("game_ids") != "42" {
				t.Error("ownership lookup was not scoped to installed IDs")
			}
			fmt.Fprint(w, `{"owned_keys":[{"id":123,"game_id":42,"purchase_id":456}],"per_page":100}`)
		case "/games/42/uploads":
			uploadCalls++
			if r.URL.Query().Get("download_key_id") != "123" {
				t.Error("paid list did not use the owned key")
			}
			fmt.Fprintf(w, `{"uploads":[{"id":7,"filename":"cart.gb","display_name":"Game Boy build","build_id":%d,"size":4}]}`, build)
		default:
			t.Errorf("update check requested a page or download endpoint despite a stored ID: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	url := srv.URL + "/game"
	inv, path := updateTestInventory(t, url)
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("TEST_TOKEN")
	svc := NewUpdateService(inv, path, client, nil)
	svc.runCheck(checkRequest{all: true})
	if inv.HasPendingUpdates(url) {
		t.Fatal("first API check invented an update")
	}
	build++
	svc.runCheck(checkRequest{all: true})
	if pending := inv.PendingUpdateFiles(url); len(pending) != 1 || pending[0].Fingerprint != "build:2" || !pending[0].Changed {
		t.Fatalf("replacement detection = %+v", pending)
	}
	if inv.IsRemoved(url) {
		t.Fatal("replacement marked game removed")
	}
	if ownershipCalls != 2 || uploadCalls != 2 {
		t.Fatalf("requests: ownership=%d uploads=%d", ownershipCalls, uploadCalls)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"TEST_TOKEN", `"purchase_id":`, `"123"`, `"456"`} {
		if strings.Contains(string(saved), secret) {
			t.Fatalf("inventory persisted account secret %s", secret)
		}
	}
}

func TestUpdateFallbackPreservesKnownFilesAndRemovalSemantics(t *testing.T) {
	const notOwned, owned = `{"owned_keys":{}}`, `{"owned_keys":[{"id":123,"game_id":42,"purchase_id":456}],"per_page":100}`
	status, pageStatus, body, keys := http.StatusForbidden, http.StatusOK, "<html>Purchase required</html>", notOwned
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("update check made %s", r.Method)
		}
		switch r.URL.Path {
		case "/profile/owned-keys":
			fmt.Fprint(w, keys)
		case "/games/42/uploads":
			w.WriteHeader(status)
			fmt.Fprint(w, `{"uploads":[]}`)
		case "/game":
			if r.Header.Get("Authorization") != "" {
				t.Error("public page received a credential")
			}
			w.WriteHeader(pageStatus)
			fmt.Fprint(w, body)
		default:
			t.Errorf("unexpected update request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	url := srv.URL + "/game"
	inv, path := updateTestInventory(t, url)
	inv.SetUpstreamFilesFrom(url, SourceAPI, []UpstreamFile{{Filename: "cart.gb", UploadID: "7", Fingerprint: "build:1"}})
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("TOKEN")
	svc := NewUpdateService(inv, path, client, nil)
	svc.runCheck(checkRequest{all: true})
	entry, _ := inv.Lookup(url)
	if inv.IsRemoved(url) || len(entry.KnownUpstreamFiles) != 1 || entry.UpstreamSource != SourceAPI {
		t.Fatalf("hidden public files were treated as authoritative: %+v", entry)
	}
	// Without a download key, an empty list may only mean you cannot access
	// a paid game, such as one that started charging after a free install.
	status = http.StatusOK
	svc.runCheck(checkRequest{all: true})
	if inv.IsRemoved(url) {
		t.Fatal("empty API list without access marked removed")
	}
	keys = owned
	svc.runCheck(checkRequest{all: true})
	if !inv.IsRemoved(url) {
		t.Fatal("complete empty API list did not mark removed")
	}
	keys = notOwned
	status, body = http.StatusForbidden, `<div class="upload"><strong class="name">replacement.gb</strong></div>`
	svc.runCheck(checkRequest{all: true})
	if inv.IsRemoved(url) {
		t.Fatal("superseding upload did not clear removal")
	}
	pageStatus = http.StatusNotFound
	svc.runCheck(checkRequest{all: true})
	if !inv.IsRemoved(url) {
		t.Fatal("public 404 did not mark removed")
	}
	pageStatus = http.StatusServiceUnavailable
	svc.runCheck(checkRequest{all: true})
	if !inv.IsRemoved(url) {
		t.Fatal("transient failure cleared removal")
	}
}

func TestUpdateDiscardsResultsAfterAccountRoundTrip(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile/owned-keys":
			fmt.Fprint(w, `{"owned_keys":{}}`)
		case "/games/42/uploads":
			close(entered)
			<-resume
			fmt.Fprint(w, `{"uploads":[{"id":7,"filename":"cart.gb","build_id":2}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	url := srv.URL + "/game"
	inv, path := updateTestInventory(t, url)
	inv.SetUpstreamFilesFrom(url, SourceAPI, []UpstreamFile{{Filename: "cart.gb", UploadID: "7", Fingerprint: "build:1"}})
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	svc := NewUpdateService(inv, path, client, nil)
	done := make(chan struct{})
	go func() { svc.runCheck(checkRequest{all: true}); close(done) }()
	<-entered
	client.SetAuthToken("B")
	client.SetAuthToken("A")
	close(resume)
	<-done
	entry, _ := inv.Lookup(url)
	if inv.HasPendingUpdates(url) || entry.KnownUpstreamFiles[0].Fingerprint != "build:1" {
		t.Fatal("a stale account scan published after A -> B -> A")
	}
}

func TestUpdateDoesNotBypassAPIFailures(t *testing.T) {
	for _, endpoint := range []string{"ownership", "uploads"} {
		for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
			t.Run(fmt.Sprintf("%s/%d", endpoint, status), func(t *testing.T) {
				requests := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests++
					if r.URL.Path == "/profile/owned-keys" && endpoint != "ownership" {
						fmt.Fprint(w, `{"owned_keys":{}}`)
						return
					}
					if r.URL.Path != "/profile/owned-keys" && r.URL.Path != "/games/42/uploads" {
						t.Errorf("API error was bypassed through %s", r.URL.Path)
					}
					w.WriteHeader(status)
				}))
				defer srv.Close()
				url := srv.URL + "/game"
				inv, path := updateTestInventory(t, url)
				inv.SetUpstreamFilesFrom(url, SourceAPI, []UpstreamFile{{Filename: "cart.gb", UploadID: "7", Fingerprint: "build:1"}})
				client := itchio.NewClientWithBase(srv.URL)
				client.HTTPClient().Transport = http.DefaultTransport // deterministic status; transport retries have their own tests
				client.SetAuthToken("A")
				NewUpdateService(inv, path, client, nil).runCheck(checkRequest{all: true})
				want := 1
				if endpoint == "uploads" {
					want = 2
				}
				if requests != want {
					t.Fatalf("made %d requests, want %d", requests, want)
				}
				entry, _ := inv.Lookup(url)
				if entry.UpstreamSource != SourceAPI || len(entry.KnownUpstreamFiles) != 1 || entry.KnownUpstreamFiles[0].Fingerprint != "build:1" {
					t.Fatalf("transient error changed known files: %+v", entry)
				}
			})
		}
	}
}

func TestUpdateDiscardsListingFetchedBeforeInstallCompletes(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile/owned-keys":
			fmt.Fprint(w, `{"owned_keys":{}}`)
		case "/games/42/uploads":
			close(entered)
			<-resume
			fmt.Fprint(w, `{"uploads":[{"id":7,"filename":"cart.gb","build_id":1}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	url := srv.URL + "/game"
	inv, path := updateTestInventory(t, url)
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	svc := NewUpdateService(inv, path, client, nil)
	done := make(chan struct{})
	go func() { svc.runCheck(checkRequest{all: true}); close(done) }()
	<-entered
	entry, _ := inv.Lookup(url)
	file := entry.Files[0]
	file.UploadFingerprint = "build:2" // also covers dedup backfill with unchanged timestamps
	inv.Add(url, entry, file)
	close(resume)
	<-done
	entry, _ = inv.Lookup(url)
	if inv.HasPendingUpdates(url) || len(entry.KnownUpstreamFiles) != 0 {
		t.Fatal("old listing was applied after a newer install")
	}
	if len(svc.triggerCh) != 1 {
		t.Fatal("fresh metadata check was not queued")
	}
	if request := svc.takeRequest(); request.all || request.automatic || len(request.games) != 1 || !request.games[url] {
		t.Fatalf("queued re-check = %+v, want only the installed game", request)
	}
}

func TestStaleInstallRecheckWaitsForTheDownload(t *testing.T) {
	entered, resume := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile/owned-keys":
			fmt.Fprint(w, `{"owned_keys":{}}`)
		case "/games/42/uploads":
			close(entered)
			<-resume
			fmt.Fprint(w, `{"uploads":[{"id":7,"filename":"cart.gb","build_id":1}]}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	url := srv.URL + "/game"
	inv, path := updateTestInventory(t, url)
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	svc := NewUpdateService(inv, path, client, nil)
	done := make(chan struct{})
	go func() { svc.runCheck(checkRequest{all: true}); close(done) }()
	<-entered
	finished := svc.DownloadStarted()
	entry, _ := inv.Lookup(url)
	file := entry.Files[0]
	file.UploadFingerprint = "build:2"
	inv.Add(url, entry, file)
	close(resume)
	<-done
	if len(svc.triggerCh) != 0 {
		t.Fatal("re-check was queued while the install was still running")
	}
	finished()
	if len(svc.triggerCh) != 1 {
		t.Fatal("re-check was not queued after the install")
	}
}

func TestEmptyListForGameInstalledFreeIsCheckedOnThePublicPage(t *testing.T) {
	// The inventory still says free when the developer starts charging, so
	// a key-less empty list cannot prove the game was removed.
	pageRequests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/profile/owned-keys":
			fmt.Fprint(w, `{"owned_keys":{}}`)
		case "/games/42/uploads":
			fmt.Fprint(w, `{"uploads":[]}`)
		case "/game":
			pageRequests++
			fmt.Fprint(w, `<div class="buy_row">Buy</div>`)
		default:
			t.Errorf("unexpected update request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	url := srv.URL + "/game"
	inv, path := updateTestInventory(t, url)
	inv.Entries[url].IsFree = true
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("TOKEN")
	NewUpdateService(inv, path, client, nil).runCheck(checkRequest{all: true})
	if inv.IsRemoved(url) || pageRequests != 1 {
		t.Fatalf("removed=%v after %d public page checks, want kept after one", inv.IsRemoved(url), pageRequests)
	}
}
