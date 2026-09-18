package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func dequeue(t *testing.T, q *WaitQueue) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	id, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	return id
}

func TestWaitQueueIsFIFO(t *testing.T) {
	q := NewWaitQueue(3)
	for _, id := range []string{"a", "b", "c"} {
		if err := q.Enqueue(id); err != nil {
			t.Fatalf("Enqueue(%q): %v", id, err)
		}
	}
	for _, want := range []string{"a", "b", "c"} {
		if got := dequeue(t, q); got != want {
			t.Errorf("Dequeue() = %q, want %q", got, want)
		}
	}
}

func TestWaitQueueRejectsDuplicates(t *testing.T) {
	q := NewWaitQueue(2)
	if err := q.Enqueue("a"); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("a"); !errors.Is(err, ErrAlreadyQueued) {
		t.Errorf("second Enqueue error = %v, want %v", err, ErrAlreadyQueued)
	}
	dequeue(t, q)
	if err := q.Enqueue("a"); err != nil {
		t.Errorf("Enqueue after Dequeue: %v", err)
	}
}

func TestWaitQueueFull(t *testing.T) {
	q := NewWaitQueue(1)
	if err := q.Enqueue("a"); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue("b"); !errors.Is(err, ErrQueueFull) {
		t.Errorf("Enqueue error = %v, want %v", err, ErrQueueFull)
	}
}

func TestWaitQueueSkipsRemovedPlayers(t *testing.T) {
	q := NewWaitQueue(3)
	for _, id := range []string{"a", "b", "c"} {
		if err := q.Enqueue(id); err != nil {
			t.Fatal(err)
		}
	}
	q.Remove("b")
	if got := dequeue(t, q); got != "a" {
		t.Errorf("first Dequeue() = %q, want a", got)
	}
	if got := dequeue(t, q); got != "c" {
		t.Errorf("second Dequeue() = %q, want c", got)
	}
}

func TestWaitQueueDequeueStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewWaitQueue(1).Dequeue(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Dequeue error = %v, want %v", err, context.Canceled)
	}
}
