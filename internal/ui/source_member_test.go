//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// installArchiveInto extracts data through the real worker into an existing
// inventory, so a test can install twice onto one card.
func installArchiveInto(t *testing.T, inv *inventory.Inventory, invPath, filename string, data []byte,
	cfg *settings.Config, configure func(plan *ZIPPlan)) *ArchiveDownloadWorker {
	t.Helper()
	srv := freeFileServer(t, map[string][]byte{"9": data})
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := manifestFromZIP(reader.File)
	if err != nil {
		t.Fatal(err)
	}
	plan := ZIPPlan{Upload: freeUpload(srv, "9", filename), CDNURL: srv.URL + "/cdn/9", Manifest: manifest, DownloadROMs: true}
	if configure != nil {
		configure(&plan)
	}
	worker := NewArchiveDownloadWorker(itchio.NewClientWithBase(srv.URL), cfg, collisionGame, &itchio.GameDetail{}, plan, inv, invPath)
	waitForWorker(t, func() bool { state := worker.loadState(); return state == zipDLDone || state == zipDLError })
	if snapshot := worker.CatSnapshot(); snapshot.State != appui.DownloadProgressDone {
		t.Fatalf("%s: archive = %+v", filename, snapshot)
	}
	return worker
}

// membersByFile maps each recorded file, by its path below dir, to the
// archive member it records.
func membersByFile(t *testing.T, inv *inventory.Inventory, dir string) map[string]string {
	t.Helper()
	entry, ok := inv.Lookup(collisionGame.URL)
	if !ok {
		t.Fatal("game missing from the inventory")
	}
	members := map[string]string{}
	for _, file := range entry.Files {
		rel, err := filepath.Rel(dir, file.DestPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		members[filepath.ToSlash(rel)] = file.SourceMember
	}
	return members
}

// F8: Glory Hunters' zip offers an EZ IV build and a plain one, and either
// installs as one title-named file. The record says which one it is; a
// later pick of the other build still replaces that file in place.
func TestArchiveRecordsTheChosenMember(t *testing.T) {
	primary, _ := transactionPaths(t)
	inv, invPath := collisionInventory(t)
	ezIV, plain := "Glory Hunters 1.3 EZ IV Patched.gba", "Glory Hunters 1.3.gba"
	data := zipOf(t, map[string][]byte{ezIV: []byte("EZ-IV"), plain: []byte("PLAIN")})
	gbaDir := filepath.Join(primary, "Roms", "GBA")

	for _, pick := range []string{ezIV, plain} {
		installArchiveInto(t, inv, invPath, "glory-hunters.zip", data, &settings.Config{UnifiedNaming: true},
			func(plan *ZIPPlan) { plan.SelectedROMs = map[string]string{".gba": pick} })
		if got := membersByFile(t, inv, gbaDir); len(got) != 1 || got["Leafbound.gba"] != pick {
			t.Fatalf("after picking %q: members = %#v", pick, got)
		}
	}
	if got := filesIn(t, gbaDir); len(got) != 1 || got["Leafbound.gba"] != "PLAIN" {
		t.Fatalf("GBA folder = %v", got)
	}
}

// Every ROM of a ZIP or 7z records its own member, including one renamed by
// its first bytes (extra.dat is a Game Boy ROM).
func TestArchiveRecordsEachROMMember(t *testing.T) {
	for name, data := range map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{"leafbound.gb": gbROM("MAIN"), "extra.dat": gbROM("EXTRA")}),
		"leafbound.7z":  decode7z(t, romsCollision7z),
	} {
		worker, gbDir := runArchive(t, name, data, &settings.Config{}, false)
		got := membersByFile(t, worker.inv, gbDir)
		files := filesIn(t, gbDir)
		if len(got) != 2 || len(files) != 2 {
			t.Fatalf("%s: members = %#v, files = %v", name, got, keys(files))
		}
		for file, member := range got {
			want := map[string]string{"MAIN": "leafbound.gb", "EXTRA": "extra.dat"}[strings.Trim(files[file][0x150:0x160], "\x00")]
			if member != want {
				t.Fatalf("%s: %s records member %q, want %q", name, file, member, want)
			}
		}
	}
}

