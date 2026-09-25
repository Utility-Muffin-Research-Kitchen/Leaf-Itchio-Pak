package itchio

import (
	"context"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

// Starting values from the upstream refresh plan. Change them only with a test
// showing why.
const (
	// rateLimitBaseDelay is the first cooldown after a 429 without a usable
	// Retry-After; each further strike from the same host doubles it.
	rateLimitBaseDelay = 2 * time.Second
	// rateLimitMaxDelay caps every single cooldown, whether computed or
	// requested by the server, so a hostile header cannot stall the app.
	rateLimitMaxDelay = 60 * time.Second
	// rateLimitMaxRetries is how often the transport replays one bodyless
	// GET/HEAD after a 429 before handing the 429 back to the caller.
	rateLimitMaxRetries = 3
	// refreshCooldownBudget bounds the wall-clock time one catalogue refresh
	// may spend waiting out cooldowns before it fails and keeps the cache.
	refreshCooldownBudget = 2 * time.Minute
)

// RateLimitedError reports that a host asked the app to slow down and the
// request could not be sent, or retried, within its limits. It matches
// ErrRateLimited with errors.Is.
type RateLimitedError struct {
	Host string
}

func (err *RateLimitedError) Error() string {
	return "rate limited by " + err.Host
}

func (err *RateLimitedError) Is(target error) bool { return target == ErrRateLimited }

// hostCooldown is the shared back-off state for one host.
type hostCooldown struct {
	notBefore time.Time // no request to the host starts before this
	strikes   int       // consecutive 429 episodes; drives the back-off
}

// rateLimitTransport pauses every request to a host after that host answers
// HTTP 429. The state lives in the client's single transport, so the metadata
// client and the per-call copies used for streaming, range reads and CDN
// resolution all share it. Hosts cool down independently: a 429 from itch.io
// does not pause api.itch.io or a CDN.
type rateLimitTransport struct {
	wrapped http.RoundTripper

	mu    sync.Mutex
	hosts map[string]*hostCooldown

	now        func() time.Time                                // replaced in tests
	sleepUntil func(ctx context.Context, wake time.Time) error // replaced in tests
	jitter     func(d time.Duration) time.Duration             // replaced in tests
}

func newRateLimitTransport(wrapped http.RoundTripper) *rateLimitTransport {
	return &rateLimitTransport{
		wrapped: wrapped,
		hosts:   make(map[string]*hostCooldown),
		now:     time.Now,
		sleepUntil: func(ctx context.Context, wake time.Time) error {
			return waitForRetry(ctx, time.Until(wake))
		},
		jitter: func(d time.Duration) time.Duration {
			// Up to +20% so parallel requests do not all return at once.
			return time.Duration(rand.Int64N(int64(d)/5 + 1))
		},
	}
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	// Only bodyless GET/HEAD requests can be replayed. Download handshakes
	// and session POSTs are never sent twice automatically.
	replayable := (req.Method == http.MethodGet || req.Method == http.MethodHead) &&
		(req.Body == nil || req.Body == http.NoBody)
	for attempt := 0; ; attempt++ {
		if err := t.waitTurn(req.Context(), host); err != nil {
			return nil, err
		}
		resp, err := t.wrapped.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			t.recordOK(host)
			return resp, nil
		}
		t.record429(host, resp.Header.Get("Retry-After"))
		if !replayable || attempt >= rateLimitMaxRetries {
			return resp, nil
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		// Never log the path or query: v1 API paths and signed CDN URLs
		// carry credentials.
		logger.Info("ratelimit: retrying %s to %s (%d/%d)", req.Method, host, attempt+1, rateLimitMaxRetries)
	}
}

// waitTurn blocks until host's cooldown has passed. It fails at once, without
// sleeping, when the request's deadline or the refresh budget would run out
// first. A cooldown extended by a concurrent 429 while this request slept is
// waited out too.
func (t *rateLimitTransport) waitTurn(ctx context.Context, host string) error {
	for {
		t.mu.Lock()
		var until time.Time
		if cooldown := t.hosts[host]; cooldown != nil {
			until = cooldown.notBefore
		}
		t.mu.Unlock()

		now := t.now()
		if !until.After(now) {
			return nil
		}
		if deadline, ok := ctx.Deadline(); ok && deadline.Before(until) {
			logger.Warn("ratelimit: %s cooling down for %s, past this request's deadline; not sent", host, until.Sub(now).Round(time.Second))
			return &RateLimitedError{Host: host}
		}
		if budget := cooldownBudgetFrom(ctx); budget != nil && !budget.charge(now, until) {
			logger.Warn("ratelimit: %s cooldown exceeds the remaining refresh budget; not sent", host)
			return &RateLimitedError{Host: host}
		}
		logger.Debug("ratelimit: waiting %s for %s cooldown", until.Sub(now).Round(time.Millisecond), host)
		if err := t.sleepUntil(ctx, until); err != nil {
			return err
		}
	}
}

func (t *rateLimitTransport) recordOK(host string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cooldown := t.hosts[host]; cooldown != nil && cooldown.strikes > 0 {
		logger.Info("ratelimit: %s answering again after %d rate-limited episode(s)", host, cooldown.strikes)
		cooldown.strikes = 0
	}
}

func (t *rateLimitTransport) record429(host, retryAfter string) {
	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	cooldown := t.hosts[host]
	if cooldown == nil {
		cooldown = &hostCooldown{}
		t.hosts[host] = cooldown
	}
	// A 429 answering a request that was already in flight when the current
	// cooldown began belongs to the same episode.
	if cooldown.strikes == 0 || !now.Before(cooldown.notBefore) {
		cooldown.strikes++
	}

	delay, fromServer := parseRetryAfter(retryAfter, now)
	source := "Retry-After"
	if !fromServer {
		delay = rateLimitMaxDelay
		if shift := cooldown.strikes - 1; shift < 8 {
			delay = min(rateLimitBaseDelay<<shift, rateLimitMaxDelay)
		}
		delay = min(delay+t.jitter(delay), rateLimitMaxDelay)
		source = "backoff, strike " + strconv.Itoa(cooldown.strikes)
	}
	if until := now.Add(delay); until.After(cooldown.notBefore) {
		cooldown.notBefore = until
	}
	logger.Warn("ratelimit: HTTP 429 from %s; pausing requests to it for %s (%s)", host, cooldown.notBefore.Sub(now).Round(time.Millisecond), source)
}

// parseRetryAfter accepts both Retry-After forms, delay-seconds and an
// HTTP-date, capped at rateLimitMaxDelay. It reports false for an absent or
// malformed value so the caller falls back to exponential back-off.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		if seconds > int64(rateLimitMaxDelay/time.Second) {
			return rateLimitMaxDelay, true
		}
		return time.Duration(seconds) * time.Second, true
	}
	if at, err := http.ParseTime(value); err == nil {
		return min(max(at.Sub(now), 0), rateLimitMaxDelay), true
	}
	return 0, false
}

// cooldownBudget bounds the wall-clock cooldown one operation may wait out
// across all of its requests. Concurrent requests waiting on the same
// cooldown window are charged once, not once each.
type cooldownBudget struct {
	mu           sync.Mutex
	remaining    time.Duration
	chargedUntil time.Time
}

type cooldownBudgetKey struct{}

func withCooldownBudget(ctx context.Context, limit time.Duration) context.Context {
	return context.WithValue(ctx, cooldownBudgetKey{}, &cooldownBudget{remaining: limit})
}

func cooldownBudgetFrom(ctx context.Context) *cooldownBudget {
	budget, _ := ctx.Value(cooldownBudgetKey{}).(*cooldownBudget)
	return budget
}

// charge reserves the part of [now, until) not already charged. It charges
// nothing and reports false when that exceeds what remains.
func (b *cooldownBudget) charge(now, until time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	start := now
	if b.chargedUntil.After(start) {
		start = b.chargedUntil
	}
	cost := until.Sub(start)
	if cost <= 0 {
		return true
	}
	if cost > b.remaining {
		return false
	}
	b.remaining -= cost
	b.chargedUntil = until
	return true
}
