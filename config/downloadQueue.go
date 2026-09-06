package config

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"kansho/cf"
)

// cfWaitTimeout is how long the queue waits for the user to provide Cloudflare
// bypass data after a challenge before moving on to queued chapters that do not
// need it. It is a variable so tests can shorten it.
var cfWaitTimeout = 5 * time.Minute

// cfWaitPollInterval is how often the queue re-checks for freshly imported
// Cloudflare bypass data while paused.
var cfWaitPollInterval = 2 * time.Second

// cfDataAvailable reports whether Cloudflare bypass data is stored for a
// domain. It is a variable so tests can substitute a fake check without
// touching the real config directory.
var cfDataAvailable = func(domain string) bool {
	_, err := cf.LoadFromFile(domain)
	return err == nil
}

// DownloadTask represents a single download task. Since the UI refactor, a task
// is a single chapter of a manga (previously a task was an entire manga).
type DownloadTask struct {
	ID            string    // Unique ID for this task
	Manga         Bookmarks // Changed from pointer to value - this creates a copy!
	Chapter       string    // CBZ filename being downloaded, e.g. "ch001.cbz" ("" for legacy manga-level tasks)
	ChapterURL    string    // URL of the chapter to download ("" for legacy manga-level tasks)
	Status        string    // "queued", "downloading", "completed", "cancelled", "failed", "waiting_cf", "skipped_cf"
	Progress      float64   // 0.0 to 1.0
	StatusMessage string
	CancelFunc    context.CancelFunc
	Error         error

	// Chapter tracking
	ActualChapter   int
	CurrentDownload int
	TotalFound      int
}

// maxDownloadWorkers caps how many chapter downloads may run at once across the
// whole queue. A worker downloads from a single site at a time, so with this
// value at most three different sites download simultaneously. It is a variable
// so tests can exercise the pool with a smaller pool.
var maxDownloadWorkers = 3

// DownloadQueue manages the download queue as a worker pool. Up to
// maxDownloadWorkers tasks run concurrently, FIFO across the queue, with at
// most one download per site at a time (so up to maxDownloadWorkers different
// sites download at once).
type DownloadQueue struct {
	tasks []*DownloadTask
	mu    sync.RWMutex

	// Worker pool state. running is the number of tasks currently executing;
	// cfWaits is the number of handleCfWait goroutines currently blocked on a
	// Cloudflare challenge. wake is signalled (non-blocking) every time the set
	// of dispatchable tasks may have changed, and startOnce guarantees the
	// dispatcher goroutine runs exactly once.
	maxWorkers   int
	running      int
	cfWaits      int
	idleNotified bool
	wake         chan struct{}
	startOnce    sync.Once

	// Callbacks for UI updates
	onTaskAdded   func(*DownloadTask)
	onTaskUpdated func(*DownloadTask)
	onTaskRemoved func(string)
	onQueueEmpty  func()
}

// Global download queue instance
var globalQueue *DownloadQueue
var queueOnce sync.Once

// GetDownloadQueue returns the singleton download queue
func GetDownloadQueue() *DownloadQueue {
	queueOnce.Do(func() {
		globalQueue = &DownloadQueue{
			tasks:      make([]*DownloadTask, 0),
			maxWorkers: maxDownloadWorkers,
			wake:       make(chan struct{}, 1),
		}
	})
	return globalQueue
}

// SetCallbacks sets the UI update callbacks
func (q *DownloadQueue) SetCallbacks(
	onAdded func(*DownloadTask),
	onUpdated func(*DownloadTask),
	onRemoved func(string),
	onEmpty func(),
) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.onTaskAdded = onAdded
	q.onTaskUpdated = onUpdated
	q.onTaskRemoved = onRemoved
	q.onQueueEmpty = onEmpty
}

