package catui

import (
	"bytes"
	"container/list"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/media"
)

func TestImageCacheSchedulesOnlyVisibleAnimations(t *testing.T) {
	cache := NewImageCache(2, nil)
	animation := &animatedTexture{
		frames: []*Texture{{}, {}},
		delays: []time.Duration{100 * time.Millisecond, 100 * time.Millisecond},
		nextAt: time.Now().Add(100 * time.Millisecond),
	}
	entry := &catImageEntry{key: "animated", animation: animation}
	cache.items[entry.key] = cache.lru.PushFront(entry)

	if _, scheduled := cache.NextFrameIn(); scheduled {
		t.Fatal("off-screen animation scheduled a frame")
	}
	if got := cache.Peek(entry.key); got == nil {
		t.Fatal("visible animation did not return its current texture")
	}
	if _, scheduled := cache.NextFrameIn(); !scheduled {
		t.Fatal("visible animation did not schedule its next frame")
	}
	cache.BeginFrame()
	if _, scheduled := cache.NextFrameIn(); scheduled {
		t.Fatal("animation remained active after BeginFrame")
	}

	// Prevent the test-only fake textures from reaching Clear/Destroy.
	cache.items = make(map[string]*list.Element)
	cache.lru.Init()
}

func TestImageCacheRapidGIFPagingStaysWithinFrameBudget(t *testing.T) {
	cache := NewImageCache(50, nil)
	const (
		frameBudget = 96
		gifFrames   = 16
	)

	for page := 0; page < 40; page++ {
		cache.mu.Lock()
		if !cache.reserveLocked(gifFrames, frameBudget) {
			cache.mu.Unlock()
			t.Fatalf("page %d could not reserve one GIF", page)
		}
		entry := &catImageEntry{
			key:       strconv.Itoa(page),
			animation: fakeAnimation(gifFrames),
		}
		cache.items[entry.key] = cache.lru.PushFront(entry)
		cache.frameCount += gifFrames
		if cache.frameCount > frameBudget {
			cache.mu.Unlock()
			t.Fatalf("page %d exceeded texture budget: got %d, want <= %d", page, cache.frameCount, frameBudget)
		}
		cache.mu.Unlock()
	}

	if got, want := cache.lru.Len(), frameBudget/gifFrames; got != want {
		t.Fatalf("cached GIF count = %d, want %d", got, want)
	}
	cache.Clear()
	if cache.frameCount != 0 {
		t.Fatalf("frame count after clear = %d, want 0", cache.frameCount)
	}
}

func TestImageCacheUploadFailureIsRecoverable(t *testing.T) {
	cache := NewImageCache(1, nil)
	cache.ready <- decodedImage{
		key: "broken-cover",
		image: &media.DecodedImage{
			Frames: []*image.RGBA{image.NewRGBA(image.Rect(0, 0, 1, 1))},
		},
	}

	uploaded, err := cache.ProcessPending(nil)
	if err != nil {
		t.Fatalf("ProcessPending returned a fatal artwork error: %v", err)
	}
	if uploaded {
		t.Fatal("failed artwork was reported as uploaded")
	}
	if !cache.Failed("broken-cover") {
		t.Fatal("failed artwork was not suppressed after the recoverable error")
	}
}

func TestImageCacheSuppressesPermanentFetchFailures(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "declared byte cap",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Length", strconv.Itoa(media.MaxSourceBytes+1))
				response.WriteHeader(http.StatusOK)
			},
		},
		{
			name: "invalid encoding",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte("not an image"))
			},
		},
		{
			name: "non-retryable HTTP",
			handler: func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusNotFound)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				test.handler(response, request)
			}))
			defer server.Close()

			cache := NewImageCache(1, server.Client())
			cache.fetch(server.URL)
			if !cache.Failed(server.URL) {
				t.Fatal("permanent failure was not suppressed")
			}
			cache.Warm(server.URL)
			cache.mu.Lock()
			_, fetching := cache.fetching[server.URL]
			cache.mu.Unlock()
			if fetching {
				t.Fatal("permanently failed URL scheduled another fetch")
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("requests = %d, want 1", got)
			}
		})
	}
}

func TestImageCacheRetriesTransientFailureAfterBackoff(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write(encoded.Bytes())
	}))
	defer server.Close()

	now := time.Unix(1000, 0)
	cache := NewImageCache(1, server.Client())
	cache.now = func() time.Time { return now }
	cache.fetch(server.URL)
	if cache.Failed(server.URL) {
		t.Fatal("transient HTTP failure was marked permanent")
	}
	if got := cache.retries[server.URL].nextAt.Sub(now); got != time.Second {
		t.Fatalf("first retry delay = %v, want 1s", got)
	}

	cache.Warm(server.URL)
	cache.mu.Lock()
	_, fetching := cache.fetching[server.URL]
	cache.mu.Unlock()
	if fetching || requests.Load() != 1 {
		t.Fatal("fetch was scheduled before its retry deadline")
	}

	now = now.Add(time.Second)
	cache.Warm(server.URL)
	select {
	case decoded := <-cache.ready:
		if decoded.image == nil {
			t.Fatal("successful retry returned a nil image")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for successful retry")
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	cache.mu.Lock()
	_, retrying := cache.retries[server.URL]
	cache.mu.Unlock()
	if retrying {
		t.Fatal("successful retry did not clear backoff state")
	}
}

func restTestServer(t *testing.T) (*httptest.Server, *sync.Map) {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	requested := &sync.Map{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requested.Store(request.URL.Path, true)
		_, _ = response.Write(encoded.Bytes())
	}))
	t.Cleanup(server.Close)
	return server, requested
}

