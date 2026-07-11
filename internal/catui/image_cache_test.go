package catui

import (
	"container/list"
	"testing"
	"time"
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
