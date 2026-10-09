package itchio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

// recordWakes makes every cooldown wait record its wake time and give up, so
// several waiters can be compared at one instant.
func recordWakes(limiter *rateLimitTransport) *[]time.Time {
	var wakes []time.Time
	limiter.sleepUntil = func(_ context.Context, wake time.Time) error {
		wakes = append(wakes, wake)
		return context.Canceled
	}
	return &wakes
}

// Each waiter adds its own jitter after the shared cooldown, so requests
// queued on one host do not all wake at the same instant.
func TestRateLimitJitterIsPerWaiter(t *testing.T) {
	clock := newFakeClock()
	limiter := newTestLimiter(newScripted(nil), clock)
	calls := 0
	limiter.jitter = func(time.Duration) time.Duration {
		calls++
		return time.Duration(calls) * 100 * time.Millisecond
	}
	limiter.record429("itch.io", "")
	until := limiter.hosts["itch.io"].notBefore
	if delay := until.Sub(clock.Now()); delay != rateLimitBaseDelay {
		t.Fatalf("shared cooldown = %v, want %v without jitter", delay, rateLimitBaseDelay)
	}
	wakes := recordWakes(limiter)
	for range 2 {
		limiter.waitTurn(context.Background(), "itch.io")
	}
	if len(*wakes) != 2 || !(*wakes)[0].After(until) || !(*wakes)[1].After((*wakes)[0]) {
		t.Fatalf("wakes %v after a cooldown until %v; want two distinct times after it", *wakes, until)
	}
}

