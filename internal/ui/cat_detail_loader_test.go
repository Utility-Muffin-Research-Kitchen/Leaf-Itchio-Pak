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

// Metadata changes the price and the free flag. It leaves the page's action
// to CatalogController.ApplyDetailAccess, which derives CanDownload and
// NeedsSignIn from the free flag and the current account before each draw.
func TestDetailMetadataChangesPricingWithoutChangingInventoryIdentity(t *testing.T) {
	for _, tc := range []struct {
		price, suggestion, key                   string
		feedFree, free, canDownload, needsSignIn bool
		label                                    string
	}{
		{"€2,50", "", "", true, false, false, true, "€2,50"},
		{"€2,50", "", "key", true, false, true, false, "€2,50"},
		{"$0.00", "$3.00", "", true, true, true, false, "suggested $3.00"},
		{"", "", "", false, true, true, false, "Free"},
	} {
		t.Run(tc.price+tc.key, func(t *testing.T) {
			game := itchio.Game{URL: "https://author.itch.io/old", IsFree: tc.feedFree}
			loader := &CatDetailLoader{game: game, updates: make(chan catDetailResult, 1)}
			loader.updates <- catDetailResult{detail: &itchio.GameDetail{Data: &itchio.GameData{
				ID: 42, Price: tc.price, SuggestedPrice: tc.suggestion,
			}}}
			model := appui.NewDetailModel(appui.DetailGame{URL: game.URL, IsFree: tc.feedFree, Downloaded: true,
				CanDownload: tc.feedFree, NeedsSignIn: !tc.feedFree})
			cfg := &settings.Config{AuthToken: tc.key}
			if !loader.Sync(model, cfg) {
				t.Fatal("no update")
			}
			if model.Game.CanDownload != tc.feedFree || model.Game.NeedsSignIn != !tc.feedFree {
				t.Fatalf("metadata set the page's action: %#v", model.Game)
			}
			controller := &CatalogController{cfg: cfg, inv: &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}}
			controller.ApplyDetailAccess(&model.Game)
			if model.Game.IsFree != tc.free || model.Game.CanDownload != tc.canDownload || model.Game.NeedsSignIn != tc.needsSignIn || !strings.Contains(model.Game.PriceLabel, tc.label) {
				t.Fatalf("detail game = %#v", model.Game)
			}
			if model.Game.URL != game.URL || loader.Game().URL != game.URL || !model.Game.Downloaded || loader.Game().IsFree != tc.free {
				t.Fatal("inventory identity or current free status lost")
			}
		})
	}
}

func TestDetailPriceLabels(t *testing.T) {
	for _, tc := range []struct {
		name  string
		data  itchio.GameData
		owned bool
		want  string
	}{
		{"owned paid game", itchio.GameData{Price: "$5.00"}, true, "Owned"},
		{"owned game on sale", itchio.GameData{Price: "$2.50", OriginalPrice: "$5.00"}, true, "Owned"},
		{"owned free game", itchio.GameData{Price: "$0.00"}, true, "Free / name your price"},
		{"pay what you want above a minimum", itchio.GameData{Price: "$2.00", SuggestedPrice: "$4.00"}, false, "$2.00 or more"},
		{"100% off sale", itchio.GameData{Price: "$0.00", OriginalPrice: "$5.00", Sale: &itchio.GameSale{Rate: 100}}, false, "Free (was $5.00)"},
		{"sale", itchio.GameData{Price: "$2.50", OriginalPrice: "$5.00", Sale: &itchio.GameSale{Rate: 50}}, false, "$2.50 (was $5.00)"},
		{"fixed price", itchio.GameData{Price: "$5.00"}, false, "$5.00"},
		{"donation", itchio.GameData{Price: "$0.00", SuggestedPrice: "$3.00"}, false, "Free / suggested $3.00"},
		{"name your price", itchio.GameData{Price: "$0.00"}, false, "Free / name your price"},
		{"free", itchio.GameData{}, false, "Free"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := tc.data
			data.ID = 42
			loader := &CatDetailLoader{game: itchio.Game{URL: "https://author.itch.io/game"}, updates: make(chan catDetailResult, 1)}
			loader.updates <- catDetailResult{detail: &itchio.GameDetail{Data: &data}}
			model := appui.NewDetailModel(appui.DetailGame{URL: "https://author.itch.io/game", Owned: tc.owned})
			loader.Sync(model, &settings.Config{})
			if got := model.Game.PriceText(); got != tc.want {
				t.Fatalf("price = %q, want %q", got, tc.want)
			}
		})
	}
}
