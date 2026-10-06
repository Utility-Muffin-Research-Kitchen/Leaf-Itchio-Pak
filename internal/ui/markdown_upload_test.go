//go:build !headless

package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/inventory"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/settings"
)

// freeGamePageServer serves a free game's page, its download page listing
// names in order, the resolver and each upload's bytes, all offline.
func freeGamePageServer(t *testing.T, names []string, files map[string][]byte) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/game", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><meta name="csrf_token" value="CSRF"/></head></html>`))
	})
	mux.HandleFunc("/game/download_url", func(w http.ResponseWriter, r *http.Request) {
		data, _ := json.Marshal(map[string]string{"url": srv.URL + "/dl/KEY"})
		w.Write(data)
	})
	mux.HandleFunc("/dl/KEY", func(w http.ResponseWriter, r *http.Request) {
		var body bytes.Buffer
		body.WriteString(`<html><head><meta name="csrf_token" value="DLCSRF"/></head><body>`)
		for index, name := range names {
			fmt.Fprintf(&body, `<div class="upload"><div class="info_column"><div class="upload_name"><strong class="name" title="%s">%s</strong></div></div>`, name, name)
			fmt.Fprintf(&body, `<div class="actions"><a class="button download_btn" href="javascript:void(0);" data-upload_id="%d">Download</a></div></div>`, 100+index)
		}
		body.WriteString(`</body></html>`)
		w.Write(body.Bytes())
	})
	mux.HandleFunc("/game/file/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"url":%q}`, srv.URL+"/cdn/"+strings.TrimPrefix(r.URL.Path, "/game/file/"))
	})
	mux.HandleFunc("/cdn/", func(w http.ResponseWriter, r *http.Request) {
		var id int
		fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/cdn/"), "%d", &id)
		name := names[id-100]
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(files[name]))
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func discoverPlan(t *testing.T, srv *httptest.Server) *CatDownloadPlan {
	t.Helper()
	transactionPaths(t)
	inv, err := inventory.Load(filepath.Join(t.TempDir(), "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	flow := NewCatDownloadFlow(itchio.NewClientWithBase(srv.URL), &settings.Config{ROMLocation: "auto"},
		itchio.Game{Title: "Fixture", URL: srv.URL + "/game", IsFree: true}, nil, inv, nil)
	model := appui.NewDownloadSelectModel("Fixture")
	deadline := time.Now().Add(10 * time.Second)
	for !flow.Sync(model) {
		if time.Now().After(deadline) {
			t.Fatal("download discovery timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if model.State == appui.DownloadSelectError {
		t.Fatalf("discovery error: %s", model.Message)
	}
	return flow.TakePlan()
}

// A README.md upload next to the game is text, not a Mega Drive ROM. The
// listing probes a top-level .md upload's first bytes and drops it when it
// reads as text, so automatic mode downloads only the game (review finding
// R23-4).
func TestDownloadListingDropsATextMarkdownUpload(t *testing.T) {
	srv := freeGamePageServer(t, []string{"game.gb", "README.md"}, map[string][]byte{
		"game.gb": gbROM("MAIN"), "README.md": []byte("# Fixture\n\nPress A to jump.\n"),
	})
	plan := discoverPlan(t, srv)
	if plan == nil || plan.Kind != CatDownloadPlanDirect || len(plan.Uploads) != 1 || plan.Uploads[0].Filename != "game.gb" {
		t.Fatalf("plan = %+v, want only game.gb", plan)
	}
}

// A real Mega Drive upload named .md stays offered.
func TestDownloadListingKeepsAMegaDriveUpload(t *testing.T) {
	sega := make([]byte, 0x200)
	copy(sega[0x100:], "SEGA MEGA DRIVE ")
	srv := freeGamePageServer(t, []string{"sonic.md", "README.md"}, map[string][]byte{
		"sonic.md": sega, "README.md": []byte("# Fixture\n"),
	})
	plan := discoverPlan(t, srv)
	if plan == nil || plan.Kind != CatDownloadPlanDirect || len(plan.Uploads) != 1 || plan.Uploads[0].Filename != "sonic.md" {
		t.Fatalf("plan = %+v, want only sonic.md", plan)
	}
}
