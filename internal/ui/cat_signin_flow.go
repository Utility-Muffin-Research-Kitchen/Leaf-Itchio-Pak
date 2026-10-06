//go:build !headless

package ui

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

const (
	// exchangeTimeout bounds the key exchange, which a cancel does not stop.
	exchangeTimeout = 30 * time.Second
	// checkTimeout bounds the account check, which may page through a large
	// library and wait out rate limits, possibly after you left the screen.
	checkTimeout = 2 * time.Minute
)

type signInUpdate struct {
	attempt uint64
	login   *itchio.DeviceLogin
	token   string // a key itch.io issued
	checked bool   // the account check of key finished
	key     string
	user    string
	owned   []itchio.OwnedGame
	err     error
}

// CatSignInFlow runs one QR sign-in: it gets a code, waits for approval on
// the user's phone, stores the key through Account, and loads the account's
// owned games. Network work runs on goroutines; Sync applies the results on
// the UI goroutine, which owns the config.
//
// A key itch.io issued is always saved, even when you cancelled or left the
// screen meanwhile: the app cannot revoke it. So the app keeps calling Sync,
// with a nil model once the screen is closed, until Idle.
type CatSignInFlow struct {
	client  *itchio.Client
	account *Account
	wake    func()

	updates chan signInUpdate
	attempt uint64
	cancel  context.CancelFunc
	// work counts goroutines that have not delivered their result yet.
	work atomic.Int32
	// keys counts keys being exchanged or received but not saved yet: the
	// only sign-in work that holds a power action.
	keys atomic.Int32
}

func NewCatSignInFlow(client *itchio.Client, account *Account, wake func()) (*CatSignInFlow, *appui.SignInModel) {
	flow := &CatSignInFlow{client: client, account: account, wake: wake, updates: make(chan signInUpdate, 8)}
	model := appui.NewSignInModel()
	flow.Start(model)
	return flow, model
}

// Start requests a new code, abandoning any earlier attempt.
func (flow *CatSignInFlow) Start(model *appui.SignInModel) {
	flow.Cancel()
	flow.attempt++
	*model = appui.SignInModel{State: appui.SignInStarting}
	ctx, cancel := context.WithCancel(context.Background())
	flow.cancel = cancel
	attempt := flow.attempt
	flow.work.Add(1)
	go func() {
		defer flow.work.Add(-1)
		login, err := flow.client.BeginDeviceLogin(ctx)
		flow.publish(signInUpdate{attempt: attempt, login: login, err: err})
		if err != nil {
			return
		}
		code, err := login.WaitForApproval(ctx)
		if err != nil {
			flow.publish(signInUpdate{attempt: attempt, err: err})
			return
		}
		// itch.io issues the key in this exchange. Cancelling it would
		// leave a key on the account that the app never saw.
		flow.keys.Add(1)
		exchangeCtx, done := context.WithTimeout(context.WithoutCancel(ctx), exchangeTimeout)
		token, err := login.Exchange(exchangeCtx, code)
		done()
		if token == "" {
			flow.keys.Add(-1)
		}
		flow.publish(signInUpdate{attempt: attempt, token: token, err: err})
	}()
}

// Cancel stops asking for or waiting on a code. Late results of the
// abandoned attempt are ignored, except a key, which Sync still saves.
func (flow *CatSignInFlow) Cancel() {
	if flow.cancel != nil {
		flow.cancel()
		flow.cancel = nil
	}
	flow.attempt++
}

// Busy reports whether a key is being exchanged or is not saved yet. That
// short window is the only sign-in work a power action waits for: waiting
// for approval is cancelled instead, and the account check starts only after
// the key is saved.
func (flow *CatSignInFlow) Busy() bool { return flow != nil && flow.keys.Load() > 0 }

// YieldToPower gives way to a power action. A sign-in that is getting a code
// or waiting for approval is cancelled, and it returns true so the caller
// closes the screen. Otherwise nothing changes; see Busy.
func (flow *CatSignInFlow) YieldToPower(model *appui.SignInModel) bool {
	if model == nil || flow.Busy() {
		return false
	}
	if model.State != appui.SignInStarting && model.State != appui.SignInWaiting {
		return false
	}
	logger.Info("sign-in: cancelled for a power action")
	flow.Cancel()
	return true
}

// Idle reports whether nothing is left to deliver, so the app can drop a
// flow whose screen is closed.
func (flow *CatSignInFlow) Idle() bool {
	return flow.work.Load() == 0 && len(flow.updates) == 0
}

