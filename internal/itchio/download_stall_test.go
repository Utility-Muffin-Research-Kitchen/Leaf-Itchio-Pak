package itchio

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

type streamLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *streamLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *streamLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func captureStreamLog(t *testing.T) *streamLog {
	t.Helper()
	capture := &streamLog{}
	log.SetOutput(capture)
	logger.SetLevel(logger.LevelInfo)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return capture
}

// The log names the partial file that is actually on disk while the stream
// runs, not only the destination it is renamed to afterwards.
func TestStreamLogsThePartialFileOnDisk(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		logs := captureStreamLog(t)
		dir := t.TempDir()
		var partial string
		client := &Client{http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			reader, writer := io.Pipe()
			go func() {
				_, _ = io.WriteString(writer, "partial")
				matches, _ := filepath.Glob(filepath.Join(dir, ".itchio-download-*.part"))
				if len(matches) == 1 {
					partial = filepath.Base(matches[0])
				}
				<-req.Context().Done()
				_ = writer.CloseWithError(req.Context().Err())
			}()
			return &http.Response{StatusCode: http.StatusOK, ContentLength: 100, Body: reader}, nil
		})}}
		if err := client.streamToFile("https://cdn.example/game", filepath.Join(dir, "game.gbc"), nil); err == nil {
			t.Fatal("stalled stream succeeded")
		}
		if partial == "" {
			t.Fatal("no partial file was on disk during the stream")
		}
		if !strings.Contains(logs.String(), partial) {
			t.Fatalf("log does not name the partial file %s:\n%s", partial, logs)
		}
	})
}
