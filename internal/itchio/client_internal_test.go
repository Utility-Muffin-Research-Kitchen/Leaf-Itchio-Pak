package itchio

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestHTTPClientHasBoundedMetadataTransport(t *testing.T) {
	client := newHTTPClient("v0.1.0")
	if client.Timeout != metadataTimeout || client.Timeout <= 0 {
		t.Fatalf("metadata timeout = %v, want %v", client.Timeout, metadataTimeout)
	}
	ua, ok := client.Transport.(*uaTransport)
	if !ok {
		t.Fatalf("transport = %T, want *uaTransport", client.Transport)
	}
	if ua.userAgent != "Leaf-Itchio-Pak/v0.1.0 (+"+productURL+")" {
		t.Fatalf("user agent = %q", ua.userAgent)
	}
	limiter, ok := ua.wrapped.(*rateLimitTransport)
	if !ok {
		t.Fatalf("wrapped transport = %T, want *rateLimitTransport", ua.wrapped)
	}
	fallback, ok := limiter.wrapped.(*h2FallbackTransport)
	if !ok {
		t.Fatalf("limited transport = %T, want *h2FallbackTransport", limiter.wrapped)
	}
	h1, ok := fallback.h1.(*http.Transport)
	if !ok {
		t.Fatalf("h1 transport = %T, want *http.Transport", fallback.h1)
	}
	if h1.ResponseHeaderTimeout != responseHeaderTimeout || h1.IdleConnTimeout != keepAlive {
		t.Fatalf("h1 bounds = header %v idle %v", h1.ResponseHeaderTimeout, h1.IdleConnTimeout)
	}
}

func TestH2FallbackCachesH1OnlyHost(t *testing.T) {
	var h2Calls, h1Calls atomic.Int32
	transport := &h2FallbackTransport{
		h2: roundTripFunc(func(*http.Request) (*http.Response, error) {
			h2Calls.Add(1)
			return nil, errH1Negotiated
		}),
		h1: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			h1Calls.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		}),
		h1hosts: make(map[string]struct{}),
	}

	request, err := http.NewRequest(http.MethodGet, "https://downloads.example/game.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		response, routeErr := transport.RoundTrip(request)
		if routeErr != nil {
			t.Fatal(routeErr)
		}
		response.Body.Close()
	}
	if got := h2Calls.Load(); got != 1 {
		t.Fatalf("h2 attempts = %d, want 1", got)
	}
	if got := h1Calls.Load(); got != 2 {
		t.Fatalf("h1 requests = %d, want 2", got)
	}
}

func TestProductUserAgentSanitizesVersion(t *testing.T) {
	for version, want := range map[string]string{
		"":             "dev",
		"  ":           "dev",
		"0.2.0":        "0.2.0",
		"1.0 (beta)/x": "1.0--beta--x",
	} {
		if got := productUserAgent(version); got != productName+"/"+want+" (+"+productURL+")" {
			t.Errorf("productUserAgent(%q) = %q", version, got)
		}
	}
}

func TestStandardTLSNegotiatesH2AndH1WithVerification(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })
	h2 := httptest.NewUnstartedServer(handler)
	h2.EnableHTTP2 = true
	h2.StartTLS()
	defer h2.Close()
	h1 := httptest.NewTLSServer(handler)
	defer h1.Close()

	roots := x509.NewCertPool()
	roots.AddCert(h2.Certificate())
	roots.AddCert(h1.Certificate())
	tlsRootCAs = roots
	defer func() { tlsRootCAs = nil }()

	client := newHTTPClient("dev")
	for _, test := range []struct {
		server *httptest.Server
		proto  string
	}{{h2, "HTTP/2.0"}, {h1, "HTTP/1.1"}, {h1, "HTTP/1.1"}} {
		resp, err := client.Get(test.server.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.Proto != test.proto {
			t.Errorf("%s: proto = %s, want %s", test.server.URL, resp.Proto, test.proto)
		}
		// Current Go defaults, not the Go 1.22 GODEBUG set (tlsmlkem=0).
		if resp.TLS == nil || resp.TLS.Version != tls.VersionTLS13 || resp.TLS.CurveID != tls.X25519MLKEM768 {
			t.Errorf("%s: TLS state = %+v, want TLS 1.3 with X25519MLKEM768", test.server.URL, resp.TLS)
		}
	}

	tlsRootCAs = x509.NewCertPool()
	for _, server := range []*httptest.Server{h2, h1} {
		if resp, err := newHTTPClient("dev").Get(server.URL); err == nil {
			resp.Body.Close()
			t.Errorf("%s: untrusted certificate accepted", server.URL)
		}
	}
}
