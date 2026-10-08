package appui

import (
	"reflect"
	"testing"
	"time"
	"unsafe"
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

// afterSuspend returns the clock reading taken awake after start when the
// device also slept for slept in between. The wall clock counts the sleep.
// The monotonic clock that a time.Now reading also carries stops during
// suspend on Linux, so it does not. Go has no API to build such a reading,
// so this lowers the reading's unexported monotonic field.
func afterSuspend(t *testing.T, start time.Time, awake, slept time.Duration) time.Time {
	t.Helper()
	woke := start.Add(awake + slept)
	field := reflect.ValueOf(&woke).Elem().FieldByName("ext")
	if !field.IsValid() || field.Kind() != reflect.Int64 {
		t.Fatal("cannot simulate a suspend: time.Time has no int64 ext field")
	}
	ext := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	ext.SetInt(ext.Int() - int64(slept))
	if woke.Sub(start) != awake || woke.Round(0).Sub(start.Round(0)) != awake+slept {
		t.Fatal("cannot simulate a suspend: time.Time no longer keeps its monotonic reading in ext")
	}
	return woke
}

// The countdown follows the wall clock. Go subtracts two time.Now readings
// with their monotonic clock, which stops while the device sleeps, so after
// a suspend the countdown showed the sleep's length more than itch.io's own
// expiry.
func TestSignInModelRemainingCountsTimeAsleep(t *testing.T) {
	issued := time.Now() // like the code's expiry, a reading with a monotonic clock
	model := &SignInModel{Expires: issued.Add(10 * time.Minute)}
	woke := afterSuspend(t, issued, time.Minute, 3*time.Minute)
	if got := model.Remaining(woke); got != 6*time.Minute {
		t.Fatalf("remaining after a 3-minute sleep = %v, want 6m0s", got)
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

// R21-2: the warning screen continues with A and leaves with B.
func TestSignInModelWarning(t *testing.T) {
	model := &SignInModel{State: SignInWarning}
	if model.Handle(InputEvent{Button: ButtonA, Pressed: true}) != SignInIntentAccept ||
		model.Handle(InputEvent{Button: ButtonB, Pressed: true}) != SignInIntentBack {
		t.Fatal("A must accept the warning and B leave")
	}
}
