//go:build !headless

package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/screentext"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

const gloryHuntersURL = "https://example.invalid/glory-hunters"

// recordROM writes a ROM at path and records it for gameURL as edit sets it.
func recordROM(t *testing.T, inv *inventory.Inventory, gameURL, title, path string, edit func(*inventory.DownloadedFile)) {
	t.Helper()
	addManagedROM(t, inv, gameURL, title, path)
	entry, _ := inv.Lookup(gameURL)
	for _, file := range entry.Files {
		if file.DestPath == path {
			edit(&file)
			inv.UpdateFile(gameURL, path, file)
		}
	}
}

// fromArchive records a ROM that unified naming renamed after the game's
// title, extracted from member of glory-hunters.zip ("" for a record made
// before members were recorded).
func fromArchive(member string) func(*inventory.DownloadedFile) {
	return func(file *inventory.DownloadedFile) {
		file.SourceArchive, file.OriginalUpload, file.SourceMember = "glory-hunters.zip", "glory-hunters.zip", member
		file.UnifiedName = true
	}
}

// downloaded records a ROM downloaded on its own from upload; unified says
// whether unified naming renamed it.
func downloaded(upload string, unified bool) func(*inventory.DownloadedFile) {
	return func(file *inventory.DownloadedFile) {
		file.Filename, file.OriginalUpload, file.UnifiedName = upload, upload, unified
	}
}

// renameRows maps each rename row Manage offers to its index.
func renameRows(model *appui.ManageModel) map[string]int {
	rows := map[string]int{}
	for index, item := range model.Items {
		if item.Kind == appui.ManageItemRename {
			rows[item.Label] = index
		}
	}
	return rows
}