// AddChapterTask adds a single chapter of a manga to the download queue.
//
// Parameters:
//   - manga: The manga bookmark the chapter belongs to
//   - chapter: The CBZ filename to produce, e.g. "ch001.cbz"
//   - chapterURL: The URL of the chapter on the target site
func (q *DownloadQueue) AddChapterTask(manga *Bookmarks, chapter, chapterURL string) (*DownloadTask, error) {
	q.mu.Lock()

	// Check if this chapter is already in queue
	for _, task := range q.tasks {
		if task.Manga.Title == manga.Title && task.Chapter == chapter {
			q.mu.Unlock()
			return nil, fmt.Errorf("chapter '%s' for '%s' is already in download queue", chapter, manga.Title)
		}
	}

	// CRITICAL FIX: Create a copy of the manga data
	// This prevents the task from being affected by changes to the original bookmarks
	mangaCopy := *manga

	task := &DownloadTask{
		ID:            fmt.Sprintf("%s-%s-%d", manga.Shortname, chapter, len(q.tasks)),
		Manga:         mangaCopy, // Store the copy, not a pointer
		Chapter:       chapter,
		ChapterURL:    chapterURL,
		Status:        "queued",
		StatusMessage: "Waiting in queue...",
		Progress:      0.0,
	}

	q.tasks = append(q.tasks, task)
	q.idleNotified = false
	q.mu.Unlock()

	log.Printf("[Queue] Added chapter task: %s - %s (%s)", task.Manga.Title, task.Chapter, task.ID)

	if q.onTaskAdded != nil {
		q.onTaskAdded(task)
	}

	// Start the worker pool if it is not running, then wake it for the new task.
	q.startDispatcher()

	return task, nil
}

// AddTask adds a legacy whole-manga download to the queue.
// Deprecated: use AddChapterTask for per-chapter downloads.
func (q *DownloadQueue) AddTask(manga *Bookmarks) (*DownloadTask, error) {
	q.mu.Lock()

	// Check if this manga is already in queue
	for _, task := range q.tasks {
		if task.Manga.Title == manga.Title {
			q.mu.Unlock()
			return nil, fmt.Errorf("manga '%s' is already in download queue", manga.Title)
		}
	}

	// CRITICAL FIX: Create a copy of the manga data
	// This prevents the task from being affected by changes to the original bookmarks
	mangaCopy := *manga

	task := &DownloadTask{
		ID:            fmt.Sprintf("%s-%d", manga.Shortname, len(q.tasks)),
		Manga:         mangaCopy, // Store the copy, not a pointer
		Status:        "queued",
		StatusMessage: "Waiting in queue...",
		Progress:      0.0,
	}

	q.tasks = append(q.tasks, task)
	q.idleNotified = false
	q.mu.Unlock()

	log.Printf("[Queue] Added task: %s (%s) - Location: %s", task.Manga.Title, task.ID, task.Manga.Location)

	if q.onTaskAdded != nil {
		q.onTaskAdded(task)
	}

	// Start the worker pool if it is not running, then wake it for the new task.
	q.startDispatcher()

	return task, nil
}

// GetTaskForChapter returns the task for the given manga title and chapter,
// or nil if no such task is in the queue.
func (q *DownloadQueue) GetTaskForChapter(mangaTitle, chapter string) *DownloadTask {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, task := range q.tasks {
		if task.Manga.Title == mangaTitle && task.Chapter == chapter {
			return task
		}
	}
	return nil
}

// ChapterQueued returns true if a task for the given manga title and chapter
// is already present in the queue.
func (q *DownloadQueue) ChapterQueued(mangaTitle, chapter string) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, task := range q.tasks {
		if task.Manga.Title == mangaTitle && task.Chapter == chapter {
			return true
		}
	}
	return false
}

// RetryTask retries a task that did not finish: it failed, was cancelled, or
// is waiting on a CF challenge.
func (q *DownloadQueue) RetryTask(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, task := range q.tasks {
		if task.ID == id {
			if task.Status == "waiting_cf" || task.Status == "skipped_cf" || task.Status == "failed" || task.Status == "cancelled" {
				log.Printf("[Queue] Retrying task: %s", task.Manga.Title)
				task.Status = "queued"
				task.StatusMessage = "Retrying..."
				task.Error = nil
				q.idleNotified = false

				if q.onTaskUpdated != nil {
					q.onTaskUpdated(task)
				}

				// Wake the worker pool so the retried task is dispatched.
				q.startDispatcher()
				return nil
			}
			return fmt.Errorf("task cannot be retried (status: %s)", task.Status)
		}
	}

	return fmt.Errorf("task not found: %s", id)
}

