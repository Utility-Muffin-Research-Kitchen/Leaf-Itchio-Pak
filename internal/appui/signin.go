package appui

import "time"

type SignInState uint8

const (
	// SignInStarting: asking itch.io for a code.
	SignInStarting SignInState = iota
	// SignInWaiting: showing the QR code until the user approves.
	SignInWaiting
	// SignInChecking: approved; loading the account and owned games.
	SignInChecking
	SignInDone
	SignInError
	// SignInWarning: the physical-access warning before the first sign-in.
	SignInWarning
)

type SignInIntent uint8

const (
	SignInIntentNone SignInIntent = iota
	SignInIntentCancel
	SignInIntentRetry
	SignInIntentBack
	SignInIntentAccept
)

// SignInModel is the QR sign-in screen. It never holds the key or the
// device code, only what the user is meant to see.
type SignInModel struct {
	State    SignInState
	UserCode string
	QRURL    string
	Expires  time.Time
	Heading  string
	Detail   string
	CanRetry bool
	// QRFailed is set by the screen when it cannot draw the QR code. itch.io
	// has no manual code entry, so A then asks for a new code.
	QRFailed bool
}

func NewSignInModel() *SignInModel { return &SignInModel{State: SignInStarting} }

// Remaining is the time left on the code at now, never negative. It
// compares wall-clock times: Go subtracts two time.Now readings with their
// monotonic clock, which stops while the device sleeps, and itch.io's
// expiry does not wait for the device.
func (m *SignInModel) Remaining(now time.Time) time.Duration {
	return max(m.Expires.Round(0).Sub(now.Round(0)), 0)
}

// Handle maps input to an intent. B on the account check after approval only
// leaves the screen: the key is saved and the check finishes in the
// background.
func (m *SignInModel) Handle(event InputEvent) SignInIntent {
	if !event.Pressed {
		return SignInIntentNone
	}
	back := event.Button == ButtonB || event.Button == ButtonQuit
	switch m.State {
	case SignInWarning:
		if event.Button == ButtonA {
			return SignInIntentAccept
		}
		if back {
			return SignInIntentBack
		}
	case SignInStarting, SignInWaiting:
		if back {
			return SignInIntentCancel
		}
		if m.State == SignInWaiting && m.QRFailed && event.Button == ButtonA {
			return SignInIntentRetry
		}
	case SignInChecking:
		if back {
			return SignInIntentBack
		}
	case SignInDone:
		if back || event.Button == ButtonA {
			return SignInIntentBack
		}
	case SignInError:
		if event.Button == ButtonA && m.CanRetry {
			return SignInIntentRetry
		}
		if back {
			return SignInIntentBack
		}
	}
	return SignInIntentNone
}
