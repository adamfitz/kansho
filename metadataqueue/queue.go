// Package metadataqueue provides a single-worker queue for CBZ metadata
// operations (Check and Repair). Only one metadata file may be opened, closed
// or rewritten at a time, so every task runs sequentially on one goroutine.
//
// The queue is UI agnostic: callbacks may be invoked from its worker goroutine,
// so callers that touch widgets must marshal back to the UI thread themselves
// (e.g. via fyne.Do).
package metadataqueue

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Job is one queued or running metadata operation shown in the UI.
type Job struct {
	Action  string
	Title   string
	Chapter string
}

// Status is a snapshot of the queue. Jobs lists the running job first (when
// present) followed by the pending jobs in execution order.
type Status struct {
	Running int
	Queued  int
	Jobs    []Job
}

// IsIdle reports whether nothing is queued or running.
func (s Status) IsIdle() bool {
	return s.Running == 0 && s.Queued == 0
}

// Task describes one metadata operation.
type Task struct {
	// Action is the human readable operation name, e.g. "Check" or "Repair".
	Action string
	// Title is the manga title shown in status and dialogs.
	Title string
	// Chapter is the chapter label shown in status and dialogs.
	Chapter string

	// DedupeKey rejects duplicate submissions while an earlier task with the
	// same key is still pending or running. Optional.
	DedupeKey string

	// Run performs the operation. Required.
	Run func(ctx context.Context) error

	// OnSuccess runs exactly once when Run succeeds.
	OnSuccess func()
	// OnError runs exactly once when Run fails or the queue is closed.
	OnError func(err error)
}

// Queue is the single-worker metadata queue.
type Queue struct {
	mu      sync.Mutex
	pending []*Task
	current *Task
	dedupe  map[string]bool

	listener func(Status)

	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
	wg     sync.WaitGroup
}

// ErrClosed is returned to tasks that never ran because the queue closed.
var ErrClosed = fmt.Errorf("metadata queue closed")

var (
	globalQueue *Queue
	queueOnce   sync.Once
)

// Get returns the process-wide metadata queue.
func Get() *Queue {
	queueOnce.Do(func() {
		globalQueue = NewQueue()
	})
	return globalQueue
}

// NewQueue creates and starts a queue with one worker.
func NewQueue() *Queue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &Queue{
		dedupe: make(map[string]bool),
		ctx:    ctx,
		cancel: cancel,
		wake:   make(chan struct{}, 1),
	}
	q.wg.Add(1)
	go q.run()
	return q
}

// SetListener registers a callback invoked whenever the queue status changes,
// plus once immediately with the current status. Callbacks run on the queue
// worker or the submitting goroutine, so UI callers must marshal to their UI
// thread. Pass nil to detach.
func (q *Queue) SetListener(fn func(Status)) {
	q.mu.Lock()
	q.listener = fn
	snapshot := q.snapshotLocked()
	q.mu.Unlock()
	if fn != nil {
		fn(snapshot)
	}
}

// Status returns a snapshot of the current queue status.
func (q *Queue) Status() Status {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.snapshotLocked()
}

// Submit enqueues a metadata task. It returns false when the task is malformed,
// duplicates a pending/running DedupeKey, or the queue is closed.
func (q *Queue) Submit(task *Task) bool {
	if task == nil || task.Run == nil {
		log.Printf("[MetadataQueue] Rejected malformed task")
		return false
	}

	q.mu.Lock()
	if q.ctx.Err() != nil {
		q.mu.Unlock()
		log.Printf("[MetadataQueue] Queue closed - dropping %s for %s %s",
			task.Action, task.Title, task.Chapter)
		return false
	}
	if task.DedupeKey != "" && q.dedupe[task.DedupeKey] {
		q.mu.Unlock()
		return false
	}
	if task.DedupeKey != "" {
		q.dedupe[task.DedupeKey] = true
	}
	q.pending = append(q.pending, task)
	log.Printf("[MetadataQueue] Queued %s for %s %s",
		task.Action, task.Title, task.Chapter)
	q.notifyLocked()
	q.mu.Unlock()

	select {
	case q.wake <- struct{}{}:
	default:
	}
	return true
}

// Close stops the worker. The running task sees a cancelled context, pending
// tasks receive OnError, and Close waits for the worker to exit.
func (q *Queue) Close() {
	q.cancel()
	q.wg.Wait()
}

func (q *Queue) run() {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			q.drain()
			return
		case <-q.wake:
		}

		for {
			task := q.next()
			if task == nil {
				break
			}

			log.Printf("[MetadataQueue] Running %s for %s %s",
				task.Action, task.Title, task.Chapter)
			err := q.safeRun(task, q.ctx)
			if q.ctx.Err() != nil {
				err = ErrClosed
			} else if err != nil {
				log.Printf("[MetadataQueue] ✗ %s for %s %s failed: %v",
					task.Action, task.Title, task.Chapter, err)
			} else {
				log.Printf("[MetadataQueue] ✓ %s for %s %s finished",
					task.Action, task.Title, task.Chapter)
			}

			q.finish(task)
			if err == nil {
				task.OnSuccessSafe()
			} else {
				task.OnErrorSafe(err)
			}

			if q.ctx.Err() != nil {
				q.drain()
				return
			}
		}
	}
}

func (q *Queue) next() *Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return nil
	}
	task := q.pending[0]
	q.pending = q.pending[1:]
	q.current = task
	q.notifyLocked()
	return task
}

func (q *Queue) finish(task *Task) {
	q.mu.Lock()
	q.current = nil
	if task.DedupeKey != "" {
		delete(q.dedupe, task.DedupeKey)
	}
	q.notifyLocked()
	q.mu.Unlock()
}

func (q *Queue) drain() {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.mu.Unlock()
			return
		}
		task := q.pending[0]
		q.pending = q.pending[1:]
		if task.DedupeKey != "" {
			delete(q.dedupe, task.DedupeKey)
		}
		q.notifyLocked()
		q.mu.Unlock()
		task.OnErrorSafe(ErrClosed)
	}
}

func (q *Queue) snapshotLocked() Status {
	jobs := make([]Job, 0, len(q.pending)+1)
	running := 0
	if q.current != nil {
		running = 1
		jobs = append(jobs, Job{
			Action:  q.current.Action,
			Title:   q.current.Title,
			Chapter: q.current.Chapter,
		})
	}
	for _, task := range q.pending {
		jobs = append(jobs, Job{
			Action:  task.Action,
			Title:   task.Title,
			Chapter: task.Chapter,
		})
	}
	return Status{Running: running, Queued: len(q.pending), Jobs: jobs}
}

func (q *Queue) notifyLocked() {
	if q.listener != nil {
		q.listener(q.snapshotLocked())
	}
}

func (q *Queue) safeRun(task *Task, ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in metadata task for %s %s: %v",
				task.Title, task.Chapter, r)
		}
	}()
	return task.Run(ctx)
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
