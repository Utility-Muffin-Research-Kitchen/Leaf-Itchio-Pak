package catui

import (
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

// F29: the main list header counts games with the right number.
func TestMainListSubtitleCountsGames(t *testing.T) {
	for _, tc := range []struct {
		games int
		want  string
	}{
		{0, "GB  ·  Owned  ·  0 games  ·  Cache 10m old"},
		{1, "GB  ·  Owned  ·  1 game  ·  Cache 10m old"},
		{2, "GB  ·  Owned  ·  2 games  ·  Cache 10m old"},
	} {
		model := &appui.MainListModel{Platform: "GB", Sort: "Owned", CacheStatus: "Cache 10m old",
			Items: make([]appui.ListItem, tc.games)}
		if got := mainListSubtitle(model); got != tc.want {
			t.Errorf("%d games: subtitle = %q, want %q", tc.games, got, tc.want)
		}
	}
}

// F29: the refresh screen counts games with the right number too.
func TestRefreshTextCountsGames(t *testing.T) {
	if got := refreshDoneText(1); got != "Saved 1 game to the local cache." {
		t.Errorf("done, one game = %q", got)
	}
	if got := refreshDoneText(2); got != "Saved 2 games to the local cache." {
		t.Errorf("done, two games = %q", got)
	}
	for n, want := range map[int]string{0: "0 games fetched", 1: "1 game fetched", 2: "2 games fetched"} {
		if got := refreshProgressText(n); !strings.HasPrefix(got, want+" ") {
			t.Errorf("progress(%d) = %q, want it to start with %q", n, got, want)
		}
	}
}
