package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brabeem/tictactoe/internal/game"
	"github.com/brabeem/tictactoe/internal/protocol"
)

// fakePeer records every message the store sends to it.
type fakePeer struct {
	id     string
	inbox  chan protocol.Envelope
	closed atomic.Bool
}

func newFakePeer(id string) *fakePeer {
	return &fakePeer{id: id, inbox: make(chan protocol.Envelope, 64)}
}

func (p *fakePeer) ID() string { return p.id }
func (p *fakePeer) Close()     { p.closed.Store(true) }
func (p *fakePeer) Send(env protocol.Envelope) error {
	p.inbox <- env
	return nil
}

// expect returns the next message, failing if it is not of type want.
func (p *fakePeer) expect(t *testing.T, want protocol.Type, payload any) {
	t.Helper()
	select {
	case env := <-p.inbox:
		if env.Type != want {
			t.Fatalf("%s: got %q (%s), want %q", p.id, env.Type, env.Payload, want)
		}
		if payload != nil {
			if err := json.Unmarshal(env.Payload, payload); err != nil {
				t.Fatalf("%s: decode %q payload: %v", p.id, want, err)
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: timed out waiting for %q", p.id, want)
	}
}

// expectNothing fails if a message arrives within a short grace period.
func (p *fakePeer) expectNothing(t *testing.T) {
	t.Helper()
	select {
	case env := <-p.inbox:
		t.Fatalf("%s: unexpected %q (%s)", p.id, env.Type, env.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

// fakeClock never fires by itself; tests fire timers explicitly.
type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

type fakeTimer struct {
	fire    func()
	stopped atomic.Bool
}

func (t *fakeTimer) Stop() bool { return !t.stopped.Swap(true) }

func (c *fakeClock) AfterFunc(_ time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{fire: f}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) latest(t *testing.T) *fakeTimer {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.timers) == 0 {
		t.Fatal("no timer was scheduled")
	}
	return c.timers[len(c.timers)-1]
}

func newTestStore(t *testing.T) (*Store, *fakeClock) {
	t.Helper()
	var n atomic.Int64
	clock := &fakeClock{}
	s := New(Deps{
		Hub:     NewHub(),
		Queue:   NewWaitQueue(16),
		Matches: NewMatchIndex(),
		Timers:  NewMatchTimers(clock),
		NewMatch: func(id, x, o string) Match {
			return game.NewMatch(id, x, o, game.NewGrid())
		},
		NewID:       func() string { return fmt.Sprintf("match-%d", n.Add(1)) },
		MoveTimeout: 15 * time.Second,
	})
	return s, clock
}

func runStore(t *testing.T, s *Store) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func connect(t *testing.T, s *Store, id string) *fakePeer {
	t.Helper()
	p := newFakePeer(id)
	s.Connect(p)
	p.expect(t, protocol.TypeWelcome, nil)
	p.expect(t, protocol.TypeWaiting, nil)
	return p
}

// startMatch connects two players and consumes the opening messages.
func startMatch(t *testing.T, s *Store) (x, o *fakePeer) {
	t.Helper()
	x = connect(t, s, "alice")
	o = connect(t, s, "bob")
	for _, p := range []*fakePeer{x, o} {
		p.expect(t, protocol.TypeMatchFound, nil)
		p.expect(t, protocol.TypeState, nil)
	}
	return x, o
}

func sendMove(s *Store, playerID string, cell int) {
	env, _ := protocol.NewEnvelope(protocol.TypeMove, protocol.Move{Cell: &cell})
	s.HandleMessage(playerID, env)
}

func TestRunPairsTwoPlayers(t *testing.T) {
	s, _ := newTestStore(t)
	runStore(t, s)
	alice := connect(t, s, "alice")
	bob := connect(t, s, "bob")

	var found protocol.MatchFound
	var state protocol.State

	alice.expect(t, protocol.TypeMatchFound, &found)
	if found.OpponentID != "bob" || found.Mark != "X" {
		t.Errorf("alice match_found = %+v", found)
	}
	alice.expect(t, protocol.TypeState, &state)
	if !state.YourTurn || state.YourMark != "X" {
		t.Errorf("alice initial state = %+v", state)
	}

	bob.expect(t, protocol.TypeMatchFound, &found)
	if found.OpponentID != "alice" || found.Mark != "O" {
		t.Errorf("bob match_found = %+v", found)
	}
	bob.expect(t, protocol.TypeState, &state)
	if state.YourTurn || state.YourMark != "O" {
		t.Errorf("bob initial state = %+v", state)
	}
}

func TestMoveOutOfTurnIsRejected(t *testing.T) {
	s, _ := newTestStore(t)
	runStore(t, s)
	_, bob := startMatch(t, s)

	sendMove(s, "bob", 0)

	var e protocol.Error
	bob.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.CodeNotYourTurn {
		t.Errorf("error code = %q, want %q", e.Code, protocol.CodeNotYourTurn)
	}
}

func TestMoveWithoutCellIsRejected(t *testing.T) {
	s, _ := newTestStore(t)
	runStore(t, s)
	alice, _ := startMatch(t, s)

	s.HandleMessage("alice", protocol.Envelope{Type: protocol.TypeMove, Payload: json.RawMessage(`{}`)})

	var e protocol.Error
	alice.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.CodeBadRequest {
		t.Errorf("error code = %q, want %q", e.Code, protocol.CodeBadRequest)
	}
}

func TestWinningMoveEndsMatchForBothPlayers(t *testing.T) {
	s, _ := newTestStore(t)
	runStore(t, s)
	alice, bob := startMatch(t, s)

	moves := []struct {
		player string
		cell   int
	}{{"alice", 0}, {"bob", 3}, {"alice", 1}, {"bob", 4}, {"alice", 2}}
	var state protocol.State
	for _, mv := range moves {
		sendMove(s, mv.player, mv.cell)
		alice.expect(t, protocol.TypeState, &state)
		bob.expect(t, protocol.TypeState, &state)
	}

	if !state.GameOver || state.Winner != "alice" {
		t.Fatalf("final state = %+v", state)
	}

	// The finished match is deregistered, so the winner can queue again.
	s.HandleMessage("alice", protocol.Envelope{Type: protocol.TypeRestart})
	alice.expect(t, protocol.TypeWaiting, nil)
}

func TestRestartDuringMatchIsRejected(t *testing.T) {
	s, _ := newTestStore(t)
	runStore(t, s)
	alice, _ := startMatch(t, s)

	s.HandleMessage("alice", protocol.Envelope{Type: protocol.TypeRestart})

	var e protocol.Error
	alice.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.CodeMatchInProgress {
		t.Errorf("error code = %q, want %q", e.Code, protocol.CodeMatchInProgress)
	}
}

func TestDisconnectNotifiesOpponent(t *testing.T) {
	s, _ := newTestStore(t)
	runStore(t, s)
	_, bob := startMatch(t, s)

	s.Disconnect("alice")

	bob.expect(t, protocol.TypeOpponentLeft, nil)
	sendMove(s, "bob", 0)
	var e protocol.Error
	bob.expect(t, protocol.TypeError, &e)
	if e.Code != protocol.CodeNotInMatch {
		t.Errorf("error code = %q, want %q", e.Code, protocol.CodeNotInMatch)
	}
}

func TestRunSkipsPlayersWhoLeftTheQueue(t *testing.T) {
	s, _ := newTestStore(t)
	connect(t, s, "ghost")
	s.Disconnect("ghost")
	bob := connect(t, s, "bob")
	carol := connect(t, s, "carol")
	runStore(t, s)

	var found protocol.MatchFound
	bob.expect(t, protocol.TypeMatchFound, &found)
	if found.OpponentID != "carol" {
		t.Errorf("bob was paired with %q, want carol", found.OpponentID)
	}
	carol.expect(t, protocol.TypeMatchFound, nil)
}

func TestMoveTimeoutDropsIdlePlayerAndFreesOpponent(t *testing.T) {
	s, clock := newTestStore(t)
	runStore(t, s)
	alice, bob := startMatch(t, s)

	// It is alice's turn and she never moves.
	clock.latest(t).fire()

	alice.expect(t, protocol.TypeTimedOut, nil)
	bob.expect(t, protocol.TypeOpponentLeft, nil)
	if !alice.closed.Load() {
		t.Error("idle player's connection was not closed")
	}
	if _, ok := s.hub.Get("alice"); ok {
		t.Error("idle player is still in the hub")
	}
	for _, id := range []string{"alice", "bob"} {
		if _, ok := s.matches.ByPlayer(id); ok {
			t.Errorf("%s is still indexed to the expired match", id)
		}
	}

	s.HandleMessage("bob", protocol.Envelope{Type: protocol.TypeRestart})
	bob.expect(t, protocol.TypeWaiting, nil)
}

func TestMoveReplacesTurnTimer(t *testing.T) {
	s, clock := newTestStore(t)
	runStore(t, s)
	alice, bob := startMatch(t, s)
	first := clock.latest(t)

	sendMove(s, "alice", 0)
	alice.expect(t, protocol.TypeState, nil)
	bob.expect(t, protocol.TypeState, nil)

	if !first.stopped.Load() {
		t.Error("move did not stop the previous turn timer")
	}
	if clock.latest(t) == first {
		t.Fatal("move did not schedule a timer for the next turn")
	}

	// A timer that had already started firing when the move landed must not
	// end the game.
	first.fire()
	alice.expectNothing(t)
	bob.expectNothing(t)
	if _, ok := s.matches.ByPlayer("bob"); !ok {
		t.Error("stale timer ended the match")
	}
}

func TestFinishedMatchStopsTurnTimer(t *testing.T) {
	s, clock := newTestStore(t)
	runStore(t, s)
	alice, bob := startMatch(t, s)

	for _, mv := range []struct {
		player string
		cell   int
	}{{"alice", 0}, {"bob", 3}, {"alice", 1}, {"bob", 4}, {"alice", 2}} {
		sendMove(s, mv.player, mv.cell)
		alice.expect(t, protocol.TypeState, nil)
		bob.expect(t, protocol.TypeState, nil)
	}

	last := clock.latest(t)
	if !last.stopped.Load() {
		t.Error("finished match left its turn timer running")
	}
	last.fire()
	bob.expectNothing(t)
}

func TestDisconnectStopsTurnTimer(t *testing.T) {
	s, clock := newTestStore(t)
	runStore(t, s)
	_, bob := startMatch(t, s)
	timer := clock.latest(t)

	s.Disconnect("alice")
	bob.expect(t, protocol.TypeOpponentLeft, nil)

	if !timer.stopped.Load() {
		t.Error("abandoned match left its turn timer running")
	}
	timer.fire()
	bob.expectNothing(t)
}
