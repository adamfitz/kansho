// Package refreshpool provides a dedicated worker pool exclusively for
// refreshing manga chapter lists (remote chapter fetches) so they never
// compete with the download queue's workers.
//
// Design:
//   - One goroutine per target site: submissions are keyed by site name and
//     every site has its own serial queue, so two chapter-list scrapes of the
//     same site never overlap (which would trigger rate limiting) while
//     different sites can be scraped simultaneously.
//   - A global semaphore caps the total number of parallel scrapes at
//     MaxWorkers regardless of how many sites are being refreshed.
//   - Each site tracks its own exponential backoff: after a failed scrape the
//     next retry waits BaseBackoff doubled on every further failure (capped at
//     MaxBackoff), and the delay resets to the base once a scrape succeeds.
//   - Tasks are retried up to MaxRetries times; scraping is slow and flaky by
//     nature, so the pool tolerates long waits ("it does not matter how long
//     these take").
//
// The pool is UI agnostic: callbacks may be invoked from any goroutine, so
// callers that touch widgets must marshal back to the UI thread themselves
// (e.g. via fyne.Do).
package refreshpool

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"kansho/parser"
)

const (
	// MaxWorkers is the maximum number of chapter-list scrapes that may run
	// in parallel across all sites.
	MaxWorkers = 10

	// BaseBackoff is the wait before the first retry of a failing task. It
	// doubles on every subsequent failure and resets to this value after a
	// successful scrape on that site.
	BaseBackoff = 5 * time.Second

	// MaxBackoff caps the exponential growth of the retry delay.
	MaxBackoff = 30 * time.Minute

	// MaxRetries is the number of retries after the initial attempt, i.e. a
	// task is attempted at most MaxRetries+1 times before OnError fires.
	MaxRetries = 10

	// siteQueueSize bounds how many tasks may wait on one site's worker.
	siteQueueSize = 64
)

// Status is a snapshot of what the pool is currently doing. It is the single
// representation of the whole pool shown in the main window status bar.
type Status struct {
	Running int // scrapes currently executing
	Queued  int // tasks waiting for their site worker or a free slot
}

// IsIdle reports whether nothing is queued or running.
func (s Status) IsIdle() bool {
	return s.Running == 0 && s.Queued == 0
}

// Task describes a single chapter-list refresh job.
type Task struct {
	// Site identifies the target site; tasks sharing a Site value are always
	// executed sequentially by the same worker goroutine. Required.
	Site string

	// Desc is a human readable description used in log lines (e.g. the manga
	// title). Optional.
	Desc string

	// DedupeKey optionally rejects duplicate submissions while an earlier
	// task with the same key is still pending or running. Optional.
	DedupeKey string

	// Run performs ONE scrape attempt. Retries, backoff sleeps and context
	// handling are owned by the pool. Required.
	Run func(ctx context.Context) error

	// NoRetry optionally reports whether err is terminal, i.e. retrying
	// cannot help and the task must fail immediately (e.g. a Cloudflare
	// challenge that waits for the user to import bypass data). When it
	// returns true OnError fires right away with no further attempts.
	// Optional; without it every error is retried.
	NoRetry func(err error) bool

	// OnSuccess runs exactly once when Run succeeds (after any retries).
	OnSuccess func()

	// OnError runs exactly once when Run fails MaxRetries+1 attempts, fails
	// terminally per NoRetry, or the pool is closed / cancelled mid-flight.
	// Receives the last error.
	OnError func(err error)
}

func (t *Task) describe() string {
	if t.Desc != "" {
		return t.Desc
	}
	return "unnamed task"
}

// siteWorker owns the serial execution and backoff state of a single site.
type siteWorker struct {
	queue chan *Task

	// backoff is the delay applied before this site's next retry. It grows
	// exponentially while scrapes keep failing and resets to the base value
	// on success, so a rate-limited site cools down across consecutive tasks.
	backoff time.Duration
}

// Pool is the chapter-list refresh worker pool. Create it via NewPool or use
// the process-wide singleton returned by Get.
type Pool struct {
	mu     sync.Mutex
	sites  map[string]*siteWorker
	dedupe map[string]bool

	sem     chan struct{} // global cap of MaxWorkers parallel scrapes
	queued  int
	running int

	listener func(Status)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	baseBackoff time.Duration
	maxRetries  int
	maxBackoff  time.Duration
}

