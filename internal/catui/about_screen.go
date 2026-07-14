package catui

import (
	"strings"

	"github.com/skip2/go-qrcode"
)

const (
	catAboutRepo = "https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak"
	catAboutText = "Browse itch.io games, download compatible ROMs and soundtracks, and manage the resulting Leaf library. Unofficial and not affiliated with itch.io.\n\nUpstream: carroarmato0/NextUI-Itchio-Pak\nLeaf port: Utility Muffin Research Kitchen"
)

type AboutScreen struct {
	ctx                     *Context
	ui                      *Composer
	qr                      *Texture
	appVersion, leafVersion string
}

func NewAboutScreen(ctx *Context, appVersion, leafVersion string) (*AboutScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	appVersion = strings.TrimSpace(appVersion)
	if appVersion == "" {
		appVersion = "unknown"
	}
	leafVersion = strings.TrimSpace(leafVersion)
	if leafVersion == "" {
		leafVersion = "unknown"
	}
	screen := &AboutScreen{ctx: ctx, ui: ui, appVersion: appVersion, leafVersion: leafVersion}
	if code, qrErr := qrcode.New(catAboutRepo, qrcode.Medium); qrErr == nil {
		screen.qr, _ = ctx.TextureFromImage(code.Image(256))
	}
	return screen, nil
}

func (screen *AboutScreen) Close() {
	if screen.qr != nil {
		_ = screen.qr.Destroy()
		screen.qr = nil
	}
}

func (screen *AboutScreen) HandleInput(event InputEvent) bool {
	return !event.Wake && event.Pressed && (event.Button == ButtonA || event.Button == ButtonB || event.Button == ButtonStart)
}

func (screen *AboutScreen) Draw() error {
	frame, err := screen.ui.BeginScreen(ScreenSpec{Title: "About Itch.io", Footer: []FooterHint{{Button: ButtonB, Label: "Back"}}})
	if err != nil {
		return err
	}
	split := ListDetailSplit(frame.Layout.Content, 66, screen.ui.BasePadding)
	if err := screen.ui.DrawScrollingBody(split.List.Content(), "Version "+screen.appVersion,
		[]string{catAboutText, "Leaf " + screen.leafVersion, "Repository: scan the QR code"}, 0); err != nil {
		return err
	}
	if screen.qr != nil {
		rect := split.Detail.Content()
		size := minInt(rect.W, rect.H)
		if err := screen.qr.Draw(Rect{X: rect.X + (rect.W-size)/2, Y: rect.Y + (rect.H-size)/2, W: size, H: size}); err != nil {
			return err
		}
	}
	return frame.Finish()
}
