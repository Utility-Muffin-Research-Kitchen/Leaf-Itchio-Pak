package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func TestDecodeAnimatedGIFPreservesFramesAndTiming(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	second := image.NewPaletted(image.Rect(0, 0, 2, 2), palette)
	for index := range second.Pix {
		second.Pix[index] = 1
	}
	var encoded bytes.Buffer
	err := gif.EncodeAll(&encoded, &gif.GIF{
		Image:  []*image.Paletted{first, second},
		Delay:  []int{5, 15},
		Config: image.Config{ColorModel: palette, Width: 2, Height: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Frames) != 2 || len(decoded.Delays) != 2 {
		t.Fatalf("decoded GIF = %d frames/%d delays, want 2/2", len(decoded.Frames), len(decoded.Delays))
	}
	if decoded.Delays[0].Milliseconds() != 50 || decoded.Delays[1].Milliseconds() != 150 {
		t.Fatalf("decoded delays = %v, want 50ms/150ms", decoded.Delays)
	}
}
