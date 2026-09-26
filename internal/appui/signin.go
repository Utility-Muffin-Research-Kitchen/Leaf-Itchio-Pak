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
)

type SignInIntent uint8

const (
	SignInIntentNone SignInIntent = iota
	SignInIntentCancel
	SignInIntentRetry
	SignInIntentBack
)

// SignInModel is the QR sign-in screen. It never holds the key or the
// device code, only what the user is meant to see.
type SignInModel struct {
	State     SignInState
	UserCode  string
	QRURL     string
	ManualURL string
	Expires   time.Time
	Heading   string
	Detail    string
	CanRetry  bool
}

func NewSignInModel() *SignInModel { return &SignInModel{State: SignInStarting} }

// Remaining is the time left on the code, never negative.
func (m *SignInModel) Remaining(now time.Time) time.Duration {
	return max(m.Expires.Sub(now), 0)
}

// Handle maps input to an intent. Nothing cancels the short account check
// after approval, so the owned-game list is always saved.
func (m *SignInModel) Handle(event InputEvent) SignInIntent {
	if !event.Pressed {
		return SignInIntentNone
	}
	back := event.Button == ButtonB || event.Button == ButtonQuit
	switch m.State {
	case SignInStarting, SignInWaiting:
		if back {
			return SignInIntentCancel
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
