package inventory

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

// removalSite is an offline itch.io for decision D8: game 42 with an
// installed game.gb (upload 1).
type removalSite struct {
	*httptest.Server
	mu       sync.Mutex
	owned    bool   // the account holds a download key for the game
	uploads  string // upload list JSON, or "" for HTTP status uploadsStatus
	status   int    // upload list status when uploads is ""
	pageBody string
}

func newRemovalSite(t *testing.T) *removalSite {
	t.Helper()
	site := &removalSite{status: http.StatusOK}
	site.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site.mu.Lock()
		defer site.mu.Unlock()
		switch r.URL.Path {
		case "/profile/owned-keys":
			if site.owned {
				fmt.Fprint(w, `{"owned_keys":[{"id":123,"game_id":42,"purchase_id":456}],"per_page":100}`)
			} else {
				fmt.Fprint(w, `{"owned_keys":{}}`)
			}
		case "/games/42/uploads":
			if site.uploads == "" {
				w.WriteHeader(site.status)
				return
			}
			fmt.Fprint(w, site.uploads)
		case "/game":
			fmt.Fprint(w, site.pageBody)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(site.Close)
	return site
}

func (site *removalSite) set(apply func(*removalSite)) {
	site.mu.Lock()
	defer site.mu.Unlock()
	apply(site)
}

const (
	listingWithROM = `{"uploads":[{"id":1,"filename":"game.gb","build_id":1},
		{"id":2,"filename":"game-windows.zip","traits":["p_windows"]},{"id":3,"filename":"game-mac.zip","traits":["p_osx"]}]}`
	listingDesktopOnly = `{"uploads":[{"id":2,"filename":"game-windows.zip","traits":["p_windows"]},
		{"id":3,"filename":"game-mac.zip","traits":["p_osx"]}]}`
	pageDesktopOnly = `<div class="upload"><strong class="name">game-windows.zip</strong></div>
		<div class="upload"><strong class="name">game-mac.zip</strong></div>`
)

// removalService installs game.gb from upload 1 and runs one check against
// initial, so later checks compare against an established baseline.
func removalService(t *testing.T, site *removalSite, free, signedIn bool, initial string) (*UpdateService, *Inventory, string) {
	t.Helper()
	dir := t.TempDir()
	rom := filepath.Join(dir, "game.gb")
	if err := os.WriteFile(rom, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}
	url := site.URL + "/game"
	inv := emptyInventory()
	inv.Add(url, Entry{GameID: "42", Title: "Game", IsFree: free},
		DownloadedFile{Filename: "game.gb", OriginalUpload: "game.gb", UploadID: "1", UploadFingerprint: "build:1", DestPath: rom})
	client := itchio.NewClientWithBase(site.URL)
	client.HTTPClient().Transport = http.DefaultTransport // deterministic statuses; transport retries have their own tests
	if signedIn {
		client.SetAuthToken("A")
	}
	site.set(func(site *removalSite) { site.uploads, site.pageBody = initial, "" })
	svc := NewUpdateService(inv, filepath.Join(dir, "inventory.json"), client, nil)
	svc.runCheck(checkRequest{all: true})
	if inv.IsRemoved(url) || inv.HasPendingUpdates(url) {
		t.Fatal("the first check changed the game's state")
	}
	return svc, inv, url
}

func TestCompleteListingWithoutTheInstalledROMMarksRemoved(t *testing.T) {
	for _, tc := range []struct {
		name        string
		free, owned bool
	}{
		{"owned paid game", false, true},
		{"free game", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := newRemovalSite(t)
			site.owned = tc.owned
			svc, inv, url := removalService(t, site, tc.free, true, listingWithROM)
			site.set(func(site *removalSite) { site.uploads = listingDesktopOnly })
			svc.runCheck(checkRequest{all: true})
			if !inv.IsRemoved(url) || inv.HasPendingUpdates(url) {
				t.Fatalf("removed=%v pending=%+v, want removed: only desktop builds remain", inv.IsRemoved(url), inv.PendingUpdateFiles(url))
			}
			// A failed check keeps the state.
			site.set(func(site *removalSite) { site.uploads, site.status = "", http.StatusServiceUnavailable })
			svc.runCheck(checkRequest{all: true})
			if !inv.IsRemoved(url) {
				t.Fatal("a failed check cleared the removed state")
			}
			// The upload comes back.
			site.set(func(site *removalSite) { site.uploads = listingWithROM })
			svc.runCheck(checkRequest{all: true})
			if inv.IsRemoved(url) {
				t.Fatal("the upload is listed again but the game stays removed")
			}
		})
	}
}