// GetTasks returns a copy of all tasks
func (q *DownloadQueue) GetTasks() []*DownloadTask {
	q.mu.RLock()
	defer q.mu.RUnlock()

	tasksCopy := make([]*DownloadTask, len(q.tasks))
	copy(tasksCopy, q.tasks)
	return tasksCopy
}

// RemoveTask removes any task from the queue by ID, regardless of status.
// It is used to drop stale (non-active) tasks so a chapter can be re-queued.
func (q *DownloadQueue) RemoveTask(id string) error {
	q.mu.Lock()

	for i, task := range q.tasks {
		if task.ID == id {
			q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)
			q.mu.Unlock()

			if q.onTaskRemoved != nil {
				q.onTaskRemoved(id)
			}
			return nil
		}
	}

	q.mu.Unlock()
	return fmt.Errorf("task not found: %s", id)
}

// GetTask returns a specific task by ID
func (q *DownloadQueue) GetTask(id string) *DownloadTask {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, task := range q.tasks {
		if task.ID == id {
			return task
		}
	}
	return nil
}

// CancelTask cancels a specific task (either downloading or queued)
func (q *DownloadQueue) CancelTask(id string) error {
	q.mu.Lock()

	for i, task := range q.tasks {
		if task.ID == id {
			if (task.Status == "downloading" || task.Status == "waiting_cf") && task.CancelFunc != nil {
				log.Printf("[Queue] Cancelling active download: %s", task.Manga.Title)
				// Immediately show cancelling status to the user
				task.Status = "cancelled"
				task.StatusMessage = "Cancelling..."

				// Notify UI immediately before the slow context cancellation unwinds
				if q.onTaskUpdated != nil {
					q.onTaskUpdated(task)
				}
				q.mu.Unlock()

				// Trigger cancellation - the download will notice and return quickly now
				// thanks to context-aware retry sleeps and rate limiter waits
				task.CancelFunc()

				// The executeTask goroutine will set the final status when it returns
				return nil
			} else if task.Status == "queued" || task.Status == "skipped_cf" {
				log.Printf("[Queue] Removing queued task: %s", task.Manga.Title)
				// Remove from queue
				q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)

				q.mu.Unlock()

				if q.onTaskRemoved != nil {
					q.onTaskRemoved(id)
				}
				return nil
			} else {
				q.mu.Unlock()
				return fmt.Errorf("task is not active or queued (status: %s)", task.Status)
			}
		}
	}

	q.mu.Unlock()
	return fmt.Errorf("task not found: %s", id)
}

// CancelAll cancels all tasks
func (q *DownloadQueue) CancelAll() {
	q.mu.Lock()

	log.Printf("[Queue] Cancelling all tasks (%d total)", len(q.tasks))

	// Step 1: Immediately mark all tasks as cancelled and notify UI
	var cancelFuncs []context.CancelFunc
	for _, task := range q.tasks {
		if (task.Status == "downloading" || task.Status == "waiting_cf") && task.CancelFunc != nil {
			task.Status = "cancelled"
			task.StatusMessage = "Cancelling..."
			cancelFuncs = append(cancelFuncs, task.CancelFunc)
		} else if task.Status == "queued" || task.Status == "skipped_cf" {
			task.Status = "cancelled"
			task.StatusMessage = "Cancelled by user"
		}

		if q.onTaskUpdated != nil {
			q.onTaskUpdated(task)
		}
	}

	q.mu.Unlock()

	// Step 2: Trigger context cancellations (no lock held)
	for _, cancel := range cancelFuncs {
		cancel()
	}
}

