package catui

import (
	"container/list"
	"errors"
	"fmt"
	"image"
	"net/http"
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
	active    bool
}

type decodedImage struct {
	key   string
	image *media.DecodedImage
}

type imageRetry struct {
	failures int
	nextAt   time.Time
}

type imageHTTPError struct {
	status int
}

func (err *imageHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d", err.status)
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
	retries  map[string]imageRetry
	maximum  int
	client   *http.Client
	ready    chan decodedImage
	sem      chan struct{}
	notify   func()
	now      func() time.Time
	// frameCount counts textures owned by cached artwork. The bridge registry
	// also contains QR/detail textures, so cache uploads retain a fixed margin.
	frameCount int
}

const (
	imageCacheTextureReserve = 32
	imageRetryInitialDelay   = time.Second
	imageRetryMaximumDelay   = 60 * time.Second
)

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
		retries:  make(map[string]imageRetry),
		maximum:  maximum,
		client:   client,
		ready:    make(chan decodedImage, 32),
		sem:      make(chan struct{}, 2),
		now:      time.Now,
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
	retry := c.retries[key]
	if cached || fetching || failed || (!retry.nextAt.IsZero() && c.now().Before(retry.nextAt)) {
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
	entry := element.Value.(*catImageEntry)
	entry.active = true
	return entry.animation.texture(time.Now())
}

// BeginFrame clears the visible-animation set. Draw paths mark only artwork
// actually used by the current screen active through Peek/Get, preventing a
// cached off-screen GIF from scheduling redraws forever.
func (c *ImageCache) BeginFrame() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for element := c.lru.Front(); element != nil; element = element.Next() {
		element.Value.(*catImageEntry).active = false
	}
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
				c.mu.Lock()
				c.failed[decoded.key] = struct{}{}
				c.mu.Unlock()
				logger.Warn("catui image cache: skipping artwork %s after upload failure: %v", decoded.key, err)
				continue
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
		entry := element.Value.(*catImageEntry)
		if !entry.active {
			continue
		}
		remaining, animated := entry.animation.nextFrameIn(now)
		if animated && (!found || remaining < minimum) {
			minimum, found = remaining, true
		}
	}
	return minimum, found
}

func (c *ImageCache) Busy() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.fetching) > 0 || len(c.ready) > 0
}

func (c *ImageCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for element := c.lru.Front(); element != nil; element = element.Next() {
		element.Value.(*catImageEntry).animation.destroy()
	}
	c.lru.Init()
	c.items = make(map[string]*list.Element)
	c.failed = make(map[string]struct{})
	c.retries = make(map[string]imageRetry)
	c.frameCount = 0
}

func (c *ImageCache) insert(ctx *Context, key string, decoded *media.DecodedImage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.items[key]; ok {
		c.removeLocked(existing)
	}
	incomingFrames := 0
	if decoded != nil {
		incomingFrames = len(decoded.Frames)
	}
	capacity := 0
	otherTextures := 0
	if ctx != nil {
		capacity = ctx.TextureCapacity()
		otherTextures = ctx.TextureCount() - c.frameCount
		if otherTextures < 0 {
			otherTextures = 0
		}
	}
	budget := capacity - imageCacheTextureReserve - otherTextures
	if budget < 1 || incomingFrames < 1 || !c.reserveLocked(incomingFrames, budget) {
		return fmt.Errorf("catui image cache: texture budget exhausted (incoming=%d cached=%d budget=%d)",
			incomingFrames, c.frameCount, budget)
	}
	animation, err := newAnimatedTexture(ctx, decoded)
	if err != nil {
		return err
	}
	element := c.lru.PushFront(&catImageEntry{key: key, animation: animation})
	c.items[key] = element
	c.frameCount += len(animation.frames)
	return nil
}

func (c *ImageCache) reserveLocked(incomingFrames, budget int) bool {
	for c.lru.Len() > 0 && (c.lru.Len() >= c.maximum || c.frameCount+incomingFrames > budget) {
		c.removeLocked(c.lru.Back())
	}
	return c.frameCount+incomingFrames <= budget
}

func (c *ImageCache) removeLocked(element *list.Element) {
	if element == nil {
		return
	}
	entry := element.Value.(*catImageEntry)
	frames := len(entry.animation.frames)
	entry.animation.destroy()
	delete(c.items, entry.key)
	c.lru.Remove(element)
	c.frameCount -= frames
	if c.frameCount < 0 {
		c.frameCount = 0
	}
}

func (c *ImageCache) fetch(key string) {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	response, err := c.client.Get(key)
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			err = &imageHTTPError{status: response.StatusCode}
		}
	}
	var data []byte
	if err == nil {
		data, err = media.ReadSource(response.Body, response.ContentLength)
	}
	var decoded *media.DecodedImage
	if err == nil {
		decoded, err = media.Decode(data)
	}

	c.mu.Lock()
	delete(c.fetching, key)
	if err == nil {
		delete(c.retries, key)
	} else if imageErrorRetryable(err) {
		c.recordRetryLocked(key)
	} else {
		c.failed[key] = struct{}{}
		delete(c.retries, key)
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

func imageErrorRetryable(err error) bool {
	if err == nil || errors.Is(err, media.ErrRejected) {
		return false
	}
	var statusErr *imageHTTPError
	if !errors.As(err, &statusErr) {
		return true
	}
	return statusErr.status == http.StatusRequestTimeout ||
		statusErr.status == http.StatusTooEarly ||
		statusErr.status == http.StatusTooManyRequests ||
		(statusErr.status >= http.StatusInternalServerError && statusErr.status <= 599)
}

func (c *ImageCache) recordRetryLocked(key string) {
	retry := c.retries[key]
	retry.failures++
	delay := imageRetryInitialDelay
	for attempt := 1; attempt < retry.failures && delay < imageRetryMaximumDelay; attempt++ {
		delay *= 2
		if delay > imageRetryMaximumDelay {
			delay = imageRetryMaximumDelay
		}
	}
	retry.nextAt = c.now().Add(delay)
	c.retries[key] = retry
}
