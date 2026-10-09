//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bodgit/sevenzip"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// Archive members can be named to leave the folder they install into: with
// "../" components, an absolute path, or backslashes ("..\\..\\evil.p8", which
// Windows archivers write and the app reads as "/"). Nothing an archive holds
// may be written outside the folder its type installs into.

// traversalPico7z is a Pico-8 set, written by libarchive (bsdtar 3.7.4 with -P
// and -s renames, since Go has no 7z writer and bsdtar drops such names
// otherwise). It holds main.p8, level2.p8 and lib.lua, and four carts named
// "../../../../evil.p8", "game/../../evil2.p8", "/tmp/abs-evil.p8" and
// "..\\..\\evil3.p8".
const traversalPico7z = "N3q8ryccAANtYmaTMwEAAAAAAAAiAAAAAAAAAEgpcb0AOBpIjiRWEBs71esuHjUZ+x1v86lyn5RdxaoqPdZYuTI/ewHG7pkH+gso8Vgcw5IT/bgIwUJi5D//+NWAAAAAgTMHrg/R1LcIoJCgd7D+k4fhqh8nai1cYm6bIz1o/20LOA7ifzb76hPspiXSIlYzSkgiwqUYkaCL+I82E6Q7qfIzdOv+jrybByADmRsgqaPMRYKGnZ/YXHV7tENWHcyPn91n35gs3BTWLVYBn5IXoodMJ/wPnRJhCwwKgRUG1Pn0RZiNfqWvl/++lv+Y628rb37ZYwxpU/G6DZWoRbEahJ3j8+aKIjtHy6J+SyAA46uupbp+t+NJmiMZCw+1Giu0OK1Bw45tQ2D9LKUCD6r2G9zRoNhu5SJddHbBRLPMrI9yaS2/CMoNEjPYeevQ//92B4AAFwY+AQmA9QAHCwEAASMDAQEFXQAAgAAMgeUKAS8ePAwAAA=="

// traversalROMs7z holds legit.gb, five more files named like the carts above
// but ending in .gb and .cue/.bin, and four soundtrack tracks: ROMs named
// "../../../../evil.gb", "game/../../evil2.gb", "/tmp/abs-evil.gb" and
// "..\\..\\evil4.gb"; tracks "cd1/../../../../01 Theme.ogg", "cd2/01 Theme.ogg",
// "..\\..\\02 Other.ogg" and "/abs/03 Abs.mp3"; and a PlayStation game,
// "../../../../disc.cue" with "x/../../../../disc.bin".
const traversalROMs7z = "N3q8ryccAAPmp084uAEAAAAAAAAiAAAAAAAAAPzPskIAI5CABCF1srFpHCIpqgbCfNVlsBywQoxaFIn4BRbErId1D7s8APE+qhWpMunPWtcW0YrJW8zyMn6i6RPvcOQZHksBSp6UnMu8itMHrWgLK27WC2coonR9pLX9jXVPPZW6ZhF8gOh7H/6mkIAAAIEzB64P1Cq6/UDAkNNDxOH56FHVd/gNZvdV4vrvHs1dH2msRiVveprpubaVTWIrIatryTPIkG//+4FP2wnnnitX3J+NO67o1V0gVZDrXythQFt3kRLLBmJoXpzsqIRYfLmKjDydYMH2B6yEJNLSOHUcWxTW1Lu37RgLjo7jiJ6kPukf6GKr7XTFjfBcRm+VhJGKDqmpyEk+rJfGst/Tuxvn9qL6Tg0z1zZ90qzETqBWzRS/NaAacC1scEm+mYKKvScQHYaADpVgbWhmTcRNNdNxT+8pcj9KjOfH6xZF5FcPZTNBZRZxrr21vg9jM2izWemZEFpJpV/W+j1wGEHdfTWh6WvjwW2WlBnfzmYiVsClwfUTYp9QqAvP4S0KxVfmc7msvh/p8zL2CqPiVsenzQu/PA8ikll8eLpknNwgyPLnKrlBh5Pf+63izBcGbAEJgUwABwsBAAEjAwEBBV0AAIAADINBCgEHb9HXAAA="