func waitReady(t *testing.T, cache *ImageCache) string {
	t.Helper()
	select {
	case decoded := <-cache.ready:
		return decoded.key
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a fetched cover")
		return ""
	}
}

func waitNotFetching(t *testing.T, cache *ImageCache, key string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		cache.mu.Lock()
		_, fetching := cache.fetching[key]
		cache.mu.Unlock()
		if !fetching {
			return
		}
	}
	t.Fatalf("%s stayed in flight", key)
}

// Holding the d-pad passes a game every 75 to 100 ms. Only the cover of the
// game the cursor stops on is fetched.
func TestWarmOnRestFetchesOnlyTheCoverTheCursorStopsOn(t *testing.T) {
	server, requested := restTestServer(t)
	cache := NewImageCache(4, server.Client())
	rest := make(chan time.Time)
	cache.restAfter = func(time.Duration) <-chan time.Time { return rest }
	passed, stopped := server.URL+"/passed.png", server.URL+"/stopped.png"

	cache.WarmOnRest(passed)
	cache.WarmOnRest(stopped)
	close(rest)
	if got := waitReady(t, cache); got != stopped {
		t.Fatalf("fetched %s, want %s", got, stopped)
	}
	waitNotFetching(t, cache, passed)
	if _, ok := requested.Load("/passed.png"); ok {
		t.Fatal("the cover of a game the cursor only passed was fetched")
	}
	if cache.Failed(passed) {
		t.Fatal("a passed cover was marked failed")
	}
}

// A cover skipped while the cursor passed it is fetched when the cursor
// comes back and stays.
func TestWarmOnRestFetchesAPassedCoverWhenTheCursorReturns(t *testing.T) {
	server, _ := restTestServer(t)
	cache := NewImageCache(4, server.Client())
	rest := make(chan time.Time)
	cache.restAfter = func(time.Duration) <-chan time.Time { return rest }
	first, second := server.URL+"/first.png", server.URL+"/second.png"

	cache.WarmOnRest(first)
	cache.WarmOnRest(second)
	close(rest)
	waitReady(t, cache)
	waitNotFetching(t, cache, first)
	cache.WarmOnRest(first)
	if got := waitReady(t, cache); got != first {
		t.Fatalf("fetched %s, want %s", got, first)
	}
}

// Detail screens fetch at once; only the list waits for the cursor to rest.
func TestWarmDoesNotWaitForRest(t *testing.T) {
	server, _ := restTestServer(t)
	cache := NewImageCache(4, server.Client())
	cache.restAfter = func(time.Duration) <-chan time.Time { return make(chan time.Time) }
	cache.Warm(server.URL + "/detail.png")
	waitReady(t, cache)
}

func TestImageCacheRetryDelayCapsAtOneMinute(t *testing.T) {
	now := time.Unix(1000, 0)
	cache := NewImageCache(1, nil)
	cache.now = func() time.Time { return now }
	wants := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second,
	}
	for index, want := range wants {
		cache.mu.Lock()
		cache.recordRetryLocked("cover")
		got := cache.retries["cover"].nextAt.Sub(now)
		cache.mu.Unlock()
		if got != want {
			t.Fatalf("failure %d retry delay = %v, want %v", index+1, got, want)
		}
	}
}

func TestImageCacheClearResetsFailuresAndBackoff(t *testing.T) {
	cache := NewImageCache(1, nil)
	cache.failed["permanent"] = struct{}{}
	cache.retries["transient"] = imageRetry{failures: 3, nextAt: time.Now().Add(time.Minute)}
	cache.Clear()
	if cache.Failed("permanent") {
		t.Fatal("Clear retained a permanent failure")
	}
	if len(cache.retries) != 0 {
		t.Fatal("Clear retained retry state")
	}
}

func TestImageErrorRetryClassification(t *testing.T) {
	if imageErrorRetryable(media.ErrRejected) {
		t.Fatal("rejected content was retryable")
	}
	if !imageErrorRetryable(errors.New("transport failed")) {
		t.Fatal("transport failure was not retryable")
	}
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooEarly,
		http.StatusTooManyRequests, http.StatusInternalServerError, 599} {
		if !imageErrorRetryable(&imageHTTPError{status: status}) {
			t.Fatalf("HTTP %d was not retryable", status)
		}
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, 600} {
		if imageErrorRetryable(&imageHTTPError{status: status}) {
			t.Fatalf("HTTP %d was retryable", status)
		}
	}
}

func fakeAnimation(frameCount int) *animatedTexture {
	frames := make([]*Texture, frameCount)
	for index := range frames {
		frames[index] = &Texture{}
	}
	return &animatedTexture{frames: frames}
}
