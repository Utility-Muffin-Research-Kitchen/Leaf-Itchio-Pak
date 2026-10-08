//go:build !headless

package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// treeIn returns every file under dir by its slash-separated relative path.
func treeIn(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func treeKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func fourTrackSoundtrack(t *testing.T) []byte {
	return zipOf(t, map[string][]byte{
		"Soundtrack/cd1/01 Theme.ogg": []byte("CD1-THEME"), "Soundtrack/cd1/02 Battle.ogg": []byte("CD1-BATTLE"),
		"Soundtrack/cd2/01 Theme.ogg": []byte("CD2-THEME"), "Soundtrack/cd2/02 Boss.ogg": []byte("CD2-BOSS"),
		"Soundtrack/bonus/99 Credits.ogg": []byte("CREDITS"),
	})
}

// When names collide, every track of the colliding folders keeps its folder
// as a subfolder, so Disco Boy, which sorts by full path, plays each disc in
// order (review finding R19-2, decision D3).
func TestArchiveMusicKeepsCollidingFoldersAsSubfolders(t *testing.T) {
	worker, _ := runArchive(t, "leafbound.zip", fourTrackSoundtrack(t), &settings.Config{}, false, musicOnly)
	got := treeIn(t, musicDir(worker))
	want := map[string]string{
		"cd1/01 Theme.ogg": "CD1-THEME", "cd1/02 Battle.ogg": "CD1-BATTLE",
		"cd2/01 Theme.ogg": "CD2-THEME", "cd2/02 Boss.ogg": "CD2-BOSS",
		"99 Credits.ogg": "CREDITS",
	}
	if len(got) != len(want) {
		t.Fatalf("Music folder = %v, want %v", treeKeys(got), treeKeys(want))
	}
	for path, data := range want {
		if got[path] != data {
			t.Fatalf("Music folder %s = %q, want %q (folder %v)", path, got[path], data, treeKeys(got))
		}
	}
	var discs []string
	for path := range got {
		if strings.HasPrefix(path, "cd") {
			discs = append(discs, path)
		}
	}
	sort.Slice(discs, func(i, j int) bool { return strings.ToLower(discs[i]) < strings.ToLower(discs[j]) })
	if strings.Join(discs, ",") != "cd1/01 Theme.ogg,cd1/02 Battle.ogg,cd2/01 Theme.ogg,cd2/02 Boss.ogg" {
		t.Fatalf("play order = %v", discs)
	}
	entry, _ := worker.inv.Lookup(collisionGame.URL)
	paths := map[string]bool{}
	for _, file := range entry.Files {
		paths[file.DestPath] = true
	}
	if len(paths) != len(want) {
		t.Fatalf("inventory rows = %+v, want one per track", entry.Files)
	}
}
