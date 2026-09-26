//go:build !headless

package ui

import (
	"context"
	"errors"
	"fmt"

	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/appui"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/itchio"
	"github.com/Utility-Muffin-Research-Kitchen/Leaf-Itchio-Pak/internal/logger"
)

type signInUpdate struct {
	attempt   uint64
	login     *itchio.DeviceLogin
	token     string
	checked   bool
	user      string
	owned     []itchio.OwnedGame
	err       error
	cancelled bool
}

// CatSignInFlow runs one QR sign-in: it gets a code, waits for approval on
// the user's phone, stores the key through Account, and loads the account's
// owned games. Network work runs on goroutines; Sync applies the results on
// the UI goroutine, which owns the config.
type CatSignInFlow struct {
	client  *itchio.Client
	account *Account
	wake    func()

	updates chan signInUpdate
	attempt uint64
	cancel  context.CancelFunc
}

func NewCatSignInFlow(client *itchio.Client, account *Account, wake func()) (*CatSignInFlow, *appui.SignInModel) {
	flow := &CatSignInFlow{client: client, account: account, wake: wake, updates: make(chan signInUpdate, 4)}
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
	go func() {
		login, err := flow.client.BeginDeviceLogin(ctx)
		flow.publish(signInUpdate{attempt: attempt, login: login, err: err, cancelled: ctx.Err() != nil})
		if err != nil {
			return
		}
		token, err := login.Wait(ctx)
		flow.publish(signInUpdate{attempt: attempt, token: token, err: err, cancelled: ctx.Err() != nil})
	}()
}

// Cancel stops waiting; late results of the abandoned attempt are ignored.
func (flow *CatSignInFlow) Cancel() {
	if flow.cancel != nil {
		flow.cancel()
		flow.cancel = nil
	}
	flow.attempt++
}

func (flow *CatSignInFlow) publish(update signInUpdate) {
	flow.updates <- update
	if flow.wake != nil {
		flow.wake()
	}
}

// SignInBusy reports whether a sign-in is in progress on model.
func SignInBusy(model *appui.SignInModel) bool {
	return model != nil && model.State != appui.SignInDone && model.State != appui.SignInError
}

// Sync applies finished work to model and reports whether anything changed.
func (flow *CatSignInFlow) Sync(model *appui.SignInModel) bool {
	changed := false
	for {
		select {
		case update := <-flow.updates:
			if update.attempt != flow.attempt || update.cancelled {
				continue
			}
			flow.apply(model, update)
			changed = true
		default:
			return changed
		}
	}
}

func (flow *CatSignInFlow) apply(model *appui.SignInModel, update signInUpdate) {
	switch {
	case update.checked:
		flow.finish(model, update)
	case update.err != nil:
		flow.fail(model, update.err)
	case update.login != nil:
		model.State = appui.SignInWaiting
		model.UserCode, model.QRURL, model.ManualURL = update.login.UserCode, update.login.QRURL, update.login.ManualURL
		model.Expires = update.login.Expires
	case update.token != "":
		if err := flow.account.Store(update.token); err != nil {
			logger.Error("sign-in: %v", err)
			model.State, model.Heading, model.CanRetry = appui.SignInError, "Couldn't save the sign-in", true
			model.Detail = "The SD card could not be written. Press A to try again."
			return
		}
		model.State = appui.SignInChecking
		attempt, token := update.attempt, update.token
		go func() {
			user, owned, err := flow.client.ValidateAPIKey(token)
			flow.publish(signInUpdate{attempt: attempt, checked: true, user: user, owned: owned, err: err})
		}()
	}
}

// finish records the account after approval. A network failure here keeps
// the sign-in; the owned games load at the next start.
func (flow *CatSignInFlow) finish(model *appui.SignInModel, update signInUpdate) {
	model.State = appui.SignInDone
	switch {
	case errors.Is(update.err, itchio.ErrSignInRejected):
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
		if err := flow.account.Validated(update.user, update.owned); err != nil {
			logger.Error("sign-in: %v", err)
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
		model.Heading = "Sign-in isn't available yet"
		model.Detail = "itch.io hasn't enabled sign-in for Leaf yet. Free games still download without signing in."
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
