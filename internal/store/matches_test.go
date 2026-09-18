package store

import (
	"testing"

	"github.com/brabeem/tictactoe/internal/game"
)

type stubMatch struct {
	id, x, o string
}

func (m *stubMatch) ID() string                              { return m.id }
func (m *stubMatch) Players() (string, string)               { return m.x, m.o }
func (m *stubMatch) Move(string, int) (game.Snapshot, error) { return game.Snapshot{}, nil }
func (m *stubMatch) Snapshot() game.Snapshot                 { return game.Snapshot{} }
func (m *stubMatch) Expire(int) (string, bool)               { return "", false }
func (m *stubMatch) End()                                    {}

func TestMatchIndexFindsMatchByEitherPlayer(t *testing.T) {
	idx := NewMatchIndex()
	m := &stubMatch{"m1", "alice", "bob"}
	idx.Add(m)

	for _, id := range []string{"alice", "bob"} {
		if got, ok := idx.ByPlayer(id); !ok || got != m {
			t.Errorf("ByPlayer(%q) = %v, %v", id, got, ok)
		}
	}
	if _, ok := idx.ByPlayer("carol"); ok {
		t.Error("found a match for a player who has none")
	}
}

func TestMatchIndexRemoveReportsOnlyOnce(t *testing.T) {
	idx := NewMatchIndex()
	m := &stubMatch{"m1", "alice", "bob"}
	idx.Add(m)

	if !idx.Remove(m) {
		t.Fatal("first Remove returned false")
	}
	if idx.Remove(m) {
		t.Error("second Remove returned true")
	}
	if _, ok := idx.ByPlayer("alice"); ok {
		t.Error("match still indexed after Remove")
	}
}

func TestMatchIndexRemoveLeavesNewerMatch(t *testing.T) {
	idx := NewMatchIndex()
	old := &stubMatch{"m1", "alice", "bob"}
	idx.Add(old)
	idx.Remove(old)
	newer := &stubMatch{"m2", "alice", "carol"}
	idx.Add(newer)

	if idx.Remove(old) {
		t.Error("removing a stale match reported success")
	}
	if got, ok := idx.ByPlayer("alice"); !ok || got != newer {
		t.Errorf("stale Remove disturbed the newer match: %v, %v", got, ok)
	}
}
