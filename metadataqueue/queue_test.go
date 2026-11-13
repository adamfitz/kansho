package metadataqueue

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestQueueRunsOneTaskAtATime(t *testing.T) {
	q := NewQueue()
	defer q.Close()

	var mu sync.Mutex
	active, maxActive := 0, 0
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		ok := q.Submit(&Task{
			Action:  "Check",
			Title:   "Test",
			Chapter: "1",
			Run: func(ctx context.Context) error {
				mu.Lock()
				active++
				if active > maxActive {
					maxActive = active
				}
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				active--
				mu.Unlock()
				return nil
			},
			OnSuccess: func() { done <- struct{}{} },
		})
		if !ok {
			t.Fatal("Submit() = false")
		}
	}
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for task")
		}
	}
	if maxActive != 1 {
		t.Fatalf("max concurrent tasks = %d, want 1", maxActive)
	}
}

func TestQueueRejectsDuplicateDedupeKey(t *testing.T) {
	q := NewQueue()
	defer q.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	first := &Task{
		DedupeKey: "same",
		Run: func(ctx context.Context) error {
			close(started)
			<-release
			return nil
		},
		OnSuccess: func() { done <- struct{}{} },
	}
	if !q.Submit(first) {
		t.Fatal("first Submit() = false")
	}
	<-started
	if q.Submit(&Task{DedupeKey: "same", Run: func(ctx context.Context) error { return nil }}) {
		t.Fatal("duplicate Submit() = true, want false")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first task")
	}
	if !q.Submit(&Task{DedupeKey: "same", Run: func(ctx context.Context) error { return nil }}) {
		t.Fatal("Submit() after completion = false, want true")
	}
}
