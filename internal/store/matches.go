package store

import "sync"

// MatchIndex is the in-memory MatchRegistry. Each match is indexed under both
// of its players.
type MatchIndex struct {
	// RWMutex because every move looks a match up, while matches are added and
	// removed only when a game starts or ends.
	mu       sync.RWMutex
	byPlayer map[string]Match
}

func NewMatchIndex() *MatchIndex {
	return &MatchIndex{byPlayer: make(map[string]Match)}
}

func (i *MatchIndex) Add(m Match) {
	x, o := m.Players()
	i.mu.Lock()
	defer i.mu.Unlock()
	i.byPlayer[x] = m
	i.byPlayer[o] = m
}

func (i *MatchIndex) ByPlayer(id string) (Match, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	m, ok := i.byPlayer[id]
	return m, ok
}

// Remove deregisters m and reports whether it was registered. A game can end
// by a finishing move and by a disconnect at the same moment; only the caller
// that gets true should act on the ending.
func (i *MatchIndex) Remove(m Match) bool {
	x, o := m.Players()
	i.mu.Lock()
	defer i.mu.Unlock()

	removed := false
	for _, id := range [2]string{x, o} {
		if i.byPlayer[id] == m {
			delete(i.byPlayer, id)
			removed = true
		}
	}
	return removed
}