var (
	globalPool *Pool
	poolOnce   sync.Once
)

// Get returns the process-wide refresh pool with default settings. This is
// the instance wired into the UI; tests use NewPool instead.
func Get() *Pool {
	poolOnce.Do(func() {
		globalPool = NewPool(MaxWorkers, BaseBackoff, MaxBackoff, MaxRetries)
	})
	return globalPool
}

// NewPool creates a pool with explicit limits; arguments <= 0 fall back to
// the package defaults. Exposed mainly for testing.
func NewPool(workers int, baseBackoff, maxBackoff time.Duration, maxRetries int) *Pool {
	if workers <= 0 {
		workers = MaxWorkers
	}
	if baseBackoff <= 0 {
		baseBackoff = BaseBackoff
	}
	if maxBackoff < baseBackoff {
		maxBackoff = baseBackoff
	}
	if maxRetries < 0 {
		maxRetries = MaxRetries
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		sites:       make(map[string]*siteWorker),
		dedupe:      make(map[string]bool),
		sem:         make(chan struct{}, workers),
		ctx:         ctx,
		cancel:      cancel,
		baseBackoff: baseBackoff,
		maxRetries:  maxRetries,
		maxBackoff:  maxBackoff,
	}
}

// SetListener registers a callback invoked whenever the pool status changes,
// plus once immediately with the current status. The callback runs while the
// pool lock is held and must therefore be fast and non-blocking; UI callers
// should hop to the UI thread inside it (e.g. fyne.Do). Pass nil to detach.
func (p *Pool) SetListener(fn func(Status)) {
	p.mu.Lock()
	p.listener = fn
	snapshot := p.snapshotLocked()
	p.mu.Unlock()
	if fn != nil {
		fn(snapshot)
	}
}

// Status returns a snapshot of the current pool status.
func (p *Pool) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotLocked()
}

// Submit enqueues a chapter-list refresh task. It returns false if the task
// is nil/malformed, duplicates a pending DedupeKey, or its site queue is full.
func (p *Pool) Submit(task *Task) bool {
	if task == nil || task.Site == "" || task.Run == nil {
		site, desc := taskSiteDesc(task)
		log.Printf("[RefreshPool] Rejected malformed task (site=%q desc=%q)", site, desc)
		return false
	}

	p.mu.Lock()
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		log.Printf("[RefreshPool] Pool closed - dropping fetch for %s/%s", task.Site, task.describe())
		return false
	}
	if task.DedupeKey != "" && p.dedupe[task.DedupeKey] {
		p.mu.Unlock()
		return false
	}

	w := p.sites[task.Site]
	if w == nil {
		w = &siteWorker{queue: make(chan *Task, siteQueueSize), backoff: p.baseBackoff}
		p.sites[task.Site] = w
		p.wg.Add(1)
		go p.runWorker(task.Site, w)
	}

	select {
	case w.queue <- task:
	default:
		p.mu.Unlock()
		log.Printf("[RefreshPool] Queue full for %s - dropping fetch for %s", task.Site, task.describe())
		return false
	}

	if task.DedupeKey != "" {
		p.dedupe[task.DedupeKey] = true
	}
	p.queued++
	log.Printf("[RefreshPool] Queued %s/%s", task.Site, task.describe())
	p.notifyLocked()
	p.mu.Unlock()
	return true
}

// Close cancels the pool: running scrapes see their context cancelled,
// waiting tasks receive OnError(context.Canceled), and Close blocks until all
// site workers have exited.
func (p *Pool) Close() {
	p.cancel()
	p.wg.Wait()
}

// snapshotLocked must be called with p.mu held.
func (p *Pool) snapshotLocked() Status {
	return Status{Running: p.running, Queued: p.queued}
}

// notifyLocked forwards the current status to the listener, if any. Must be
// called with p.mu held.
func (p *Pool) notifyLocked() {
	if p.listener != nil {
		p.listener(p.snapshotLocked())
	}
}

// removeDedupeLocked clears a finished task's dedupe marker. Must be called
// with p.mu held.
func (p *Pool) removeDedupeLocked(task *Task) {
	if task.DedupeKey != "" {
		delete(p.dedupe, task.DedupeKey)
	}
}