func TestRateLimitJitterStaysWithinAFifthOfTheWait(t *testing.T) {
	limiter := newRateLimitTransport(newScripted(nil))
	now := time.Now()
	limiter.now = func() time.Time { return now }
	limiter.record429("itch.io", "60")
	until := limiter.hosts["itch.io"].notBefore
	wakes := recordWakes(limiter)
	for range 50 {
		limiter.waitTurn(context.Background(), "itch.io")
	}
	for _, wake := range *wakes {
		if wake.Before(until) || wake.Sub(until) > rateLimitMaxDelay/5 {
			t.Fatalf("wake %v after the cooldown, want within a fifth of %v", wake.Sub(until), rateLimitMaxDelay)
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

// Retry-After: 0, or a date already past, still pauses for the base delay,
// so the replays do not fire back to back.
func TestRateLimitRetryAfterZeroOrPastStillWaits(t *testing.T) {
	for _, form := range []string{"seconds", "date"} {
		t.Run(form, func(t *testing.T) {
			clock := newFakeClock()
			value := "0"
			if form == "date" {
				value = clock.Now().Add(-time.Minute).Format(http.TimeFormat)
			}
			server := newScripted(map[string][]scripted{"itch.io": {{status: 429, retryAfter: value}, {status: 200}}})
			limiter := newTestLimiter(server, clock)
			if resp, err := do(t, limiter, context.Background(), http.MethodGet, "https://itch.io/games/a.xml"); err != nil || resp.StatusCode != 200 {
				t.Fatalf("request = %v, %v", resp, err)
			}
			if got := clock.Slept(); len(got) != 1 || got[0] != rateLimitBaseDelay {
				t.Fatalf("slept %v, want one %v pause before the replay", got, rateLimitBaseDelay)
			}
		})
	}
}

// The transport owns 429 retries; the feed loop must not multiply them, and
// the refresh fails with the typed error so the caller keeps its cache.
func TestFetchAllGames_RateLimitFailsTypedWithoutFeedRetries(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/games/tag-homebrew/tag-psx.xml" {
			w.Write([]byte(`<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`))
			return
		}
		requests.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := newClockedClient(srv, newFakeClock()).FetchAllGames(context.Background(), nil)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("rate-limited feed requests = %d, want 1 plus 3 transport retries", got)
	}
}

// Only itch.io's own hosts are replayed after a 429; CDNs and look-alike
// names are not.
func TestRateLimitReplaysOnlyItchHosts(t *testing.T) {
	limiter := newRateLimitTransport(newScripted(nil), "127.0.0.1:8080")
	for host, want := range map[string]bool{
		"itch.io": true, "api.itch.io": true, "someone.itch.io": true, "ITCH.IO:443": true,
		"127.0.0.1:8080": true, "127.0.0.1:9090": false,
		"cdn.example": false, "img.itch.zone": false, "evilitch.io": false, "itch.io.example": false,
	} {
		if got := limiter.replays(host); got != want {
			t.Errorf("replays(%q) = %v, want %v", host, got, want)
		}
	}
}

// A refresh that stays rate limited past its cooldown budget stops, but the
// feeds that finished before it stopped are kept: their systems take the new
// games, while the rate-limited feed's system and a feed still in flight keep
// their cached games.
func TestFetchAllGamesKeepsFeedsFinishedBeforeTheRateLimitStop(t *testing.T) {
	var p8Page strings.Builder
	for index := range PerPage {
		fmt.Fprintf(&p8Page, "<item><title>P8 %d</title><link>https://dev.itch.io/p8-%d</link><price>0</price></item>", index, index)
	}
	othersDone := make(chan struct{})
	var closeOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch r.URL.Path {
		case "/games/tag-pico-8.xml":
			if page == "1" {
				fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel>%s</channel></rss>`, p8Page.String())
				return
			}
			// Page 2 is rate limited once every other finishing feed merged.
			<-othersDone
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
		case "/games/tag-genesis-rom.xml":
			<-r.Context().Done() // still in flight when the refresh stops
		default:
			slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/games/"), ".xml")
			if page != "1" {
				slug = "none"
			}
			item := ""
			if slug != "none" {
				item = fmt.Sprintf("<item><title>%s</title><link>https://dev.itch.io/%s</link><price>0</price></item>",
					slug, strings.ReplaceAll(slug, "/", "-"))
			}
			fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel>%s</channel></rss>`, item)
		}
	}))
	defer srv.Close()

	// Every feed but Pico-8 and tag-genesis-rom finishes with one game.
	finishing := len(catalogFeeds()) - 2
	fetch, err := newClockedClient(srv, newFakeClock()).FetchAllGames(context.Background(), func(partial []Game) {
		if len(partial) == finishing {
			closeOnce.Do(func() { close(othersDone) })
		}
	})
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	for _, feed := range fetch.Feeds {
		stopped := feed.Slug == "tag-pico-8" || feed.Slug == "tag-genesis-rom"
		if (feed.Err != nil) != stopped {
			t.Errorf("feed %s error = %v", feed.Slug, feed.Err)
		}
		if !stopped && len(feed.Games) != 1 {
			t.Errorf("feed %s kept %d games, want 1", feed.Slug, len(feed.Games))
		}
	}
	previous := []Game{
		{Title: "Old P8", URL: "https://dev.itch.io/old-p8", Platform: "P8"},
		{Title: "Old MD", URL: "https://dev.itch.io/old-md", Platform: "MD"},
		{Title: "Old NES", URL: "https://dev.itch.io/old-nes", Platform: "NES"},
	}
	merge := fetch.Merge(previous)
	if got := strings.Join(merge.Kept, " "); got != "MD P8" {
		t.Errorf("systems that kept their cached games = %s, want MD P8", got)
	}
	var urls []string
	for _, game := range merge.Games {
		urls = append(urls, game.Platform+":"+strings.TrimPrefix(game.URL, "https://dev.itch.io/"))
	}
	want := "PSX:tag-homebrew-tag-psx GBC:tag-gameboy-color GBC:tag-gbc GB:made-with-gb-studio GB:tag-gbstudio " +
		"GB:tag-gameboy-rom GBA:tag-gameboy-advance NES:tag-nes-rom MD:old-md P8:old-p8"
	if got := strings.Join(urls, " "); got != want {
		t.Errorf("merged games = %s\nwant %s", got, want)
	}
}
