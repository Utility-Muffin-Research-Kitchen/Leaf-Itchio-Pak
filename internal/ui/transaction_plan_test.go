//go:build !headless

package ui

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

func transactionPaths(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	primary := filepath.Join(root, "primary")
	secondary := filepath.Join(root, "secondary")
	systems := []string{"GB", "GBC", "GBA", "FC", "MD", "PICO8", "PS"}
	configs := make([]roms.SourcePathConfig, 0, 2)
	for _, source := range []struct {
		id, root string
	}{{"primary", primary}, {"secondary_sd", secondary}} {
		dirs := make(map[string]string, len(systems))
		images := make(map[string]string, len(systems))
		for _, system := range systems {
			dirs[system] = filepath.Join(source.root, "Roms", system)
			images[system] = filepath.Join(source.root, "Images", system)
			if err := os.MkdirAll(dirs[system], 0o755); err != nil {
				t.Fatal(err)
			}
		}
		music := filepath.Join(source.root, "Music")
		if err := os.MkdirAll(music, 0o755); err != nil {
			t.Fatal(err)
		}
		configs = append(configs, roms.SourcePathConfig{
			SourceID: source.id, Root: source.root, MusicRoot: music,
			StatesRoot: filepath.Join(source.root, "States"), SystemDirs: dirs, ImageDirs: images,
		})
	}
	if err := roms.ConfigurePaths(roms.PathConfig{
		SourceID: "primary", PrimaryRoot: primary, MusicRoot: configs[0].MusicRoot,
		StatesRoot: configs[0].StatesRoot, SystemDirs: configs[0].SystemDirs,
		ImageDirs: configs[0].ImageDirs, Sources: configs,
	}); err != nil {
		t.Fatal(err)
	}
	return primary, secondary
}

func TestCatDownloadPlanSealFreezesSourceAwareFiles(t *testing.T) {
	_, secondary := transactionPaths(t)
	plan := &CatDownloadPlan{
		Kind: CatDownloadPlanMulti,
		Uploads: []roms.Upload{
			{Filename: "adventure.gbc", UploadID: "11", DownloadKeyID: "purchase-7"},
			{Filename: "manual.gb", UploadID: "12", DownloadKeyID: "purchase-7"},
		},
		DestPaths: []string{
			filepath.Join(secondary, "Roms", "GBC", "RPG", "adventure.gbc"),
			filepath.Join(secondary, "Roms", "GB", "manual.gb"),
		},
	}
	sealed, err := plan.Seal(itchio.Game{URL: "https://example.invalid/game"}, &itchio.GameDetail{GameID: "game-42"})
	if err != nil {
		t.Fatal(err)
	}
	if sealed == plan || len(sealed.Transaction.Files) != 2 {
		t.Fatalf("sealed transaction = %#v", sealed)
	}
	transaction := sealed.Transaction
	if transaction.GameID != "game-42" || transaction.PurchaseID != "purchase-7" || transaction.RequiredBytes != -1 {
		t.Fatalf("transaction identity = %#v", transaction)
	}
	file := transaction.Files[0]
	if file.SourceID != "secondary_sd" || file.CanonicalSystem != "GBC" ||
		file.RelativePath != "Roms/GBC/RPG/adventure.gbc" {
		t.Fatalf("planned file identity = %#v", file)
	}
	if file.TempDir != filepath.Dir(file.FinalPath) || !strings.Contains(file.TempPattern, ".itchio-download-*.part") {
		t.Fatalf("planned temp path = %#v", file)
	}
	if file.InventoryMutation.SourceID != file.SourceID || file.InventoryMutation.DestPath != file.FinalPath || file.ArtworkPath == "" {
		t.Fatalf("planned mutations = %#v", file)
	}

	plan.Uploads[0].Filename = "changed.gbc"
	plan.DestPaths[0] = filepath.Join(secondary, "changed.gbc")
	if sealed.Uploads[0].Filename != "adventure.gbc" || sealed.Transaction.Files[0].FinalPath != file.FinalPath {
		t.Fatal("sealed transaction changed with the mutable selection plan")
	}
}

