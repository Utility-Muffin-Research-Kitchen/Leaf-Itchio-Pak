package itchio

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

const (
	productName = "Leaf-Itchio-Pak"
	productURL  = "https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak"

	dialTimeout           = 10 * time.Second
	keepAlive             = 30 * time.Second
	responseHeaderTimeout = 15 * time.Second
	metadataTimeout       = 30 * time.Second

	apiItchIO = "https://api.itch.io"
)

// errH1Negotiated is returned by dialTLS when the server selects http/1.1
// via ALPN. h2FallbackTransport catches it to route the request (and all
// future requests to that host) through the h1 transport instead.
var errH1Negotiated = errors.New("server negotiated http/1.1")

// tlsRootCAs is nil in production, which selects the system roots. Tests
// substitute the roots of their local TLS servers.
var tlsRootCAs *x509.CertPool

// uaTransport identifies the app on every outbound request that does not set
// its own headers, then delegates to the wrapped RoundTripper. itch.io asked
// clients to say what they are rather than pose as a browser
// (carroarmato0/NextUI-Itchio-Pak#4).
type uaTransport struct {
	wrapped   http.RoundTripper
	userAgent string
}

func setDefaultHeader(req *http.Request, key, value string) {
	if req.Header.Get(key) == "" {
		req.Header.Set(key, value)
	}
}

func (t *uaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	setDefaultHeader(req, "User-Agent", t.userAgent)
	setDefaultHeader(req, "Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	setDefaultHeader(req, "Accept-Language", "en-US,en;q=0.9")
	return t.wrapped.RoundTrip(req)
}

// dialTLSWithALPN dials a standard crypto/tls connection offering protos
// via ALPN. Certificates are verified against the system roots, which the Pak
// points at its packaged bundle through SSL_CERT_FILE.
func dialTLSWithALPN(ctx context.Context, network, addr string, protos []string) (*tls.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive},
		Config:    &tls.Config{ServerName: host, NextProtos: protos, RootCAs: tlsRootCAs},
	}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return conn.(*tls.Conn), nil
}

// dialTLS advertises ["h2", "http/1.1"] via ALPN. If the server selects h2
// the conn is returned to http2.Transport. If it selects http/1.1, the conn
// is closed and errH1Negotiated is returned so h2FallbackTransport can retry
// over the h1 transport. The cfg parameter satisfies http2.Transport's
// DialTLSContext signature but is ignored; ALPN is chosen here.
func dialTLS(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
	conn, err := dialTLSWithALPN(ctx, network, addr, []string{"h2", "http/1.1"})
	if err != nil {
		return nil, err
	}
	state := conn.ConnectionState()
	logger.Debug("client: TLS addr=%s proto=%s version=%s", addr, state.NegotiatedProtocol, tls.VersionName(state.Version))
	if state.NegotiatedProtocol != "h2" {
		conn.Close()
		return nil, errH1Negotiated
	}
	return conn, nil
}

// dialTLSH1 is the http.Transport-compatible dialer (no *tls.Config param)
// with http/1.1-only ALPN, for servers that do not support h2 (signed
// download CDNs, custom game hosting).
func dialTLSH1(ctx context.Context, network, addr string) (net.Conn, error) {
	return dialTLSWithALPN(ctx, network, addr, []string{"http/1.1"})
}

// h2FallbackTransport routes HTTPS requests through http2.Transport for h2
// servers (itch.io game pages, API, image CDN) and falls back to an h1
// transport for servers that only negotiate http/1.1 (Cloudflare R2 signed
// download URLs, custom game hosting). Per-host routing is cached so the
// extra handshake only occurs on the first request to each h1-only host.
// Plain HTTP requests (httptest servers in tests) always use the h1 transport.
type h2FallbackTransport struct {
	h2 http.RoundTripper
	h1 http.RoundTripper

	mu      sync.RWMutex
	h1hosts map[string]struct{} // hosts that negotiated http/1.1
}

