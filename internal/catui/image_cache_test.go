package catui

import (
	"container/list"
	"image"
	"strconv"
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

func TestImageCacheBusyCoversFetchAndPendingUpload(t *testing.T) {
	cache := NewImageCache(1, nil)
	cache.fetching["cover"] = struct{}{}
	if !cache.Busy() {
		t.Fatal("active fetch was not busy")
	}
	delete(cache.fetching, "cover")
	cache.ready <- decodedImage{key: "cover"}
	if !cache.Busy() {
		t.Fatal("pending owner-thread upload was not busy")
	}
	<-cache.ready
	if cache.Busy() {
		t.Fatal("idle cache remained busy")
	}
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

func fakeAnimation(frameCount int) *animatedTexture {
	frames := make([]*Texture, frameCount)
	for index := range frames {
		frames[index] = &Texture{}
	}
	return &animatedTexture{frames: frames}
}