// CancelMangaTasks cancels every active task (downloading, waiting on a CF
// challenge, or queued) whose manga title matches. Completed, failed and
// already-cancelled tasks are left untouched.
func (q *DownloadQueue) CancelMangaTasks(mangaTitle string) {
	q.mu.Lock()

	log.Printf("[Queue] Cancelling tasks for manga: %s", mangaTitle)

	var cancelFuncs []context.CancelFunc
	for _, task := range q.tasks {
		if task.Manga.Title != mangaTitle {
			continue
		}
		if (task.Status == "downloading" || task.Status == "waiting_cf") && task.CancelFunc != nil {
			task.Status = "cancelled"
			task.StatusMessage = "Cancelling..."
			cancelFuncs = append(cancelFuncs, task.CancelFunc)
		} else if task.Status == "queued" || task.Status == "skipped_cf" {
			task.Status = "cancelled"
			task.StatusMessage = "Cancelled by user"
		}

		if q.onTaskUpdated != nil {
			q.onTaskUpdated(task)
		}
	}

	q.mu.Unlock()

	for _, cancel := range cancelFuncs {
		cancel()
	}
}

// ClearRetries removes every task that did not finish and can be retried
// (failed, cancelled, waiting on a CF challenge, or CF-skipped) from the queue,
// across ALL manga titles. Active downloads and still-queued tasks are left
// untouched. Unlike CancelAll, the tasks are removed outright, so nothing is
// left to retry.
func (q *DownloadQueue) ClearRetries() {
	q.mu.Lock()

	log.Printf("[Queue] Clearing retryable tasks (%d total)", len(q.tasks))

	var cancelFuncs []context.CancelFunc
	removed := make([]string, 0, len(q.tasks))
	newTasks := make([]*DownloadTask, 0, len(q.tasks))
	for _, task := range q.tasks {
		if !isRetryableStatus(task.Status) {
			newTasks = append(newTasks, task)
			continue
		}
		// Abort any CF-wait goroutine still parked on this task before removing
		// it, so queue processing can move on to the next queued task.
		if task.CancelFunc != nil {
			task.Status = "cancelled"
			cancelFuncs = append(cancelFuncs, task.CancelFunc)
		}
		removed = append(removed, task.ID)
	}
	q.tasks = newTasks

	onTaskRemoved := q.onTaskRemoved
	q.mu.Unlock()

	for _, id := range removed {
		if onTaskRemoved != nil {
			onTaskRemoved(id)
		}
	}

	for _, cancel := range cancelFuncs {
		cancel()
	}
}

// isRetryableStatus reports whether a task status is one the user can retry:
// it failed, was cancelled, or is blocked on a Cloudflare challenge.
func isRetryableStatus(status string) bool {
	return status == "failed" || status == "cancelled" || status == "waiting_cf" || status == "skipped_cf"
}

// RemoveCompletedTasks removes all completed or cancelled tasks
func (q *DownloadQueue) RemoveCompletedTasks() {
	q.mu.Lock()
	defer q.mu.Unlock()

	newTasks := make([]*DownloadTask, 0)
	for _, task := range q.tasks {
		if task.Status == "queued" || task.Status == "downloading" || task.Status == "waiting_cf" || task.Status == "skipped_cf" {
			newTasks = append(newTasks, task)
		} else {
			if q.onTaskRemoved != nil {
				q.onTaskRemoved(task.ID)
			}
		}
	}

	q.tasks = newTasks
	log.Printf("[Queue] Cleaned up completed tasks, %d remaining", len(q.tasks))
}

// startDispatcher guarantees the dispatcher goroutine is running, then wakes it
// so any newly queued task is considered for dispatch.
func (q *DownloadQueue) startDispatcher() {
	if q.maxWorkers <= 0 {
		q.maxWorkers = maxDownloadWorkers
	}
	q.startOnce.Do(func() {
		if q.wake == nil {
			q.wake = make(chan struct{}, 1)
		}
		go q.dispatcherLoop()
	})
	q.signalWake()
}