func TestSameKindReplacementShowsAnUpdateInsteadOfRemoved(t *testing.T) {
	for _, tc := range []struct {
		name, first, then string
	}{
		{"new build", listingWithROM, `{"uploads":[{"id":4,"filename":"game-v2.gb","build_id":1},
			{"id":2,"filename":"game-windows.zip","traits":["p_windows"]}]}`},
		// R18-3: another build was always listed; once the installed one is
		// gone, it is the update.
		{"build listed before that you did not install", `{"uploads":[{"id":1,"filename":"game.gb"},{"id":4,"filename":"game-ez.gb"},
			{"id":2,"filename":"game-windows.zip","traits":["p_windows"]}]}`, `{"uploads":[{"id":4,"filename":"game-ez.gb"},
			{"id":2,"filename":"game-windows.zip","traits":["p_windows"]}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := newRemovalSite(t)
			site.owned = true
			svc, inv, url := removalService(t, site, false, true, tc.first)
			site.set(func(site *removalSite) { site.uploads = tc.then })
			svc.runCheck(checkRequest{all: true})
			if inv.IsRemoved(url) {
				t.Fatal("a same-kind replacement marked the game removed")
			}
			if pending := inv.PendingUpdateFiles(url); len(pending) != 1 || pending[0].UploadID != "4" {
				t.Fatalf("pending = %+v, want the replacement as an update", pending)
			}
		})
	}
}

func TestOnlyACompleteAccessibleListingProvesRemoval(t *testing.T) {
	for _, tc := range []struct {
		name     string
		signedIn bool
		apply    func(*removalSite)
	}{
		{"signed out, page lists only desktop builds", false, func(site *removalSite) {
			site.pageBody = pageDesktopOnly
		}},
		{"no download key for a paid game", true, func(site *removalSite) {
			site.owned, site.uploads, site.pageBody = false, listingDesktopOnly, pageDesktopOnly
		}},
		{"upload list refused", true, func(site *removalSite) {
			site.owned, site.uploads, site.status, site.pageBody = false, "", http.StatusForbidden, pageDesktopOnly
		}},
		{"empty list without a key", true, func(site *removalSite) {
			site.owned, site.uploads, site.pageBody = false, `{"uploads":[]}`, pageDesktopOnly
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := newRemovalSite(t)
			site.owned = true
			svc, inv, url := removalService(t, site, false, tc.signedIn, listingWithROM)
			site.set(tc.apply)
			svc.runCheck(checkRequest{all: true})
			if inv.IsRemoved(url) {
				t.Fatal("a listing that cannot prove removal marked the game removed")
			}
		})
	}
}

func TestPublicPageKeepsARemovalItCannotDisprove(t *testing.T) {
	site := newRemovalSite(t)
	site.owned = true
	svc, inv, url := removalService(t, site, false, true, listingWithROM)
	site.set(func(site *removalSite) { site.uploads = listingDesktopOnly })
	svc.runCheck(checkRequest{all: true})
	if !inv.IsRemoved(url) {
		t.Fatal("setup: game not removed")
	}
	// Signed out, the page loads but lists only desktop builds.
	svc.client.SetAuthToken("")
	site.set(func(site *removalSite) { site.pageBody = pageDesktopOnly })
	svc.runCheck(checkRequest{all: true})
	if !inv.IsRemoved(url) {
		t.Fatal("a public page without the file cleared the removed state")
	}
	site.set(func(site *removalSite) {
		site.pageBody = `<div class="upload"><strong class="name">game.gb</strong></div>`
	})
	svc.runCheck(checkRequest{all: true})
	if inv.IsRemoved(url) {
		t.Fatal("the public page lists the file again but the game stays removed")
	}
}