func TestValidatePlannedPathRejectsRemovedSource(t *testing.T) {
	_, secondary := transactionPaths(t)
	dest := filepath.Join(secondary, "Roms", "GBC", "game.gbc")
	if _, _, err := validatePlannedPath(dest); err != nil {
		t.Fatalf("mounted source rejected: %v", err)
	}
	if err := os.RemoveAll(secondary); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validatePlannedPath(dest); err == nil || !strings.Contains(err.Error(), "not mounted") {
		t.Fatalf("removed source error = %v", err)
	}
}

func TestValidateArchiveManifestLimits(t *testing.T) {
	base := roms.ZIPManifest{Entries: []roms.ZIPEntry{{Name: "game.gbc", Size: 100, CompressedSize: 50}}}
	if err := ValidateArchiveManifest(base, ArchiveLimits{MaxEntries: 1, MaxFileBytes: 100, MaxTotalBytes: 100, MaxCompressionRatio: 2}); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	tests := []struct {
		name     string
		manifest roms.ZIPManifest
		limits   ArchiveLimits
	}{
		{"entries", roms.ZIPManifest{Entries: append(base.Entries, base.Entries[0])}, ArchiveLimits{MaxEntries: 1}},
		{"file", base, ArchiveLimits{MaxFileBytes: 99}},
		{"total", base, ArchiveLimits{MaxTotalBytes: 99}},
		{"ratio", base, ArchiveLimits{MaxCompressionRatio: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateArchiveManifest(test.manifest, test.limits); err == nil {
				t.Fatal("unsafe manifest accepted")
			}
		})
	}
}

func TestExtractEntryCommitsAtomically(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "game.gbc")
	if err := os.WriteFile(dest, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	open := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("replacement")), nil }
	if err := extractEntry(open, int64(len("replacement")), dest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "replacement" {
		t.Fatalf("committed contents = %q", got)
	}
	bad := func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("short")), nil }
	if err := extractEntry(bad, 99, dest); err == nil {
		t.Fatal("short extraction succeeded")
	}
	if got, _ := os.ReadFile(dest); string(got) != "replacement" {
		t.Fatalf("failed extraction changed destination to %q", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".itchio-extract-*.part"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("partial extraction files = %v, %v", matches, err)
	}
}

func TestArchivePlanPreflightUsesSelectedSourceFilesystem(t *testing.T) {
	_, secondary := transactionPaths(t)
	dest := filepath.Join(secondary, "Roms", "GBC", "RPG")
	plan := ZIPPlan{DownloadROMs: true, ROMDirs: map[string]string{".gbc": dest}}
	manifest := roms.ZIPManifest{Entries: []roms.ZIPEntry{{Name: "game.gbc", Kind: roms.KindROM, Size: 32, CompressedSize: 16}}}
	tempDir, err := plan.preflight(&settings.Config{}, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if tempDir != dest {
		t.Fatalf("temp directory = %q, want %q", tempDir, dest)
	}
	if info, err := os.Stat(dest); err != nil || !info.IsDir() {
		t.Fatalf("destination directory was not prepared: %v", err)
	}
}

func TestArchivePlanSealDeepCopiesSelections(t *testing.T) {
	plan := ZIPPlan{
		Manifest:     roms.ZIPManifest{Entries: []roms.ZIPEntry{{Name: "game.gbc"}}},
		SelectedROMs: map[string]string{".gbc": "game.gbc"},
		ROMDirs:      map[string]string{".gbc": "/card/Roms/GBC"},
	}
	sealed := plan.Seal()
	plan.Manifest.Entries[0].Name = "changed.gbc"
	plan.SelectedROMs[".gbc"] = "changed.gbc"
	plan.ROMDirs[".gbc"] = "/elsewhere"
	if sealed.Manifest.Entries[0].Name != "game.gbc" || sealed.SelectedROMs[".gbc"] != "game.gbc" ||
		sealed.ROMDirs[".gbc"] != "/card/Roms/GBC" {
		t.Fatalf("sealed archive plan changed: %#v", sealed)
	}
}