// signalWake pokes the dispatcher (non-blocking). Call it whenever the set of
// dispatchable tasks may have changed: a task was added/retried/resumed, a
// download finished, or a CF wait re-queued or skipped tasks.
func (q *DownloadQueue) signalWake() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// dispatcherLoop is the single goroutine that owns dispatch. It fills free
// worker slots up to maxWorkers, then sleeps until the queue changes.
func (q *DownloadQueue) dispatcherLoop() {
	for {
		q.fillSlots()
		<-q.wake
	}
}

// fillSlots starts downloads for every currently dispatchable queued task, up
// to maxWorkers concurrent runs. A task is dispatchable when a worker slot is
// free, its site is not already downloading a chapter, and it would not open a
// second Cloudflare browser while another challenge is being resolved.
func (q *DownloadQueue) fillSlots() {
	for {
		q.mu.Lock()
		if q.running >= q.maxWorkers {
			q.mu.Unlock()
			return
		}
		task := q.nextDispatchable()
		if task == nil {
			onEmpty := q.maybeNotifyIdle()
			q.mu.Unlock()
			if onEmpty != nil {
				onEmpty()
			}
			return
		}

		log.Printf("[Queue] Dispatching task: %s (%s, site %s)", task.Manga.Title, task.Chapter, task.Manga.Site)
		task.Status = "downloading"
		task.StatusMessage = "Starting download..."
		task.CancelFunc = nil
		q.running++
		q.mu.Unlock()

		if q.onTaskUpdated != nil {
			q.onTaskUpdated(task)
		}

		go q.runTask(task)
	}
}

// nextDispatchable returns the first queued task that can start now, or nil.
// Callers must hold q.mu. Tasks are scanned in FIFO order, so within a site
// chapters keep their enqueue order; a site is busy while any of its tasks has
// status "downloading", so two workers never download from the same site.
func (q *DownloadQueue) nextDispatchable() *DownloadTask {
	busySites := make(map[string]bool)
	for _, t := range q.tasks {
		if t.Status == "downloading" {
			busySites[t.Manga.Site] = true
		}
	}

	for _, t := range q.tasks {
		if t.Status != "queued" {
			continue
		}
		if busySites[t.Manga.Site] {
			continue
		}
		// While any CF challenge is being resolved we must not start another
		// CF-protected download: each one that runs could open its own browser
		// window. Downloads that cannot need bypass data are unaffected.
		if q.cfWaits > 0 && taskNeedsCF(t) {
			continue
		}
		return t
	}
	return nil
}

// maybeNotifyIdle fires the onQueueEmpty callback the first time the queue
// becomes fully idle (nothing downloading and nothing queued). It returns the
// callback to invoke, or nil. Callers must hold q.mu.
func (q *DownloadQueue) maybeNotifyIdle() func() {
	if q.idleNotified || q.running > 0 {
		return nil
	}
	for _, t := range q.tasks {
		if t.Status == "queued" {
			return nil
		}
	}
	q.idleNotified = true
	return q.onQueueEmpty
}

// runTask executes one dispatched task in a worker goroutine. On completion it
// releases the worker slot so the dispatcher can start the next task. A task
// blocked on a Cloudflare challenge keeps the queue gated via handleCfWait so a
// second CF-protected download does not open another browser.
func (q *DownloadQueue) runTask(task *DownloadTask) {
	defer func() {
		q.mu.Lock()
		q.running--
		q.mu.Unlock()
		q.signalWake()
	}()

	q.executeTask(task)

	if task.Status == "waiting_cf" {
		// Bump the gate synchronously (before releasing the worker slot) so the
		// dispatcher never starts another CF-protected download in the window
		// between the challenge and handleCfWait starting. handleCfWait releases
		// the gate when it finishes.
		q.mu.Lock()
		q.cfWaits++
		q.mu.Unlock()
		go q.handleCfWait(task)
	}
}

