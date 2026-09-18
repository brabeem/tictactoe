package game

import "errors"

// Mark is the content of a single cell.
type Mark uint8

const (
	Empty Mark = iota
	X
	O
)

func (m Mark) String() string {
	switch m {
	case X:
		return "X"
	case O:
		return "O"
	default:
		return ""
	}
}

// CellCount is the number of cells on the board. Cells are indexed 0-8, row by row.
const CellCount = 9

var (
	ErrCellOutOfRange = errors.New("cell out of range")
	ErrCellOccupied   = errors.New("cell already occupied")
	ErrInvalidMark    = errors.New("invalid mark")
)

// Board holds the marks on a tic-tac-toe grid and knows the rules for a win.
type Board interface {
	Place(cell int, mark Mark) error
	Cells() [CellCount]Mark
	Winner() Mark
	IsFull() bool
}

var winningLines = [8][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
	{0, 4, 8}, {2, 4, 6},
}

// Grid is the standard 3x3 Board. It is not safe for concurrent use;
// Match serialises every access to it.
type Grid struct {
	cells [CellCount]Mark
}

func NewGrid() *Grid {
	return &Grid{}
}

func (g *Grid) Place(cell int, mark Mark) error {
	if cell < 0 || cell >= CellCount {
		return ErrCellOutOfRange
	}
	if mark != X && mark != O {
		return ErrInvalidMark
	}
	if g.cells[cell] != Empty {
		return ErrCellOccupied
	}
	g.cells[cell] = mark
	return nil
}

func (g *Grid) Cells() [CellCount]Mark {
	return g.cells
}

func (g *Grid) Winner() Mark {
	for _, line := range winningLines {
		m := g.cells[line[0]]
		if m != Empty && m == g.cells[line[1]] && m == g.cells[line[2]] {
			return m
		}
	}
	return Empty
}

func (g *Grid) IsFull() bool {
	for _, m := range g.cells {
		if m == Empty {
			return false
		}
	}
	return true
}
