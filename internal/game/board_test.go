package game

import (
	"errors"
	"testing"
)

func gridFrom(t *testing.T, marks ...Mark) *Grid {
	t.Helper()
	g := NewGrid()
	for cell, m := range marks {
		if m == Empty {
			continue
		}
		if err := g.Place(cell, m); err != nil {
			t.Fatalf("place %v at %d: %v", m, cell, err)
		}
	}
	return g
}

func TestGridWinner(t *testing.T) {
	tests := []struct {
		name  string
		marks []Mark
		want  Mark
	}{
		{"empty board", nil, Empty},
		{"top row", []Mark{X, X, X}, X},
		{"middle row", []Mark{Empty, Empty, Empty, O, O, O}, O},
		{"left column", []Mark{X, Empty, Empty, X, Empty, Empty, X}, X},
		{"main diagonal", []Mark{O, Empty, Empty, Empty, O, Empty, Empty, Empty, O}, O},
		{"anti diagonal", []Mark{Empty, Empty, X, Empty, X, Empty, X}, X},
		{"no line", []Mark{X, O, X, X, O, O, O, X, X}, Empty},
		{"mixed line", []Mark{X, X, O}, Empty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gridFrom(t, tt.marks...).Winner(); got != tt.want {
				t.Errorf("Winner() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGridIsFull(t *testing.T) {
	if gridFrom(t, X, O, X).IsFull() {
		t.Error("partly filled board reported full")
	}
	if !gridFrom(t, X, O, X, X, O, O, O, X, X).IsFull() {
		t.Error("filled board reported not full")
	}
}

func TestGridPlaceRejectsInvalidMoves(t *testing.T) {
	g := gridFrom(t, X)
	tests := []struct {
		name string
		cell int
		mark Mark
		want error
	}{
		{"below range", -1, O, ErrCellOutOfRange},
		{"above range", CellCount, O, ErrCellOutOfRange},
		{"occupied", 0, O, ErrCellOccupied},
		{"empty mark", 1, Empty, ErrInvalidMark},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := g.Place(tt.cell, tt.mark); !errors.Is(err, tt.want) {
				t.Errorf("Place() error = %v, want %v", err, tt.want)
			}
		})
	}
	if got := g.Cells(); got[0] != X || got[1] != Empty {
		t.Errorf("rejected moves changed the board: %v", got)
	}
}
