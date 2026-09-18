// Package store holds all server state in memory and coordinates
// matchmaking, moves and disconnects.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/brabeem/tictactoe/internal/game"
	"github.com/brabeem/tictactoe/internal/protocol"
)

// Peer is a connected player that messages can be sent to.
type Peer interface {
	ID() string
	// Send must not block: a slow peer must not stall the game for others.
	Send(protocol.Envelope) error
	// Close must deliver messages already sent before closing the connection.
	Close()
}

// Hub tracks connected players.
type Hub interface {
	Add(Peer)
	Remove(id string)
	Get(id string) (Peer, bool)
}

// Queue holds players waiting for an opponent, in arrival order.
type Queue interface {
	Enqueue(id string) error
	Remove(id string)
	Dequeue(ctx context.Context) (string, error)
}

// Match is a game in progress. game.Match implements it.
type Match interface {
	ID() string
	Players() (x, o string)
	Move(player string, cell int) (game.Snapshot, error)
	Snapshot() game.Snapshot
	Expire(version int) (idle string, ok bool)
	End()
}

// TurnTimers holds the move deadline of each active match.
type TurnTimers interface {
	Reset(matchID string, d time.Duration, fire func())
	Stop(matchID string)
}

// MatchRegistry tracks active matches by player.
type MatchRegistry interface {
	Add(Match)
	ByPlayer(id string) (Match, bool)
	// Remove reports whether the match was still registered.
	Remove(Match) bool
}

// MatchFactory creates a match between x and o, including its fresh board.
type MatchFactory func(id, x, o string) Match

// Deps are the components a Store is built from.
type Deps struct {
	Hub      Hub
	Queue    Queue
	Matches  MatchRegistry
	Timers   TurnTimers
	NewMatch MatchFactory
	NewID    func() string
	// MoveTimeout is how long a player may take over a move before they are
	// treated as disconnected.
	MoveTimeout time.Duration
	Logger      *slog.Logger // optional
}

type Store struct {
	hub         Hub
	queue       Queue
	matches     MatchRegistry
	timers      TurnTimers
	newMatch    MatchFactory
	newID       func() string
	moveTimeout time.Duration
	log         *slog.Logger
}

// New builds a Store. It panics if a required dependency is missing, since
// that is a wiring bug rather than a runtime condition.
func New(d Deps) *Store {
	if d.Hub == nil || d.Queue == nil || d.Matches == nil || d.Timers == nil || d.NewMatch == nil || d.NewID == nil {
		panic("store: Hub, Queue, Matches, Timers, NewMatch and NewID are required")
	}
	if d.MoveTimeout <= 0 {
		panic("store: MoveTimeout must be positive")
	}
	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Store{
		hub:         d.Hub,
		queue:       d.Queue,
		matches:     d.Matches,
		timers:      d.Timers,
		newMatch:    d.NewMatch,
		newID:       d.NewID,
		moveTimeout: d.MoveTimeout,
		log:         log,
	}
}

// Connect registers a newly connected player and queues them for a match.
func (s *Store) Connect(p Peer) {
	s.hub.Add(p)
	s.send(p.ID(), protocol.TypeWelcome, protocol.Welcome{PlayerID: p.ID()})
	s.enqueue(p.ID())
}

// HandleMessage processes one message received from a player.
func (s *Store) HandleMessage(playerID string, env protocol.Envelope) {
	switch env.Type {
	case protocol.TypeMove:
		var mv protocol.Move
		if err := json.Unmarshal(env.Payload, &mv); err != nil || mv.Cell == nil {
			s.sendError(playerID, protocol.CodeBadRequest, "move needs a cell")
			return
		}
		s.move(playerID, *mv.Cell)
	case protocol.TypeRestart:
		s.restart(playerID)
	default:
		s.sendError(playerID, protocol.CodeBadRequest, "unknown message type")
	}
}

// Disconnect forgets a player and ends any match they were in.
func (s *Store) Disconnect(playerID string) {
	s.hub.Remove(playerID)
	s.queue.Remove(playerID)
	if m, ok := s.matches.ByPlayer(playerID); ok {
		s.abandon(m, playerID)
	}
}

// Run pairs waiting players until ctx is done. Run it in exactly one goroutine.
func (s *Store) Run(ctx context.Context) error {
	var first string
	for {
		id, err := s.queue.Dequeue(ctx)
		if err != nil {
			return err
		}
		// A player can be dequeued twice if they asked to restart while already
		// held here, so the same ID must never be paired with itself.
		if !s.available(id) || id == first {
			continue
		}
		if first == "" || !s.available(first) {
			first = id
			continue
		}
		s.startMatch(first, id)
		first = ""
	}
}

func (s *Store) available(playerID string) bool {
	_, connected := s.hub.Get(playerID)
	_, playing := s.matches.ByPlayer(playerID)
	return connected && !playing
}

func (s *Store) startMatch(x, o string) {
	m := s.newMatch(s.newID(), x, o)
	s.matches.Add(m)

	// Disconnect can only end matches that are registered. A player who left
	// between the availability check and Add would be missed, so check again.
	for _, id := range [2]string{x, o} {
		if _, ok := s.hub.Get(id); !ok {
			s.abandon(m, id)
			return
		}
	}

	s.log.Info("match started", "match", m.ID(), "x", x, "o", o)
	snap := m.Snapshot()
	s.armTurnTimer(m, snap)
	s.send(x, protocol.TypeMatchFound, protocol.MatchFound{MatchID: m.ID(), OpponentID: o, Mark: game.X.String()})
	s.send(o, protocol.TypeMatchFound, protocol.MatchFound{MatchID: m.ID(), OpponentID: x, Mark: game.O.String()})
	s.broadcastState(m, snap)
}

