package catui

import (
	"strings"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

const (
	catAboutRepo    = "https://github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak"
	catAboutText    = "Browse itch.io games, download compatible ROMs and soundtracks, and manage the resulting Leaf library. Unofficial and not affiliated with itch.io."
	catAboutCredits = "Upstream: carroarmato0/NextUI-Itchio-Pak\nLeaf port: Utility Muffin Research Kitchen"
)

type AboutScreen struct {
	ctx                     *Context
	ui                      *Composer
	qr                      *Texture
	appVersion, leafVersion string
	scroll                  appui.BodyScroll
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
	screen.qr = newQRTexture(ctx, catAboutRepo)
	return screen, nil
}

func (screen *AboutScreen) Close() {
	if screen.qr != nil {
		_ = screen.qr.Destroy()
		screen.qr = nil
	}
}

// HandleInput reports whether event closes About. Up and Down scroll the
// body when it does not fit.
func (screen *AboutScreen) HandleInput(event InputEvent) bool {
	if event.Wake || !event.Pressed {
		return false
	}
	if event.Button == ButtonA || event.Button == ButtonB || event.Button == ButtonStart {
		return true
	}
	screen.scroll.HandleScroll(appButton(event.Button))
	return false
}

func (screen *AboutScreen) Draw() error {
	frame, err := screen.ui.BeginScreen(ScreenSpec{Title: "About Itch.io", Footer: []FooterHint{{Button: ButtonB, Label: "Back"}}})
	if err != nil {
		return err
	}
	split := ListDetailSplit(frame.Layout.Content, 66, screen.ui.BasePadding)
	if err := screen.ui.DrawScrollingBody(split.List.Content(), "Version "+screen.appVersion,
		aboutParagraphs(screen.leafVersion), &screen.scroll); err != nil {
		return err
	}
	if screen.qr != nil {
		column := split.Detail.Content()
		qr := captionedQRRect(column, screen.ui.QRCaptionHeight(aboutQRCaption, column.W))
		if err := screen.qr.Draw(qr); err != nil {
			return err
		}
		if _, err := screen.ui.DrawQRCaption(qr, column.X, column.W, aboutQRCaption); err != nil {
			return err
		}
	}
	return frame.Finish()
}

// aboutQRCaption says where the QR code goes, as Detail's caption does.
const aboutQRCaption = "Scan for the source code"

// aboutParagraphs is the About body: what the app does, then the credits
// and the installed Leaf version as one block. The repository is the QR
// code's caption. Every line fits at the device's font size (bump 2) with
// room for a font that wraps the upstream name, as the device's does.
func aboutParagraphs(leafVersion string) []string {
	return []string{catAboutText, catAboutCredits + "\nLeaf " + leafVersion}
}

// captionedQRRect is the largest square for a QR code in rect that leaves
// captionHeight under it, with the code and caption centered together.
func captionedQRRect(rect Rect, captionHeight int) Rect {
	size := maxInt(0, minInt(rect.W, rect.H-captionHeight))
	return Rect{X: rect.X + (rect.W-size)/2, Y: rect.Y + (rect.H-size-captionHeight)/2, W: size, H: size}
}
