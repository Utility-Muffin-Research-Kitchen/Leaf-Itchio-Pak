package roms_test

import (
	"os"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func TestMain(m *testing.M) {
	err := roms.ConfigurePaths(roms.PathConfig{
		SystemDirs: map[string]string{
			"GB": "/leaf/Roms/GB", "GBC": "/leaf/Roms/GBC", "GBA": "/leaf/Roms/GBA",
			"FC": "/leaf/Roms/NES", "MD": "/leaf/Roms/GENESIS", "PICO8": "/leaf/Roms/PICO8", "PS": "/leaf/Roms/PSX",
		},
		SourceID:    "primary",
		PrimaryRoot: "/leaf",
		MusicRoot:   "/leaf/Music",
		StatesRoot:  "/leaf/States",
		Sources: []roms.SourcePathConfig{
			{
				SourceID: "primary", Root: "/leaf", MusicRoot: "/leaf/Music", StatesRoot: "/leaf/States",
				SystemDirs: map[string]string{"GB": "/leaf/Roms/GB", "GBC": "/leaf/Roms/GBC", "GBA": "/leaf/Roms/GBA", "FC": "/leaf/Roms/NES", "MD": "/leaf/Roms/GENESIS", "PICO8": "/leaf/Roms/PICO8", "PS": "/leaf/Roms/PSX"},
			},
			{
				SourceID: "secondary_sd", Root: "/secondary", MusicRoot: "/secondary/Music", StatesRoot: "/secondary/States",
				SystemDirs: map[string]string{"GB": "/secondary/Roms/GB", "GBC": "/secondary/Roms/GBC", "GBA": "/secondary/Roms/GBA", "FC": "/secondary/Roms/NES", "MD": "/secondary/Roms/GENESIS", "PICO8": "/secondary/Roms/PICO8", "PS": "/secondary/Roms/PSX"},
			},
		},
	})
	if err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestDescribeDestinationPreservesSecondarySource(t *testing.T) {
	got, ok := roms.DescribeDestination("/secondary/Roms/GBC/RPG/game.gbc")
	if !ok {
		t.Fatal("secondary destination was not described")
	}
	if got.SourceID != "secondary_sd" || got.RelativePath != "Roms/GBC/RPG/game.gbc" || got.CanonicalSystem != "GBC" {
		t.Fatalf("identity = %#v", got)
	}
}

func TestScoreUpload(t *testing.T) {
	tests := []struct {
		filename string
		want     int
	}{
		{"game.gbc", 2},
		{"game.GBC", 2},
		{"game.gb", 1},
		{"game.GB", 1},
		{"game.gba", 1},
		{"game.nes", 1},
		{"game.NES", 1},
		{"game.md", 1},
		{"game.gen", 1},
		{"game.smd", 1},
		{"game.p8.png", 2},
		{"game.P8.PNG", 2},
		{"game.p8", 1},
		{"game.P8", 1},
		{"game.chd", 1},
		{"game.pbp", 1},
		{"game.cue", 1},
		{"game.zip", 0},
		{"game.pocket", 0},
		{"game.pdf", 0},
	}
	for _, tt := range tests {
		got := roms.ScoreUpload(tt.filename)
		if got != tt.want {
			t.Errorf("ScoreUpload(%q) = %d, want %d", tt.filename, got, tt.want)
		}
	}
}

func TestDestinationDir(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".gbc", "/leaf/Roms/GBC/"},
		{".GBC", "/leaf/Roms/GBC/"},
		{".gb", "/leaf/Roms/GB/"},
		{".gba", "/leaf/Roms/GBA/"},
		{".nes", "/leaf/Roms/NES/"},
		{".md", "/leaf/Roms/GENESIS/"},
		{".gen", "/leaf/Roms/GENESIS/"},
		{".smd", "/leaf/Roms/GENESIS/"},
		{".p8", "/leaf/Roms/PICO8/"},
		{".P8", "/leaf/Roms/PICO8/"},
		{".p8.png", "/leaf/Roms/PICO8/"},
		{".zip", "/leaf/Roms/GBC/"},
		{".chd", "/leaf/Roms/PSX/"},
		{".cue", "/leaf/Roms/PSX/"},
		{".bin", "/leaf/Roms/PSX/"},
		{".m3u", "/leaf/Roms/PSX/"},
		{".unknown", ""},
	}
	for _, tt := range tests {
		got := roms.DestinationDir(tt.ext)
		if got != tt.want {
			t.Errorf("DestinationDir(%q) = %q, want %q", tt.ext, got, tt.want)
		}
	}
}

