package main

import (
	"testing"
	"time"
)

func TestCatPollDelayIdlesWithNothingInFlight(t *testing.T) {
	if milliseconds, poll := catPollDelay(catPollState{}); poll {
		t.Fatalf("catPollDelay(idle) = %d ms, want no poll so the loop sleeps until input", milliseconds)
	}
}

func TestCatPollDelayPollsForEveryWorkerAScreenWaitsOn(t *testing.T) {
	tests := []struct {
		name  string
		state catPollState
		want  uint32
	}{
		{"GIF frame due", catPollState{Animated: true, AnimationIn: 80 * time.Millisecond}, 80},
		{"GIF frame overdue", catPollState{Animated: true, AnimationIn: -time.Millisecond}, 1},
		{"first catalogue page loading", catPollState{ListLoading: true}, 100},
		{"game page loading", catPollState{DetailLoading: true}, 100},
		{"file list loading", catPollState{FilesLoading: true}, 100},
		{"archive inspection", catPollState{ArchiveInspecting: true}, 100},
		{"download running", catPollState{Downloading: true}, 50},
		{"catalogue refresh", catPollState{CacheRefreshing: true}, 100},
		{"sign-in code countdown", catPollState{SignInWaiting: true}, 1000},
		{"sign-in starting or checking", catPollState{SignInChecking: true}, 100},
		{"background catalogue build", catPollState{CatalogBuilding: true}, 250},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			milliseconds, poll := catPollDelay(test.state)
			if !poll || milliseconds != test.want {
				t.Fatalf("catPollDelay = %d ms, poll=%v; want %d ms", milliseconds, poll, test.want)
			}
		})
	}
}

// F3: after a download or a Manage rename the screen said "Requesting Leaf
// library rescan…" for 12 to 42 s on the device. jawakad answered within the
// IPC client's 2 s deadline, but nothing asked for a frame while the answer
// was outstanding, so it was shown only after the next button press or at
// the next wall-clock minute.
func TestCatPollDelayPollsWhileALibraryRescanRequestIsOutstanding(t *testing.T) {
	milliseconds, poll := catPollDelay(catPollState{LibraryScanRequested: true})
	if !poll || milliseconds > 50 {
		t.Fatalf("catPollDelay(rescan requested) = %d ms, poll=%v; want a poll within 50 ms", milliseconds, poll)
	}
}

// Settings shows "Checking your itch.io account…" until its worker reports
// back, the same wait as the rescan status.
func TestCatPollDelayPollsWhileSettingsChecksTheAccount(t *testing.T) {
	milliseconds, poll := catPollDelay(catPollState{AccountChecking: true})
	if !poll || milliseconds > 100 {
		t.Fatalf("catPollDelay(account check) = %d ms, poll=%v; want a poll within 100 ms", milliseconds, poll)
	}
}

// The loop must come back by the earliest time any worker needs, so a slow
// poll for one worker never delays a faster one.
func TestCatPollDelayUsesTheShortestDelayInFlight(t *testing.T) {
	tests := []struct {
		name  string
		state catPollState
		want  uint32
	}{
		{"rescan during a background catalogue build",
			catPollState{CatalogBuilding: true, LibraryScanRequested: true}, 50},
		{"rescan while a slow GIF shows",
			catPollState{Animated: true, AnimationIn: 400 * time.Millisecond, LibraryScanRequested: true}, 50},
		{"download during the sign-in countdown",
			catPollState{SignInWaiting: true, Downloading: true}, 50},
		{"GIF frame sooner than download polling",
			catPollState{Animated: true, AnimationIn: 20 * time.Millisecond, Downloading: true}, 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			milliseconds, poll := catPollDelay(test.state)
			if !poll || milliseconds != test.want {
				t.Fatalf("catPollDelay = %d ms, poll=%v; want %d ms", milliseconds, poll, test.want)
			}
		})
	}
}
