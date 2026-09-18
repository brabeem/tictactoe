// Package protocol defines the JSON messages exchanged between server and client.
package protocol

import "encoding/json"

type Type string

// Client to server.
const (
	TypeMove    Type = "move"
	TypeRestart Type = "restart"
)

// Server to client.
const (
	TypeWelcome      Type = "welcome"
	TypeWaiting      Type = "waiting"
	TypeMatchFound   Type = "match_found"
	TypeState        Type = "state"
	TypeOpponentLeft Type = "opponent_left"
	// TypeTimedOut tells a player they took too long to move. The server
	// closes their connection straight after sending it.
	TypeTimedOut Type = "timed_out"
	TypeError    Type = "error"
)

// Envelope wraps every message. Payload is decoded according to Type.
type Envelope struct {
	Type    Type            `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// NewEnvelope encodes payload into an envelope of type t. A nil payload
// produces an envelope with no payload.
func NewEnvelope(t Type, payload any) (Envelope, error) {
	if payload == nil {
		return Envelope{Type: t}, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Type: t, Payload: raw}, nil
}

// Move asks to place the sender's mark. Cell is 0-8, row by row. It is a
// pointer so that a missing cell is rejected instead of silently meaning 0.
type Move struct {
	Cell *int `json:"cell"`
}

type Welcome struct {
	PlayerID string `json:"playerId"`
}

type MatchFound struct {
	MatchID    string `json:"matchId"`
	OpponentID string `json:"opponentId"`
	Mark       string `json:"mark"`
}

// State is the board as seen by one player.
type State struct {
	Cells    [9]string `json:"cells"` // "X", "O" or ""
	Version  int       `json:"version"`
	YourMark string    `json:"yourMark"`
	YourTurn bool      `json:"yourTurn"`
	GameOver bool      `json:"gameOver"`
	Winner   string    `json:"winner,omitempty"` // player ID
	Draw     bool      `json:"draw,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	CodeBadRequest      = "bad_request"
	CodeNotInMatch      = "not_in_match"
	CodeNotYourTurn     = "not_your_turn"
	CodeCellOccupied    = "cell_occupied"
	CodeCellOutOfRange  = "cell_out_of_range"
	CodeGameOver        = "game_over"
	CodeMatchInProgress = "match_in_progress"
	CodeServerBusy      = "server_busy"
	CodeInternal        = "internal"
)
