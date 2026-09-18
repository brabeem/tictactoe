package store

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrQueueFull     = errors.New("matchmaking queue is full")
	ErrAlreadyQueued = errors.New("player is already queued")
)

// WaitQueue is the in-memory Queue: a buffered channel for FIFO order plus a
// set recording who is still waiting.
//
// A channel cannot drop an element from the middle, so Remove only deletes the
// player from the set. Their entry stays in the channel and Dequeue skips it.
// Such stale entries still occupy capacity until they are read.
type WaitQueue struct {
	ch chan string

	// A plain Mutex: every operation modifies the set.
	mu      sync.Mutex
	waiting map[string]struct{}
}

// NewWaitQueue returns a queue holding at most capacity entries.
func NewWaitQueue(capacity int) *WaitQueue {
	if capacity < 1 {
		panic("store: queue capacity must be at least 1")
	}
	return &WaitQueue{
		ch:      make(chan string, capacity),
		waiting: make(map[string]struct{}),
	}
}

// Enqueue adds a player without blocking.
func (q *WaitQueue) Enqueue(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, ok := q.waiting[id]; ok {
		return ErrAlreadyQueued
	}
	select {
	case q.ch <- id:
		q.waiting[id] = struct{}{}
		return nil
	default:
		return ErrQueueFull
	}
}

// Remove withdraws a player who is waiting. Removing a player who is not
// waiting is a no-op.
func (q *WaitQueue) Remove(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.waiting, id)
}

// Dequeue blocks until a waiting player is available or ctx is done.
func (q *WaitQueue) Dequeue(ctx context.Context) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case id := <-q.ch:
			if q.take(id) {
				return id, nil
			}
		}
	}
}

func (q *WaitQueue) take(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, ok := q.waiting[id]; !ok {
		return false
	}
	delete(q.waiting, id)
	return true
}
