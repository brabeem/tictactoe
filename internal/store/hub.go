package store

import "sync"

// ConnHub is the in-memory Hub.
type ConnHub struct {
	// RWMutex because lookups happen on every message sent, while adds and
	// removes happen only when a player connects or disconnects.
	mu    sync.RWMutex
	peers map[string]Peer
}

func NewHub() *ConnHub {
	return &ConnHub{peers: make(map[string]Peer)}
}

func (h *ConnHub) Add(p Peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.peers[p.ID()] = p
}

func (h *ConnHub) Remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.peers, id)
}

func (h *ConnHub) Get(id string) (Peer, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.peers[id]
	return p, ok
}
