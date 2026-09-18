package store

import (
	"testing"
	"time"
)

func TestMatchTimersResetReplacesPendingTimer(t *testing.T) {
	clock := &fakeClock{}
	timers := NewMatchTimers(clock)

	timers.Reset("m1", time.Second, func() {})
	first := clock.latest(t)
	timers.Reset("m1", time.Second, func() {})

	if !first.stopped.Load() {
		t.Error("Reset left the previous timer running")
	}
	if clock.latest(t).stopped.Load() {
		t.Error("Reset stopped the new timer")
	}
}

func TestMatchTimersKeepsMatchesIndependent(t *testing.T) {
	clock := &fakeClock{}
	timers := NewMatchTimers(clock)

	timers.Reset("m1", time.Second, func() {})
	m1 := clock.latest(t)
	timers.Reset("m2", time.Second, func() {})

	if m1.stopped.Load() {
		t.Error("scheduling m2 stopped m1's timer")
	}
}

func TestMatchTimersStopForgetsMatch(t *testing.T) {
	clock := &fakeClock{}
	timers := NewMatchTimers(clock)

	timers.Reset("m1", time.Second, func() {})
	timer := clock.latest(t)
	timers.Stop("m1")

	if !timer.stopped.Load() {
		t.Error("Stop did not stop the timer")
	}
	if _, ok := timers.timers["m1"]; ok {
		t.Error("Stop kept the match in the map")
	}
	timers.Stop("m1") // must be a no-op
}
