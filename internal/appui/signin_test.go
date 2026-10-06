package appui

import (
	"testing"
	"time"
)

func TestSignInModelInput(t *testing.T) {
	press := func(model *SignInModel, button Button) SignInIntent {
		return model.Handle(InputEvent{Button: button, Pressed: true})
	}
	for _, state := range []SignInState{SignInStarting, SignInWaiting} {
		model := &SignInModel{State: state}
		if press(model, ButtonB) != SignInIntentCancel || press(model, ButtonA) != SignInIntentNone {
			t.Errorf("state %v: B must cancel and A do nothing", state)
		}
	}
	// R21-7: B leaves the account check, which finishes in the background.
	if checking := (&SignInModel{State: SignInChecking}); press(checking, ButtonB) != SignInIntentBack || press(checking, ButtonA) != SignInIntentNone {
		t.Error("B must leave the account check and A do nothing")
	}
	if done := (&SignInModel{State: SignInDone}); press(done, ButtonA) != SignInIntentBack || press(done, ButtonB) != SignInIntentBack {
		t.Error("A and B leave a finished sign-in")
	}
	failed := &SignInModel{State: SignInError, CanRetry: true}
	if press(failed, ButtonA) != SignInIntentRetry || press(failed, ButtonB) != SignInIntentBack {
		t.Error("A retries and B leaves after an error")
	}
	if (&SignInModel{State: SignInWaiting}).Handle(InputEvent{Button: ButtonB}) != SignInIntentNone {
		t.Error("a release must not cancel")
	}
}

func TestSignInModelRemainingNeverNegative(t *testing.T) {
	now := time.Now()
	model := &SignInModel{Expires: now.Add(90 * time.Second)}
	if model.Remaining(now) != 90*time.Second || model.Remaining(now.Add(time.Hour)) != 0 {
		t.Fatal("remaining time is wrong")
	}
}

// R21-10: when the QR code cannot be drawn, A asks for a new code.
func TestSignInModelRetriesWhenTheQRCodeFails(t *testing.T) {
	model := &SignInModel{State: SignInWaiting, QRFailed: true}
	if model.Handle(InputEvent{Button: ButtonA, Pressed: true}) != SignInIntentRetry {
		t.Fatal("A must ask for a new code")
	}
	if model.Handle(InputEvent{Button: ButtonB, Pressed: true}) != SignInIntentCancel {
		t.Fatal("B must still cancel")
	}
}
