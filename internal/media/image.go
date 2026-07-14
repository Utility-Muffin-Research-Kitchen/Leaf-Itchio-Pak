package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	stdraw "image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"time"

	xdraw "golang.org/x/image/draw"
)

const (
	MaxGIFFrames       = 16
	MaxGIFSourcePixels = 1280 * 1280
	MaxImageWidth      = 640
	DefaultFrameDelay  = 100 * time.Millisecond
	// MaxSourceBytes caps how many bytes of an encoded image we accept from an
	// untrusted (game-author-controlled) URL, so a very large download cannot
	// exhaust memory on a 1GB device before it is even decoded.
	MaxSourceBytes = 16 << 20 // 16 MiB
)

// DecodedImage is renderer-independent decoded artwork. Animated GIFs retain
// their composited frames and timing so each GUI backend can upload them using
// its own texture API.
type DecodedImage struct {
	Frames []*image.RGBA
	Delays []time.Duration
}

func Decode(data []byte) (*DecodedImage, error) {
	// Reject wildly oversized images before decoding: a small but bomb-crafted
	// PNG/JPEG/GIF can declare enormous dimensions and blow past memory on a 1GB
	// device once image.Decode allocates the pixel buffer. DecodeConfig only
	// reads the header, so this is cheap.
	cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(data))
	if cfgErr != nil {
		return nil, fmt.Errorf("decode image config: %w", cfgErr)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 ||
		int64(cfg.Width)*int64(cfg.Height) > int64(MaxGIFSourcePixels) {
		return nil, fmt.Errorf("decode image: unsafe dimensions %dx%d", cfg.Width, cfg.Height)
	}

	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if format == "gif" {
		animated, gifErr := gif.DecodeAll(bytes.NewReader(data))
		if gifErr == nil && len(animated.Image) > 1 {
			return decodeGIF(animated)
		}
	}
	return &DecodedImage{
		Frames: []*image.RGBA{scaleRGBA(decoded)},
		Delays: []time.Duration{DefaultFrameDelay},
	}, nil
}

func decodeGIF(source *gif.GIF) (*DecodedImage, error) {
	if len(source.Image) == 0 {
		return nil, fmt.Errorf("decode image: empty GIF")
	}
	w, h := source.Config.Width, source.Config.Height
	if w <= 0 || h <= 0 {
		bounds := source.Image[0].Bounds()
		w, h = bounds.Max.X, bounds.Max.Y
	}
	if w <= 0 || h <= 0 || w*h > MaxGIFSourcePixels {
		return nil, fmt.Errorf("decode image: unsafe GIF dimensions %dx%d", w, h)
	}

	total := len(source.Image)
	stored := total
	if stored > MaxGIFFrames {
		stored = MaxGIFFrames
	}
	samples := make([]int, stored)
	sampleSlots := make(map[int]int, stored)
	delays := make([]time.Duration, stored)
	for index := range samples {
		samples[index] = index * total / stored
		sampleSlots[samples[index]] = index
	}
	for index, start := range samples {
		end := total
		if index+1 < len(samples) {
			end = samples[index+1]
		}
		centiseconds := 0
		for frame := start; frame < end && frame < len(source.Delay); frame++ {
			centiseconds += source.Delay[frame]
		}
		if centiseconds == 0 {
			delays[index] = DefaultFrameDelay
		} else {
			delays[index] = time.Duration(centiseconds) * 10 * time.Millisecond
		}
	}

	bounds := image.Rect(0, 0, w, h)
	background := color.Color(color.RGBA{A: 255})
	if palette, ok := source.Config.ColorModel.(color.Palette); ok && int(source.BackgroundIndex) < len(palette) {
		background = palette[source.BackgroundIndex]
	}
	fill := image.NewUniform(background)
	canvas := image.NewRGBA(bounds)
	stdraw.Draw(canvas, bounds, fill, image.Point{}, stdraw.Src)
	var previous *image.RGBA
	frames := make([]*image.RGBA, stored)

	for index, frame := range source.Image {
		disposal := byte(gif.DisposalNone)
		if index < len(source.Disposal) {
			disposal = source.Disposal[index]
		}
		if disposal == gif.DisposalPrevious {
			if previous == nil {
				previous = image.NewRGBA(bounds)
			}
			stdraw.Draw(previous, bounds, canvas, image.Point{}, stdraw.Src)
		}
		stdraw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, stdraw.Over)
		if slot, ok := sampleSlots[index]; ok {
			frames[slot] = scaleRGBA(canvas)
		}
		switch disposal {
		case gif.DisposalBackground:
			stdraw.Draw(canvas, frame.Bounds(), fill, image.Point{}, stdraw.Src)
		case gif.DisposalPrevious:
			if previous != nil {
				stdraw.Draw(canvas, frame.Bounds(), previous, frame.Bounds().Min, stdraw.Src)
			}
		}
	}
	return &DecodedImage{Frames: frames, Delays: delays}, nil
}

func scaleRGBA(source image.Image) *image.RGBA {
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w > MaxImageWidth {
		h = h * MaxImageWidth / w
		w = MaxImageWidth
	}
	destination := image.NewRGBA(image.Rect(0, 0, w, h))
	if w == bounds.Dx() && h == bounds.Dy() {
		stdraw.Draw(destination, destination.Bounds(), source, bounds.Min, stdraw.Src)
	} else {
		xdraw.NearestNeighbor.Scale(destination, destination.Bounds(), source, bounds, xdraw.Src, nil)
	}
	return destination
}
