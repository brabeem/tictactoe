package game

import (
	"errors"
	"testing"
)

type move struct {
	player string
	cell   int
}

func play(t *testing.T, m *Match, moves ...move) Snapshot {
	t.Helper()
	var snap Snapshot
	for _, mv := range moves {
		var err error
		if snap, err = m.Move(mv.player, mv.cell); err != nil {
			t.Fatalf("%s at %d: %v", mv.player, mv.cell, err)
		}
	}
	return snap
}

func TestMatchStartsWithXToMove(t *testing.T) {
	snap := NewMatch("m", "alice", "bob", NewGrid()).Snapshot()
	if snap.Turn != "alice" || snap.Over || snap.Version != 0 {
		t.Errorf("initial snapshot = %+v", snap)
	}
}

func TestMatchAlternatesTurns(t *testing.T) {
	m := NewMatch("m", "alice", "bob", NewGrid())

	snap := play(t, m, move{"alice", 0})
	if snap.Turn != "bob" || snap.Version != 1 || snap.Cells[0] != X {
		t.Fatalf("after alice: %+v", snap)
	}
	snap = play(t, m, move{"bob", 4})
	if snap.Turn != "alice" || snap.Version != 2 || snap.Cells[4] != O {
		t.Fatalf("after bob: %+v", snap)
	}
}

func TestMatchRejectsInvalidMoves(t *testing.T) {
	m := NewMatch("m", "alice", "bob", NewGrid())
	play(t, m, move{"alice", 0})

	tests := []struct {
		name   string
		player string
		cell   int
		want   error
	}{
		{"out of turn", "alice", 1, ErrNotYourTurn},
		{"stranger", "mallory", 1, ErrNotAPlayer},
		{"occupied cell", "bob", 0, ErrCellOccupied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := m.Move(tt.player, tt.cell); !errors.Is(err, tt.want) {
				t.Errorf("Move() error = %v, want %v", err, tt.want)
			}
		})
	}
	if v := m.Snapshot().Version; v != 1 {
		t.Errorf("rejected moves changed the version to %d", v)
	}
}

func TestMatchWin(t *testing.T) {
	m := NewMatch("m", "alice", "bob", NewGrid())
	snap := play(t, m,
		move{"alice", 0}, move{"bob", 3},
		move{"alice", 1}, move{"bob", 4},
		move{"alice", 2},
	)
	if !snap.Over || snap.Winner != "alice" || snap.Draw || snap.Turn != "" {
		t.Fatalf("final snapshot = %+v", snap)
	}
	if _, err := m.Move("bob", 5); !errors.Is(err, ErrGameOver) {
		t.Errorf("move after win: error = %v, want %v", err, ErrGameOver)
	}
}

func TestMatchDraw(t *testing.T) {
	m := NewMatch("m", "alice", "bob", NewGrid())
	// X O X
	// X O O
	// O X X
	snap := play(t, m,
		move{"alice", 0}, move{"bob", 1},
		move{"alice", 2}, move{"bob", 4},
		move{"alice", 3}, move{"bob", 5},
		move{"alice", 7}, move{"bob", 6},
		move{"alice", 8},
	)
	if !snap.Over || !snap.Draw || snap.Winner != "" {
		t.Fatalf("final snapshot = %+v", snap)
	}
}

func TestMatchExpireEndsGameForPlayerToMove(t *testing.T) {
	m := NewMatch("m", "alice", "bob", NewGrid())
	snap := play(t, m, move{"alice", 0})

	idle, ok := m.Expire(snap.Version)
	if !ok || idle != "bob" {
		t.Fatalf("Expire() = %q, %v, want bob, true", idle, ok)
	}
	if _, err := m.Move("bob", 1); !errors.Is(err, ErrGameOver) {
		t.Errorf("move after expiry: error = %v, want %v", err, ErrGameOver)
	}
	if _, ok := m.Expire(snap.Version); ok {
		t.Error("second Expire reported ending the game again")
	}
}

func TestMatchExpireIgnoresStaleVersion(t *testing.T) {
	m := NewMatch("m", "alice", "bob", NewGrid())
	stale := m.Snapshot().Version
	play(t, m, move{"alice", 0})

	if _, ok := m.Expire(stale); ok {
		t.Fatal("Expire ended the game even though a move landed after the timer was armed")
	}
	play(t, m, move{"bob", 1})
}

func TestMatchEndStopsFurtherMoves(t *testing.T) {
	m := NewMatch("m", "alice", "bob", NewGrid())
	m.End()
	if _, err := m.Move("alice", 0); !errors.Is(err, ErrGameOver) {
		t.Errorf("Move after End: error = %v, want %v", err, ErrGameOver)
	}
}

// failingBoard shows that Match depends only on the Board interface.
type failingBoard struct {
	Grid
	err error
}

func (b *failingBoard) Place(int, Mark) error { return b.err }

func TestMatchPropagatesBoardErrorWithoutChangingState(t *testing.T) {
	boom := errors.New("boom")
	m := NewMatch("m", "alice", "bob", &failingBoard{err: boom})

	if _, err := m.Move("alice", 0); !errors.Is(err, boom) {
		t.Fatalf("Move() error = %v, want %v", err, boom)
	}
	if snap := m.Snapshot(); snap.Version != 0 || snap.Turn != "alice" {
		t.Errorf("failed move changed state: %+v", snap)
	}
}
