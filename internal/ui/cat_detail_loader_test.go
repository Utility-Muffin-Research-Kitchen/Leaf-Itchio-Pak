//go:build !headless

package ui

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func TestUnavailableDetailRetainsCachedMetadataAndWarnings(t *testing.T) {
	for _, warning := range []bool{false, true} {
		game := itchio.Game{Title: "Cached title", Author: "Creator", URL: "https://example.itch.io/game", Tags: []string{"adventure"}}
		if warning {
			game.Tags = append(game.Tags, "nsfw")
		}
		loader := &CatDetailLoader{game: game, updates: make(chan catDetailResult, 1)}
		loader.updates <- catDetailResult{err: errors.New("GET https://example.invalid/private?token=secret failed")}
		model := appui.NewDetailModel(appui.DetailGame{Title: game.Title, Author: game.Author, URL: game.URL, Downloaded: true})
		cfg := &settings.Config{Filter: settings.ContentFilter{AdultContent: settings.CategoryFilter{Enabled: true}}}
		if !loader.Sync(model, cfg) {
			t.Fatal("failed result was not published")
		}
		want := appui.DetailError
		if warning {
			want = appui.DetailWarning
		}
		if model.State != want || !reflect.DeepEqual(model.Tags, game.Tags) || model.Game.Title != game.Title || model.Game.Author != game.Author || model.Game.URL != game.URL {
			t.Fatalf("cached detail = %+v, want state %v with cached metadata", model, want)
		}
		if strings.Contains(model.ErrorDetail, "https://") || strings.Contains(model.ErrorDetail, "secret") {
			t.Fatalf("raw request error: %q", model.ErrorDetail)
		}
		if loader.Detail() != nil {
			t.Fatal("failed page supplied download metadata")
		}
	}
}

func TestUnavailableDetailManagesLocalFilesSignedOut(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	sources, catalog, invPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	game := itchio.Game{Title: "Offline Game", Author: "Creator", URL: srv.URL + "/game"}
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	romPath := filepath.Join(sources[0].RomsPath, "GBC", "Original.gbc")
	addManagedROM(t, inv, game.URL, game.Title, romPath)
	model := appui.NewDetailModel(appui.DetailGame{Title: game.Title, Author: game.Author, URL: game.URL, Downloaded: true})
	cfg := &settings.Config{} // no credential
	done := make(chan struct{})
	loader := NewCatDetailLoader(itchio.NewClientWithBase(srv.URL), cfg, game, func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("detail load did not finish")
	}
	if !loader.Sync(model, cfg) || model.State != appui.DetailError {
		t.Fatalf("model = %+v", model)
	}
	if got := model.Handle(appui.InputEvent{Button: appui.ButtonA, Pressed: true}); got != appui.DetailIntentNone {
		t.Fatalf("A starts download: %v", got)
	}
	if got := model.Handle(appui.InputEvent{Button: appui.ButtonX, Pressed: true}); got != appui.DetailIntentManage {
		t.Fatalf("X = %v, want local manage", got)
	}
	manage, manageModel, err := NewCatManageFlow(inv, invPath, game.URL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range manageModel.Items {
		if item.Kind == appui.ManageItemRename {
			manageModel.Cursor = index
			break
		}
	}
	rename, renameModel, err := manage.Activate(manageModel)
	if err != nil || rename == nil {
		t.Fatalf("rename unavailable: %v", err)
	}
	if err := rename.Confirm(renameModel); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sources[0].RomsPath, "GBC", "Offline Game.gbc")); err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	manage, manageModel, err = NewCatManageFlow(inv, invPath, game.URL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manage.Activate(manageModel); err != nil {
		t.Fatal(err)
	}
	if _, err := manage.Confirm(manageModel); err != nil {
		t.Fatal(err)
	}
	if inv.IsPresent(game.URL) {
		t.Fatal("local deletion failed")
	}
	if requests.Load() != 1 {
		t.Fatalf("local management made network requests: %d requests, want the initial detail fetch only", requests.Load())
	}
}
