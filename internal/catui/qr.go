package catui

import "github.com/skip2/go-qrcode"

// newQRTexture renders text as a QR code, or returns nil when text is empty
// or cannot be encoded. The caller destroys the texture.
func newQRTexture(ctx *Context, text string) *Texture {
	if text == "" {
		return nil
	}
	code, err := qrcode.New(text, qrcode.Medium)
	if err != nil {
		return nil
	}
	texture, err := ctx.TextureFromImage(code.Image(256))
	if err != nil {
		return nil
	}
	return texture
}
