//go:build !headless

package ui

import (
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

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
				ID: 42, URL: "https://author.itch.io/new", Price: tc.price, SuggestedPrice: tc.suggestion,
			}}}
			model := appui.NewDetailModel(appui.DetailGame{URL: game.URL, IsFree: tc.feedFree, Downloaded: true,
				CanDownload: tc.feedFree, NeedsSignIn: !tc.feedFree})
			if !loader.Sync(model, &settings.Config{AuthToken: tc.key}) {
				t.Fatal("no update")
			}
			if model.Game.IsFree != tc.free || model.Game.CanDownload != tc.canDownload || model.Game.NeedsSignIn != tc.needsSignIn || !strings.Contains(model.Game.PriceLabel, tc.label) {
				t.Fatalf("detail game = %#v", model.Game)
			}
			if model.Game.URL != game.URL || loader.Game().URL != game.URL || !model.Game.Downloaded || loader.Game().IsFree != tc.free {
				t.Fatal("inventory identity or current free status lost")
			}
		})
	}
}
