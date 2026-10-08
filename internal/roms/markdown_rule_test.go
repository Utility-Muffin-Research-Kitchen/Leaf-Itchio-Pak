package roms_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

// megaDriveHeaderVariants are .md images that installed before the Markdown
// check: a header with a leading space (TMSS accepts it), "SEGA_" and no
// header at all (review finding R23-1).
func megaDriveHeaderVariants() map[string][]byte {
	leadingSpace := make([]byte, 0x200)
	copy(leadingSpace[0x100:], " SEGA MEGA DRIVE")
	underscore := make([]byte, 0x200)
	copy(underscore[0x100:], "SEGA_MEGA_DRIVE ")
	headerless := make([]byte, 0x200)
	for index := range headerless {
		headerless[index] = byte(index * 7)
	}
	return map[string][]byte{"space.md": leadingSpace, "underscore.md": underscore, "headerless.md": headerless}
}

func markdownTexts() map[string][]byte {
	license := strings.Repeat("Copyright © 2026 Leafbound. Permission is hereby granted, free of charge. ", 80)
	return map[string][]byte{"README.md": []byte("# About\n\nUse the arrows.\n"), "LICENSE.md": []byte(license)}
}

func TestMDIsROM(t *testing.T) {
	sega := make([]byte, 0x200)
	copy(sega[0x100:], "SEGA GENESIS    ")
	if !roms.MDIsROM(sega) {
		t.Error("SGDK-style header was not a ROM")
	}
	for name, data := range megaDriveHeaderVariants() {
		if !roms.MDIsROM(data) {
			t.Errorf("%s was not a ROM", name)
		}
	}
	for name, data := range markdownTexts() {
		if roms.MDIsROM(data) {
			t.Errorf("%s was a ROM", name)
		}
	}
	// A 4 KB window that ends inside a multi-byte character is still text.
	cut := []byte(strings.Repeat("a", 4095) + "©")
	if roms.MDIsROM(cut) {
		t.Error("text cut inside a character was a ROM")
	}
	if roms.MDIsROM(nil) {
		t.Error("an empty file was a ROM")
	}
}

func TestInspectRemoteZIPKeepsMegaDriveVariantsAndDropsMarkdown(t *testing.T) {
	files := map[string]string{}
	for name, data := range megaDriveHeaderVariants() {
		files["game/"+name] = string(data)
	}
	for name, data := range markdownTexts() {
		files[name] = string(data)
	}
	data := buildTestZIP(t, files)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "game.zip", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	manifest, err := roms.InspectRemoteZIP(srv.Client(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ROMCount() != 3 {
		t.Fatalf("manifest = %+v, want the three Mega Drive images as ROMs", manifest)
	}
	for _, entry := range manifest.Entries {
		if (entry.Name == "README.md" || entry.Name == "LICENSE.md") && entry.Kind != roms.KindOther {
			t.Fatalf("%s classified as %v", entry.Name, entry.Kind)
		}
	}
}

// A failed range read while probing a .md member must not hide a Mega Drive
// ROM as Markdown. The probe error fails the range inspection, which falls
// back to a full download (review finding R23-3).
func TestInspectRemoteZIPRetriesWhenAMarkdownProbeFails(t *testing.T) {
	sega := make([]byte, 0x200)
	copy(sega[0x100:], " SEGA MEGA DRIVE")
	pad := make([]byte, 300*1024)
	for index := range pad {
		pad[index] = byte(index*31 + index/7)
	}
	// zip entries are written in name order, so a.md starts at offset 0.
	data := buildTestZIP(t, map[string]string{"a.md": string(sega), "pad.txt": string(pad)})
	failed := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Range"), "bytes=0-") {
			failed++
			http.Error(w, "flaky", http.StatusBadGateway)
			return
		}
		http.ServeContent(w, r, "game.zip", time.Time{}, bytes.NewReader(data))
	}))
	defer srv.Close()
	manifest, err := roms.InspectRemoteZIP(srv.Client(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if failed == 0 {
		t.Fatal("the probe never hit the failing range")
	}
	if manifest.ROMCount() != 1 {
		t.Fatalf("manifest = %+v, want a.md as a ROM", manifest)
	}
}
