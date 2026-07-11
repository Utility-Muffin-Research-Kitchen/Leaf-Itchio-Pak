package catui

import (
	"container/list"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/media"
)

type animatedTexture struct {
	frames  []*Texture
	delays  []time.Duration
	current int
	nextAt  time.Time
}

func newAnimatedTexture(ctx *Context, decoded *media.DecodedImage) (*animatedTexture, error) {
	if decoded == nil || len(decoded.Frames) == 0 {
		return nil, fmt.Errorf("catui image cache: decoded image has no frames")
	}
	animation := &animatedTexture{delays: append([]time.Duration(nil), decoded.Delays...)}
	for _, frame := range decoded.Frames {
		texture, err := ctx.TextureFromImage(frame)
		if err != nil {
			animation.destroy()
			return nil, err
		}
		animation.frames = append(animation.frames, texture)
	}
	for len(animation.delays) < len(animation.frames) {
		animation.delays = append(animation.delays, media.DefaultFrameDelay)
	}
	animation.nextAt = time.Now().Add(animation.delay(0))
	return animation, nil
}

func (a *animatedTexture) delay(index int) time.Duration {
	if index >= 0 && index < len(a.delays) && a.delays[index] > 0 {
		return a.delays[index]
	}
	return media.DefaultFrameDelay
}

func (a *animatedTexture) texture(now time.Time) *Texture {
	if len(a.frames) == 0 {
		return nil
	}
	if len(a.frames) > 1 && !now.Before(a.nextAt) {
		a.current = (a.current + 1) % len(a.frames)
		a.nextAt = now.Add(a.delay(a.current))
	}
	return a.frames[a.current]
}

func (a *animatedTexture) nextFrameIn(now time.Time) (time.Duration, bool) {
	if len(a.frames) <= 1 {
		return 0, false
	}
	remaining := a.nextAt.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	return remaining, true
}

func (a *animatedTexture) destroy() {
	for _, frame := range a.frames {
		if frame != nil {
			_ = frame.Destroy()
		}
	}
	a.frames = nil
}

type catImageEntry struct {
	key       string
	animation *animatedTexture
}

type decodedImage struct {
	key   string
	image *media.DecodedImage
}

// ImageCache keeps all Catastrophe texture creation and destruction on the
// owning GUI thread. Workers only fetch and decode renderer-independent image
// data; the notify callback should call Context.Wake.
type ImageCache struct {
	mu       sync.Mutex
	lru      *list.List
	items    map[string]*list.Element
	fetching map[string]struct{}
	failed   map[string]struct{}
	maximum  int
	client   *http.Client
	ready    chan decodedImage
	sem      chan struct{}
	notify   func()
}

func NewImageCache(maximum int, client *http.Client) *ImageCache {
	if maximum < 1 {
		maximum = 1
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &ImageCache{
		lru:      list.New(),
		items:    make(map[string]*list.Element),
		fetching: make(map[string]struct{}),
		failed:   make(map[string]struct{}),
		maximum:  maximum,
		client:   client,
		ready:    make(chan decodedImage, 32),
		sem:      make(chan struct{}, 2),
	}
}

func (c *ImageCache) SetNotify(notify func()) { c.notify = notify }

func (c *ImageCache) Warm(key string) {
	if key == "" {
		return
	}
	c.mu.Lock()
	_, cached := c.items[key]
	_, fetching := c.fetching[key]
	_, failed := c.failed[key]
	if cached || fetching || failed {
		c.mu.Unlock()
		return
	}
	c.fetching[key] = struct{}{}
	c.mu.Unlock()
	go c.fetch(key)
}

func (c *ImageCache) Get(key string) *Texture {
	if texture := c.Peek(key); texture != nil {
		return texture
	}
	c.Warm(key)
	return nil
}

func (c *ImageCache) Peek(key string) *Texture {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.items[key]
	if !ok {
		return nil
	}
	c.lru.MoveToFront(element)
	return element.Value.(*catImageEntry).animation.texture(time.Now())
}

func (c *ImageCache) Failed(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, failed := c.failed[key]
	return failed
}

func (c *ImageCache) ProcessPending(ctx *Context) (bool, error) {
	uploaded := false
	for {
		select {
		case decoded := <-c.ready:
			if err := c.insert(ctx, decoded.key, decoded.image); err != nil {
				return uploaded, err
			}
			uploaded = true
		default:
			return uploaded, nil
		}
	}
}

func (c *ImageCache) Seed(ctx *Context, key string, frames []image.Image, delays []time.Duration) error {
	decoded := &media.DecodedImage{Delays: append([]time.Duration(nil), delays...)}
	for _, source := range frames {
		bounds := source.Bounds()
		rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		for y := 0; y < bounds.Dy(); y++ {
			for x := 0; x < bounds.Dx(); x++ {
				rgba.Set(x, y, source.At(bounds.Min.X+x, bounds.Min.Y+y))
			}
		}
		decoded.Frames = append(decoded.Frames, rgba)
	}
	return c.insert(ctx, key, decoded)
}

func (c *ImageCache) NextFrameIn() (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	var minimum time.Duration
	found := false
	for element := c.lru.Front(); element != nil; element = element.Next() {
		remaining, animated := element.Value.(*catImageEntry).animation.nextFrameIn(now)
		if animated && (!found || remaining < minimum) {
			minimum, found = remaining, true
		}
	}
	return minimum, found
}

func (c *ImageCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for element := c.lru.Front(); element != nil; element = element.Next() {
		element.Value.(*catImageEntry).animation.destroy()
	}
	c.lru.Init()
	c.items = make(map[string]*list.Element)
}

func (c *ImageCache) insert(ctx *Context, key string, decoded *media.DecodedImage) error {
	animation, err := newAnimatedTexture(ctx, decoded)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.items[key]; ok {
		existing.Value.(*catImageEntry).animation.destroy()
		c.lru.Remove(existing)
	}
	element := c.lru.PushFront(&catImageEntry{key: key, animation: animation})
	c.items[key] = element
	for c.lru.Len() > c.maximum {
		back := c.lru.Back()
		entry := back.Value.(*catImageEntry)
		entry.animation.destroy()
		delete(c.items, entry.key)
		c.lru.Remove(back)
	}
	return nil
}

func (c *ImageCache) fetch(key string) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	response, err := c.client.Get(key)
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err = fmt.Errorf("HTTP %d", response.StatusCode)
		}
	}
	var data []byte
	if err == nil {
		data, err = io.ReadAll(response.Body)
	}
	var decoded *media.DecodedImage
	if err == nil {
		decoded, err = media.Decode(data)
	}

	c.mu.Lock()
	delete(c.fetching, key)
	if err != nil && strings.Contains(err.Error(), "decode image") {
		c.failed[key] = struct{}{}
	}
	c.mu.Unlock()
	if err != nil {
		logger.Warn("catui image cache: %s: %v", key, err)
		return
	}
	c.ready <- decodedImage{key: key, image: decoded}
	if c.notify != nil {
		c.notify()
	}
}
