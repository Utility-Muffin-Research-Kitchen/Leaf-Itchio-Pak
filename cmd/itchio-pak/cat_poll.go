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
}

// catPollDelay returns how soon, in milliseconds, the loop must run again to
// collect that work, and false when it may sleep until the next input.
func catPollDelay(state catPollState) (uint32, bool) {
	switch {
	case state.Animated:
		milliseconds := state.AnimationIn.Milliseconds()
		if milliseconds < 1 {
			milliseconds = 1
		}
		return uint32(milliseconds), true
	case state.ImagesLoading:
		return 50, true
	case state.ListLoading, state.DetailLoading, state.FilesLoading, state.ArchiveInspecting:
		return 100, true
	case state.Downloading:
		return 50, true
	case state.CacheRefreshing:
		return 100, true
	case state.SignInWaiting:
		// The code's countdown changes once a second.
		return 1000, true
	case state.SignInChecking:
		return 100, true
	case state.CatalogBuilding:
		return 250, true
	}
	return 0, false
}
