package itchio

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

const (
	feedMaxRetries = 3
	feedRetryDelay = 2 * time.Second
)

type Game struct {
	Title       string    `json:"title"`
	Author      string    `json:"author"`
	URL         string    `json:"url"`
	CoverURL    string    `json:"cover_url"`
	Price       float64   `json:"price"`
	IsFree      bool      `json:"is_free"`
	Tags        []string  `json:"tags,omitempty"`     // extracted from [Tag] brackets in the RSS title
	PublishedAt time.Time `json:"published_at"`       // parsed from <pubDate> in RSS feed
	Platform    string    `json:"platform,omitempty"` // Leaf system code set by FetchAllGames, e.g. "GB"
}

var (
	coverRegex = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	tagRegex   = regexp.MustCompile(`\s*\[([^\]]+)\]`)
)

// parseTitle strips [Tag] brackets from the raw RSS title.
func parseTitle(raw string) string {
	return strings.TrimSpace(tagRegex.ReplaceAllString(raw, ""))
}

// parseTags extracts the contents of every [Tag] bracket in the raw RSS title.
func parseTags(raw string) []string {
	matches := tagRegex.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return nil
	}
	tags := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) > 1 && m[1] != "" {
			tags = append(tags, m[1])
		}
	}
	return tags
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	ImageURL    string `xml:"imageurl"`
	Price       string `xml:"price"`
	PubDate     string `xml:"pubDate"`
}

type rssFeed struct {
	Items []rssItem `xml:"channel>item"`
}

type metadataHTTPError struct {
	operation string
	status    int
}

func (err *metadataHTTPError) Error() string {
	return fmt.Sprintf("%s: HTTP %d", err.operation, err.status)
}

