package refreshpool

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestPool(workers int, base time.Duration, retries int) *Pool {
	return NewPool(workers, base, time.Hour, retries)
}

// waitFor polls pred until it holds true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition not met within %v", timeout)
}

func TestSubmitRunsTaskAndReportsSuccess(t *testing.T) {
	p := newTestPool(1, time.Millisecond, 2)
	defer p.Close()

	var ran, succeeded atomic.Int32
	done := make(chan struct{})
	p.Submit(&Task{
		Site: "site-a",
		Run: func(ctx context.Context) error {
			ran.Add(1)
			return nil
		},
		OnSuccess: func() {
			succeeded.Add(1)
			close(done)
		},
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("OnSuccess was not called")
	}

	if ran.Load() != 1 || succeeded.Load() != 1 {
		t.Fatalf("ran=%d succeeded=%d, want 1/1", ran.Load(), succeeded.Load())
	}
	if !p.Status().IsIdle() {
		t.Fatalf("pool not idle after success: %+v", p.Status())
	}
}

func TestRetryThenSucceeds(t *testing.T) {
	p := newTestPool(1, time.Millisecond, 3)
	defer p.Close()

	var attempts, errorsSeen atomic.Int32
	done := make(chan struct{})
	p.Submit(&Task{
		Site: "flaky",
		Run: func(ctx context.Context) error {
			if attempts.Add(1) < 3 {
				return errors.New("boom")
			}
			return nil
		},
		OnError: func(err error) {
			errorsSeen.Add(1)
		},
		OnSuccess: func() { close(done) },
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("task never succeeded")
	}

	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts=%d, want 3", got)
	}
	if errorsSeen.Load() != 0 {
		t.Fatal("OnError must not fire when a retry succeeds")
	}
}

func TestRetriesExhaustedCallsOnErrorWithLastError(t *testing.T) {
	p := newTestPool(1, time.Millisecond, 2)
	defer p.Close()

	var attempts atomic.Int32
	errCh := make(chan error, 1)
	lastErr := errors.New("still failing")
	p.Submit(&Task{
		Site: "dead",
		Desc: "hopeless",
		Run: func(ctx context.Context) error {
			attempts.Add(1)
			return lastErr
		},
		OnError: func(err error) { errCh <- err },
	})

	var got error
	select {
	case got = <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("OnError was not called")
	}

	if attempts.Load() != 3 { // initial attempt + maxRetries
		t.Fatalf("attempts=%d, want 3", attempts.Load())
	}
	if !errors.Is(got, lastErr) {
		t.Fatalf("OnError got %v, want the wrapped last error", got)
	}
}

func TestSameSiteTasksAreSerialized(t *testing.T) {
	p := newTestPool(MaxWorkers, time.Millisecond, 0)
	defer p.Close()

	var active, maxActive atomic.Int32
	var wg sync.WaitGroup
	runBlocked := func() {
		wg.Add(1)
		p.Submit(&Task{
			Site: "one-site",
			Run: func(ctx context.Context) error {
				cur := active.Add(1)
				for {
					old := maxActive.Load()
					if cur <= old || maxActive.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				active.Add(-1)
				wg.Done()
				return nil
			},
		})
	}
	runBlocked()
	runBlocked()
	runBlocked()

	waitFor(t, 2*time.Second, func() bool { return p.Status().Queued == 0 })
	wg.Wait()

	if maxActive.Load() > 1 {
		t.Fatalf("tasks overlapped on the same site (max concurrent=%d)", maxActive.Load())
	}
}

func TestDifferentSitesRunInParallel(t *testing.T) {
	p := newTestPool(2, time.Millisecond, 0)
	defer p.Close()

	var entered sync.WaitGroup
	entered.Add(2)
	release := make(chan struct{})

	taskFor := func(site string) *Task {
		return &Task{
			Site: site,
			Run: func(ctx context.Context) error {
				entered.Done()
				<-release // block until BOTH sites are inside Run
				return nil
			},
		}
	}

	if !p.Submit(taskFor("a")) || !p.Submit(taskFor("b")) {
		t.Fatal("submissions rejected")
	}

	// If the two sites were serialized this would time out.
	crossed := make(chan struct{})
	go func() {
		entered.Wait()
		close(crossed)
	}()
	select {
	case <-crossed:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("sites did not run in parallel")
	}
	close(release)
}

func TestDuplicateDedupeKeyRejectedUntilFinished(t *testing.T) {
	p := newTestPool(1, time.Millisecond, 0)
	defer p.Close()

	block := make(chan struct{})
	started := make(chan struct{})
	p.Submit(&Task{
		Site:      "dup",
		DedupeKey: "chapters:manga-1",
		Run: func(ctx context.Context) error {
			close(started)
			<-block
			return nil
		},
	})

	<-started
	if p.Submit(&Task{Site: "dup", DedupeKey: "chapters:manga-1", Run: func(context.Context) error { return nil }}) {
		t.Fatal("duplicate submission with pending key must be rejected")
	}

	close(block)
	waitFor(t, 2*time.Second, func() bool { return p.Status().IsIdle() })

	// Once finished the key is free again.
	if !p.Submit(&Task{Site: "dup", DedupeKey: "chapters:manga-1", Run: func(context.Context) error { return nil }}) {
		t.Fatal("resubmission after completion must be accepted")
	}
}

func TestBackoffGrowsWhileFailingAndResetsAfterSuccess(t *testing.T) {
	const (
		site = "backoff-site"
		base = 5 * time.Millisecond
	)
	p := newTestPool(1, base, 5)
	defer p.Close()

	backoffOf := func() time.Duration {
		p.mu.Lock()
		defer p.mu.Unlock()
		w := p.sites[site]
		if w == nil {
			return -1
		}
		return w.backoff
	}

	var attempts atomic.Int32
	seenDuringSecondAttempt := make(chan time.Duration, 1)
	p.Submit(&Task{
		Site: site,
		Run: func(ctx context.Context) error {
			n := attempts.Add(1)
			if n == 2 {
				// By the second attempt the first failure has doubled the delay.
				seenDuringSecondAttempt <- backoffOf()
				return nil
			}
			return errors.New("fail once")
		},
	})

	select {
	case got := <-seenDuringSecondAttempt:
		if got != 2*base {
			t.Fatalf("backoff during second attempt=%v, want %v", got, 2*base)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task never reached its second attempt")
	}

	waitFor(t, 2*time.Second, func() bool { return p.Status().IsIdle() })

	if got := backoffOf(); got != base {
		t.Fatalf("backoff after success=%v, want reset to base %v", got, base)
	}
}

func TestStatusCountsTransitions(t *testing.T) {
	p := newTestPool(1, time.Millisecond, 0)
	defer p.Close()

	started := make(chan struct{})
	block := make(chan struct{})
	p.Submit(&Task{
		Site: "status-site",
		Run: func(ctx context.Context) error {
			close(started)
			<-block
			return nil
		},
	})
	<-started

	if s := p.Status(); s.Running != 1 || s.Queued != 0 {
		t.Fatalf("while running: %+v, want Running=1 Queued=0", s)
	}

	p.Submit(&Task{Site: "status-site", Run: func(ctx context.Context) error { return nil }})
	if s := p.Status(); s.Running != 1 || s.Queued != 1 {
		t.Fatalf("with one queued behind: %+v, want Running=1 Queued=1", s)
	}

	close(block)
	waitFor(t, 2*time.Second, func() bool { return p.Status().IsIdle() })
}

func TestCloseCancelsPendingTasksAndRejectsSubmissions(t *testing.T) {
	base := 50 * time.Millisecond
	p := newTestPool(1, base, 5)

	errCh := make(chan error, 1)
	var once sync.Once
	started := make(chan struct{})
	p.Submit(&Task{
		Site: "closing",
		Run: func(ctx context.Context) error {
			once.Do(func() { close(started) })
			return errors.New("trigger retry backoff")
		},
		OnError: func(err error) { errCh <- err },
	})
	<-started // first attempt runs, then enters the backoff sleep

	p.Close() // cancel mid-backoff

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("OnError got %v, want context.Canceled", err)
		}
	default:
		t.Fatal("OnError was not called for a cancelled task")
	}

	if p.Submit(&Task{Site: "closing", Run: func(context.Context) error { return nil }}) {
		t.Fatal("Submit after Close must be rejected")
	}
}

func TestTerminalErrorSkipsRetries(t *testing.T) {
	p := newTestPool(1, 30*time.Millisecond, 5)
	defer p.Close()

	var attempts atomic.Int32
	errCh := make(chan error, 1)
	terminalErr := errors.New("needs user interaction")
	p.Submit(&Task{
		Site: "terminal",
		Run: func(ctx context.Context) error {
			attempts.Add(1)
			return terminalErr
		},
		NoRetry: func(err error) bool { return errors.Is(err, terminalErr) },
		OnError: func(err error) { errCh <- err },
	})

	select {
	case err := <-errCh:
		if !errors.Is(err, terminalErr) {
			t.Fatalf("OnError got %v, want the terminal error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnError was not called for a terminal error")
	}

	// Exactly one attempt: no retries despite maxRetries=5.
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts=%d, want 1 (terminal errors must not retry)", got)
	}
}

func TestMalformedTaskRejected(t *testing.T) {
	p := newTestPool(1, time.Millisecond, 0)
	defer p.Close()

	if p.Submit(nil) {
		t.Fatal("nil task must be rejected")
	}
	noSite := &Task{Run: func(context.Context) error { return nil }}
	if p.Submit(noSite) {
		t.Fatal("task without Site must be rejected")
	}
	noRun := &Task{Site: "x"}
	if p.Submit(noRun) {
		t.Fatal("task without Run must be rejected")
	}
}

func TestPanicInRunBecomesError(t *testing.T) {
	p := newTestPool(1, time.Millisecond, 1)
	defer p.Close()

	errCh := make(chan error, 1)
	p.Submit(&Task{
		Site: "panic-site",
		Run: func(ctx context.Context) error {
			panic("kaboom")
		},
		OnError: func(err error) { errCh <- err },
	})

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "panic") {
			t.Fatalf("expected panic converted to error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnError was not called after panic")
	}
}
