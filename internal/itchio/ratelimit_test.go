package itchio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock drives rateLimitTransport without real sleeps.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept []time.Duration
	// onSleep runs before the clock advances, e.g. to extend a cooldown the
	// way a concurrent 429 would.
	onSleep func(call int)
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// SleepUntil advances the clock to the wake time. Concurrent sleeps overlap
// instead of adding up, as they would in real time. slept records each wait
// as seen from the clock when it began.
func (c *fakeClock) SleepUntil(ctx context.Context, wake time.Time) error {
	c.mu.Lock()
	call := len(c.slept)
	c.slept = append(c.slept, wake.Sub(c.now))
	hook := c.onSleep
	c.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if wake.After(c.now) {
		c.now = wake
	}
	c.mu.Unlock()
	return nil
}

func (c *fakeClock) Slept() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.slept...)
}

// scriptedServer answers each host from its own queue of responses, and
// repeats the last one when the queue runs dry.
type scriptedServer struct {
	mu        sync.Mutex
	responses map[string][]scripted
	requests  map[string]int
}

type scripted struct {
	status     int
	retryAfter string
}

func (s *scriptedServer) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests[req.URL.Host]++
	queue := s.responses[req.URL.Host]
	next := scripted{status: http.StatusOK}
	if len(queue) > 0 {
		next = queue[0]
		if len(queue) > 1 {
			s.responses[req.URL.Host] = queue[1:]
		}
	}
	header := make(http.Header)
	if next.retryAfter != "" {
		header.Set("Retry-After", next.retryAfter)
	}
	return &http.Response{
		StatusCode: next.status, Header: header, Request: req,
		Body: io.NopCloser(strings.NewReader("body")),
	}, nil
}

func (s *scriptedServer) Requests(host string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[host]
}

func newTestLimiter(server *scriptedServer, clock *fakeClock) *rateLimitTransport {
	limiter := newRateLimitTransport(server)
	limiter.now = clock.Now
	limiter.sleepUntil = clock.SleepUntil
	limiter.jitter = func(time.Duration) time.Duration { return 0 }
	return limiter
}

func newScripted(responses map[string][]scripted) *scriptedServer {
	return &scriptedServer{responses: responses, requests: map[string]int{}}
}

func do(t *testing.T, rt http.RoundTripper, ctx context.Context, method, rawURL string) (*http.Response, error) {
	t.Helper()
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader("csrf_token=x")
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}
	return resp, err
}

