package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"testing"
	"time"
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

func TestDecodeSamplesThirtyFrameGIFWithinLimits(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	animation := &gif.GIF{
		Delay:  make([]int, 30),
		Config: image.Config{ColorModel: palette, Width: 160, Height: 90},
	}
	for index := 0; index < 30; index++ {
		frame := image.NewPaletted(image.Rect(0, 0, 160, 90), palette)
		frame.Pix[index%len(frame.Pix)] = 1
		animation.Image = append(animation.Image, frame)
		animation.Delay[index] = 1
	}
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, animation); err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(decoded.Frames); got != MaxGIFFrames {
		t.Fatalf("decoded GIF = %d frames, want %d", got, MaxGIFFrames)
	}
	var duration time.Duration
	for _, delay := range decoded.Delays {
		duration += delay
	}
	if duration != 300*time.Millisecond {
		t.Fatalf("sampled duration = %v, want 300ms", duration)
	}
}

func TestDecodeRejectsGIFOverSourceFrameLimit(t *testing.T) {
	data := gifDescriptorFixture(1, 1, 1, 1, MaxGIFSourceFrames+1, true)
	if _, err := Decode(data); !errors.Is(err, ErrRejected) {
		t.Fatalf("Decode error = %v, want ErrRejected", err)
	}
}

func TestDecodeRejectsGIFOverCumulativePixelLimit(t *testing.T) {
	data := gifDescriptorFixture(1280, 1280, 1280, 1280, MaxGIFFrames+1, true)
	if _, err := Decode(data); !errors.Is(err, ErrRejected) {
		t.Fatalf("Decode error = %v, want ErrRejected", err)
	}
}

func TestDecodeRejectsTruncatedGIFBeforeFrameDecode(t *testing.T) {
	data := gifDescriptorFixture(2, 2, 2, 2, 1, false)
	if _, err := Decode(data); !errors.Is(err, ErrRejected) {
		t.Fatalf("Decode error = %v, want ErrRejected", err)
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
	if _, err := Decode(encoded.Bytes()); !errors.Is(err, ErrRejected) {
		t.Fatalf("Decode error = %v for %dx%d image, want ErrRejected", err, side, side)
	}
}

func TestDecodeRejectsInvalidEncoding(t *testing.T) {
	if _, err := Decode([]byte("not an image")); !errors.Is(err, ErrRejected) {
		t.Fatalf("Decode error = %v, want ErrRejected", err)
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

func TestReadSourceRejectsDeclaredOversizeWithoutReading(t *testing.T) {
	reader := &countingReader{}
	if _, err := ReadSource(reader, MaxSourceBytes+1); !errors.Is(err, ErrRejected) {
		t.Fatalf("ReadSource error = %v, want ErrRejected", err)
	}
	if reader.reads != 0 {
		t.Fatalf("oversized declared source was read %d times", reader.reads)
	}
}

func TestReadSourceRejectsStreamOverByteLimit(t *testing.T) {
	reader := io.LimitReader(zeroReader{}, MaxSourceBytes+1)
	if _, err := ReadSource(reader, -1); !errors.Is(err, ErrRejected) {
		t.Fatalf("ReadSource error = %v, want ErrRejected", err)
	}
}

type countingReader struct {
	reads int
}

func (reader *countingReader) Read([]byte) (int, error) {
	reader.reads++
	return 0, errors.New("unexpected read")
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

func gifDescriptorFixture(canvasWidth, canvasHeight, frameWidth, frameHeight, frames int, trailer bool) []byte {
	data := append([]byte("GIF89a"), uint16LE(canvasWidth)...)
	data = append(data, uint16LE(canvasHeight)...)
	data = append(data, 0x80, 0x00, 0x00) // two-entry global color table
	data = append(data, 0, 0, 0, 255, 255, 255)
	for index := 0; index < frames; index++ {
		data = append(data, 0x2c)
		data = append(data, 0, 0, 0, 0) // left, top
		data = append(data, uint16LE(frameWidth)...)
		data = append(data, uint16LE(frameHeight)...)
		data = append(data, 0x00)             // no local color table
		data = append(data, 0x02, 0x01, 0, 0) // LZW size, one data byte, terminator
	}
	if trailer {
		data = append(data, 0x3b)
	}
	return data
}

func uint16LE(value int) []byte {
	encoded := make([]byte, 2)
	binary.LittleEndian.PutUint16(encoded, uint16(value))
	return encoded
}
