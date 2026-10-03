package inventory_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
)

func TestUpstreamBaselinesAndPerUploadAcknowledgment(t *testing.T) {
	const url = "https://dev.itch.io/game"
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{}}
	inv.Add(url, inventory.Entry{}, inventory.DownloadedFile{Filename: "unified.gb", OriginalUpload: "first.zip", SourceArchive: "first.zip", UploadID: "1", DestPath: "/unified.gb"})
	inv.Add(url, inventory.Entry{}, inventory.DownloadedFile{Filename: "second.gbc", UploadID: "2", DestPath: "/second.gbc"})
	// Legacy checks lack a source and fingerprints. Their first API scan is a
	// baseline even if it exposes uploads the old download page hid.
	inv.Entries[url].UpdateCheckedAt = time.Now().Add(-time.Hour)
	inv.Entries[url].KnownUpstreamFiles = []inventory.UpstreamFile{{Filename: "first.zip", UploadID: "1"}}
	files := []inventory.UpstreamFile{
		{Filename: "first.zip", UploadID: "1", Fingerprint: "build:1"},
		{Filename: "second.gbc", UploadID: "2", Fingerprint: "md5:old"},
	}
	inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, files)
	if inv.HasPendingUpdates(url) {
		t.Fatal("legacy API migration invented an update")
	}
	files[0].Fingerprint, files[1].Fingerprint = "build:2", "md5:new"
	inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, files)
	if pending := inv.PendingUpdateFiles(url); len(pending) != 2 || !pending[0].Changed || !pending[1].Changed {
		t.Fatalf("same-name replacements = %+v, want both changed", pending)
	}
	// Completing an older in-flight download must not acknowledge a newer
	// fingerprint observed by the background check.
	inv.Add(url, inventory.Entry{}, inventory.DownloadedFile{Filename: "unified.gb", SourceArchive: "first.zip", UploadID: "1", UploadFingerprint: "build:1", DestPath: "/unified.gb"})
	if len(inv.PendingUpdateFiles(url)) != 2 {
		t.Fatal("older download acknowledged a newer replacement")
	}
	inv.Add(url, inventory.Entry{}, inventory.DownloadedFile{Filename: "unified.gb", SourceArchive: "first.zip", UploadID: "1", UploadFingerprint: "build:2", DestPath: "/unified.gb"})
	if pending := inv.PendingUpdateFiles(url); len(pending) != 1 || pending[0].UploadID != "2" {
		t.Fatalf("installing first archive cleared unrelated pending update: %+v", pending)
	}
	inv.DismissUpdate(url)
	path := filepath.Join(t.TempDir(), "inventory.json")
	if err := inv.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := inventory.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.SetUpstreamFilesFrom(url, inventory.SourceAPI, files)
	if loaded.HasPendingUpdates(url) {
		t.Fatal("dismissed replacement reappeared after restart")
	}
	files[1].Fingerprint = "md5:newer"
	loaded.SetUpstreamFilesFrom(url, inventory.SourceAPI, files)
	if pending := loaded.PendingUpdateFiles(url); len(pending) != 1 || pending[0].UploadID != "2" {
		t.Fatalf("newer replacement after dismissal = %+v", pending)
	}
}

func TestUpstreamSourceSwitchAndReplacementIdentity(t *testing.T) {
	const url = "https://dev.itch.io/game"
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{}}
	inv.Add(url, inventory.Entry{}, inventory.DownloadedFile{Filename: "cart.gb", UploadID: "1", DestPath: "/cart.gb"})
	inv.SetUpstreamFiles(url, []inventory.UpstreamFile{{Filename: "Game Boy build"}})
	apiFiles := []inventory.UpstreamFile{{Filename: "cart.gb", DisplayName: "Game Boy build", UploadID: "1", Fingerprint: "build:1"}, {Filename: "hidden.gbc", UploadID: "2"}}
	inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, apiFiles)
	if inv.HasPendingUpdates(url) {
		t.Fatal("signing in invented updates")
	}
	apiFiles[0].Filename = "renamed.gb"
	inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, apiFiles)
	if inv.HasPendingUpdates(url) {
		t.Fatal("same upload with renamed file invented an update")
	}
	// Replacing an upload ID under the same name is a change even when the API
	// does not provide any fingerprint.
	apiFiles[0].UploadID, apiFiles[0].Fingerprint = "3", ""
	inv.SetUpstreamFilesFrom(url, inventory.SourceAPI, apiFiles)
	if !inv.HasPendingUpdates(url) {
		t.Fatal("replaced upload ID was missed")
	}
	inv.Add(url, inventory.Entry{}, inventory.DownloadedFile{Filename: "renamed.gb", UploadID: "1", DestPath: "/cart.gb"})
	if !inv.HasPendingUpdates(url) {
		t.Fatal("different ID with same filename acknowledged replacement")
	}
	inv.SetUpstreamFiles(url, []inventory.UpstreamFile{{Filename: "Game Boy build"}})
	if !inv.HasPendingUpdates(url) {
		t.Fatal("signing out cleared a known replacement")
	}
	inv.Add(url, inventory.Entry{}, inventory.DownloadedFile{Filename: "local.gb", OriginalUpload: "Game Boy build", DestPath: "/cart.gb"})
	if inv.HasPendingUpdates(url) {
		t.Fatal("legacy original-name install did not acknowledge matching upload")
	}
}

