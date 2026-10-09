package roms_test

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/roms"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// F27: which archive members are Pico-8 carts. The name decides for anything
// with an image name; a member with no image name is a cart when it starts
// like a text cart. A PNG is never a cart by its bytes: a cart image is 160x205
// pixels, so a 128-pixel-wide PNG is a screenshot or label, and a PNG of cart
// size is as likely a cover.
func TestClassifyArchiveMemberChoosesPico8Carts(t *testing.T) {
	textCart := []byte("pico-8 cartridge // http://www.pico-8.com\nversion 41\n")
	for _, tc := range []struct {
		name     string
		member   string
		data     []byte
		kind     roms.FileKind
		wantName string
	}{
		{"a named .p8.png", "game/cart.p8.png", pngBytes(t, 160, 205), roms.KindROM, "game/cart.p8.png"},
		{"a named .p8.png in capitals", "game/CART.P8.PNG", []byte("compiled"), roms.KindROM, "game/CART.P8.PNG"},
		{"a named .p8", "game/main.p8", textCart, roms.KindROM, "game/main.p8"},
		{"a text cart without a Pico-8 name", "game/soulbound_v1_0", textCart, roms.KindROM, "game/soulbound_v1_0.p8"},
		{"a text cart named .txt", "game/main.txt", textCart, roms.KindROM, "game/main.p8"},
		{"a 128-pixel PNG without an extension", "game/shot", pngBytes(t, 128, 128), roms.KindOther, "game/shot"},
		{"a cart-sized PNG without an extension", "game/cartdata", pngBytes(t, 160, 205), roms.KindOther, "game/cartdata"},
		{"a 128-pixel PNG named .png", "game/cover.png", pngBytes(t, 128, 128), roms.KindOther, "game/cover.png"},
		{"a cart-sized PNG named .png", "game/label.png", pngBytes(t, 160, 205), roms.KindOther, "game/label.png"},
		{"a macOS stub of a cart", "game/._main.p8", textCart, roms.KindOther, "game/._main.p8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, name, err := roms.ClassifyArchiveMember(tc.member, func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(tc.data)), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if kind != tc.kind || name != tc.wantName {
				t.Fatalf("ClassifyArchiveMember(%q) = %v, %q; want %v, %q", tc.member, kind, name, tc.kind, tc.wantName)
			}
		})
	}
}
