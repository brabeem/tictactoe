package ws

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/brabeem/tictactoe/internal/protocol"
	"github.com/brabeem/tictactoe/internal/store"
)

// Sessions is the game logic a Handler drives. store.Store implements it.
type Sessions interface {
	Connect(store.Peer)
	HandleMessage(playerID string, env protocol.Envelope)
	Disconnect(playerID string)
}

// Handler upgrades HTTP requests to websocket connections, one per player.
type Handler struct {
	sessions Sessions
	newID    func() string
	cfg      Config
	log      *slog.Logger
	upgrader websocket.Upgrader
}

func NewHandler(sessions Sessions, newID func() string, cfg Config, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Handler{
		sessions: sessions,
		newID:    newID,
		cfg:      cfg,
		log:      log,
		upgrader: websocket.Upgrader{
			// Players connect from their own machines, and the browser client
			// is served from a different port, so the default same-origin
			// check would reject them. There is nothing to protect against
			// here: connections carry no cookies and no stored identity.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade has already written an HTTP error response.
		h.log.Debug("upgrade failed", "remote", r.RemoteAddr, "err", err)
		return
	}

	c := newClient(h.newID(), conn, h.cfg, h.log)
	h.log.Info("player connected", "player", c.id, "remote", r.RemoteAddr)

	go c.writePump()
	h.sessions.Connect(c)
	c.readPump(h.sessions)
	h.sessions.Disconnect(c.id)

	h.log.Info("player disconnected", "player", c.id)
}
