package catui

import "testing"

func TestUnavailableTextOffersManageOnlyForDownloadedGames(t *testing.T) {
	const removed = "This game was removed from itch.io."
	if got, want := unavailableText(removed, true), "This game was removed from itch.io. You can still manage your files."; got != want {
		t.Fatalf("downloaded = %q, want %q", got, want)
	}
	if got := unavailableText(removed, false); got != removed {
		t.Fatalf("not downloaded = %q, want %q", got, removed)
	}
}