func TestRateLimitCooldownIsSharedWithinAHostOnly(t *testing.T) {
	clock := newFakeClock()
	server := newScripted(map[string][]scripted{
		"itch.io": {{status: 429, retryAfter: "5"}, {status: 200}},
	})
	limiter := newTestLimiter(server, clock)

	// The first request is retried after the 5 s cooldown and succeeds.
	if resp, err := do(t, limiter, context.Background(), http.MethodGet, "https://itch.io/games/a.xml"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("first request = %v, %v", resp, err)
	}
	// Re-arm a cooldown and check that other hosts do not wait for it.
	limiter.record429("itch.io", "30")
	for _, host := range []string{"api.itch.io", "cdn.example"} {
		if _, err := do(t, limiter, context.Background(), http.MethodGet, "https://"+host+"/x"); err != nil {
			t.Fatal(err)
		}
	}
	if got := clock.Slept(); len(got) != 1 || got[0] != 5*time.Second {
		t.Fatalf("slept %v, want only the 5s itch.io cooldown", got)
	}
	// A second itch.io request waits out the shared cooldown first.
	if _, err := do(t, limiter, context.Background(), http.MethodGet, "https://itch.io/games/b.xml"); err != nil {
		t.Fatal(err)
	}
	if got := clock.Slept(); len(got) != 2 || got[1] != 30*time.Second {
		t.Fatalf("slept %v, want the 30s cooldown on the next itch.io request", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		value string
		want  time.Duration
		ok    bool
	}{
		{"", 0, false},
		{"abc", 0, false},
		{"-5", 0, false},
		{"0", 0, true},
		{"7", 7 * time.Second, true},
		{"600", rateLimitMaxDelay, true},
		{"99999999999999999999", 0, false},
		{now.Add(20 * time.Second).Format(http.TimeFormat), 20 * time.Second, true},
		{now.Add(10 * time.Minute).Format(http.TimeFormat), rateLimitMaxDelay, true},
		{now.Add(-time.Minute).Format(http.TimeFormat), 0, true},
		{now.Add(15 * time.Second).Format(time.RFC850), 15 * time.Second, true},
	}
	for _, test := range tests {
		got, ok := parseRetryAfter(test.value, now)
		if got != test.want || ok != test.ok {
			t.Errorf("parseRetryAfter(%q) = %v, %v; want %v, %v", test.value, got, ok, test.want, test.ok)
		}
	}
}

func TestRateLimitBackoffDoublesAndCaps(t *testing.T) {
	clock := newFakeClock()
	limiter := newTestLimiter(newScripted(nil), clock)
	var got []time.Duration
	for range 8 {
		limiter.record429("itch.io", "")
		got = append(got, limiter.hosts["itch.io"].notBefore.Sub(clock.Now()))
		clock.SleepUntil(context.Background(), limiter.hosts["itch.io"].notBefore) // a new episode each time
	}
	want := []time.Duration{2, 4, 8, 16, 32, 60, 60, 60}
	for index := range want {
		if got[index] != want[index]*time.Second {
			t.Fatalf("back-off = %v, want %v seconds", got, want)
		}
	}
	limiter.recordOK("itch.io")
	limiter.record429("itch.io", "")
	if delay := limiter.hosts["itch.io"].notBefore.Sub(clock.Now()); delay != rateLimitBaseDelay {
		t.Fatalf("delay after a success = %v, want the base delay again", delay)
	}
}

func TestRateLimitJitterNeverExceedsTheCap(t *testing.T) {
	limiter := newRateLimitTransport(newScripted(nil))
	now := time.Now()
	limiter.now = func() time.Time { return now }
	limiter.hosts["itch.io"] = &hostCooldown{strikes: 20}
	for range 50 {
		limiter.hosts["itch.io"].notBefore = time.Time{}
		limiter.record429("itch.io", "")
		if delay := limiter.hosts["itch.io"].notBefore.Sub(now); delay > rateLimitMaxDelay || delay < rateLimitMaxDelay/2 {
			t.Fatalf("delay = %v, want within the cap", delay)
		}
	}
}

func TestRateLimitInFlight429sCountAsOneStrike(t *testing.T) {
	clock := newFakeClock()
	limiter := newTestLimiter(newScripted(nil), clock)
	for range 3 {
		limiter.record429("itch.io", "")
	}
	if strikes := limiter.hosts["itch.io"].strikes; strikes != 1 {
		t.Fatalf("strikes = %d, want 1 for 429s inside one cooldown", strikes)
	}
}

func TestRateLimitRetriesGetAtMostThreeTimes(t *testing.T) {
	clock := newFakeClock()
	server := newScripted(map[string][]scripted{"itch.io": {{status: 429, retryAfter: "1"}}})
	limiter := newTestLimiter(server, clock)
	resp, err := do(t, limiter, context.Background(), http.MethodGet, "https://itch.io/games/a.xml")
	if err != nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("response = %v, %v; want the final 429 handed back", resp, err)
	}
	if got := server.Requests("itch.io"); got != 1+rateLimitMaxRetries {
		t.Fatalf("requests = %d, want %d", got, 1+rateLimitMaxRetries)
	}
}

func TestRateLimitNeverReplaysPostButWaitsForCooldown(t *testing.T) {
	clock := newFakeClock()
	server := newScripted(map[string][]scripted{"itch.io": {{status: 429, retryAfter: "3"}, {status: 200}}})
	limiter := newTestLimiter(server, clock)
	resp, err := do(t, limiter, context.Background(), http.MethodPost, "https://itch.io/game/file/1")
	if err != nil || resp.StatusCode != http.StatusTooManyRequests || server.Requests("itch.io") != 1 {
		t.Fatalf("POST = %v, %v after %d request(s); want one 429, not replayed", resp, err, server.Requests("itch.io"))
	}
	if _, err := do(t, limiter, context.Background(), http.MethodPost, "https://itch.io/game/file/1"); err != nil {
		t.Fatal(err)
	}
	if got := clock.Slept(); len(got) != 1 || got[0] != 3*time.Second {
		t.Fatalf("slept %v, want the next POST to wait out the 3s cooldown", got)
	}
}