func TestInventoryLookupDoesNotExposeUpstreamSlice(t *testing.T) {
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{"game": {KnownUpstreamFiles: []inventory.UpstreamFile{{Filename: "cart.gb"}}}}}
	entry, _ := inv.Lookup("game")
	entry.KnownUpstreamFiles[0].Filename = "changed"
	entry, _ = inv.Lookup("game")
	if entry.KnownUpstreamFiles[0].Filename != "cart.gb" {
		t.Fatal("Lookup returned a shared upstream slice")
	}
}

func TestFirstCheckCanCompareCapturedInstalledFingerprint(t *testing.T) {
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{}}
	inv.Add("game", inventory.Entry{}, inventory.DownloadedFile{Filename: "cart.gb", UploadID: "1", UploadFingerprint: "build:1"})
	inv.SetUpstreamFilesFrom("game", inventory.SourceAPI, []inventory.UpstreamFile{{Filename: "cart.gb", UploadID: "1", Fingerprint: "build:2"}})
	if !inv.HasPendingUpdates("game") {
		t.Fatal("first check lost a known installed version change")
	}
}

func TestPublicSubsetCannotClearHiddenPendingUpdate(t *testing.T) {
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{}}
	inv.Add("game", inventory.Entry{}, inventory.DownloadedFile{Filename: "paid.gb", UploadID: "2"})
	files := []inventory.UpstreamFile{{Filename: "demo.gb", UploadID: "1", Fingerprint: "build:1"}, {Filename: "paid.gb", UploadID: "2", Fingerprint: "build:1"}}
	inv.SetUpstreamFilesFrom("game", inventory.SourceAPI, files)
	files[1].Fingerprint = "build:2"
	inv.SetUpstreamFilesFrom("game", inventory.SourceAPI, files)
	for range 2 {
		inv.SetUpstreamFiles("game", []inventory.UpstreamFile{{Filename: "demo.gb"}})
		if pending := inv.PendingUpdateFiles("game"); len(pending) != 1 || pending[0].UploadID != "2" {
			t.Fatalf("public subset dropped the hidden update: %+v", pending)
		}
	}
	inv.DismissUpdate("game")
	inv.SetUpstreamFiles("game", []inventory.UpstreamFile{{Filename: "demo.gb"}})
	inv.SetUpstreamFilesFrom("game", inventory.SourceAPI, files)
	if inv.HasPendingUpdates("game") {
		t.Fatal("public omission reopened a dismissed API update")
	}
	inv.SetUpstreamFilesFrom("game", inventory.SourceAPI, files[:1])
	if inv.HasPendingUpdates("game") {
		t.Fatal("authoritative API list did not prune removed upload")
	}
}

func TestArchiveUpdateRequiresEveryTrackedMember(t *testing.T) {
	inv := &inventory.Inventory{Entries: map[string]*inventory.Entry{}}
	for _, name := range []string{"one.gb", "two.gbc"} {
		inv.Add("game", inventory.Entry{}, inventory.DownloadedFile{Filename: name, DestPath: "/" + name, OriginalUpload: "bundle.zip", UploadID: "1", UploadFingerprint: "build:1"})
	}
	current := []inventory.UpstreamFile{{Filename: "bundle.zip", UploadID: "1", Fingerprint: "build:2"}}
	inv.SetUpstreamFilesFrom("game", inventory.SourceAPI, current)
	for i, name := range []string{"one.gb", "two.gbc"} {
		inv.Add("game", inventory.Entry{}, inventory.DownloadedFile{Filename: name, DestPath: "/" + name, OriginalUpload: "bundle.zip", UploadID: "1", UploadFingerprint: "build:2"})
		if got, want := inv.HasPendingUpdates("game"), i == 0; got != want {
			t.Fatalf("after member %s: pending=%v, want %v", name, got, want)
		}
		inv.SetUpstreamFilesFrom("game", inventory.SourceAPI, current)
		if got, want := inv.HasPendingUpdates("game"), i == 0; got != want {
			t.Fatalf("after recheck for member %s: pending=%v, want %v", name, got, want)
		}
	}
}