func TestPSXFormatPolicy(t *testing.T) {
	for _, ext := range []string{".cbn", ".chd", ".cue", ".img", ".iso", ".mdf", ".pbp", ".toc", ".m3u", ".bin"} {
		if !roms.IsSupportedUploadExt(ext) || !roms.IsPSXExt(ext) {
			t.Errorf("PSX extension %q is not supported", ext)
		}
	}
	for _, name := range []string{"disc.cue", "track.bin", "set.m3u", "disc.img", "disc.iso", "disc.cbn", "disc.mdf"} {
		if roms.SupportsUnifiedNaming(name) {
			t.Errorf("reference-sensitive %q allowed unified naming", name)
		}
	}
	for _, name := range []string{"game.chd", "eboot.pbp"} {
		if !roms.SupportsUnifiedNaming(name) {
			t.Errorf("standalone %q rejected unified naming", name)
		}
	}
}

func TestPico8ROMDir(t *testing.T) {
	if got := roms.Pico8ROMDir(); got != "/leaf/Roms/PICO8/" {
		t.Errorf("Pico8ROMDir() = %q, want %q", got, "/leaf/Roms/PICO8/")
	}
}

func TestPico8GameSubDir(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Poom", "/leaf/Roms/PICO8/Poom/"},
		{"Celeste", "/leaf/Roms/PICO8/Celeste/"},
	}
	for _, tc := range cases {
		got := roms.Pico8GameSubDir(tc.title)
		if got != tc.want {
			t.Errorf("Pico8GameSubDir(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestSelectBest(t *testing.T) {
	uploads := []roms.Upload{
		{Filename: "game.pdf", URL: "u1"},
		{Filename: "game.gb", URL: "u2"},
		{Filename: "game.gbc", URL: "u3"},
	}
	got := roms.SelectBest(uploads)
	if got == nil || got.URL != "u3" {
		t.Errorf("SelectBest: expected gbc upload (u3), got %v", got)
	}
}

func TestSelectBestNoROMs(t *testing.T) {
	uploads := []roms.Upload{
		{Filename: "manual.pdf", URL: "u1"},
	}
	got := roms.SelectBest(uploads)
	if got != nil {
		t.Errorf("SelectBest with no ROMs: expected nil, got %v", got)
	}
}

func TestMusicDestinationDir(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"Solastra", "/leaf/Music/Solastra/"},
		{"Game: Title?", "/leaf/Music/Game Title/"},
		{"", "/leaf/Music/Unknown/"},
	}
	for _, tt := range tests {
		got := roms.MusicDestinationDir(tt.title)
		if got != tt.want {
			t.Errorf("MusicDestinationDir(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestPico8GameSubDirSanitizesTitle(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"Celeste Classic", "/leaf/Roms/PICO8/Celeste Classic/"},
		{"Game: Title?", "/leaf/Roms/PICO8/Game Title/"},
		{"", "/leaf/Roms/PICO8/Unknown/"},
	}
	for _, tt := range tests {
		got := roms.Pico8GameSubDir(tt.title)
		if got != tt.want {
			t.Errorf("Pico8GameSubDir(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestROMExt(t *testing.T) {
	tests := []struct {
		filename string
		want     string
	}{
		{"game.p8.png", ".p8.png"},
		{"game.P8.PNG", ".p8.png"},
		{"GAME.P8.PNG", ".p8.png"},
		{"game.p8", ".p8"},
		{"game.gbc", ".gbc"},
		{"game.png", ".png"},
		{"game", ""},
	}
	for _, tt := range tests {
		got := roms.ROMExt(tt.filename)
		if got != tt.want {
			t.Errorf("ROMExt(%q) = %q, want %q", tt.filename, got, tt.want)
		}
	}
}