// retryableMetadataError reports whether the feed loop should retry. Rate
// limiting is not retried here: the transport already waited out and replayed
// the 429s it could, so another layer of retries would multiply them.
func retryableMetadataError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrCloudflareBlocked) || errors.Is(err, ErrRateLimited) {
		return false
	}
	var statusErr *metadataHTTPError
	if errors.As(err, &statusErr) {
		switch statusErr.status {
		case http.StatusRequestTimeout, http.StatusTooEarly,
			http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	var networkErr net.Error
	return errors.As(err, &networkErr)
}

// feedPastEnd reports whether err is the 404/410 itch.io can answer for a page
// past the end of a feed.
func feedPastEnd(err error) bool {
	var statusErr *metadataHTTPError
	return errors.As(err, &statusErr) &&
		(statusErr.status == http.StatusNotFound || statusErr.status == http.StatusGone)
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SlugToTitle derives a display title from the URL slug when the RSS title has
// nothing to draw (see hasDisplayableChar). It extracts the path segment after
// ".itch.io/", splits on hyphens and underscores, and capitalises the first
// letter of each word.
func SlugToTitle(gameURL string) string {
	s := gameURL
	if idx := strings.Index(s, ".itch.io/"); idx >= 0 {
		s = s[idx+len(".itch.io/"):]
	}
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	words := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// hasDisplayableChar reports whether s has something to draw: a letter,
// mark, number, punctuation or symbol, emoji included. Whitespace and
// control, format, private-use and unassigned code points draw nothing, and
// U+FFFD only stands in for bytes that failed to decode. This goes by Unicode
// category, not font coverage; the bundled fonts include an emoji fallback.
func hasDisplayableChar(s string) bool {
	for _, r := range s {
		if r != utf8.RuneError && unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S) {
			return true
		}
	}
	return false
}

// slugFallbackLog remembers which items one fetch has already logged as
// using the slug fallback. A refresh parses the same item many times: games
// are listed in several feeds, and past the last page itch.io repeats it.
type slugFallbackLog struct {
	mu     sync.Mutex
	logged map[string]bool
}

type slugFallbackLogKey struct{}

func withSlugFallbackLog(ctx context.Context) context.Context {
	return context.WithValue(ctx, slugFallbackLogKey{}, &slugFallbackLog{logged: make(map[string]bool)})
}

func slugFallbackLogFrom(ctx context.Context) *slugFallbackLog {
	fallbacks, _ := ctx.Value(slugFallbackLogKey{}).(*slugFallbackLog)
	return fallbacks
}

// logSlugFallback logs an item's slug fallback once per fetch, at debug
// level: an unreadable title is the developer's choice, not an app fault.
func logSlugFallback(ctx context.Context, gameURL, rawTitle, fallback string) {
	if fallbacks := slugFallbackLogFrom(ctx); fallbacks != nil {
		fallbacks.mu.Lock()
		logged := fallbacks.logged[gameURL]
		fallbacks.logged[gameURL] = true
		fallbacks.mu.Unlock()
		if logged {
			return
		}
	}
	logger.Debug("feed: item %s has no readable title %q, using slug fallback %q", gameURL, rawTitle, fallback)
}

func parseAuthor(gameURL string) string {
	// https://{author}.itch.io/{game}
	s := strings.TrimPrefix(gameURL, "https://")
	s = strings.TrimPrefix(s, "http://")
	if idx := strings.Index(s, ".itch.io"); idx > 0 {
		return s[:idx]
	}
	return ""
}

func parseCover(imageURL, desc string) string {
	if imageURL != "" {
		return imageURL
	}
	m := coverRegex.FindStringSubmatch(desc)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func parsePrice(raw string) float64 {
	s := strings.TrimSpace(raw)
	s = strings.Trim(s, "$€£¥")
	s = strings.TrimSpace(s)
	price, _ := strconv.ParseFloat(s, 64)
	return price
}

func parsePubDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC1123, time.RFC1123Z} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	logger.Warn("feed: unrecognised pubDate format %q, treating as undated", raw)
	return time.Time{}
}

func (c *Client) FetchGamesFromURL(url string) ([]Game, error) {
	return c.FetchGamesFromURLContext(context.Background(), url)
}

// FetchGamesFromURLContext fetches idempotent catalogue metadata. Only
// transient transport/server failures are retried, and both requests and retry
// waits stop immediately when ctx is cancelled.
func (c *Client) FetchGamesFromURLContext(ctx context.Context, url string) ([]Game, error) {
	if slugFallbackLogFrom(ctx) == nil {
		ctx = withSlugFallbackLog(ctx)
	}
	var lastErr error
	for attempt := 0; attempt <= feedMaxRetries; attempt++ {
		if attempt > 0 {
			logger.Warn("feed: retry %d/%d after %v (last error: %v)", attempt, feedMaxRetries, feedRetryDelay, lastErr)
			if err := waitForRetry(ctx, feedRetryDelay); err != nil {
				return nil, err
			}
		}
		games, err := c.fetchGamesFromURLOnce(ctx, url)
		if err == nil {
			return games, nil
		}
		lastErr = err
		if !retryableMetadataError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) fetchGamesFromURLOnce(ctx context.Context, url string) ([]Game, error) {
	logger.Debug("feed: fetching %s", url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build feed request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch feed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		logger.Error("feed: HTTP 403 from %s (Cloudflare bot-protection)", url)
		return nil, ErrCloudflareBlocked
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		logger.Error("feed: HTTP 429 from %s after rate-limit retries", url)
		return nil, fmt.Errorf("fetch feed: %w", &RateLimitedError{Host: req.URL.Host})
	}
	if resp.StatusCode != http.StatusOK {
		logger.Error("feed: HTTP %d from %s", resp.StatusCode, url)
		return nil, &metadataHTTPError{operation: "fetch feed", status: resp.StatusCode}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read feed: %w", err)
	}

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		logger.Error("feed: parse XML: %v", err)
		logger.Debug("feed: response body (first 512 bytes): %.512s", body)
		return nil, fmt.Errorf("parse feed xml: %w", err)
	}
	logger.Debug("feed: parsed %d items from XML", len(feed.Items))

	games := make([]Game, 0, len(feed.Items))
	for _, item := range feed.Items {
		price := parsePrice(item.Price)
		title := parseTitle(item.Title)
		if !hasDisplayableChar(title) {
			fallback := SlugToTitle(item.Link)
			logSlugFallback(ctx, item.Link, title, fallback)
			title = fallback
		}
		games = append(games, Game{
			Title:       title,
			Tags:        parseTags(item.Title),
			Author:      parseAuthor(item.Link),
			URL:         item.Link,
			CoverURL:    parseCover(item.ImageURL, item.Description),
			Price:       price,
			IsFree:      price == 0,
			PublishedAt: parsePubDate(item.PubDate),
		})
	}
	return games, nil
}

const PerPage = 36 // itch.io XML feeds return 36 items per page

// FetchGames fetches one page of the GB Studio feed. It is used as a quick
// live-feed preview when no local cache exists yet; the full multi-platform
// catalogue is built by FetchAllGames. The browse feeds ignore a q= search
// parameter, so searching always filters locally.
func (c *Client) FetchGames(page int) ([]Game, error) {
	return c.FetchGamesContext(context.Background(), page)
}

func (c *Client) FetchGamesContext(ctx context.Context, page int) ([]Game, error) {
	feedURL := fmt.Sprintf("%s/games/made-with-gb-studio.xml?page=%d", c.base, page)
	return c.FetchGamesFromURLContext(ctx, feedURL)
}

// feedConcurrency is the maximum number of feed slugs fetched in parallel.
// All requests share the same HTTP/2 connection to itch.io, so this caps
// the number of concurrent in-flight page requests rather than connections.
const feedConcurrency = 3

// newestQuietPages is how many pages in a row without a game the cache lacks
// end a feed in the daily check (D1). The newest-first feeds are mostly, not
// strictly, in date order, so one page of known games is not enough.
const newestQuietPages = 2

// FeedFetch is one feed slug's part of a catalogue refresh.
type FeedFetch struct {
	Platform string // Leaf system code, e.g. "GB"
	Slug     string // itch.io browse path, e.g. "tag-gbstudio"
	// Games are the feed's games in feed order, Platform set; in a daily
	// check only those the cache lacked. Incomplete when Err is set.
	Games []Game
	Pages int   // feed pages read
	Err   error // why the feed did not finish; nil when it was read to its end
}

// CatalogFetch is a catalogue refresh feed by feed, so a caller can keep what
// finished when some feeds failed.
type CatalogFetch struct {
	Feeds []FeedFetch // one per feed slug, in AllPlatforms order
	// Incremental marks a daily check (FetchNewGames): the feeds hold only
	// new games, which Merge adds to the cached ones.
	Incremental bool
	RateLimited int           // HTTP 429 answers, replays included
	Duration    time.Duration // from the first request to the last result
}

// catalogFeeds lists every (platform, slug) pair of AllPlatforms, in order.
func catalogFeeds() []FeedFetch {
	var feeds []FeedFetch
	for _, platform := range AllPlatforms {
		for _, slug := range platform.FeedSlugs {
			feeds = append(feeds, FeedFetch{Platform: platform.Code, Slug: slug})
		}
	}
	return feeds
}

// Failed lists the feeds that did not finish.
func (fetch *CatalogFetch) Failed() []FeedFetch {
	var failed []FeedFetch
	for _, feed := range fetch.Feeds {
		if feed.Err != nil {
			failed = append(failed, feed)
		}
	}
	return failed
}

// Err joins the errors of the feeds that did not finish; nil when every feed
// finished.
func (fetch *CatalogFetch) Err() error {
	var errs []error
	for _, feed := range fetch.Failed() {
		errs = append(errs, feed.Err)
	}
	return errors.Join(errs...)
}

// Pages is the number of feed pages the refresh read.
func (fetch *CatalogFetch) Pages() int {
	pages := 0
	for _, feed := range fetch.Feeds {
		pages += feed.Pages
	}
	return pages
}

// Games returns the games of the systems whose feeds all finished,
// deduplicated as Merge does.
func (fetch *CatalogFetch) Games() []Game { return fetch.Merge(nil).Games }

// CatalogMerge is a refresh applied to the previous catalogue.
type CatalogMerge struct {
	Games   []Game
	Updated []string // systems whose feeds all finished, which took the new games
	Kept    []string // systems with a feed that did not finish, which kept their previous games
}

// Merge applies a refresh to the previous catalogue, system by system. A
// system with a feed that did not finish keeps its games from previous, and
// what its other feeds returned is dropped. A system whose feeds all finished
// takes the games they returned: after a full crawl, those replace its
// previous games; after a daily check (Incremental), they go on top of its
// previous games, newest first, and every previous game stays as it was.
// Systems follow AllPlatforms order and games are deduplicated by URL, so a
// game listed in several feeds keeps the first system's code: the more
// specific feeds come first.
func (fetch *CatalogFetch) Merge(previous []Game) CatalogMerge {
	fetched := make(map[string]bool)
	failed := make(map[string]bool)
	for _, feed := range fetch.Feeds {
		fetched[feed.Platform] = true
		if feed.Err != nil {
			failed[feed.Platform] = true
		}
	}
	var merge CatalogMerge
	seen := make(map[string]bool)
	if fetch.Incremental {
		// New games are new to the whole cache, never only to their system.
		for _, game := range previous {
			seen[game.URL] = true
		}
	}
	add := func(game Game) {
		if !seen[game.URL] {
			seen[game.URL] = true
			merge.Games = append(merge.Games, game)
		}
	}
	addPrevious := func(platform string) {
		for _, game := range previous {
			switch {
			case game.Platform != platform:
			case fetch.Incremental:
				// Already in seen, so new games never repeat it.
				merge.Games = append(merge.Games, game)
			default:
				add(game)
			}
		}
	}
	for _, platform := range AllPlatforms {
		if !fetched[platform.Code] || failed[platform.Code] {
			merge.Kept = append(merge.Kept, platform.Code)
			addPrevious(platform.Code)
			continue
		}
		merge.Updated = append(merge.Updated, platform.Code)
		first := len(merge.Games)
		for _, feed := range fetch.Feeds {
			if feed.Platform == platform.Code {
				for _, game := range feed.Games {
					add(game)
				}
			}
		}
		if fetch.Incremental {
			// D3: newest first on top of the system, until the next full
			// crawl restores itch.io's order.
			added := merge.Games[first:]
			sort.SliceStable(added, func(i, j int) bool { return added[i].PublishedAt.After(added[j].PublishedAt) })
			addPrevious(platform.Code)
		}
	}
	if fetch.Incremental {
		// A daily check changes no previous entry, whatever its system.
		known := make(map[string]bool, len(AllPlatforms))
		for _, platform := range AllPlatforms {
			known[platform.Code] = true
		}
		for _, game := range previous {
			if !known[game.Platform] {
				merge.Games = append(merge.Games, game)
			}
		}
	}
	return merge
}

// pageReader reads one feed of a refresh. ping is called after each page
// that found a game.
type pageReader func(ctx context.Context, feed *FeedFetch, ping func())

// fetchSlug reads every page of one feed slug into feed: its games and the
// pages read, or the error that stopped it. It maintains its own seen-URL set
// purely for within-slug wrap-around detection (itch.io repeats the last real
// page indefinitely past the end). ping is called after each page that adds
// at least one game.
func (c *Client) fetchSlug(ctx context.Context, feed *FeedFetch, ping func()) {
	platformCode, slug := feed.Platform, feed.Slug
	logger.Info("feed: fetching platform=%s slug=%s", platformCode, slug)
	localSeen := make(map[string]bool)
	for page := 1; ; page++ {
		select {
		case <-ctx.Done():
			feed.Err = ctx.Err()
			return
		default:
		}
		url := fmt.Sprintf("%s/games/%s.xml?page=%d", c.base, slug, page)
		pageGames, err := c.FetchGamesFromURLContext(ctx, url)
		if err != nil && page > 1 && feedPastEnd(err) {
			// A missing later page ends the feed; a missing first page is
			// still an error.
			logger.Info("feed: platform=%s slug=%s page=%d: %v, treating as end of feed", platformCode, slug, page, err)
			return
		}
		if err != nil {
			logger.Warn("feed: platform=%s slug=%s page=%d error: %v", platformCode, slug, page, err)
			feed.Err = fmt.Errorf("platform=%s slug=%s page %d: %w", platformCode, slug, page, err)
			return
		}
		feed.Pages++
		added := 0
		for i := range pageGames {
			if !localSeen[pageGames[i].URL] {
				localSeen[pageGames[i].URL] = true
				pageGames[i].Platform = platformCode
				feed.Games = append(feed.Games, pageGames[i])
				added++
			}
		}
		logger.Debug("feed: platform=%s slug=%s page=%d: %d new, %d deduped", platformCode, slug, page, added, len(pageGames)-added)
		if added > 0 {
			ping()
		}
		if len(pageGames) < PerPage {
			return
		}
		if added == 0 {
			logger.Debug("feed: platform=%s slug=%s page=%d: full page all-duplicates, stopping (itch.io wrap-around)", platformCode, slug, page)
			return
		}
	}
}

// fetchNewSlug reads the newest-first form of one feed slug,
// games/newest/<slug>.xml, into feed: the games known does not know, newest
// first, and the pages read. It stops after newestQuietPages pages in a row
// without such a game, at a short or missing later page, and when itch.io
// repeats a page past the end.
func (c *Client) fetchNewSlug(ctx context.Context, feed *FeedFetch, known func(url string) bool, ping func()) {
	platformCode, slug := feed.Platform, feed.Slug
	localSeen := make(map[string]bool)
	quiet := 0
	for page := 1; ; page++ {
		select {
		case <-ctx.Done():
			feed.Err = ctx.Err()
			return
		default:
		}
		url := fmt.Sprintf("%s/games/newest/%s.xml?page=%d", c.base, slug, page)
		pageGames, err := c.FetchGamesFromURLContext(ctx, url)
		if err != nil && page > 1 && feedPastEnd(err) {
			logger.Debug("feed: newest platform=%s slug=%s page=%d: %v, treating as end of feed", platformCode, slug, page, err)
			return
		}
		if err != nil {
			logger.Warn("feed: newest platform=%s slug=%s page=%d error: %v", platformCode, slug, page, err)
			feed.Err = fmt.Errorf("platform=%s slug=%s newest page %d: %w", platformCode, slug, page, err)
			return
		}
		feed.Pages++
		unseen, found := 0, 0
		for i := range pageGames {
			game := pageGames[i]
			if localSeen[game.URL] {
				continue
			}
			localSeen[game.URL] = true
			unseen++
			if known(game.URL) {
				continue
			}
			game.Platform = platformCode
			feed.Games = append(feed.Games, game)
			found++
		}
		logger.Debug("feed: newest platform=%s slug=%s page=%d: %d new game(s)", platformCode, slug, page, found)
		if found > 0 {
			ping()
			quiet = 0
		} else {
			quiet++
		}
		switch {
		case len(pageGames) < PerPage:
			return
		case unseen == 0:
			logger.Debug("feed: newest platform=%s slug=%s page=%d: full page all-duplicates, stopping (itch.io wrap-around)", platformCode, slug, page)
			return
		case quiet >= newestQuietPages:
			return
		}
	}
}

// FetchAllGames fetches every page of every platform feed in AllPlatforms in
// parallel (up to feedConcurrency slugs at a time) and reports each feed's
// games and error; CatalogFetch.Merge combines them. A failing feed does not
// stop the others. Rate limiting is the exception: the first feed that fails
// with ErrRateLimited stops the whole refresh, and all feeds together wait out
// at most refreshCooldownBudget of cooldown. The feeds that finished before
// the stop are still reported. The error is the rate limit or ctx's error
// when the refresh stopped, else the failed feeds' errors joined, else nil.
//
// progress is called with the games of the feeds finished so far,
// deduplicated by URL in the order the feeds finished, after each feed
// finishes and while feeds are paging, but never before the first game is
// merged.
func (c *Client) FetchAllGames(ctx context.Context, progress func(partial []Game)) (*CatalogFetch, error) {
	return c.fetchCatalog(ctx, false, progress, c.fetchSlug)
}

// FetchNewGames is the daily check: it reads the newest-first form of every
// feed, games/newest/<slug>.xml, and reports per feed the games known does not
// know. A feed stops after newestQuietPages pages in a row without one, at a
// short or missing later page, or when itch.io repeats a page past the end. It
// shares FetchAllGames' concurrency, rate limiter, cooldown budget, stop rule
// and error. known is called from several goroutines at once.
func (c *Client) FetchNewGames(ctx context.Context, known func(url string) bool) (*CatalogFetch, error) {
	return c.fetchCatalog(ctx, true, nil, func(ctx context.Context, feed *FeedFetch, ping func()) {
		c.fetchNewSlug(ctx, feed, known, ping)
	})
}

// fetchCatalog reads every feed with read, feedConcurrency at a time, for
// FetchAllGames and FetchNewGames.
func (c *Client) fetchCatalog(ctx context.Context, incremental bool, progress func(partial []Game), read pageReader) (*CatalogFetch, error) {
	fetch := &CatalogFetch{Feeds: catalogFeeds(), Incremental: incremental}
	if err := ctx.Err(); err != nil {
		for index := range fetch.Feeds {
			fetch.Feeds[index].Err = err
		}
		return fetch, err
	}
	start := time.Now()
	stats := &refreshStats{}
	runCtx, cancel := context.WithCancel(withRefreshStats(withSlugFallbackLog(withCooldownBudget(ctx, refreshCooldownBudget)), stats))
	defer cancel()

	type feedDone struct {
		index int
		feed  FeedFetch
	}
	resultCh := make(chan feedDone, len(fetch.Feeds))
	// pingCh carries per-page notifications from goroutines so the collect loop
	// can fire progress(all) more frequently than once per completed slug.
	// Capacity = len(feeds)*2 to avoid blocking goroutines on a slow main loop.
	pingCh := make(chan struct{}, len(fetch.Feeds)*2)
	ping := func() {
		select {
		case pingCh <- struct{}{}:
		default:
		}
	}
	sem := make(chan struct{}, feedConcurrency)

	for index, feed := range fetch.Feeds {
		go func() {
			// Acquire semaphore slot, or abort if the refresh stopped.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-runCtx.Done():
				feed.Err = runCtx.Err()
				resultCh <- feedDone{index: index, feed: feed}
				return
			}
			read(runCtx, &feed, ping)
			resultCh <- feedDone{index: index, feed: feed}
		}()
	}

	// Collect every feed's result; merge the finished ones sequentially (no
	// mutex needed) for progress. Pings from in-flight goroutines also fire
	// progress so the caller sees live updates during long slug fetches (e.g.
	// the large P8 feed). After a stop, the remaining feeds end promptly with
	// the cancelled context.
	seen := make(map[string]bool)
	var all []Game
	var stopped error
	for remaining := len(fetch.Feeds); remaining > 0; {
		select {
		case <-pingCh:
			// A goroutine finished a page — fire a live-count progress update
			// using whatever has been merged so far. Drain all pending pings to
			// avoid a flood of identical callbacks.
			for len(pingCh) > 0 {
				<-pingCh
			}
			// Games join all only when their slug finishes. Until then there
			// is nothing to report, and an empty snapshot would look like an
			// empty catalogue.
			if progress != nil && len(all) > 0 {
				progress(all)
			}
		case done := <-resultCh:
			remaining--
			fetch.Feeds[done.index] = done.feed
			feed := &fetch.Feeds[done.index]
			if feed.Err != nil {
				if stopped == nil && errors.Is(feed.Err, ErrRateLimited) {
					logger.Warn("feed: platform=%s slug=%s stays rate limited; stopping the refresh", feed.Platform, feed.Slug)
					stopped = feed.Err
					cancel()
				}
				continue
			}
			added := 0
			for _, g := range feed.Games {
				if !seen[g.URL] {
					seen[g.URL] = true
					all = append(all, g)
					added++
				}
			}
			logger.Debug("feed: platform=%s merged %d game(s) (%d cross-platform deduped)", feed.Platform, added, len(feed.Games)-added)
			if added > 0 && progress != nil {
				progress(all)
			}
		}
	}
	fetch.RateLimited = int(stats.rateLimited.Load())
	fetch.Duration = time.Since(start)
	if stopped == nil {
		stopped = ctx.Err()
	}
	if stopped == nil {
		return fetch, fetch.Err()
	}
	// Name the stop as the reason for the feeds it cut short.
	for index := range fetch.Feeds {
		feed := &fetch.Feeds[index]
		if feed.Err != nil && errors.Is(feed.Err, context.Canceled) && !errors.Is(stopped, context.Canceled) {
			feed.Err = fmt.Errorf("platform=%s slug=%s not finished, the refresh stopped: %w", feed.Platform, feed.Slug, stopped)
		}
	}
	return fetch, stopped
}

var resultCountRegex = regexp.MustCompile(`(?i)(\d[\d,]*)\s+result`)

// FetchTotalGames scrapes the HTML browse page to find the total result count.
func (c *Client) FetchTotalGames() (int, error) {
	logger.Debug("feed: fetching total games count")
	resp, err := c.http.Get("https://itch.io/games/made-with-gb-studio")
	if err != nil {
		return 0, fmt.Errorf("fetch browse page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		logger.Error("feed: total-games HTTP 403 (Cloudflare bot-protection)")
		return 0, fmt.Errorf("fetch total games: %w", ErrCloudflareBlocked)
	}
	if resp.StatusCode != http.StatusOK {
		logger.Error("feed: total-games HTTP %d", resp.StatusCode)
		return 0, fmt.Errorf("fetch total games: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read browse page: %w", err)
	}
	m := resultCountRegex.FindStringSubmatch(string(body))
	if len(m) < 2 {
		logger.Warn("feed: result count not found on browse page")
		return 0, fmt.Errorf("result count not found on browse page")
	}
	countStr := strings.ReplaceAll(m[1], ",", "")
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return 0, fmt.Errorf("parse result count: %w", err)
	}
	return count, nil
}
