package catui

import (
	"fmt"
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
	case appui.SignInWarning:
		return []FooterHint{{Button: ButtonA, Label: "Continue", IsConfirm: true}, {Button: ButtonB, Label: "Back"}}
	case appui.SignInStarting, appui.SignInWaiting:
		if screen.model.State == appui.SignInWaiting && screen.model.QRFailed {
			return []FooterHint{{Button: ButtonA, Label: "Try again"}, {Button: ButtonB, Label: "Cancel"}}
		}
		return []FooterHint{{Button: ButtonB, Label: "Cancel"}}
	case appui.SignInError:
		if screen.model.CanRetry {
			return []FooterHint{{Button: ButtonA, Label: "Try again"}, {Button: ButtonB, Label: "Back"}}
		}
	}
	return []FooterHint{{Button: ButtonB, Label: "Back"}}
}

func (screen *SignInScreen) Draw() error {
	if screen.model.State == appui.SignInWaiting {
		screen.prepareQR() // before the footer, which depends on the outcome
	}
	frame, err := screen.ui.BeginScreen(ScreenSpec{Title: "Sign in with itch.io", Footer: screen.footer()})
	if err != nil {
		return err
	}
	body := frame.Layout.Content.Content()
	model := screen.model
	switch model.State {
	case appui.SignInWarning:
		err = screen.ui.DrawScrollingBody(body, "Before you sign in", []string{
			"Signing in stores an itch.io key in App Data on the SD card.",
			"FAT32 cannot protect it from someone with physical access to the card.",
			"The key is redacted from logs and never shown on screen.",
			"You can sign out in Settings, and delete the key on itch.io.",
		}, 0)
	case appui.SignInStarting:
		err = screen.ui.DrawState(body, StateLoading, "Getting a sign-in code", "Contacting itch.io…")
	case appui.SignInWaiting:
		if model.QRFailed {
			err = screen.ui.DrawState(body, StateError, "Can't show the QR code", "Press A for a new code.")
		} else {
			err = screen.drawCode(frame)
		}
	case appui.SignInChecking:
		err = screen.ui.DrawState(body, StateLoading, "Signed in", "Loading your owned games. You can go back meanwhile.")
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

// prepareQR builds the QR texture once per code. A failure is recorded on
// the model so the screen explains it and A asks for a new code.
func (screen *SignInScreen) prepareQR() {
	model := screen.model
	if screen.qrText == model.QRURL {
		return
	}
	screen.Close()
	screen.qr, screen.qrText = newQRTexture(screen.ctx, model.QRURL), model.QRURL
	model.QRFailed = screen.qr == nil
}

func (screen *SignInScreen) drawCode(frame *ScreenFrame) error {
	model := screen.model
	remaining := model.Remaining(screen.now()).Round(time.Second)
	lines := []string{
		"Scan the QR code with your phone.",
		"Check that itch.io shows the same code, then approve Leaf.",
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
