//go:build !headless

package ui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func syncFailedDetail(t *testing.T, err error) *appui.DetailModel {
	t.Helper()
	game := itchio.Game{Title: "Cached title", URL: "https://example.itch.io/game"}
	loader := &CatDetailLoader{game: game, updates: make(chan catDetailResult, 1)}
	loader.updates <- catDetailResult{err: err}
	model := appui.NewDetailModel(appui.DetailGame{Title: game.Title, URL: game.URL, Downloaded: true})
	if !loader.Sync(model, &settings.Config{}) || model.State != appui.DetailError {
		t.Fatalf("model = %+v, want the unavailable page", model)
	}
	return model
}

// Reopening never brings back a removed game, and an immediate retry is what
// the rate limiter exists to prevent, so neither suggests it.
func TestUnavailableDetailExplainsRemovedAndRateLimitedGames(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"removed", fmt.Errorf("fetch game detail: %w", itchio.ErrGameRemoved),
			"This game was removed from itch.io."},
		{"rate limited", fmt.Errorf("fetch game page: %w", &itchio.RateLimitedError{Host: "itch.io"}),
			"itch.io is limiting requests. Wait a minute, then reopen this game."},
		{"other", errors.New("fetch game detail: HTTP 503"),
			"Go back and reopen this game to try again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncFailedDetail(t, tc.err).ErrorDetail; got != tc.want {
				t.Fatalf("detail = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUnavailableDetailForAGoneGamePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()
	game := itchio.Game{Title: "Removed Game", URL: srv.URL + "/game"}
	cfg := &settings.Config{}
	done := make(chan struct{})
	loader := NewCatDetailLoader(itchio.NewClientWithBase(srv.URL), cfg, game, func() { close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("detail load did not finish")
	}
	model := appui.NewDetailModel(appui.DetailGame{Title: game.Title, URL: game.URL})
	if !loader.Sync(model, cfg) || model.ErrorDetail != "This game was removed from itch.io." {
		t.Fatalf("model = %+v, want the removed-game text", model)
	}
}