func traversalPicoArchives(t *testing.T) map[string][]byte {
	t.Helper()
	cart := func(name string) []byte { return []byte("pico-8 cartridge // " + name + "\n") }
	return map[string][]byte{
		"moss.zip": zipOf(t, map[string][]byte{
			"main.p8": cart("MAIN"), "level2.p8": cart("LEVEL2"), "lib.lua": []byte("-- lib\n"),
			"../../../../evil.p8": cart("EVIL"), "game/../../evil2.p8": cart("EVIL2"),
			"/tmp/abs-evil.p8": cart("ABS"), "..\\..\\evil3.p8": cart("EVIL3"),
		}),
		"moss.7z": decode7z(t, traversalPico7z),
	}
}

func traversalROMArchives(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{
			"legit.gb": []byte("GB ROM\n"), "../../../../evil.gb": []byte("GB 1"), "game/../../evil2.gb": []byte("GB 2"),
			"/tmp/abs-evil.gb": []byte("GB 3"), "..\\..\\evil4.gb": []byte("GB 4"),
			"cd1/../../../../01 Theme.ogg": []byte("OGG"), "cd2/01 Theme.ogg": []byte("OGG2"),
			"..\\..\\02 Other.ogg": []byte("OGG3"), "/abs/03 Abs.mp3": []byte("MP3"),
			"../../../../disc.cue":   []byte("FILE \"disc.bin\" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n"),
			"x/../../../../disc.bin": []byte("BIN DATA\n"),
		}),
		"leafbound.7z": decode7z(t, traversalROMs7z),
	}
}

// memberNames lists the raw member names of an archive.
func memberNames(t *testing.T, name string, data []byte) []string {
	t.Helper()
	var names []string
	if strings.HasSuffix(name, ".7z") {
		reader, err := sevenzip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range reader.File {
			names = append(names, file.Name)
		}
	} else {
		reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range reader.File {
			names = append(names, file.Name)
		}
	}
	return names
}

// requireTraversalMembers fails unless the archive holds each kind of name
// these tests are about, so a fixture that lost them cannot pass quietly.
func requireTraversalMembers(t *testing.T, name string, data []byte) {
	t.Helper()
	var parent, absolute, backslash bool
	for _, member := range memberNames(t, name, data) {
		parent = parent || strings.Contains(member, "../")
		absolute = absolute || strings.HasPrefix(member, "/")
		backslash = backslash || strings.Contains(member, "\\")
	}
	if !parent || !absolute || !backslash {
		t.Fatalf("%s members %v lack a parent folder (%v), absolute (%v) or backslash (%v) name", name,
			memberNames(t, name, data), parent, absolute, backslash)
	}
}

// filesOutside lists the files under root, relative to it, that are not inside
// one of the allowed folders.
func filesOutside(t *testing.T, root string, allowed ...string) []string {
	t.Helper()
	var outside []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		for _, dir := range allowed {
			if rel, relErr := filepath.Rel(dir, path); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}
		}
		rel, _ := filepath.Rel(root, path)
		outside = append(outside, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(outside)
	return outside
}

