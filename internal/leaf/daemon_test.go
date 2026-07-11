package leaf

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestResolveDaemonSocketPrecedence(t *testing.T) {
	values := map[string]string{
		"UMRK_DAEMON_SOCKET": "/explicit/umrk.sock",
		"JAWAKA_SOCKET_PATH": "/compat/jawaka.sock",
	}
	getenv := func(name string) string { return values[name] }
	path, err := ResolveDaemonSocket(getenv, "/runtime")
	if err != nil || path != "/explicit/umrk.sock" {
		t.Fatalf("explicit socket = %q, %v", path, err)
	}
	delete(values, "UMRK_DAEMON_SOCKET")
	path, _ = ResolveDaemonSocket(getenv, "/runtime")
	if path != "/compat/jawaka.sock" {
		t.Fatalf("compat socket = %q", path)
	}
	delete(values, "JAWAKA_SOCKET_PATH")
	path, _ = ResolveDaemonSocket(getenv, "/runtime")
	if path != "/runtime/jawakad.sock" {
		t.Fatalf("fallback socket = %q", path)
	}
}

func TestDaemonClientReferenceCountsOneLease(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "leaf-daemon-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "jawakad.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var mu sync.Mutex
	var types []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			var header [4]byte
			_, _ = io.ReadFull(conn, header[:])
			body := make([]byte, binary.BigEndian.Uint32(header[:]))
			_, _ = io.ReadFull(conn, body)
			var request map[string]any
			_ = json.Unmarshal(body, &request)
			requestType, _ := request["type"].(string)
			mu.Lock()
			types = append(types, requestType)
			mu.Unlock()
			response := map[string]any{"type": "ok"}
			if requestType == "suspend-inhibit-acquire" {
				response = map[string]any{"type": "suspend-inhibit-acquired", "token": "0123456789abcdef0123456789abcdef"}
			}
			encoded, _ := json.Marshal(response)
			binary.BigEndian.PutUint32(header[:], uint32(len(encoded)))
			_, _ = conn.Write(header[:])
			_, _ = conn.Write(encoded)
			_ = conn.Close()
		}
	}()

	client := NewDaemonClient(socket)
	client.timeout = time.Second
	one, err := client.Begin(context.Background(), "download", false)
	if err != nil || !one.Protected {
		t.Fatalf("first Begin = %#v, %v", one, err)
	}
	two, err := client.Begin(context.Background(), "archive", false)
	if err != nil || !two.Protected {
		t.Fatalf("nested Begin = %#v, %v", two, err)
	}
	one.Release()
	select {
	case <-done:
		t.Fatal("nested release sent the daemon release too early")
	case <-time.After(20 * time.Millisecond):
	}
	two.Release()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(types) != 2 || types[0] != "suspend-inhibit-acquire" || types[1] != "suspend-inhibit-release" {
		t.Fatalf("daemon requests = %#v", types)
	}
}

func TestDaemonUnavailableRequiresExplicitContinue(t *testing.T) {
	client := NewDaemonClient(filepath.Join(t.TempDir(), "missing.sock"))
	client.timeout = 50 * time.Millisecond
	if _, err := client.Begin(context.Background(), "download", false); err == nil {
		t.Fatal("automatic operation started without suspend protection")
	}
	lease, err := client.Begin(context.Background(), "download", true)
	if err != nil || lease.Protected {
		t.Fatalf("explicit continue = %#v, %v", lease, err)
	}
	lease.Release()
}
