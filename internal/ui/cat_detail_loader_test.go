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
				ID: 42, Price: tc.price, SuggestedPrice: tc.suggestion,
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