// executeTask executes a download task
func (q *DownloadQueue) executeTask(task *DownloadTask) {
	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	q.mu.Lock()
	task.Status = "downloading"
	task.StatusMessage = "Starting download..."
	task.CancelFunc = cancel
	q.mu.Unlock()

	if q.onTaskUpdated != nil {
		q.onTaskUpdated(task)
	}

	// Progress callback
	progressCallback := func(status string, progress float64, actualChapter, currentDownload, totalFound int) {
		q.mu.Lock()
		task.Progress = progress
		task.StatusMessage = status
		task.ActualChapter = actualChapter
		task.CurrentDownload = currentDownload
		task.TotalFound = totalFound
		q.mu.Unlock()

		if q.onTaskUpdated != nil {
			q.onTaskUpdated(task)
		}
	}

	// CRITICAL: Pass a pointer to the manga copy
	// This ensures the download uses the snapshot taken when the task was created
	log.Printf("[Queue] Starting download for: %s to location: %s", task.Manga.Title, task.Manga.Location)
	var err error
	if task.Chapter != "" && task.ChapterURL != "" {
		err = ExecuteChapterDownload(ctx, &task.Manga, task.ChapterURL, task.Chapter, progressCallback)
	} else {
		err = ExecuteSiteDownload(ctx, &task.Manga, progressCallback)
	}

	q.mu.Lock()
	if err != nil {
		if errors.Is(err, context.Canceled) {
			task.Status = "cancelled"
			task.StatusMessage = "Cancelled by user"
		} else {
			// Check if this is a Cloudflare challenge error (including wrapped errors)
			var cfErr *cf.CfChallengeError
			if errors.As(err, &cfErr) {
				task.Status = "waiting_cf"
				task.StatusMessage = "Cloudflare challenge detected - browser opened"
				task.Error = cfErr

				log.Printf("[Queue] CF challenge detected for %s (URL: %s)", task.Manga.Title, cfErr.URL)

				q.mu.Unlock()
				if q.onTaskUpdated != nil {
					q.onTaskUpdated(task)
				}
				return
			}

			task.Status = "failed"
			task.StatusMessage = fmt.Sprintf("Error: %v", err)
			task.Error = err
		}
	} else {
		task.Status = "completed"
		task.StatusMessage = "Download complete"
		task.Progress = 1.0
	}
	task.CancelFunc = nil
	q.mu.Unlock()

	if q.onTaskUpdated != nil {
		q.onTaskUpdated(task)
	}

	if task.Status == "completed" {
		// Successfully downloaded chapters are removed from the queue so they do
		// not linger. A manga title disappears from the queue once all of its
		// queued chapters have completed. Unfinished chapters (failed, cancelled
		// or waiting on a CF challenge) are kept so the user can retry them.
		if err := q.RemoveTask(task.ID); err != nil {
			log.Printf("[Queue] Failed to remove completed task %s: %v", task.ID, err)
		}
	}

	log.Printf("[Queue] Task completed: %s (status: %s)", task.Manga.Title, task.Status)
}

// handleCfWait gates CF-protected downloads after a Cloudflare challenge so the
// user can provide bypass data. While it runs, cfWaits is bumped so the worker
// pool does not start another download that needs Cloudflare bypass data — only
// the single browser window already opened by the blocked download fires.
// Downloads that do not need Cloudflare continue on other worker slots.
// See openspec/specs/download-queue/spec.md ("CF Challenge Handling").
//
// It returns once bypass data for the blocked domain is detected (the task is
// re-queued so it downloads normally on the next iteration), or once
// cfWaitTimeout elapses without data (the remaining CF-protected queued tasks
// are marked as skipped so downloads that do not need Cloudflare can proceed).
func (q *DownloadQueue) handleCfWait(task *DownloadTask) {
	defer func() {
		q.mu.Lock()
		q.cfWaits--
		q.mu.Unlock()
		q.signalWake()
	}()

	cfErr, ok := task.Error.(*cf.CfChallengeError)
	if !ok || cfErr == nil {
		return
	}

	domain := domainFromURL(cfErr.URL)
	log.Printf("[Queue] CF challenge detected - pausing queue, waiting for CF data for domain: %s", domain)

	deadline := time.Now().Add(cfWaitTimeout)
	ticker := time.NewTicker(cfWaitPollInterval)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		// Stop waiting if the task (or the whole queue) was cancelled.
		q.mu.RLock()
		status := task.Status
		q.mu.RUnlock()
		if status != "waiting_cf" {
			log.Printf("[Queue] CF wait aborted for %s (status: %s)", task.Manga.Title, status)
			return
		}

		if cfDataAvailable(domain) {
			log.Printf("[Queue] CF data received for domain %s - resuming download for %s", domain, task.Manga.Title)
			q.mu.Lock()
			task.Status = "queued"
			task.StatusMessage = "CF data received - resuming download"
			task.Error = nil
			q.mu.Unlock()
			if q.onTaskUpdated != nil {
				q.onTaskUpdated(task)
			}
			return
		}

		<-ticker.C
	}

	log.Printf("[Queue] No CF data provided within %v for domain %s - skipping CF-protected queued tasks", cfWaitTimeout, domain)

	// The task that actually hit the challenge stays as waiting_cf so the user
	// can retry it later; the remaining CF-protected queued tasks are skipped so
	// downloads that do not need Cloudflare can start.
	q.skipCfBlockedTasks(task.ID)
}

