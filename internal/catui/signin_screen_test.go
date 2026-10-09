package catui

import (
	"testing"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
)

// F34: every sign-in state keeps Back or Cancel on the left and puts the
// action that moves the flow forward on the right, as the other screens do.
// The footer reads only the model, so the test builds the screen around one.
func TestSignInFooterPutsTheActionOnTheRight(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model appui.SignInModel
		b     string // the B hint, always on the left
		a     string // the A hint on the right, or "" when the state has none
		// aLeaves marks the finished screen, where A also leaves like B and
		// the footer shows only B.
		aLeaves bool
	}{
		{"warning before sign-in", appui.SignInModel{State: appui.SignInWarning}, "Back", "Continue", false},
		{"asking itch.io for a code", appui.SignInModel{State: appui.SignInStarting}, "Cancel", "", false},
		{"QR code", appui.SignInModel{State: appui.SignInWaiting}, "Cancel", "", false},
		{"QR code that cannot be drawn", appui.SignInModel{State: appui.SignInWaiting, QRFailed: true}, "Cancel", "Try again", false},
		{"checking the account", appui.SignInModel{State: appui.SignInChecking}, "Back", "", false},
		{"signed in", appui.SignInModel{State: appui.SignInDone}, "Back", "", true},
		{"failed, can retry", appui.SignInModel{State: appui.SignInError, CanRetry: true}, "Back", "Try again", false},
		{"failed, cannot retry", appui.SignInModel{State: appui.SignInError}, "Back", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := tc.model
			hints := (&SignInScreen{model: &model}).footer()
			buttons := footerButtons(hints)
			if len(buttons) != len(hints) {
				t.Fatalf("footer = %+v repeats a button", hints)
			}
			b, ok := buttons[ButtonB]
			if !ok || b.Label != tc.b || b.IsConfirm {
				t.Fatalf("B hint = %+v (present %v), want %q in the left group", b, ok, tc.b)
			}
			a, ok := buttons[ButtonA]
			if tc.a == "" {
				if ok {
					t.Fatalf("A hint = %+v, want none", a)
				}
			} else {
				if !ok || a.Label != tc.a {
					t.Fatalf("A hint = %+v (present %v), want %q", a, ok, tc.a)
				}
				if !a.IsConfirm {
					t.Fatalf("A %s sits in the left group next to B %s; want it in the right group", a.Label, b.Label)
				}
			}

			// The footer and the model agree: a state shows an A hint when A
			// does something, and A does nothing in a state without one,
			// except where it only repeats B.
			model = tc.model
			intent := model.Handle(appui.InputEvent{Button: appui.ButtonA, Pressed: true})
			switch {
			case tc.a != "" && intent == appui.SignInIntentNone:
				t.Fatalf("A hint %q but A does nothing", tc.a)
			case tc.a == "" && !tc.aLeaves && intent != appui.SignInIntentNone:
				t.Fatalf("no A hint but A gives intent %v", intent)
			case tc.aLeaves && intent != appui.SignInIntentBack:
				t.Fatalf("A = intent %v, want it to leave like B", intent)
			}
		})
	}
}
