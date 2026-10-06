//go:build !headless

package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// Inside archives a .md member is a Mega Drive ROM unless it reads as text:
// a header with a leading space, "SEGA_" and a headerless image install;
// README.md and LICENSE.md do not (review finding R23-1).
func TestArchiveInstallsMegaDriveVariantsButNotMarkdown(t *testing.T) {
	leadingSpace := make([]byte, 0x200)
	copy(leadingSpace[0x100:], " SEGA MEGA DRIVE")
	underscore := make([]byte, 0x200)
	copy(underscore[0x100:], "SEGA_MEGA_DRIVE ")
	headerless := make([]byte, 0x200)
	for index := range headerless {
		headerless[index] = byte(index * 7)
	}
	license := strings.Repeat("Copyright © 2026 Leafbound. Permission is hereby granted, free of charge. ", 80)
	data := zipOf(t, map[string][]byte{
		"game/space.md": leadingSpace, "game/underscore.md": underscore, "game/headerless.md": headerless,
		"README.md": []byte("# About\n"), "LICENSE.md": []byte(license),
	})
	worker, gbDir := runArchive(t, "bundle.zip", data, &settings.Config{}, false)
	got := filesIn(t, filepath.Join(filepath.Dir(gbDir), "MD"))
	if len(got) != 3 || got["space.md"] == "" || got["underscore.md"] == "" || got["headerless.md"] == "" {
		t.Fatalf("MD folder = %v (extracted %v)", keys(got), worker.extracted)
	}
}

// The ROM chooser picks by the classified name. A member whose bytes make it
// a .md ROM under another extension (sonic.dat) is not installed when a
// different .md was picked (review finding R23-5).
func TestArchiveChoiceComparesTheClassifiedName(t *testing.T) {
	sega := make([]byte, 0x200)
	copy(sega[0x100:], "SEGA MEGA DRIVE ")
	alt := append([]byte(nil), sega...)
	alt[0x1F0] = 1
	data := zipOf(t, map[string][]byte{"sonic.dat": sega, "alt.md": alt})
	worker, gbDir := runArchive(t, "bundle.zip", data, &settings.Config{}, false, func(plan *ZIPPlan, _ string) {
		plan.SelectedROMs = map[string]string{".md": "alt.md"}
	})
	got := filesIn(t, filepath.Join(filepath.Dir(gbDir), "MD"))
	if len(got) != 1 || got["alt.md"] == "" {
		t.Fatalf("MD folder = %v (extracted %v), want only the chosen alt.md", keys(got), worker.extracted)
	}
}