// Pico-8 archives record each cart and support file; the playlist the app
// writes has no member.
func TestPico8ArchiveRecordsMembers(t *testing.T) {
	data := zipOf(t, map[string][]byte{
		"release/game/main.p8": []byte("pico-8 cartridge // MAIN\n"), "release/game/level2.p8": []byte("pico-8 cartridge // TWO\n"),
		"release/game/lib.lua": []byte("-- lib\n"),
	})
	worker, dir := runArchive(t, "leafbound.zip", data, &settings.Config{}, true)
	got := membersByFile(t, worker.inv, dir)
	want := map[string]string{
		"main.p8": "release/game/main.p8", "level2.p8": "release/game/level2.p8", "lib.lua": "release/game/lib.lua",
		"Leafbound.m3u": "",
	}
	if len(got) != len(want) {
		t.Fatalf("members = %#v, want %#v", got, want)
	}
	for file, member := range want {
		if got[file] != member {
			t.Fatalf("members = %#v, want %#v", got, want)
		}
	}

	worker, dir = runArchive(t, "leafbound.7z", decode7z(t, pico8Collision7z), &settings.Config{}, true)
	got = membersByFile(t, worker.inv, dir)
	if len(got) != 1 {
		t.Fatalf("7z members = %#v, want one", got)
	}
	for file, member := range got {
		if !strings.EqualFold(member, "game/"+file) {
			t.Fatalf("7z: %s records member %q", file, member)
		}
	}
}

// Soundtrack tracks record their member too, folders included.
func TestArchiveMusicRecordsMembers(t *testing.T) {
	for name, data := range map[string][]byte{
		"leafbound.zip": zipOf(t, map[string][]byte{
			"Soundtrack/cd1/01 Theme.ogg": []byte("CD1-THEME"), "Soundtrack/cd2/01 Theme.ogg": []byte("CD2-THEME"),
		}),
		"leafbound.7z": decode7z(t, musicFolders7z),
	} {
		worker, _ := runArchive(t, name, data, &settings.Config{}, false, musicOnly)
		got := membersByFile(t, worker.inv, musicDir(worker))
		if len(got) != 2 || got["cd1/01 Theme.ogg"] != "Soundtrack/cd1/01 Theme.ogg" ||
			got["cd2/01 Theme.ogg"] != "Soundtrack/cd2/01 Theme.ogg" {
			t.Fatalf("%s: members = %#v", name, got)
		}
	}
}

// A ROM already on the card byte for byte is adopted by the new archive
// rather than written again; its record then names the new member.
func TestArchiveIdenticalROMRecordsTheNewMember(t *testing.T) {
	primary, _ := transactionPaths(t)
	inv, invPath := collisionInventory(t)
	rom := gbROM("SAME")
	installArchiveInto(t, inv, invPath, "leafbound-1.0.zip", zipOf(t, map[string][]byte{"leafbound.gb": rom}), &settings.Config{}, nil)
	installArchiveInto(t, inv, invPath, "leafbound-1.1.zip", zipOf(t, map[string][]byte{"v1.1/leafbound.gb": rom}), &settings.Config{}, nil)

	gbDir := filepath.Join(primary, "Roms", "GB")
	if got := membersByFile(t, inv, gbDir); len(got) != 1 || got["leafbound.gb"] != "v1.1/leafbound.gb" {
		t.Fatalf("members = %#v", got)
	}
}

// Manage shows the member under a file whose name on the card differs from
// it, and nothing where the name says it already.
func TestCatManageShowsTheArchiveMember(t *testing.T) {
	sources, catalog, cfgPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gameURL := "https://example.invalid/glory-hunters"
	for name, member := range map[string]string{
		"Glory Hunters.gba": "Glory Hunters 1.3 EZ IV Patched.gba",
		"Leafbound.gbc":     "release/LEAFBOUND.gbc",
		"bonus.gb":          "",
	} {
		dir := filepath.Join(sources[0].RomsPath, strings.ToUpper(strings.TrimPrefix(roms.ROMExt(name), ".")))
		path := filepath.Join(dir, name)
		addManagedROM(t, inv, gameURL, "Glory Hunters", path)
		entry, _ := inv.Lookup(gameURL)
		for _, file := range entry.Files {
			if file.DestPath == path {
				file.SourceArchive, file.SourceMember = "glory-hunters.zip", member
				if member == "" {
					file.SourceArchive = ""
				}
				inv.UpdateFile(gameURL, path, file)
			}
		}
	}
	_, model, err := NewCatManageFlow(inv, cfgPath, gameURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	notes := map[string]string{}
	for _, item := range model.Items {
		if item.Kind == appui.ManageItemFile {
			notes[item.Label] = item.Note
		} else if item.Note != "" {
			t.Fatalf("%q has a note: %q", item.Label, item.Note)
		}
	}
	want := map[string]string{
		"Glory Hunters.gba": "From Glory Hunters 1.3 EZ IV Patched.gba",
		"Leafbound.gbc":     "",
		"bonus.gb":          "",
	}
	if len(notes) != len(want) {
		t.Fatalf("notes = %#v, want %#v", notes, want)
	}
	for label, note := range want {
		if notes[label] != note {
			t.Fatalf("notes = %#v, want %#v", notes, want)
		}
	}
}
