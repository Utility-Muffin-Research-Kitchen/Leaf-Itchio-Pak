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
		{"cover art loading", catPollState{ImagesLoading: true}, 50},
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
