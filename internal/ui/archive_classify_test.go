//go:build !headless

package ui

import (
	"archive/zip"
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// openCounter wraps each entry's opener and counts the opens per entry.
type openCounter struct {
	mu    sync.Mutex
	opens map[string]int
}

func (c *openCounter) wrap(entries []archiveEntry) {
	c.opens = map[string]int{}
	for index := range entries {
		name, open := entries[index].name, entries[index].open
		entries[index].open = func() (io.ReadCloser, error) {
			c.mu.Lock()
			c.opens[name]++
			c.mu.Unlock()
			return open()
		}
	}
}

// Every member is classified once, in the manifest pass. Naming, preflight
// and extraction reuse that record, so a member is opened at most once to
// sniff its type and once to extract it. In a solid 7z every open decodes
// everything before the member (review findings R18-4, R19-1, R23-2).
// The identical-file check also reads a member, but only when the game
// already has a ROM of the same type and size; the ROMs here differ in size.
func TestArchiveOpensEachEntryAtMostOnceToSniffAndOnceToExtract(t *testing.T) {
	primary, _ := transactionPaths(t)
	sega := make([]byte, 512)
	copy(sega[0x100:], "SEGA GENESIS    ")
	data := zipOf(t, map[string][]byte{
		"game/sonic.md":               sega,
		"game/README.md":              []byte("# About\n"),
		"extra.dat":                   append(gbROM("EXTRA"), make([]byte, 64)...),
		"notes.txt":                   []byte("notes"),
		"leafbound.gb":                gbROM("MAIN"),
		"Soundtrack/cd1/01 Theme.ogg": []byte("ONE"),
		"Soundtrack/cd2/01 Theme.ogg": []byte("TWO"),
	})
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := zipEntries(reader.File)
	counter := &openCounter{}
	counter.wrap(entries)

	if _, err := classifyArchive(entries); err != nil {
		t.Fatal(err)
	}
	sniffed := map[string]int{}
	for name, opens := range counter.opens {
		sniffed[name] = opens
	}
	for _, name := range []string{"leafbound.gb", "Soundtrack/cd1/01 Theme.ogg"} {
		if sniffed[name] != 0 {
			t.Fatalf("%s was opened to sniff although its extension decides it", name)
		}
	}

	inv, invPath := collisionInventory(t)
	worker := &ArchiveDownloadWorker{
		client: itchio.NewClientWithBase("http://127.0.0.1:0"), cfg: &settings.Config{UnifiedNaming: true},
		game: collisionGame, inv: inv, invPath: invPath, names: &roms.NameReservations{},
		plan: ZIPPlan{Upload: roms.Upload{Filename: "leafbound.zip"}},
	}
	musicOnly(&worker.plan, primary)
	worker.plan.DownloadROMs = true
	worker.installEntries(entries, "test")

	if len(worker.extracted) != 5 {
		t.Fatalf("extracted %v skipped %v, want sonic.md, extra, leafbound.gb and two tracks", worker.extracted, worker.skipped)
	}
	for _, entry := range entries {
		if sniffed[entry.name] > 1 {
			t.Fatalf("%s was sniffed %d times", entry.name, sniffed[entry.name])
		}
		if later := counter.opens[entry.name] - sniffed[entry.name]; later > 1 {
			t.Fatalf("%s was opened %d times after classification", entry.name, later)
		}
	}
}

// The downloaded archive is classified the same way: a .md member that
// cannot be read fails the install rather than being skipped as Markdown
// (review finding R23-3).
func TestClassifyArchiveFailsWhenAMarkdownMemberCannotBeRead(t *testing.T) {
	entries := []archiveEntry{
		{name: "game/sonic.md", size: 512, open: func() (io.ReadCloser, error) { return nil, io.ErrUnexpectedEOF }},
		{name: "notes.txt", size: 5, open: func() (io.ReadCloser, error) { return nil, io.ErrClosedPipe }},
	}
	if _, err := classifyArchive(entries); err == nil {
		t.Fatal("an unreadable .md member passed as Markdown")
	}
	if _, err := classifyArchive(entries[1:]); err != nil {
		t.Fatalf("an unreadable non-.md member failed the archive: %v", err)
	}
}
