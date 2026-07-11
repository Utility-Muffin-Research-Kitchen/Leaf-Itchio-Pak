//go:build !headless

package ui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
)

func TestCatCacheRefreshCancellationDoesNotSavePartialCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "games_cache.json")
	flow, model := NewCatCacheRefreshFlow(itchio.NewClientWithBase(server.URL), path, nil)
	flow.Cancel()
	deadline := time.Now().Add(2 * time.Second)
	for model.State == appui.RefreshLoading && time.Now().Before(deadline) {
		flow.Sync(model)
		time.Sleep(time.Millisecond)
	}
	if model.State != appui.RefreshCancelled {
		t.Fatalf("state = %v", model.State)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("partial cache exists: %v", err)
	}
}

func TestCatCacheRefreshCommitsCompleteCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "games_cache.json")
	flow, model := NewCatCacheRefreshFlow(itchio.NewClientWithBase(server.URL), path, nil)
	deadline := time.Now().Add(2 * time.Second)
	var games []itchio.Game
	for model.State == appui.RefreshLoading && time.Now().Before(deadline) {
		if result, changed := flow.Sync(model); changed && result != nil {
			games = result
		}
		time.Sleep(time.Millisecond)
	}
	if model.State != appui.RefreshDone || games == nil {
		t.Fatalf("state=%v games=%v", model.State, games)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("committed cache missing: %v", err)
	}
}