func (flow *CatSignInFlow) publish(update signInUpdate) {
	flow.updates <- update
	if flow.wake != nil {
		flow.wake()
	}
}

// Sync applies finished work and reports whether model changed. model is
// nil once the screen is closed; keys and account checks are still applied.
func (flow *CatSignInFlow) Sync(model *appui.SignInModel) bool {
	changed := false
	for {
		select {
		case update := <-flow.updates:
			if flow.apply(model, update) {
				changed = true
			}
		default:
			return changed
		}
	}
}

// apply handles one result. Only the current attempt updates a shown model;
// a key or an account check is applied whatever the attempt.
func (flow *CatSignInFlow) apply(model *appui.SignInModel, update signInUpdate) bool {
	if update.attempt != flow.attempt {
		model = nil
	}
	switch {
	case update.token != "":
		flow.store(model, update)
	case update.checked:
		flow.finish(model, update)
	case model == nil:
		return false
	case update.err != nil:
		flow.fail(model, update.err)
	case update.login != nil:
		model.State = appui.SignInWaiting
		model.UserCode, model.QRURL = update.login.UserCode, update.login.QRURL
		model.Expires = update.login.Expires
	}
	return model != nil
}

// store saves a key itch.io issued and starts the account check.
func (flow *CatSignInFlow) store(model *appui.SignInModel, update signInUpdate) {
	defer flow.keys.Add(-1)
	if err := flow.account.Store(update.token); err != nil {
		logger.Error("sign-in: %v", err)
		if model != nil {
			model.State, model.Heading, model.CanRetry = appui.SignInError, "Couldn't save the sign-in", true
			model.Detail = "The SD card could not be written. Press A to try again."
		}
		return
	}
	if model != nil {
		model.State = appui.SignInChecking
	}
	attempt, key := update.attempt, update.token
	flow.work.Add(1)
	go func() {
		defer flow.work.Add(-1)
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()
		user, owned, err := flow.client.ValidateAPIKeyContext(ctx, key)
		flow.publish(signInUpdate{attempt: attempt, checked: true, key: key, user: user, owned: owned, err: err})
	}()
}

// finish records the account after approval. A network failure here keeps
// the sign-in; the owned games load at the next start. A check of a key that
// is no longer the stored one (you signed out or in again) changes nothing.
func (flow *CatSignInFlow) finish(model *appui.SignInModel, update signInUpdate) {
	current := update.key == flow.account.cfg.Credential()
	if !current {
		logger.Debug("sign-in: discarded the account check of a replaced key")
	}
	if model == nil {
		model = &appui.SignInModel{} // record the outcome without a screen
	}
	model.State = appui.SignInDone
	switch {
	case current && errors.Is(update.err, itchio.ErrSignInRejected):
		if err := flow.account.SignOut(); err != nil {
			logger.Error("sign-in: %v", err)
		}
		model.State, model.CanRetry = appui.SignInError, true
		model.Heading, model.Detail = "itch.io didn't accept the sign-in", "Press A to try again."
	case update.err != nil:
		logger.Warn("sign-in: account check failed: %v", update.err)
		model.Heading = "Signed in to itch.io"
		model.Detail = "Your owned games will load the next time you're online."
	default:
		if current {
			if err := flow.account.Validated(update.user, update.owned); err != nil {
				logger.Error("sign-in: %v", err)
			}
		}
		model.Heading = "Signed in to itch.io"
		if update.user != "" {
			model.Heading = "Signed in as " + update.user
		}
		model.Detail = fmt.Sprintf("%d owned game(s) found.", len(update.owned))
	}
}

func (flow *CatSignInFlow) fail(model *appui.SignInModel, err error) {
	model.State, model.CanRetry = appui.SignInError, true
	switch {
	case errors.Is(err, itchio.ErrSignInUnavailable):
		model.Heading = "Sign-in is unavailable"
		model.Detail = "itch.io could not start sign-in for Leaf. Free games still download without signing in."
	case errors.Is(err, itchio.ErrSignInExpired):
		model.Heading, model.Detail = "The code expired", "Press A for a new code."
	case errors.Is(err, itchio.ErrSignInDenied):
		model.Heading, model.Detail = "Sign-in was declined", "Press A to try again."
	case errors.Is(err, itchio.ErrRateLimited):
		model.Heading, model.Detail = "itch.io is busy", "Wait a moment, then press A to try again."
	default:
		logger.Warn("sign-in: %v", err)
		model.Heading, model.Detail = "Can't reach itch.io", "Check the connection, then press A to try again."
	}
}
