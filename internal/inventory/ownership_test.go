package inventory_test

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// OwnerOf compares paths the way FAT32 does, so a differently cased path is
// the same file (review finding R18-1).
func TestOwnerOfMatchesPathsCaseInsensitively(t *testing.T) {
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	inv.Add("https://dev.itch.io/a", inventory.Entry{Title: "A"},
		inventory.DownloadedFile{Filename: "disc1.chd", DestPath: "/leaf/Roms/PSX/disc1.chd"})
	inv.Add("https://dev.itch.io/b", inventory.Entry{Title: "B"},
		inventory.DownloadedFile{Filename: "game.zip", DestPath: "/secondary/Roms/GB/Game.gb", SourceArchive: "game.zip"})

	owners := inv.OwnerOf("primary", "Roms/PSX/DISC1.CHD")
	if len(owners) != 1 || owners[0].GameURL != "https://dev.itch.io/a" {
		t.Fatalf("owners = %+v, want game a", owners)
	}
	if got := owners[0].File.UploadName(); got != "disc1.chd" {
		t.Fatalf("upload = %q, want disc1.chd", got)
	}
	owners = inv.OwnerOf("secondary_sd", "Roms/GB/game.gb")
	if len(owners) != 1 || owners[0].File.UploadName() != "game.zip" {
		t.Fatalf("archive owners = %+v, want game.zip", owners)
	}
	if owners := inv.OwnerOf("primary", "Roms/GB/game.gb"); len(owners) != 0 {
		t.Fatalf("a path on the other card matched: %+v", owners)
	}
}

// Inventories written before R18-1 can hold one file under two games. Load
// reports it and changes nothing.
func TestLoadWarnsAboutPathsSharedBetweenGames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	file := func(name string) map[string]any {
		return map[string]any{"content_kind": "rom", "filename": name, "dest_path": "/leaf/Roms/PSX/" + name,
			"source_id": "primary", "relative_path": "Roms/PSX/" + name}
	}
	shared := file("disc1.chd")
	upper := file("DISC1.chd")
	upper["dest_path"], upper["relative_path"] = "/leaf/Roms/PSX/DISC1.chd", "Roms/PSX/DISC1.chd"
	data, err := json.Marshal(map[string]any{"version": inventory.SchemaVersion, "entries": map[string]any{
		"https://dev.itch.io/a": map[string]any{"game_url": "https://dev.itch.io/a", "title": "A", "files": []any{shared, file("a-only.chd")}},
		"https://dev.itch.io/b": map[string]any{"game_url": "https://dev.itch.io/b", "title": "B", "files": []any{upper}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	inv, err := inventory.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "[WARN]") || !strings.Contains(logged, "disc1.chd") ||
		!strings.Contains(logged, "https://dev.itch.io/a") || !strings.Contains(logged, "https://dev.itch.io/b") {
		t.Fatalf("no shared-path warning in log:\n%s", logged)
	}
	if strings.Contains(logged, "a-only.chd") {
		t.Fatalf("an unshared file was reported:\n%s", logged)
	}
	if entry, ok := inv.Lookup("https://dev.itch.io/b"); !ok || len(entry.Files) != 1 {
		t.Fatalf("load changed the inventory: %+v", entry)
	}
}
