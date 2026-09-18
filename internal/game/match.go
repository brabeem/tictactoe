package game

import (
	"errors"
	"sync"
)

var (
	ErrNotAPlayer  = errors.New("player is not in this match")
	ErrNotYourTurn = errors.New("not your turn")
	ErrGameOver    = errors.New("game is over")
)

// Snapshot is a consistent point-in-time copy of a match's state.
type Snapshot struct {
	Cells [CellCount]Mark
	// Version increments on every accepted move. States reach each player from
	// different goroutines, so clients use it to discard out-of-order updates.
	Version int
	Turn    string // player to move; empty once the game is over
	Over    bool
	Winner  string // winning player; empty while playing or on a draw
	Draw    bool
}

// Match is one game between two players. The first player plays X and moves first.
type Match struct {
	id      string
	players [2]string // index 0 plays X, index 1 plays O

	// A plain Mutex rather than an RWMutex: every Move reads and writes, and
	// read-only calls are rare, so a shared reader lock would buy nothing.
	mu      sync.Mutex
	board   Board
	turn    int
	version int
	over    bool
	winner  string
	draw    bool
}

// NewMatch creates a match played on board. The match takes ownership of
// board; the caller must not use it afterwards.
func NewMatch(id, x, o string, board Board) *Match {
	return &Match{
		id:      id,
		players: [2]string{x, o},
		board:   board,
	}
}

func (m *Match) ID() string {
	return m.id
}

// Players returns the X and O players. They never change after construction,
// so no lock is taken.
func (m *Match) Players() (x, o string) {
	return m.players[0], m.players[1]
}

// Move places the player's mark on cell and returns the resulting state.
// The check and the update happen under one lock, so the snapshot always
// reflects exactly this move.
func (m *Match) Move(player string, cell int) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	seat := m.seat(player)
	switch {
	case seat < 0:
		return Snapshot{}, ErrNotAPlayer
	case m.over:
		return Snapshot{}, ErrGameOver
	case seat != m.turn:
		return Snapshot{}, ErrNotYourTurn
	}

	if err := m.board.Place(cell, seatMark(seat)); err != nil {
		return Snapshot{}, err
	}
	m.version++

	switch {
	case m.board.Winner() != Empty:
		m.over, m.winner = true, player
	case m.board.IsFull():
		m.over, m.draw = true, true
	default:
		m.turn = 1 - seat
	}
	return m.snapshot(), nil
}

// Expire ends the game because the player to move has not moved since
// version. It reports that player and whether this call ended the game.
//
// A turn timer can fire while a move is being applied. Checking the version
// under the match lock means exactly one of them wins: if the move got in
// first the version has changed and Expire does nothing, and if Expire got in
// first the move fails with ErrGameOver.
func (m *Match) Expire(version int) (idle string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.over || m.version != version {
		return "", false
	}
	m.over = true
	return m.players[m.turn], true
}

// End stops the game early, for example when a player disconnects. Any later
// move fails with ErrGameOver.
func (m *Match) End() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.over = true
}

func (m *Match) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot()
}

func (m *Match) snapshot() Snapshot {
	s := Snapshot{
		Cells:   m.board.Cells(),
		Version: m.version,
		Over:    m.over,
		Winner:  m.winner,
		Draw:    m.draw,
	}
	if !m.over {
		s.Turn = m.players[m.turn]
	}
	return s
}

func (m *Match) seat(player string) int {
	for i, p := range m.players {
		if p == player {
			return i
		}
	}
	return -1
}

func seatMark(seat int) Mark {
	if seat == 0 {
		return X
	}
	return O
}
