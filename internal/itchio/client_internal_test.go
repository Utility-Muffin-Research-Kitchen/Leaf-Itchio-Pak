package itchio

import (
	"io"
	"net/http"
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
	if !strings.Contains(ua.userAgent, browserUserAgent) || !strings.Contains(ua.userAgent, productName+"/v0.1.0") {
		t.Fatalf("user agent = %q", ua.userAgent)
	}
	fallback, ok := ua.wrapped.(*h2FallbackTransport)
	if !ok {
		t.Fatalf("wrapped transport = %T, want *h2FallbackTransport", ua.wrapped)
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
