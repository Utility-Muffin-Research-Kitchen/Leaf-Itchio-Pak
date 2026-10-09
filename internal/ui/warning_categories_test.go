//go:build !headless

package ui

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// F30: the warning names the categories that matched, whether the page loaded
// or not, so you know which filter to change.
func TestDetailWarningNamesTheMatchedCategories(t *testing.T) {
	cfg := &settings.Config{Filter: settings.ContentFilter{
		AdultContent: settings.CategoryFilter{Enabled: true},
		HeavyThemes:  settings.CategoryFilter{Enabled: true},
	}}
	for _, tc := range []struct {
		name    string
		catalog []string
		page    []string
		failed  bool
		want    []string
	}{
		{"one category", []string{"NSFW"}, []string{"adventure"}, false, []string{"Adult Content"}},
		{"two categories", []string{"nsfw"}, []string{"suicide"}, false, []string{"Adult Content", "Heavy Themes"}},
		{"one category, page failed", []string{"suicide", "adventure"}, nil, true, []string{"Heavy Themes"}},
		{"two categories, page failed", []string{"nsfw", "grief"}, nil, true, []string{"Adult Content", "Heavy Themes"}},
		{"no match", []string{"adventure"}, []string{"puzzle"}, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			game := itchio.Game{Title: "Tagged", URL: "https://example.itch.io/tagged", Tags: tc.catalog}
			loader := &CatDetailLoader{game: game, updates: make(chan catDetailResult, 1)}
			if tc.failed {
				loader.updates <- catDetailResult{err: errors.New("offline")}
			} else {
				loader.updates <- catDetailResult{detail: &itchio.GameDetail{PageTags: tc.page}}
			}
			model := appui.NewDetailModel(appui.DetailGame{Title: game.Title, URL: game.URL})
			if !loader.Sync(model, cfg) {
				t.Fatal("result was not published")
			}
			if !reflect.DeepEqual(model.WarningCategories, tc.want) {
				t.Fatalf("warning categories = %q, want %q", model.WarningCategories, tc.want)
			}
			if got := model.State == appui.DetailWarning; got != (len(tc.want) > 0) {
				t.Fatalf("state = %v, want a warning only when a category matched", model.State)
			}
		})
	}
}