func TestRateLimitFailsFastWhenCooldownOutlastsDeadline(t *testing.T) {
	clock := newFakeClock()
	server := newScripted(nil)
	limiter := newTestLimiter(server, clock)
	limiter.record429("itch.io", "60")
	ctx, cancel := context.WithDeadline(context.Background(), clock.Now().Add(10*time.Second))
	defer cancel()
	_, err := do(t, limiter, ctx, http.MethodGet, "https://itch.io/games/a.xml")
	var limited *RateLimitedError
	if !errors.As(err, &limited) || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want RateLimitedError", err)
	}
	if server.Requests("itch.io") != 0 || len(clock.Slept()) != 0 {
		t.Fatalf("sent %d request(s), slept %v; want neither", server.Requests("itch.io"), clock.Slept())
	}
}

func TestRateLimitRechecksCooldownExtendedWhileWaiting(t *testing.T) {
	clock := newFakeClock()
	server := newScripted(nil)
	limiter := newTestLimiter(server, clock)
	limiter.record429("itch.io", "5")
	clock.onSleep = func(call int) {
		if call == 0 {
			// A concurrent request's 429 pushes the cooldown out.
			limiter.record429("itch.io", "20")
		}
	}
	if _, err := do(t, limiter, context.Background(), http.MethodGet, "https://itch.io/games/a.xml"); err != nil {
		t.Fatal(err)
	}
	if got := clock.Slept(); len(got) != 2 || got[0] != 5*time.Second || got[1] != 15*time.Second {
		t.Fatalf("slept %v, want 5s then the remaining 15s of the extension", got)
	}
}

func TestRateLimitWaitStopsOnCancellation(t *testing.T) {
	limiter := newRateLimitTransport(newScripted(nil))
	limiter.record429("itch.io", "60")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := do(t, limiter, ctx, http.MethodGet, "https://itch.io/games/a.xml")
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cooldown wait ignored cancellation")
	}
}

func TestCooldownBudgetChargesOverlappingWaitsOnce(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	budget := &cooldownBudget{remaining: time.Minute}
	// Three parallel requests wait out the same 40 s window.
	for range 3 {
		if !budget.charge(now, now.Add(40*time.Second)) {
			t.Fatal("overlapping wait refused")
		}
	}
	if budget.remaining != 20*time.Second {
		t.Fatalf("remaining = %v, want 20s", budget.remaining)
	}
	// Extending the window charges only the new part.
	if !budget.charge(now.Add(10*time.Second), now.Add(55*time.Second)) || budget.remaining != 5*time.Second {
		t.Fatalf("remaining = %v, want 5s", budget.remaining)
	}
	// A wait that no longer fits is refused without charging anything.
	if budget.charge(now.Add(60*time.Second), now.Add(70*time.Second)) || budget.remaining != 5*time.Second {
		t.Fatalf("over-budget wait accepted or charged; remaining = %v", budget.remaining)
	}
}

// FetchAllGames stops once rate limiting outlasts its cooldown budget, and the
// refresh reports the typed error instead of partial success.
func TestFetchAllGamesStopsAtTheRefreshCooldownBudget(t *testing.T) {
	clock := newFakeClock()
	server := newScripted(map[string][]scripted{"itch.io": {{status: 429, retryAfter: "60"}}})
	limiter := newTestLimiter(server, clock)
	client := &Client{http: &http.Client{Transport: limiter}, base: "https://itch.io"}

	start := clock.Now()
	_, err := client.FetchAllGames(context.Background(), nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if elapsed := clock.Now().Sub(start); elapsed == 0 || elapsed > refreshCooldownBudget {
		t.Fatalf("waited %v of cooldown, want some but at most %v", elapsed, refreshCooldownBudget)
	}
	if got := server.Requests("itch.io"); got > (1+rateLimitMaxRetries)*feedConcurrency {
		t.Fatalf("sent %d requests while rate limited", got)
	}
}

func TestSafeRequestErrorKeepsRateLimitTypedAndURLFree(t *testing.T) {
	clock := newFakeClock()
	limiter := newTestLimiter(newScripted(nil), clock)
	limiter.record429("cdn.example", "60")
	ctx, cancel := context.WithDeadline(context.Background(), clock.Now().Add(time.Second))
	defer cancel()
	client := &http.Client{Transport: limiter}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://cdn.example/file.zip?X-Amz-Signature=secret", nil)
	_, err := client.Do(req)
	safe := safeRequestError("fetch file", err)
	if !errors.Is(safe, ErrRateLimited) || strings.Contains(safe.Error(), "secret") || strings.Contains(safe.Error(), "cdn.example") {
		t.Fatalf("safe error = %q", safe)
	}
}
