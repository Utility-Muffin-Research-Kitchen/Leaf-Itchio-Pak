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
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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
// substitute the roots of their local TLS servers. Each client captures it
// once when built.
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
func dialTLSWithALPN(ctx context.Context, network, addr string, protos []string, roots *x509.CertPool) (*tls.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: dialTimeout, KeepAlive: keepAlive},
		Config:    &tls.Config{ServerName: host, NextProtos: protos, RootCAs: roots},
	}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return conn.(*tls.Conn), nil
}

// dialTLS returns the http2.Transport dialer, which advertises ["h2",
// "http/1.1"] via ALPN. If the server selects h2 the conn is returned to
// http2.Transport. If it selects http/1.1, the conn is closed and
// errH1Negotiated is returned so h2FallbackTransport can retry over the h1
// transport. The cfg parameter satisfies http2.Transport's DialTLSContext
// signature but is ignored; ALPN is chosen here.
func dialTLS(roots *x509.CertPool) func(context.Context, string, string, *tls.Config) (net.Conn, error) {
	return func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
		conn, err := dialTLSWithALPN(ctx, network, addr, []string{"h2", "http/1.1"}, roots)
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
}

// dialTLSH1 returns the http.Transport-compatible dialer (no *tls.Config
// param) with http/1.1-only ALPN, for servers that do not support h2 (signed
// download CDNs, custom game hosting).
func dialTLSH1(roots *x509.CertPool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialTLSWithALPN(ctx, network, addr, []string{"http/1.1"}, roots)
	}
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

// productToken is "Leaf-Itchio-Pak/<version>", with "dev" for an empty
// version.
func productToken(version string) string {
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
	return productName + "/" + version
}

func productUserAgent(version string) string {
	return fmt.Sprintf("%s (+%s)", productToken(version), productURL)
}

// safeRequestError keeps credential-bearing request URLs out of UI/crash
// messages and out of the log, which gets only the operation, the host and
// the underlying failure. Cancellation identity is preserved for transaction
// rollback logic.
func safeRequestError(operation string, err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		host := "unknown host"
		if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil && parsed.Host != "" {
			host = parsed.Host
		}
		logger.Debug("%s request to %s failed: %v", operation, host, urlErr.Err)
	} else {
		logger.Debug("%s request failed: %v", operation, err)
	}
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("%s: %w", operation, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	case errors.Is(err, ErrRateLimited):
		// No operation prefix: the download screens show this text as is.
		var limited *RateLimitedError
		if errors.As(err, &limited) {
			return limited
		}
		return ErrRateLimited
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return fmt.Errorf("%s: network timeout", operation)
	}
	return fmt.Errorf("%s: network request failed", operation)
}

// newHTTPClient builds the shared client. replayHosts names extra hosts
// (host:port) to treat as itch.io when replaying 429s, for test servers.
func newHTTPClient(version string, replayHosts ...string) *http.Client {
	jar, _ := cookiejar.New(nil)
	roots := tlsRootCAs
	h2t := &http2.Transport{
		DialTLSContext:  dialTLS(roots),
		ReadIdleTimeout: responseHeaderTimeout,
		PingTimeout:     dialTimeout,
	}
	h1t := &http.Transport{
		DialTLSContext:        dialTLSH1(roots),
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
			}, replayHosts...),
		},
	}
}

type Client struct {
	http    *http.Client
	product string // productToken, also sent as sign-in device_info
	base    string // itch.io web base URL (pages, feeds, free downloads)
	butler  string // api.itch.io base URL (API v2, bearer-authenticated)

	// keyGeneration changes whenever the API key is replaced or removed, so
	// account-derived results computed under an older key are discarded.
	keyGeneration atomic.Uint64
	// Credential snapshot for background checks; never serialized or logged.
	authToken atomic.Pointer[string]
	// purchaseCounts maps purchase ID to the number of distinct games it
	// grants, from the last complete owned-library scan under the current
	// key. nil until such a scan; never persisted.
	ownedMu        sync.Mutex
	purchaseCounts map[int64]int
	// purchaseCountsPartial marks counts from a scan stopped at the page cap.
	purchaseCountsPartial bool
	// prices holds the price fields of every data.json fetched this session,
	// by the requested game URL, for list badges.
	pricesMu sync.Mutex
	prices   map[string]GameData
}

func NewClient() *Client {
	return NewClientWithVersion("dev")
}

// NewClientWithVersion builds the production client, which identifies itself
// as Leaf-Itchio-Pak/<version> with the project URL. Development and test
// clients use "dev" as the version.
func NewClientWithVersion(version string) *Client {
	return &Client{
		http:    newHTTPClient(version),
		product: productToken(version),
		base:    "https://itch.io",
		butler:  apiItchIO,
	}
}

// NewClientWithBase is used in tests. The API base is the same server, so a
// test client can never reach the real api.itch.io.
func NewClientWithBase(base string) *Client {
	return NewClientWithBaseAndButler(base, base)
}

// NewClientWithBaseAndButler is used in tests to override both base URLs.
func NewClientWithBaseAndButler(base, butler string) *Client {
	return &Client{
		http:    newHTTPClient("dev", urlHost(base), urlHost(butler)),
		product: productToken("dev"),
		base:    base,
		butler:  butler,
	}
}

// urlHost returns the host:port of rawURL, or "" when it does not parse.
func urlHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// rateLimiter returns the client's 429 cooldown transport, or nil when the
// client was built without one.
func (c *Client) rateLimiter() *rateLimitTransport {
	transport := c.http.Transport
	for {
		switch layer := transport.(type) {
		case *rateLimitTransport:
			return layer
		case *uaTransport:
			transport = layer.wrapped
		default:
			return nil
		}
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