// runWorker is the body of one site's dedicated worker goroutine. It executes
// that site's tasks strictly sequentially until the pool is closed.
func (p *Pool) runWorker(site string, w *siteWorker) {
	defer p.wg.Done()
	for {
		select {
		case <-p.ctx.Done():
			// Drain anything still queued so callers get a terminal callback.
			for {
				select {
				case t := <-w.queue:
					p.abandon(t)
				default:
					return
				}
			}
		case task := <-w.queue:
			p.runTask(w, task)
		}
	}
}

// runTask executes one task with the pool-owned retry/backoff policy:
// initial attempt followed by up to maxRetries retries, waiting the site's
// current backoff between attempts and doubling it after each failure. On
// success the site's backoff resets to the base value.
func (p *Pool) runTask(w *siteWorker, task *Task) {
	// Wait for a free global slot; the site stays serialized because each
	// site only ever has this one worker.
	select {
	case p.sem <- struct{}{}:
	case <-p.ctx.Done():
		p.finish(task, context.Canceled, false)
		return
	}
	defer func() { <-p.sem }()

	p.mu.Lock()
	p.queued--
	p.running++
	p.notifyLocked()
	p.mu.Unlock()

	attempt := 0
	for {
		err := p.safeRun(task)
		if err == nil {
			p.mu.Lock()
			w.backoff = p.baseBackoff
			p.mu.Unlock()
			log.Printf("[RefreshPool] ✓ Fetched chapters for %s/%s", task.Site, task.describe())
			p.finish(task, nil, true)
			return
		}

		if task.NoRetry != nil && task.NoRetry(err) {
			log.Printf("[RefreshPool] ✗ %s/%s failed terminally (no retry): %v",
				task.Site, task.describe(), err)
			p.finish(task, err, false)
			return
		}

		if attempt >= p.maxRetries {
			log.Printf("[RefreshPool] ✗ Giving up on %s/%s after %d attempts: %v",
				task.Site, task.describe(), attempt+1, err)
			p.finish(task, err, false)
			return
		}

		wait := w.backoff
		log.Printf("[RefreshPool] Failed %s/%s (attempt %d/%d): %v - retrying in %v",
			task.Site, task.describe(), attempt+1, p.maxRetries+1, err, wait)

		p.mu.Lock()
		w.backoff *= 2
		if w.backoff > p.maxBackoff {
			w.backoff = p.maxBackoff
		}
		p.mu.Unlock()

		if !parser.SleepCtx(p.ctx, wait) {
			log.Printf("[RefreshPool] Cancelled during backoff - dropping fetch for %s/%s",
				task.Site, task.describe())
			p.finish(task, context.Canceled, false)
			return
		}
		attempt++
	}
}

// abandon terminates a task that never ran because the pool was closed.
func (p *Pool) abandon(task *Task) {
	p.mu.Lock()
	p.queued--
	p.removeDedupeLocked(task)
	p.notifyLocked()
	p.mu.Unlock()
	task.OnErrorSafe(context.Canceled)
}

// finish releases a completed task: updates counters, clears its dedupe
// marker and invokes exactly one of the terminal callbacks.
func (p *Pool) finish(task *Task, err error, success bool) {
	p.mu.Lock()
	p.running--
	p.removeDedupeLocked(task)
	p.notifyLocked()
	p.mu.Unlock()

	if success {
		task.OnSuccessSafe()
	} else {
		task.OnErrorSafe(err)
	}
}

// safeRun invokes Run once, converting panics into errors so a broken task
// cannot take down its site worker.
func (p *Pool) safeRun(task *Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in refresh task for %s/%s: %v", task.Site, task.describe(), r)
		}
	}()
	return task.Run(p.ctx)
}

// OnSuccessSafe invokes OnSuccess if set.
func (t *Task) OnSuccessSafe() {
	if t.OnSuccess != nil {
		t.OnSuccess()
	}
}

// OnErrorSafe invokes OnError if set.
func (t *Task) OnErrorSafe(err error) {
	if t.OnError != nil {
		t.OnError(err)
	}
}

func taskSiteDesc(task *Task) (site, desc string) {
	if task == nil {
		return "<nil>", "<nil>"
	}
	return task.Site, task.describe()
}
