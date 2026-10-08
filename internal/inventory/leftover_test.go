package inventory_test

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// MarkLeftOver flags the files one upload's reinstall did not write, for
// one content kind, and clears the flag again once a reinstall writes the
// file (review finding R19-3).
func TestMarkLeftOverFlagsUnwrittenFilesOfTheSameUpload(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	const game = "https://dev.itch.io/game"
	add := func(name, kind, archive string) {
		fileType := inventory.FileTypeROM
		if kind == inventory.ContentKindMusic {
			fileType = inventory.FileTypeMusic
		}
		inv.Add(game, inventory.Entry{Title: "Game"}, inventory.DownloadedFile{
			Filename: name, DestPath: "/leaf/Music/Game/" + name, FileType: fileType, SourceArchive: archive,
		})
	}
	add("kept.ogg", inventory.ContentKindMusic, "ost.zip")
	add("dropped.ogg", inventory.ContentKindMusic, "ost.zip")
	add("other.ogg", inventory.ContentKindMusic, "bonus.zip")
	add("game.gb", inventory.ContentKindROM, "ost.zip")

	flagged := inv.MarkLeftOver(game, "ost.zip", inventory.ContentKindMusic, []string{"/leaf/Music/Game/KEPT.ogg"})
	if len(flagged) != 1 || flagged[0].Filename != "dropped.ogg" {
		t.Fatalf("flagged = %+v, want dropped.ogg", flagged)
	}
	entry, _ := inv.Lookup(game)
	for _, file := range entry.Files {
		if file.LeftOver != (file.Filename == "dropped.ogg") {
			t.Fatalf("%s left over = %v", file.Filename, file.LeftOver)
		}
	}

	if flagged := inv.MarkLeftOver(game, "ost.zip", "", []string{
		"/leaf/Music/Game/kept.ogg", "/leaf/Music/Game/dropped.ogg", "/leaf/Music/Game/game.gb",
	}); len(flagged) != 0 {
		t.Fatalf("flagged after a full reinstall = %+v", flagged)
	}
	entry, _ = inv.Lookup(game)
	for _, file := range entry.Files {
		if file.LeftOver {
			t.Fatalf("%s still left over", file.Filename)
		}
	}
}