// A multi-file Pico-8 set installs only the members that stay in its game
// folder. The others are skipped and listed, and are in no record or playlist.
func TestPico8SetSkipsMembersThatLeaveItsFolder(t *testing.T) {
	for name, data := range traversalPicoArchives(t) {
		t.Run(name, func(t *testing.T) {
			requireTraversalMembers(t, name, data)
			primary, _ := transactionPaths(t)
			inv, invPath := collisionInventory(t)
			gameDir := filepath.Join(primary, "Roms", "PICO8", "Moss Garden")
			worker := runArchiveFor(t, primary, ownerGame, inv, invPath, name, data, &settings.Config{},
				func(plan *ZIPPlan, _ string) { plan.Pico8GameDir = gameDir + string(filepath.Separator) })

			if outside := filesOutside(t, filepath.Dir(primary), gameDir); len(outside) != 0 {
				t.Fatalf("%s: files written outside the game folder: %v", name, outside)
			}
			want := []string{"level2.p8", "lib.lua", "main.p8"}
			if strings.HasSuffix(name, ".zip") {
				want = []string{"Moss Garden.m3u", "level2.p8", "lib.lua", "main.p8"}
			}
			tree := treeOf(t, gameDir)
			if got := keys(tree); strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s: game folder = %v, want %v", name, got, want)
			}
			skipped := append([]string(nil), worker.skipped...)
			sort.Strings(skipped)
			if got := strings.Join(skipped, ","); got != "abs-evil.p8,evil.p8,evil2.p8,evil3.p8" {
				t.Fatalf("%s: skipped = %v, want the four members that leave the folder", name, skipped)
			}
			if strings.HasSuffix(name, ".zip") {
				if got := tree["Moss Garden.m3u"]; got != "level2.p8\nmain.p8\n" {
					t.Fatalf("playlist = %q, want the two carts that stayed", got)
				}
			}
			for _, path := range entryPaths(t, inv, ownerGame.URL) {
				if rel, err := filepath.Rel(gameDir, path); err != nil || strings.HasPrefix(rel, "..") {
					t.Fatalf("%s: a record points at %s, outside the game folder", name, path)
				}
			}
			if got := len(entryPaths(t, inv, ownerGame.URL)); got != len(want) {
				t.Fatalf("%s: %d records, want %d", name, got, len(want))
			}
		})
	}
}

// ROMs, soundtrack tracks and PlayStation .cue/.bin sets install under the
// base name of each member, so no member name can place a file outside its
// system or soundtrack folder.
func TestArchiveROMsAndMusicStayInsideTheirFolders(t *testing.T) {
	for name, data := range traversalROMArchives(t) {
		for _, unified := range []bool{false, true} {
			t.Run(name, func(t *testing.T) {
				requireTraversalMembers(t, name, data)
				primary, _ := transactionPaths(t)
				inv, invPath := collisionInventory(t)
				music := filepath.Join(primary, "Music", "Leafbound")
				runArchiveFor(t, primary, ownerGame, inv, invPath, name, data, &settings.Config{UnifiedNaming: unified},
					func(plan *ZIPPlan, _ string) { plan.DownloadMusic, plan.MusicDir = true, music })

				romDir, soundtrack := filepath.Join(primary, "Roms"), music
				if outside := filesOutside(t, filepath.Dir(primary), romDir, soundtrack, filepath.Join(primary, "Images")); len(outside) != 0 {
					t.Fatalf("%s (unified %v): files written outside the ROM, soundtrack and image folders: %v", name, unified, outside)
				}
				for dir, want := range map[string]string{
					filepath.Join(romDir, "GB"): "abs-evil.gb,evil.gb,evil2.gb,evil4.gb,legit.gb",
					filepath.Join(romDir, "PS"): "disc.bin,disc.cue",
					soundtrack:                  "01 Theme.ogg,02 Other.ogg,03 Abs.mp3,cd2/01 Theme.ogg",
				} {
					got := keys(treeOf(t, dir))
					if unified && strings.HasSuffix(dir, "GB") {
						continue // unified naming may rename these; only their folder is under test
					}
					if strings.Join(got, ",") != want {
						t.Fatalf("%s (unified %v): %s holds %v, want %s", name, unified, dir, got, want)
					}
				}
			})
		}
	}
}

// A game titled ".." is the other way to name a folder above the one the game
// installs into: the multi-file Pico-8 folder is named after the title.
func TestAGameTitledAsAParentFolderStaysInItsSystemFolder(t *testing.T) {
	manifest := roms.ZIPManifest{Entries: []roms.ZIPEntry{
		{Name: "main.p8", Kind: roms.KindROM}, {Name: "level2.p8", Kind: roms.KindROM}, {Name: "lib.lua", Kind: roms.KindOther},
	}}
	for _, title := range []string{"..", ".", "../..", "..\\.."} {
		flow := archiveFlowFixture(t, &settings.Config{ROMLocation: "auto"}, manifest)
		flow.game.Title = title
		flow.prepareInitialAction()
		dir := filepath.Clean(flow.plan.Pico8GameDir)
		if flow.plan.Pico8GameDir == "" || filepath.Base(filepath.Dir(dir)) != "PICO8" {
			t.Fatalf("a game titled %q installs into %q, want a folder inside the Pico-8 folder", title, flow.plan.Pico8GameDir)
		}
	}
}