// skipCfBlockedTasks marks queued tasks that require Cloudflare bypass data
// (and have none stored) as skipped_cf, leaving them in the queue for the user
// to retry. The task that originally hit the challenge is left as waiting_cf.
func (q *DownloadQueue) skipCfBlockedTasks(blockedTaskID string) {
	q.mu.Lock()
	var updated []*DownloadTask
	for _, t := range q.tasks {
		if t.ID == blockedTaskID || t.Status != "queued" {
			continue
		}
		if taskNeedsCF(t) {
			t.Status = "skipped_cf"
			t.StatusMessage = "No Cloudflare data provided - skipped"
			updated = append(updated, t)
		}
	}
	q.mu.Unlock()

	for _, t := range updated {
		if q.onTaskUpdated != nil {
			q.onTaskUpdated(t)
		}
	}
}

// ResumeCfTasks re-queues every task that is blocked on a Cloudflare challenge
// (waiting_cf or skipped_cf) and whose bypass data is now available, then
// restarts queue processing so those downloads resume automatically. Tasks that
// still have no data for their domain stay blocked.
//
// This is called by the UI after the user imports Cloudflare bypass data, so
// CF-protected downloads that were paused or skipped resume as soon as the data
// is provided instead of waiting for a manual retry.
// See openspec/specs/download-queue/spec.md ("CF Challenge Handling").
func (q *DownloadQueue) ResumeCfTasks() {
	q.mu.Lock()
	var resumed []*DownloadTask
	for _, t := range q.tasks {
		if t.Status != "waiting_cf" && t.Status != "skipped_cf" {
			continue
		}
		if taskNeedsCF(t) {
			continue
		}
		t.Status = "queued"
		t.StatusMessage = "CF data received - resuming download"
		t.Error = nil
		resumed = append(resumed, t)
	}
	q.mu.Unlock()

	for _, t := range resumed {
		if q.onTaskUpdated != nil {
			q.onTaskUpdated(t)
		}
	}

	if len(resumed) > 0 {
		log.Printf("[Queue] Resumed %d CF-blocked tasks with fresh bypass data", len(resumed))
		q.startDispatcher()
	}
}

// taskNeedsCF reports whether a queued task would need Cloudflare bypass data
// to download: its site is registered as CF-protected AND no bypass data is
// currently stored for the manga's domain.
func taskNeedsCF(task *DownloadTask) bool {
	if !SiteNeedsCF(task.Manga.Site) {
		return false
	}
	if task.Manga.Url != "" {
		if cfDataAvailable(domainFromURL(task.Manga.Url)) {
			return false
		}
	}
	return true
}

// domainFromURL returns the hostname portion of a URL, or the raw string when
// it cannot be parsed. This matches the domain key under which Cloudflare
// bypass data is stored by the downloader.
func domainFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" {
		return rawURL
	}
	return parsed.Hostname()
}
