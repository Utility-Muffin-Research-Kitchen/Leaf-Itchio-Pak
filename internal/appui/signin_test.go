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
	if checking := (&SignInModel{State: SignInChecking}); press(checking, ButtonB) != SignInIntentNone {
		t.Error("the account check after approval must not be cancellable")
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
