package config

import (
	"context"
	"fmt"
	"log"
	"sync"

	"kansho/cf"
)

// DownloadTask represents a single manga download task
type DownloadTask struct {
	ID            string // Unique ID for this task
	Manga         *Bookmarks
	Status        string  // "queued", "downloading", "completed", "cancelled", "failed", "waiting_cf"
	Progress      float64 // 0.0 to 1.0
	StatusMessage string
	CancelFunc    context.CancelFunc
	Error         error

	// Chapter tracking
	ActualChapter   int
	CurrentDownload int
	TotalFound      int
}

// DownloadQueue manages FIFO download queue
type DownloadQueue struct {
	tasks        []*DownloadTask
	mu           sync.RWMutex
	processing   bool
	processingMu sync.Mutex

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
			tasks: make([]*DownloadTask, 0),
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

// AddTask adds a manga download to the queue
func (q *DownloadQueue) AddTask(manga *Bookmarks) (*DownloadTask, error) {
	q.mu.Lock()

	// Check if this manga is already in queue
	for _, task := range q.tasks {
		if task.Manga.Title == manga.Title {
			q.mu.Unlock()
			return nil, fmt.Errorf("manga '%s' is already in download queue", manga.Title)
		}
	}

	task := &DownloadTask{
		ID:            fmt.Sprintf("%s-%d", manga.Shortname, len(q.tasks)),
		Manga:         manga,
		Status:        "queued",
		StatusMessage: "Waiting in queue...",
		Progress:      0.0,
	}

	q.tasks = append(q.tasks, task)
	q.mu.Unlock()

	log.Printf("[Queue] Added task: %s (%s)", task.Manga.Title, task.ID)

	if q.onTaskAdded != nil {
		q.onTaskAdded(task)
	}

	// Start processing if not already running
	go q.processQueue()

	return task, nil
}

// RetryTask retries a task that failed due to CF challenge
func (q *DownloadQueue) RetryTask(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, task := range q.tasks {
		if task.ID == id {
			if task.Status == "waiting_cf" || task.Status == "failed" {
				log.Printf("[Queue] Retrying task: %s", task.Manga.Title)
				task.Status = "queued"
				task.StatusMessage = "Retrying..."
				task.Error = nil

				if q.onTaskUpdated != nil {
					q.onTaskUpdated(task)
				}

				// Restart queue processing
				go q.processQueue()
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
	defer q.mu.Unlock()

	for i, task := range q.tasks {
		if task.ID == id {
			if task.Status == "downloading" && task.CancelFunc != nil {
				log.Printf("[Queue] Cancelling active download: %s", task.Manga.Title)
				task.CancelFunc() // Stop the download
				task.Status = "cancelled"
				task.StatusMessage = "Cancelled by user"
			} else if task.Status == "queued" {
				log.Printf("[Queue] Removing queued task: %s", task.Manga.Title)
				// Remove from queue
				q.tasks = append(q.tasks[:i], q.tasks[i+1:]...)

				if q.onTaskRemoved != nil {
					q.onTaskRemoved(id)
				}
				return nil
			} else {
				return fmt.Errorf("task is not active or queued (status: %s)", task.Status)
			}

			if q.onTaskUpdated != nil {
				q.onTaskUpdated(task)
			}
			return nil
		}
	}

	return fmt.Errorf("task not found: %s", id)
}

// CancelAll cancels all tasks
func (q *DownloadQueue) CancelAll() {
	q.mu.Lock()
	defer q.mu.Unlock()

	log.Printf("[Queue] Cancelling all tasks (%d total)", len(q.tasks))

	for _, task := range q.tasks {
		if task.Status == "downloading" && task.CancelFunc != nil {
			task.CancelFunc()
			task.Status = "cancelled"
			task.StatusMessage = "Cancelled by user"
		} else if task.Status == "queued" {
			task.Status = "cancelled"
			task.StatusMessage = "Cancelled by user"
		}

		if q.onTaskUpdated != nil {
			q.onTaskUpdated(task)
		}
	}
}

// RemoveCompletedTasks removes all completed or cancelled tasks
func (q *DownloadQueue) RemoveCompletedTasks() {
	q.mu.Lock()
	defer q.mu.Unlock()

	newTasks := make([]*DownloadTask, 0)
	for _, task := range q.tasks {
		if task.Status == "queued" || task.Status == "downloading" || task.Status == "waiting_cf" {
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

// processQueue processes tasks in FIFO order
func (q *DownloadQueue) processQueue() {
	q.processingMu.Lock()
	if q.processing {
		q.processingMu.Unlock()
		return // Already processing
	}
	q.processing = true
	q.processingMu.Unlock()

	defer func() {
		q.processingMu.Lock()
		q.processing = false
		q.processingMu.Unlock()
	}()

	for {
		task := q.getNextTask()
		if task == nil {
			log.Println("[Queue] No more tasks to process")
			if q.onQueueEmpty != nil {
				q.onQueueEmpty()
			}
			break
		}

		log.Printf("[Queue] Processing task: %s", task.Manga.Title)
		q.executeTask(task)

		// Check if we should continue
		q.mu.RLock()
		hasMore := false
		for _, t := range q.tasks {
			if t.Status == "queued" {
				hasMore = true
				break
			}
		}
		q.mu.RUnlock()

		if !hasMore {
			break
		}
	}
}

// getNextTask gets the next queued task
func (q *DownloadQueue) getNextTask() *DownloadTask {
	q.mu.Lock()
	defer q.mu.Unlock()

	for _, task := range q.tasks {
		if task.Status == "queued" {
			return task
		}
	}
	return nil
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

	// Execute the download using the site-specific download function
	err := ExecuteSiteDownload(ctx, task.Manga, progressCallback)

	q.mu.Lock()
	if err != nil {
		if err == context.Canceled {
			task.Status = "cancelled"
			task.StatusMessage = "Cancelled by user"
		} else {
			// Check if this is a Cloudflare challenge error
			if cfErr, ok := err.(*cf.CfChallengeError); ok {
				task.Status = "waiting_cf"
				task.StatusMessage = fmt.Sprintf("Cloudflare challenge detected - browser opened")
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

	log.Printf("[Queue] Task completed: %s (status: %s)", task.Manga.Title, task.Status)
}