func (t *h2FallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return t.h1.RoundTrip(req)
	}

	host := req.URL.Host
	if !strings.Contains(host, ":") {
		host = net.JoinHostPort(host, "443")
	}

	t.mu.RLock()
	_, isH1 := t.h1hosts[host]
	t.mu.RUnlock()

	if isH1 {
		return t.h1.RoundTrip(req)
	}

	resp, err := t.h2.RoundTrip(req)
	if errors.Is(err, errH1Negotiated) {
		logger.Info("client: %s negotiates http/1.1, caching as h1-only", host)
		t.mu.Lock()
		t.h1hosts[host] = struct{}{}
		t.mu.Unlock()
		return t.h1.RoundTrip(req)
	}
	return resp, err
}

func productUserAgent(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "dev"
	}
	// Product tokens cannot contain whitespace. Build versions are normally
	// semver, but keep developer overrides safe and deterministic too.
	version = strings.Map(func(value rune) rune {
		if value <= ' ' || value == '/' || value == ';' || value == '(' || value == ')' {
			return '-'
		}
		return value
	}, version)
	return fmt.Sprintf("%s/%s (+%s)", productName, version, productURL)
}

// safeRequestError keeps credential-bearing request URLs out of UI/crash
// messages while retaining the full failure in the local, redacted debug log.
// Cancellation identity is preserved for transaction rollback logic.
func safeRequestError(operation string, err error) error {
	logger.Debug("%s request failed: %v", operation, err)
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s: %w", operation, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	case errors.Is(err, ErrRateLimited):
		return fmt.Errorf("%s: %w", operation, ErrRateLimited)
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return fmt.Errorf("%s: network timeout", operation)
	}
	return fmt.Errorf("%s: network request failed", operation)
}

func newHTTPClient(version string) *http.Client {
	jar, _ := cookiejar.New(nil)
	h2t := &http2.Transport{
		DialTLSContext:  dialTLS,
		ReadIdleTimeout: responseHeaderTimeout,
		PingTimeout:     dialTimeout,
	}
	h1t := &http.Transport{
		DialTLSContext:        dialTLSH1,
		ResponseHeaderTimeout: responseHeaderTimeout,
		IdleConnTimeout:       keepAlive,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
	}
	return &http.Client{
		Jar:     jar,
		Timeout: metadataTimeout,
		Transport: &uaTransport{
			userAgent: productUserAgent(version),
			wrapped: newRateLimitTransport(&h2FallbackTransport{
				h2:      h2t,
				h1:      h1t,
				h1hosts: make(map[string]struct{}),
			}),
		},
	}
}

type Client struct {
	http   *http.Client
	base   string // itch.io/api/1/... base URL
	butler string // api.itch.io base URL (butler-style endpoints)

	// Background API key validation state (atomic, written once per session).
	apiKeyStatus   int32 // stores APIKeyStatus constants
	apiKeyChecking int32 // 0 = not started, 1 = started (CAS gate)
}

func NewClient() *Client {
	return NewClientWithVersion("dev")
}

// NewClientWithVersion builds the production client, which identifies itself
// as Leaf-Itchio-Pak/<version> with the project URL. Development and test
// clients use "dev" as the version.
func NewClientWithVersion(version string) *Client {
	return &Client{
		http:   newHTTPClient(version),
		base:   "https://itch.io",
		butler: apiItchIO,
	}
}

func NewClientWithBase(base string) *Client {
	return &Client{
		http:   newHTTPClient("dev"),
		base:   base,
		butler: apiItchIO,
	}
}

// NewClientWithBaseAndButler is used in tests to override both base URLs.
func NewClientWithBaseAndButler(base, butler string) *Client {
	return &Client{
		http:   newHTTPClient("dev"),
		base:   base,
		butler: butler,
	}
}

// HTTPClient returns the underlying *http.Client used for all requests.
func (c *Client) HTTPClient() *http.Client {
	return c.http
}

// DownloadURL streams directly from a pre-resolved CDN URL to dest.
// Use when the CDN URL was already resolved by ResolveFreeURL or ResolveAuthURL.
func (c *Client) DownloadURL(cdnURL, dest string, progress func(int64, int64)) error {
	return c.streamToFile(cdnURL, dest, progress)
}