func writeFiles(t *testing.T, files map[string]string) {
	t.Helper()
	for path, data := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// F13: Glory Hunters' zip holds its Game Boy build three folders down. The
// install named it after the title; Manage offers its archive name back, and
// the save and state files follow the ROM.
func TestManageRestoresAnArchiveROMsOriginalName(t *testing.T) {
	sources, catalog, invPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gbDir := filepath.Join(sources[0].RomsPath, "GB")
	romPath := filepath.Join(gbDir, "Glory Hunters.gb")
	recordROM(t, inv, gloryHuntersURL, "Glory Hunters", romPath,
		fromArchive("Glory Hunters 2.0/Glory Hunters/Glory Hunters Version 2.0.1/Glory Hunters 2.0.1.gb"))
	saveDir := filepath.Join(sources[0].SavesPath, "GB")
	stateDir := filepath.Join(sources[0].StatesPath, "GB-gambatte")
	writeFiles(t, map[string]string{
		filepath.Join(saveDir, "Glory Hunters.srm"):         "save",
		filepath.Join(stateDir, "Glory Hunters.state1"):     "state",
		filepath.Join(stateDir, "Glory Hunters.state1.png"): "thumb",
	})

	flow, model, err := NewCatManageFlow(inv, invPath, gloryHuntersURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	rows := renameRows(model)
	if len(rows) != 1 {
		t.Fatalf("rename rows = %v", rows)
	}
	for label, index := range rows {
		if label != "Use original name for Glory Hunters.gb" {
			t.Errorf("rename row = %q", label)
		}
		model.Cursor = index
	}
	rename, renameModel, err := flow.Activate(model)
	if err != nil || rename == nil {
		t.Fatalf("rename = %v, %q", rename, screentext.FromError(err))
	}
	if len(renameModel.Lines) != 2 || renameModel.Lines[1] != "→ Roms/GB/Glory Hunters 2.0.1.gb" {
		t.Fatalf("rename prompt = %q", renameModel.Lines)
	}
	for _, want := range []appui.RenameState{appui.RenameConfirmSaves, appui.RenameConfirmStates, appui.RenameDone} {
		if err := rename.Confirm(renameModel); err != nil || renameModel.State != want {
			t.Fatalf("confirm = state %v, %v; want %v", renameModel.State, err, want)
		}
	}
	for _, path := range []string{
		filepath.Join(gbDir, "Glory Hunters 2.0.1.gb"),
		filepath.Join(saveDir, "Glory Hunters 2.0.1.srm"),
		filepath.Join(stateDir, "Glory Hunters 2.0.1.state1"),
		filepath.Join(stateDir, "Glory Hunters 2.0.1.state1.png"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("renamed path: %v", err)
		}
	}
	for _, path := range []string{romPath, filepath.Join(saveDir, "Glory Hunters.srm"), filepath.Join(stateDir, "Glory Hunters.state1")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", filepath.Base(path), err)
		}
	}
	entry, _ := inv.Lookup(gloryHuntersURL)
	file := entry.Files[0]
	if file.DestPath != filepath.Join(gbDir, "Glory Hunters 2.0.1.gb") || file.UnifiedName || file.RelativePath != "Roms/GB/Glory Hunters 2.0.1.gb" {
		t.Fatalf("renamed record = %+v", file)
	}
	if !entry.UnifiedNamingDisabled {
		t.Fatal("a reinstall would rename the ROM after its title again")
	}
	if !rename.TakeLibraryScanRequest() {
		t.Fatal("the rename did not request a library rescan")
	}
	if groups := rename.LibraryTitleGroups(); len(groups) != 1 || groups[0].Title != "Glory Hunters" {
		t.Fatalf("title groups = %#v", groups)
	}

	// Back in Manage, the only rename left is the title one.
	if _, model, err = NewCatManageFlow(inv, invPath, gloryHuntersURL, sources, catalog); err != nil {
		t.Fatal(err)
	}
	rows = renameRows(model)
	if _, ok := rows["Use title for Glory Hunters 2.0.1.gb"]; !ok || len(rows) != 1 {
		t.Fatalf("rename rows after the rename = %v", rows)
	}
}

// D7: Manage offers a rename only when it knows the name and that name is
// not the one the file has, in any letter case. Each offered row renames to
// the name it promises.
func TestManageOffersOnlyRenamesThatChangeTheName(t *testing.T) {
	cases := []struct {
		name    string
		current string
		record  func(*inventory.DownloadedFile)
		label   string // "" when Manage offers no rename
		target  string
	}{
		{name: "archive record from before members were recorded", current: "Glory Hunters.gb", record: fromArchive("")},
		{name: "member already has the name in another case", current: "Glory Hunters.gb", record: fromArchive("release/GLORY HUNTERS.GB")},
		{name: "member name with characters FAT32 rejects", current: "Glory Hunters.gb",
			record: fromArchive(`Legacy (OLD FILES)\Glory Hunters: Director's Cut?.GB`),
			label:  "Use original name for Glory Hunters.gb", target: "Glory Hunters Director's Cut.gb"},
		{name: "member named by its first bytes", current: "Glory Hunters.gb", record: fromArchive("bonus/extra.dat"),
			label: "Use original name for Glory Hunters.gb", target: "extra.gb"},
		{name: "download renamed after the title", current: "Glory Hunters.gb", record: downloaded("glory_hunters_v2.gb", true),
			label: "Use original name for Glory Hunters.gb", target: "glory_hunters_v2.gb"},
		{name: "download that kept its upload name in another case", current: "Glory Hunters.gb",
			record: downloaded("GLORY HUNTERS.gb", true)},
		{name: "download with its upload name", current: "glory_hunters_v2.gb", record: downloaded("glory_hunters_v2.gb", false),
			label: "Use title for glory_hunters_v2.gb", target: "Glory Hunters.gb"},
		{name: "upload already named after the title", current: "Glory Hunters.gb", record: downloaded("Glory Hunters.gb", false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sources, catalog, invPath := destinationFixture(t)
			configureManageFixture(t, sources, catalog)
			inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
			romPath := filepath.Join(sources[0].RomsPath, "GB", tc.current)
			recordROM(t, inv, gloryHuntersURL, "Glory Hunters", romPath, tc.record)
			flow, model, err := NewCatManageFlow(inv, invPath, gloryHuntersURL, sources, catalog)
			if err != nil {
				t.Fatal(err)
			}
			rows := renameRows(model)
			if tc.label == "" {
				if len(rows) != 0 {
					t.Fatalf("rename rows = %v, want none", rows)
				}
				return
			}
			index, ok := rows[tc.label]
			if !ok || len(rows) != 1 {
				t.Fatalf("rename rows = %v, want %q", rows, tc.label)
			}
			model.Cursor = index
			rename, renameModel, err := flow.Activate(model)
			if err != nil || rename == nil {
				t.Fatalf("rename = %v, %v", rename, err)
			}
			if want := "→ Roms/GB/" + tc.target; len(renameModel.Lines) != 2 || renameModel.Lines[1] != want {
				t.Fatalf("rename prompt = %q, want %q", renameModel.Lines, want)
			}
			if err := rename.Confirm(renameModel); err != nil || renameModel.State != appui.RenameDone {
				t.Fatalf("confirm = state %v, %v", renameModel.State, err)
			}
			if got := filesIn(t, filepath.Join(sources[0].RomsPath, "GB")); len(got) != 1 || got[tc.target] != "rom" {
				t.Fatalf("GB folder = %v, want only %s", keys(got), tc.target)
			}
		})
	}
}

// The rename flow still refuses a rename Manage would not offer, should one
// reach it.
func TestRenameRefusesAnUnknownOrUnchangedOriginalName(t *testing.T) {
	for member, want := range map[string]string{
		"":                         "This ROM's original name isn't known.",
		"release/GLORY HUNTERS.GB": "This ROM already has that name.",
	} {
		sources, catalog, invPath := destinationFixture(t)
		configureManageFixture(t, sources, catalog)
		inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
		recordROM(t, inv, gloryHuntersURL, "Glory Hunters", filepath.Join(sources[0].RomsPath, "GB", "Glory Hunters.gb"), fromArchive(member))
		_, _, err := NewCatRenameFlow(inv, invPath, gloryHuntersURL, 0, sources)
		if err == nil {
			t.Fatalf("member %q: rename was allowed", member)
		}
		assertScreenSentence(t, screentext.FromError(err), want)
	}
}

// Going back to the archive name when another file has it stops before
// anything moves.
func TestRestoringATakenArchiveNameIsRefused(t *testing.T) {
	sources, catalog, invPath := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv := &inventory.Inventory{Entries: make(map[string]*inventory.Entry)}
	gbDir := filepath.Join(sources[0].RomsPath, "GB")
	recordROM(t, inv, gloryHuntersURL, "Glory Hunters", filepath.Join(gbDir, "Glory Hunters.gb"),
		fromArchive("Glory Hunters 2.0/Glory Hunters 2.0.1.gb"))
	writeFiles(t, map[string]string{filepath.Join(gbDir, "Glory Hunters 2.0.1.gb"): "other"})
	flow, model, err := NewCatManageFlow(inv, invPath, gloryHuntersURL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	index, ok := renameRows(model)["Use original name for Glory Hunters.gb"]
	if !ok {
		t.Fatalf("rename rows = %v", renameRows(model))
	}
	model.Cursor = index
	if _, _, err := flow.Activate(model); err == nil {
		t.Fatal("rename onto another file was allowed")
	} else {
		assertScreenSentence(t, screentext.FromError(err), "A file named Glory Hunters 2.0.1.gb already exists.")
	}
	if got := filesIn(t, gbDir); len(got) != 2 || got["Glory Hunters.gb"] != "rom" || got["Glory Hunters 2.0.1.gb"] != "other" {
		t.Fatalf("GB folder = %v", got)
	}
}

// The restored name is the one an install with Rename ROM Files off gives
// the member, so an update of the archive writes over the restored file
// instead of adding the title-named one next to it.
func TestRestoredArchiveNameMatchesAnInstallWithoutRenaming(t *testing.T) {
	member := "Leafbound 2.0/Leafbound: Director's Cut?.GB"
	v1 := zipOf(t, map[string][]byte{member: gbROM("V1")})

	sources, catalog, _ := destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv, invPath := collisionInventory(t)
	installArchiveInto(t, inv, invPath, "leafbound.zip", v1, &settings.Config{}, nil)
	plain := filesIn(t, filepath.Join(sources[0].RomsPath, "GB"))
	if len(plain) != 1 || plain["Leafbound Director's Cut.gb"] == "" {
		t.Fatalf("install without renaming wrote %v", keys(plain))
	}
	want := keys(plain)[0]

	sources, catalog, _ = destinationFixture(t)
	configureManageFixture(t, sources, catalog)
	inv, invPath = collisionInventory(t)
	gbDir := filepath.Join(sources[0].RomsPath, "GB")
	installArchiveInto(t, inv, invPath, "leafbound.zip", v1, &settings.Config{UnifiedNaming: true}, nil)
	if got := filesIn(t, gbDir); len(got) != 1 || got["Leafbound.gb"] == "" {
		t.Fatalf("install with renaming wrote %v", keys(got))
	}
	flow, model, err := NewCatManageFlow(inv, invPath, collisionGame.URL, sources, catalog)
	if err != nil {
		t.Fatal(err)
	}
	index, ok := renameRows(model)["Use original name for Leafbound.gb"]
	if !ok {
		t.Fatalf("rename rows = %v", renameRows(model))
	}
	model.Cursor = index
	rename, renameModel, err := flow.Activate(model)
	if err != nil || rename == nil {
		t.Fatalf("rename = %v, %v", rename, err)
	}
	if err := rename.Confirm(renameModel); err != nil || renameModel.State != appui.RenameDone {
		t.Fatalf("confirm = state %v, %v", renameModel.State, err)
	}
	if got := filesIn(t, gbDir); len(got) != 1 || got[want] == "" {
		t.Fatalf("restored %v, want %s", keys(got), want)
	}

	// An update of the archive keeps the restored name.
	v2 := zipOf(t, map[string][]byte{member: gbROM("V2")})
	installArchiveInto(t, inv, invPath, "leafbound.zip", v2, &settings.Config{UnifiedNaming: true}, nil)
	if got := filesIn(t, gbDir); len(got) != 1 || got[want] != string(gbROM("V2")) {
		t.Fatalf("after the update: %v, want only %s with the new bytes", keys(got), want)
	}
}
