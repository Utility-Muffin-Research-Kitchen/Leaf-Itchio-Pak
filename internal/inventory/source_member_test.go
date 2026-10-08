package inventory_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

// F8: a file installed from an archive records the member it came from, so
// "Glory Hunters.gba" says whether it is the EZ IV build or the plain one.
func TestSourceMemberIsSavedAndLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://dev.itch.io/glory-hunters"
	inv.Add(gameURL, inventory.Entry{Title: "Glory Hunters"},
		inventory.DownloadedFile{Filename: "Glory Hunters.gba", DestPath: "/leaf/Roms/GBA/Glory Hunters.gba",
			SourceArchive: "glory-hunters.zip", SourceMember: "Glory Hunters 1.3 EZ IV Patched.gba"})
	inv.Add(gameURL, inventory.Entry{Title: "Glory Hunters"},
		inventory.DownloadedFile{Filename: "bonus.gb", DestPath: "/leaf/Roms/GB/bonus.gb"})
	if err := inv.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), `"source_member"`); got != 1 {
		t.Fatalf("source_member appears %d times, want once (omitted when empty):\n%s", got, data)
	}

	loaded, err := inventory.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, _ := loaded.Lookup(gameURL)
	members := map[string]string{}
	for _, file := range entry.Files {
		members[file.Filename] = file.SourceMember
	}
	if members["Glory Hunters.gba"] != "Glory Hunters 1.3 EZ IV Patched.gba" || members["bonus.gb"] != "" {
		t.Fatalf("loaded members = %#v", members)
	}
}

// Inventories written before F8 have no member and still load.
func TestInventoryWithoutSourceMemberLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	data, err := json.Marshal(map[string]any{"version": inventory.SchemaVersion, "entries": map[string]any{
		"https://dev.itch.io/glory-hunters": map[string]any{
			"game_url": "https://dev.itch.io/glory-hunters", "title": "Glory Hunters",
			"files": []any{map[string]any{
				"content_kind": "rom", "filename": "Glory Hunters.gba", "dest_path": "/leaf/Roms/GBA/Glory Hunters.gba",
				"source_id": "primary", "relative_path": "Roms/GBA/Glory Hunters.gba", "source_archive": "glory-hunters.zip",
			}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	inv, err := inventory.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := inv.Lookup("https://dev.itch.io/glory-hunters")
	if !ok || len(entry.Files) != 1 || entry.Files[0].SourceArchive != "glory-hunters.zip" || entry.Files[0].SourceMember != "" {
		t.Fatalf("old inventory = %+v", entry)
	}
}
