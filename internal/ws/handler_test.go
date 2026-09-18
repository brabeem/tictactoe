package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/brabeem/tictactoe/internal/game"
	"github.com/brabeem/tictactoe/internal/protocol"
	"github.com/brabeem/tictactoe/internal/store"
)

// counter returns a goroutine-safe ID generator; the handler and the pairing
// loop call their generators from different goroutines.
func counter(prefix string) func() string {
	var n atomic.Int64
	return func() string { return fmt.Sprintf("%s-%d", prefix, n.Add(1)) }
}

// startServer runs a store and websocket handler, returning the URL to dial.
func startServer(t *testing.T, moveTimeout time.Duration) string {
	t.Helper()
	s := store.New(store.Deps{
		Hub:     store.NewHub(),
		Queue:   store.NewWaitQueue(4),
		Matches: store.NewMatchIndex(),
		Timers:  store.NewMatchTimers(store.RealClock{}),
		NewMatch: func(id, x, o string) store.Match {
			return game.NewMatch(id, x, o, game.NewGrid())
		},
		NewID:       counter("match"),
		MoveTimeout: moveTimeout,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = s.Run(ctx) }()

	srv := httptest.NewServer(NewHandler(s, counter("player"), DefaultConfig(), nil))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readUntil skips messages until one of type want arrives.
func readUntil(t *testing.T, conn *websocket.Conn, want protocol.Type) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if env.Type == want {
			return
		}
	}
}

// The browser client is served from a different port than the game server.
func TestClientFromAnotherOriginIsAccepted(t *testing.T) {
	url := startServer(t, 15*time.Second)

	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Origin": []string{"http://localhost:3000"},
	})
	if err != nil {
		t.Fatalf("dial with a different origin: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	readUntil(t, conn, protocol.TypeWelcome)
}

func TestTwoClientsArePairedOverWebsocket(t *testing.T) {
	url := startServer(t, 15*time.Second)

	alice := dial(t, url)
	readUntil(t, alice, protocol.TypeWaiting)
	bob := dial(t, url)

	readUntil(t, alice, protocol.TypeMatchFound)
	readUntil(t, bob, protocol.TypeMatchFound)
}

// read returns the next message of type want, decoding its payload.
func read(t *testing.T, conn *websocket.Conn, want protocol.Type, payload any) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var env protocol.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			t.Fatalf("waiting for %q: %v", want, err)
		}
		if env.Type != want {
			continue
		}
		if payload != nil {
			if err := json.Unmarshal(env.Payload, payload); err != nil {
				t.Fatalf("decode %q: %v", want, err)
			}
		}
		return
	}
}

func sendMove(t *testing.T, conn *websocket.Conn, cell int) {
	t.Helper()
	env, err := protocol.NewEnvelope(protocol.TypeMove, protocol.Move{Cell: &cell})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(env); err != nil {
		t.Fatalf("send move: %v", err)
	}
}

func TestPlayingAFullGameOverWebsocket(t *testing.T) {
	url := startServer(t, 15*time.Second)

	alice := dial(t, url)
	var welcome protocol.Welcome
	read(t, alice, protocol.TypeWelcome, &welcome)
	readUntil(t, alice, protocol.TypeWaiting)

	bob := dial(t, url)
	var state protocol.State
	read(t, alice, protocol.TypeState, &state)
	read(t, bob, protocol.TypeState, nil)
	if !state.YourTurn || state.YourMark != "X" {
		t.Fatalf("alice should move first as X: %+v", state)
	}

	// Alice takes the top row while Bob takes the middle one.
	for _, mv := range []struct {
		conn *websocket.Conn
		cell int
	}{{alice, 0}, {bob, 3}, {alice, 1}, {bob, 4}, {alice, 2}} {
		sendMove(t, mv.conn, mv.cell)
		read(t, alice, protocol.TypeState, &state)
		read(t, bob, protocol.TypeState, nil)
	}

	if !state.GameOver || state.Draw || state.Winner != welcome.PlayerID {
		t.Fatalf("final state = %+v, want alice (%s) to win", state, welcome.PlayerID)
	}
	if state.Cells[0] != "X" || state.Cells[3] != "O" {
		t.Errorf("final board = %v", state.Cells)
	}
}

func TestIdleClientIsDisconnectedAfterMoveTimeout(t *testing.T) {
	url := startServer(t, 200*time.Millisecond)

	alice := dial(t, url)
	readUntil(t, alice, protocol.TypeWaiting)
	bob := dial(t, url)
	readUntil(t, alice, protocol.TypeState)
	readUntil(t, bob, protocol.TypeState)

	// Alice moves first and does nothing.
	readUntil(t, alice, protocol.TypeTimedOut)
	readUntil(t, bob, protocol.TypeOpponentLeft)

	// The server must end with a close handshake, not by dropping the socket.
	_ = alice.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := alice.ReadMessage()
	var closeErr *websocket.CloseError
	switch {
	case err == nil:
		t.Fatalf("unexpected message after timed_out: %s", data)
	case errors.As(err, &closeErr):
		if closeErr.Code != websocket.CloseNormalClosure {
			t.Errorf("close code = %d, want %d", closeErr.Code, websocket.CloseNormalClosure)
		}
	default:
		t.Fatalf("connection ended without a close frame: %v", err)
	}

	// Bob is free again and can queue for a new match.
	restart, err := protocol.NewEnvelope(protocol.TypeRestart, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.WriteJSON(restart); err != nil {
		t.Fatalf("send restart: %v", err)
	}
	readUntil(t, bob, protocol.TypeWaiting)
}
