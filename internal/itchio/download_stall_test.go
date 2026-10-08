package itchio

import (
	"bytes"
	"errors"
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

type transportTimeout struct{}

func (transportTimeout) Error() string   { return "net/http: timeout awaiting response headers" }
func (transportTimeout) Timeout() bool   { return true }
func (transportTimeout) Temporary() bool { return true }

// failingBody returns some data, then fails the way a lost connection does.
type failingBody struct {
	data []byte
	err  error
}

func (b *failingBody) Read(p []byte) (int, error) {
	if len(b.data) > 0 {
		n := copy(p, b.data)
		b.data = b.data[n:]
		return n, nil
	}
	return 0, b.err
}

func (b *failingBody) Close() error { return nil }

func streamWithFailure(t *testing.T, respond func(*http.Request) (*http.Response, error)) error {
	t.Helper()
	client := &Client{http: &http.Client{Transport: roundTripFunc(respond)}}
	dir := t.TempDir()
	dest := filepath.Join(dir, "game.gbc")
	if err := os.WriteFile(dest, []byte("installed"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := client.streamToFile("https://cdn.example/game?token=private", dest, nil)
	if data, readErr := os.ReadFile(dest); readErr != nil || string(data) != "installed" {
		t.Fatalf("installed file = %q, %v", data, readErr)
	}
	if entries, readErr := os.ReadDir(dir); readErr != nil || len(entries) != 1 {
		t.Fatalf("files after failure = %v, %v", entries, readErr)
	}
	return err
}

// The h1 response-header timeout (15 s) and the h2 ping timeout (about 25 s)
// fire before the 30-second idle guard. Both still mean a quiet connection,
// so you see the same stall message, not raw transport text.
func TestStreamTransportTimeoutsReportAStall(t *testing.T) {
	lost := errors.New("http2: client connection lost")
	body := func(err error) func(*http.Request) (*http.Response, error) {
		return func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, ContentLength: 100,
				Body: &failingBody{data: []byte("partial"), err: err}}, nil
		}
	}
	for _, tc := range []struct {
		name    string
		respond func(*http.Request) (*http.Response, error)
	}{
		{"response header timeout", func(*http.Request) (*http.Response, error) { return nil, transportTimeout{} }},
		{"connection lost before headers", func(*http.Request) (*http.Response, error) { return nil, lost }},
		{"connection lost during body", body(lost)},
		{"read timeout during body", body(os.ErrDeadlineExceeded)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureStreamLog(t)
			err := streamWithFailure(t, tc.respond)
			var stall downloadIdleTimeout
			if !errors.As(err, &stall) || err.Error() != stall.Error() {
				t.Fatalf("error = %v, want %q", err, stall.Error())
			}
			if !strings.Contains(logs.String(), "[WARN]  stream: stalled") {
				t.Fatalf("stall was not logged:\n%s", logs)
			}
			if strings.Contains(logs.String(), "private") {
				t.Fatalf("log exposes the signed URL:\n%s", logs)
			}
		})
	}
}

func TestStreamOtherNetworkFailuresAreNotStalls(t *testing.T) {
	reset := errors.New("read tcp 10.0.0.2:51000->1.2.3.4:443: read: connection reset by peer")
	err := streamWithFailure(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, ContentLength: 100,
			Body: &failingBody{data: []byte("partial"), err: reset}}, nil
	})
	var stall downloadIdleTimeout
	if err == nil || errors.As(err, &stall) {
		t.Fatalf("error = %v, want the read failure, not a stall", err)
	}
}
