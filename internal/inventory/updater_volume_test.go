package inventory

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func init() {
	// Tests that are not about pacing run their requests back to back.
	backgroundRequestInterval = 0
}

// volumeServer is an offline api.itch.io that counts upload-list requests
// per game and owned-key lookups.
type volumeServer struct {
	*httptest.Server
	mu       sync.Mutex
	uploads  map[string]int
	owned    int
	times    []time.Time
	onUpload func(gameID string)
}

func newVolumeServer(t *testing.T) *volumeServer {
	t.Helper()
	srv := &volumeServer{uploads: map[string]int{}}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.mu.Lock()
		srv.times = append(srv.times, time.Now())
		onUpload := srv.onUpload
		srv.mu.Unlock()
		switch {
		case r.URL.Path == "/profile/owned-keys":
			srv.mu.Lock()
			srv.owned++
			srv.mu.Unlock()
			fmt.Fprint(w, `{"owned_keys":{}}`)
		case strings.HasPrefix(r.URL.Path, "/games/") && strings.HasSuffix(r.URL.Path, "/uploads"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/games/"), "/uploads")
			srv.mu.Lock()
			srv.uploads[id]++
			srv.mu.Unlock()
			if onUpload != nil {
				onUpload(id)
			}
			fmt.Fprintf(w, `{"uploads":[{"id":%s0,"filename":"cart%s.gb","build_id":1}]}`, id, id)
		default:
			t.Errorf("unexpected update request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (srv *volumeServer) uploadRequests(id string) int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.uploads[id]
}

func (srv *volumeServer) ownedRequests() int {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.owned
}

// volumeInventory records one installed ROM for each game ID.
func volumeInventory(t *testing.T, srv *volumeServer, ids ...string) (*Inventory, string) {
	t.Helper()
	dir := t.TempDir()
	inv := emptyInventory()
	for _, id := range ids {
		rom := filepath.Join(dir, "cart"+id+".gb")
		if err := os.WriteFile(rom, []byte("rom"), 0o644); err != nil {
			t.Fatal(err)
		}
		inv.Add(srv.URL+"/game"+id, Entry{GameID: id, Title: "Game " + id},
			DownloadedFile{Filename: "cart" + id + ".gb", UploadID: id + "0", DestPath: rom})
	}
	path := filepath.Join(dir, "inventory.json")
	if err := inv.Save(path); err != nil {
		t.Fatal(err)
	}
	return inv, path
}

// launch runs the startup check of a fresh service and waits for it.
func launch(t *testing.T, inv *Inventory, path string, client *itchio.Client) *UpdateService {
	t.Helper()
	done := make(chan struct{}, 4)
	svc := NewUpdateService(inv, path, client, nil)
	svc.Start(func() { done <- struct{}{} })
	t.Cleanup(svc.Stop)
	waitForRun(t, done)
	return svc
}

func waitForRun(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("update check did not finish")
	}
}

func TestLaunchesWithinSixHoursDoNotRecheckUploads(t *testing.T) {
	srv := newVolumeServer(t)
	inv, path := volumeInventory(t, srv, "42")
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	launch(t, inv, path, client)
	if srv.uploadRequests("42") != 1 || srv.ownedRequests() != 1 {
		t.Fatalf("first launch: uploads=%d owned=%d", srv.uploadRequests("42"), srv.ownedRequests())
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	launch(t, reloaded, path, client)
	if srv.uploadRequests("42") != 1 || srv.ownedRequests() != 1 {
		t.Fatalf("second launch within 6 hours: uploads=%d owned=%d, want no new requests",
			srv.uploadRequests("42"), srv.ownedRequests())
	}
	reloaded.mu.Lock()
	reloaded.Entries[srv.URL+"/game42"].UpdateCheckedAt = time.Now().Add(-7 * time.Hour)
	reloaded.mu.Unlock()
	launch(t, reloaded, path, client)
	if srv.uploadRequests("42") != 2 {
		t.Fatalf("launch after 7 hours: uploads=%d, want a new check", srv.uploadRequests("42"))
	}
}

func TestUpdateInventoryChecksEveryEntry(t *testing.T) {
	srv := newVolumeServer(t)
	inv, path := volumeInventory(t, srv, "42")
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	done := make(chan struct{}, 4)
	svc := NewUpdateService(inv, path, client, nil)
	svc.Start(func() { done <- struct{}{} })
	defer svc.Stop()
	waitForRun(t, done)
	svc.TriggerNow()
	waitForRun(t, done)
	if srv.uploadRequests("42") != 1 {
		t.Fatalf("automatic re-check within 6 hours made %d upload requests, want 1", srv.uploadRequests("42"))
	}
	svc.CheckAllNow()
	waitForRun(t, done)
	if srv.uploadRequests("42") != 2 {
		t.Fatalf("Update Inventory made %d upload requests, want a second", srv.uploadRequests("42"))
	}
}

func TestSignInRechecksEntriesWithoutAnAPIListing(t *testing.T) {
	srv := newVolumeServer(t)
	inv, path := volumeInventory(t, srv, "42")
	inv.SetUpstreamFiles(srv.URL+"/game42", []UpstreamFile{{Filename: "cart42.gb"}}) // checked signed out
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	launch(t, inv, path, client)
	if srv.uploadRequests("42") != 1 {
		t.Fatalf("first signed-in check skipped an entry last checked signed out: uploads=%d", srv.uploadRequests("42"))
	}
}

func TestBackgroundRequestsArePaced(t *testing.T) {
	srv := newVolumeServer(t)
	inv, path := volumeInventory(t, srv, "41", "42", "43")
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	svc := NewUpdateService(inv, path, client, nil)
	svc.requestInterval = 40 * time.Millisecond
	svc.runCheck(checkRequest{all: true})
	srv.mu.Lock()
	times := append([]time.Time(nil), srv.times...)
	srv.mu.Unlock()
	if len(times) != 4 {
		t.Fatalf("made %d requests, want ownership plus three upload lists", len(times))
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < 35*time.Millisecond {
			t.Fatalf("request %d followed the previous one after %v", i, gap)
		}
	}
}

func TestNoBackgroundCheckWhileDownloading(t *testing.T) {
	srv := newVolumeServer(t)
	inv, path := volumeInventory(t, srv, "41", "42")
	client := itchio.NewClientWithBase(srv.URL)
	client.SetAuthToken("A")
	done := make(chan struct{}, 4)
	svc := NewUpdateService(inv, path, client, nil)
	finished := svc.DownloadStarted()
	svc.Start(func() { done <- struct{}{} })
	defer svc.Stop()
	waitForRun(t, done)
	if srv.uploadRequests("41")+srv.uploadRequests("42") != 0 || srv.ownedRequests() != 0 {
		t.Fatal("startup check ran while a download was running")
	}
	finished()
	finished() // a second call must not count twice
	waitForRun(t, done)
	if srv.uploadRequests("41") != 1 || srv.uploadRequests("42") != 1 {
		t.Fatalf("deferred check after the download: 41=%d 42=%d", srv.uploadRequests("41"), srv.uploadRequests("42"))
	}

	// A download that starts during a check stops it before the next game.
	var finishSecond func()
	srv.mu.Lock()
	srv.onUpload = func(string) {
		srv.mu.Lock()
		srv.onUpload = nil
		srv.mu.Unlock()
		finishSecond = svc.DownloadStarted()
	}
	srv.mu.Unlock()
	svc.CheckAllNow()
	waitForRun(t, done)
	if total := srv.uploadRequests("41") + srv.uploadRequests("42"); total != 3 {
		t.Fatalf("check continued after a download started: %d upload requests, want 3", total)
	}
	finishSecond()
	waitForRun(t, done)
	if srv.uploadRequests("41") != 2 || srv.uploadRequests("42") != 2 {
		t.Fatalf("the rest of Update Inventory after the download: 41=%d 42=%d", srv.uploadRequests("41"), srv.uploadRequests("42"))
	}
}
