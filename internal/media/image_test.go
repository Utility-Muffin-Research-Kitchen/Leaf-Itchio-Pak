package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
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

func TestDecodeRejectsOversizedImage(t *testing.T) {
	// A valid PNG whose declared dimensions exceed MaxGIFSourcePixels must be
	// rejected by the DecodeConfig guard before a full (bomb) decode.
	const side = 1300 // 1300*1300 = 1,690,000 > MaxGIFSourcePixels (1,638,400)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(encoded.Bytes()); err == nil {
		t.Fatalf("Decode accepted a %dx%d image, want rejection", side, side)
	}
}

func TestDecodeAcceptsNormalImage(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Frames) != 1 || decoded.Frames[0] == nil {
		t.Fatalf("decoded PNG = %d frames, want 1 non-nil", len(decoded.Frames))
	}
}
