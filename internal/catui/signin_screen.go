package catui

import (
	"fmt"
	"strings"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

// SignInScreen shows the QR code and user code for itch.io sign-in, then
// the outcome. The countdown needs a redraw about once a second.
type SignInScreen struct {
	ctx    *Context
	ui     *Composer
	model  *appui.SignInModel
	qr     *Texture
	qrText string
	now    func() time.Time
}

func NewSignInScreen(ctx *Context, model *appui.SignInModel) (*SignInScreen, error) {
	ui, err := NewComposer(ctx)
	if err != nil {
		return nil, err
	}
	return &SignInScreen{ctx: ctx, ui: ui, model: model, now: time.Now}, nil
}

func (screen *SignInScreen) Close() {
	if screen.qr != nil {
		_ = screen.qr.Destroy()
		screen.qr, screen.qrText = nil, ""
	}
}

func (screen *SignInScreen) HandleInput(event InputEvent) appui.SignInIntent {
	if event.Wake {
		return appui.SignInIntentNone
	}
	return screen.model.Handle(appui.InputEvent{
		Button: appButton(event.Button), Pressed: event.Pressed, Repeated: event.Repeated,
	})
}

func (screen *SignInScreen) footer() []FooterHint {
	switch screen.model.State {
	case appui.SignInStarting, appui.SignInWaiting:
		return []FooterHint{{Button: ButtonB, Label: "Cancel"}}
	case appui.SignInChecking:
		return nil
	case appui.SignInError:
		if screen.model.CanRetry {
			return []FooterHint{{Button: ButtonA, Label: "Try again"}, {Button: ButtonB, Label: "Back"}}
		}
	}
	return []FooterHint{{Button: ButtonB, Label: "Back"}}
}

func (screen *SignInScreen) Draw() error {
	frame, err := screen.ui.BeginScreen(ScreenSpec{Title: "Sign in with itch.io", Footer: screen.footer()})
	if err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	model := screen.model
	switch model.State {
	case appui.SignInStarting:
		err = screen.ui.DrawState(body, StateLoading, "Getting a sign-in code", "Contacting itch.io…")
	case appui.SignInWaiting:
		err = screen.drawCode(frame)
	case appui.SignInChecking:
		err = screen.ui.DrawState(body, StateLoading, "Signed in", "Loading your owned games…")
	case appui.SignInDone:
		err = screen.ui.DrawState(body, StateEmpty, model.Heading, model.Detail)
	default:
		err = screen.ui.DrawState(body, StateError, model.Heading, model.Detail)
	}
	if err != nil {
		return err
	}
	return frame.Finish()
}

func (screen *SignInScreen) drawCode(frame *ScreenFrame) error {
	model := screen.model
	if screen.qrText != model.QRURL {
		screen.Close()
		screen.qr, screen.qrText = newQRTexture(screen.ctx, model.QRURL), model.QRURL
	}
	remaining := model.Remaining(screen.now()).Round(time.Second)
	lines := []string{
		"Scan the QR code with your phone and approve Leaf on itch.io.",
		"Can't scan it? Open " + shortURL(model.ManualURL) + " and enter the code.",
		fmt.Sprintf("Expires in %d:%02d", int(remaining.Minutes()), int(remaining.Seconds())%60),
	}
	split := ListDetailSplit(frame.Layout.Content, 60, screen.ui.BasePadding)
	if err := screen.ui.DrawScrollingBody(split.List.Content(), model.UserCode, lines, 0); err != nil {
		return err
	}
	if screen.qr == nil {
		return nil
	}
	rect := split.Detail.Content()
	size := minInt(rect.W, rect.H)
	return screen.qr.Draw(Rect{X: rect.X + (rect.W-size)/2, Y: rect.Y + (rect.H-size)/2, W: size, H: size})
}

// shortURL drops the scheme so the address fits on one line.
func shortURL(url string) string {
	if url == "" {
		return "itch.io"
	}
	return strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
}
