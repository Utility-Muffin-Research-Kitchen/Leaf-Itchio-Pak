package main

import "time"

// catPollState is the work in flight that the Cat loop must collect without
// waiting for input. Context.Wake cannot end the idle wait in Present on the
// device: Catastrophe sleeps in poll() on the gamepad's evdev nodes until a
// button, a requested frame or the next wall-clock minute. So a screen that
// waits for a worker asks for frames until the worker reports back.
type catPollState struct {
	// AnimationIn is the time to the next frame of a visible GIF; Animated
	// says whether one is showing.
	AnimationIn       time.Duration
	Animated          bool
	ImagesLoading     bool
	ListLoading       bool
	DetailLoading     bool
	FilesLoading      bool
	ArchiveInspecting bool
	Downloading       bool
	CacheRefreshing   bool
	SignInWaiting     bool
	SignInChecking    bool
	CatalogBuilding   bool
	// LibraryScanRequested is true while a Leaf library rescan request from
	// a download, Manage or Rename has not answered yet.
	LibraryScanRequested bool
	// AccountChecking is true while Settings checks the itch.io sign-in.
	AccountChecking bool
}

// catPollDelay returns how soon, in milliseconds, the loop must run again to
// collect that work, and false when it may sleep until the next input. With
// several workers in flight it is the shortest delay any of them needs, so a
// slow poll for one never holds back another.
func catPollDelay(state catPollState) (uint32, bool) {
	var shortest uint32
	poll := false
	need := func(milliseconds uint32, inFlight bool) {
		if inFlight && (!poll || milliseconds < shortest) {
			shortest, poll = milliseconds, true
		}
	}
	if state.Animated {
		milliseconds := state.AnimationIn.Milliseconds()
		if milliseconds < 1 {
			milliseconds = 1
		}
		need(uint32(milliseconds), true)
	}
	need(50, state.ImagesLoading)
	need(100, state.ListLoading || state.DetailLoading || state.FilesLoading || state.ArchiveInspecting)
	need(50, state.Downloading)
	need(100, state.CacheRefreshing)
	// The code's countdown changes once a second.
	need(1000, state.SignInWaiting)
	need(100, state.SignInChecking)
	need(250, state.CatalogBuilding)
	// jawakad answers within the IPC client's 2 s deadline; show its answer
	// as soon as it arrives.
	need(50, state.LibraryScanRequested)
	need(100, state.AccountChecking)
	return shortest, poll
}
