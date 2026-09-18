package store

import (
	"sync"
	"time"
)

// Timer is a pending callback that can be cancelled.
type Timer interface {
	Stop() bool
}

// Clock schedules callbacks. It exists so tests can fire timers on demand
// instead of sleeping.
type Clock interface {
	AfterFunc(d time.Duration, f func()) Timer
}

// RealClock is the Clock backed by the time package.
type RealClock struct{}

func (RealClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

// MatchTimers is the in-memory TurnTimers: at most one pending timer per match.
type MatchTimers struct {
	clock Clock

	// A plain Mutex: every operation modifies the map.
	mu     sync.Mutex
	timers map[string]Timer
}

func NewMatchTimers(clock Clock) *MatchTimers {
	return &MatchTimers{
		clock:  clock,
		timers: make(map[string]Timer),
	}
}

// Reset cancels the match's pending timer, if any, and schedules fire after d.
func (t *MatchTimers) Reset(matchID string, d time.Duration, fire func()) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if old, ok := t.timers[matchID]; ok {
		old.Stop()
	}
	t.timers[matchID] = t.clock.AfterFunc(d, fire)
}

// Stop cancels the match's pending timer and forgets the match. A callback
// that has already started still runs; callers guard against that themselves.
func (t *MatchTimers) Stop(matchID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if old, ok := t.timers[matchID]; ok {
		old.Stop()
		delete(t.timers, matchID)
	}
}