// armTurnTimer gives the player to move MoveTimeout to act. The version is
// captured now so a timer outrun by a move cannot end the game.
func (s *Store) armTurnTimer(m Match, snap game.Snapshot) {
	version := snap.Version
	s.timers.Reset(m.ID(), s.moveTimeout, func() { s.expire(m, version) })
}

// expire ends a match whose player to move ran out of time. That player is
// treated as disconnected: their connection is closed and they are forgotten,
// and their opponent is told they left.
func (s *Store) expire(m Match, version int) {
	idle, ok := m.Expire(version)
	if !ok {
		return
	}
	s.timers.Stop(m.ID())
	if !s.matches.Remove(m) {
		// A disconnect already ended this match and notified the opponent.
		return
	}

	s.log.Info("move timed out", "match", m.ID(), "player", idle)
	s.send(idle, protocol.TypeTimedOut, nil)
	s.send(opponentOf(m, idle), protocol.TypeOpponentLeft, nil)
	s.drop(idle)
}

// drop forgets a player and closes their connection. Closing makes the
// transport call Disconnect, which then finds nothing left to clean up.
func (s *Store) drop(playerID string) {
	peer, ok := s.hub.Get(playerID)
	s.hub.Remove(playerID)
	s.queue.Remove(playerID)
	if ok {
		peer.Close()
	}
}

func (s *Store) move(playerID string, cell int) {
	m, ok := s.matches.ByPlayer(playerID)
	if !ok {
		s.sendError(playerID, protocol.CodeNotInMatch, "you are not in a match")
		return
	}

	snap, err := m.Move(playerID, cell)
	if err != nil {
		s.sendError(playerID, errorCode(err), err.Error())
		return
	}

	if snap.Over {
		// Deregister before broadcasting, so a disconnect arriving now cannot
		// report "opponent left" for a game that has already finished.
		s.timers.Stop(m.ID())
		s.matches.Remove(m)
		s.log.Info("match finished", "match", m.ID(), "winner", snap.Winner, "draw", snap.Draw)
	} else {
		s.armTurnTimer(m, snap)
	}
	s.broadcastState(m, snap)
}

func (s *Store) restart(playerID string) {
	if _, ok := s.matches.ByPlayer(playerID); ok {
		s.sendError(playerID, protocol.CodeMatchInProgress, "finish the current match first")
		return
	}
	s.enqueue(playerID)
}

func (s *Store) enqueue(playerID string) {
	// Send "waiting" before enqueueing: once queued the player can be paired
	// at any moment, and "waiting" must not arrive after "match_found".
	s.send(playerID, protocol.TypeWaiting, nil)

	switch err := s.queue.Enqueue(playerID); {
	case err == nil, errors.Is(err, ErrAlreadyQueued):
	case errors.Is(err, ErrQueueFull):
		s.sendError(playerID, protocol.CodeServerBusy, "matchmaking queue is full, try again later")
	default:
		s.log.Error("enqueue player", "player", playerID, "err", err)
		s.sendError(playerID, protocol.CodeInternal, "could not join the queue")
	}
}

// abandon ends m because leaver disconnected, and tells the other player.
func (s *Store) abandon(m Match, leaver string) {
	if !s.matches.Remove(m) {
		return
	}
	// End the game itself too: a move the opponent had already started must
	// fail rather than broadcast a board for a match that no longer exists.
	m.End()
	s.timers.Stop(m.ID())

	s.log.Info("match abandoned", "match", m.ID(), "leaver", leaver)
	s.send(opponentOf(m, leaver), protocol.TypeOpponentLeft, nil)
}

func opponentOf(m Match, player string) string {
	x, o := m.Players()
	if player == x {
		return o
	}
	return x
}

func (s *Store) broadcastState(m Match, snap game.Snapshot) {
	x, o := m.Players()
	s.send(x, protocol.TypeState, stateFor(x, game.X, snap))
	s.send(o, protocol.TypeState, stateFor(o, game.O, snap))
}

func stateFor(playerID string, mark game.Mark, snap game.Snapshot) protocol.State {
	st := protocol.State{
		Version:  snap.Version,
		YourMark: mark.String(),
		YourTurn: snap.Turn == playerID,
		GameOver: snap.Over,
		Winner:   snap.Winner,
		Draw:     snap.Draw,
	}
	for i, c := range snap.Cells {
		st.Cells[i] = c.String()
	}
	return st
}

func (s *Store) sendError(playerID, code, message string) {
	s.send(playerID, protocol.TypeError, protocol.Error{Code: code, Message: message})
}

func (s *Store) send(playerID string, t protocol.Type, payload any) {
	peer, ok := s.hub.Get(playerID)
	if !ok {
		return
	}
	env, err := protocol.NewEnvelope(t, payload)
	if err != nil {
		s.log.Error("encode message", "type", t, "err", err)
		return
	}
	if err := peer.Send(env); err != nil {
		s.log.Warn("send message", "player", playerID, "type", t, "err", err)
	}
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, game.ErrNotYourTurn):
		return protocol.CodeNotYourTurn
	case errors.Is(err, game.ErrCellOccupied):
		return protocol.CodeCellOccupied
	case errors.Is(err, game.ErrCellOutOfRange):
		return protocol.CodeCellOutOfRange
	case errors.Is(err, game.ErrGameOver):
		return protocol.CodeGameOver
	case errors.Is(err, game.ErrNotAPlayer):
		return protocol.CodeNotInMatch
	default:
		return protocol.CodeInternal
	}
}
