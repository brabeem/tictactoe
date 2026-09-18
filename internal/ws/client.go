// Package ws carries protocol messages over websocket connections.
package ws

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/brabeem/tictactoe/internal/protocol"
)

var (
	ErrClosed       = errors.New("connection closed")
	ErrSlowConsumer = errors.New("send buffer full")
)

type Config struct {
	WriteWait      time.Duration // deadline for a single write
	PongWait       time.Duration // a peer silent this long is considered dead
	PingPeriod     time.Duration // must be shorter than PongWait
	CloseWait      time.Duration // how long Close waits for the peer to acknowledge
	MaxMessageSize int64
	SendBuffer     int // queued outbound messages before a peer counts as slow
}

func DefaultConfig() Config {
	return Config{
		WriteWait:      5 * time.Second,
		PongWait:       12 * time.Second,
		PingPeriod:     5 * time.Second,
		CloseWait:      2 * time.Second,
		MaxMessageSize: 4096,
		SendBuffer:     32,
	}
}

// MessageHandler receives messages read from a client.
type MessageHandler interface {
	HandleMessage(playerID string, env protocol.Envelope)
}

// Client is one player's websocket connection. gorilla/websocket allows one
// concurrent reader and one concurrent writer, so all writes go through
// writePump and all reads through readPump.
type Client struct {
	id   string
	conn *websocket.Conn
	cfg  Config
	log  *slog.Logger

	// out is never closed, so a Send racing with shutdown cannot panic.
	out chan []byte

	closing     chan struct{} // closed when a graceful close is requested
	closingOnce sync.Once
	done        chan struct{} // closed when the connection is gone
	doneOnce    sync.Once
}

func newClient(id string, conn *websocket.Conn, cfg Config, log *slog.Logger) *Client {
	return &Client{
		id:      id,
		conn:    conn,
		cfg:     cfg,
		log:     log,
		out:     make(chan []byte, cfg.SendBuffer),
		closing: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (c *Client) ID() string {
	return c.id
}

// Send queues env for writing without blocking. A client whose buffer is full
// is dropped immediately: it has stopped reading, and waiting on it would
// stall its opponent.
func (c *Client) Send(env protocol.Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	select {
	case <-c.closing:
		return ErrClosed
	case <-c.done:
		return ErrClosed
	default:
	}
	select {
	case c.out <- data:
		return nil
	default:
		c.abort()
		return ErrSlowConsumer
	}
}

// Close shuts the connection down gracefully: messages already accepted by
// Send are delivered, then a close frame is sent. Safe to call repeatedly and
// from any goroutine; it does not wait for the shutdown to finish.
func (c *Client) Close() {
	c.closingOnce.Do(func() { close(c.closing) })
}

// abort closes the connection immediately, discarding anything unsent.
func (c *Client) abort() {
	c.doneOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func (c *Client) writePump() {
	ticker := time.NewTicker(c.cfg.PingPeriod)
	defer ticker.Stop()
	defer c.abort()

	for {
		select {
		case <-c.done:
			return
		case <-c.closing:
			c.closeGracefully()
			return
		case msg := <-c.out:
			if err := c.write(msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(c.cfg.WriteWait)); err != nil {
				c.log.Debug("ping failed", "player", c.id, "err", err)
				return
			}
		}
	}
}

// closeGracefully flushes queued messages and performs the websocket close
// handshake. Closing the socket straight after writing is not enough: if
// unread data is still arriving, the TCP stack may reset the connection and
// the peer can lose the final messages before it reads them.
func (c *Client) closeGracefully() {
	for drained := false; !drained; {
		select {
		case msg := <-c.out:
			if err := c.write(msg); err != nil {
				return
			}
		default:
			drained = true
		}
	}

	frame := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	if err := c.conn.WriteControl(websocket.CloseMessage, frame, time.Now().Add(c.cfg.WriteWait)); err != nil {
		return
	}

	// readPump returns once the peer answers with its own close frame.
	timer := time.NewTimer(c.cfg.CloseWait)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
	}
}

func (c *Client) write(msg []byte) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteWait)); err != nil {
		return err
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		c.log.Debug("write failed", "player", c.id, "err", err)
		return err
	}
	return nil
}

// readPump blocks until the connection fails or is closed.
func (c *Client) readPump(h MessageHandler) {
	defer c.abort()

	c.conn.SetReadLimit(c.cfg.MaxMessageSize)
	if err := c.conn.SetReadDeadline(time.Now().Add(c.cfg.PongWait)); err != nil {
		return
	}
	// Only pongs extend the deadline, so a peer that stops answering pings is
	// detected even if it is still sending messages.
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(c.cfg.PongWait))
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.log.Debug("connection ended", "player", c.id, "err", err)
			return
		}

		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			c.sendError(protocol.CodeBadRequest, "message is not valid JSON")
			continue
		}
		h.HandleMessage(c.id, env)
	}
}

func (c *Client) sendError(code, message string) {
	env, err := protocol.NewEnvelope(protocol.TypeError, protocol.Error{Code: code, Message: message})
	if err != nil {
		return
	}
	_ = c.Send(env)
}
