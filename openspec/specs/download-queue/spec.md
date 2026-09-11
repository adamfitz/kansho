# download-queue Specification

## Purpose
Provide a concurrent download queue that manages single-chapter download tasks with lifecycle tracking, cancellation, and retry support. Each task represents one chapter of a manga (since the UI refactor, tasks are no longer whole-manga downloads). The queue runs up to three downloads concurrently as a worker pool, with at most one download per site at a time (so up to three different sites download at once). When a task hits a Cloudflare challenge it gates other CF-protected downloads so only one browser window opens, waits for the user to import bypass data (up to a timeout), skips CF-protected tasks on timeout so non-CF downloads proceed, and resumes CF downloads automatically once the data is imported.

## Requirements

### Requirement: Queue as Singleton
The system SHALL provide a single, globally accessible download queue instance.

#### Scenario: Get queue instance
- GIVEN the application is running
- WHEN `GetDownloadQueue()` is called
- THEN the same singleton instance SHALL always be returned

### Requirement: Task Lifecycle
The queue SHALL manage download tasks through defined states.

#### Scenario: Task states
- GIVEN a download task is created
- WHEN it is added to the queue
- THEN its status SHALL be "queued"
- WHEN processing begins
- THEN its status SHALL be "downloading"
- WHEN the download completes successfully
- THEN its status SHALL be "completed"
- WHEN the user cancels
- THEN its status SHALL be "cancelled"
- AND its StatusMessage SHALL provide a human-readable reason (e.g., "Cancelling..." while the cancel unwinds)
- WHEN the download encounters an error
- THEN its status SHALL be "failed"
- WHEN a CF challenge is detected
- THEN its status SHALL be "waiting_cf"
- WHEN a queued task requires Cloudflare bypass data but none was provided within the wait timeout
- THEN its status SHALL be "skipped_cf" and it SHALL stay in the queue for a later retry

#### Scenario: Add chapter task to queue
- GIVEN the queue is empty
- WHEN `AddChapterTask(manga, chapter, chapterURL)` is called
- THEN a task with a unique ID SHALL be created using `fmt.Sprintf("%s-%s-%d", manga.Shortname, chapter, len(q.tasks))`
- AND the task SHALL store the chapter CBZ filename and its download URL
- AND a value copy of the manga data SHALL be stored (not a pointer) to prevent external mutation
- AND a callback SHALL notify UI of the new task
- AND queue processing SHALL start automatically in a goroutine

#### Scenario: Duplicate chapter rejected
- GIVEN a chapter task for a manga is already in the queue
- WHEN `AddChapterTask` is called for the same manga title and chapter filename
- THEN the operation SHALL return an error indicating the chapter is already queued

#### Scenario: Look up task for a chapter
- GIVEN tasks exist in the queue
- WHEN `GetTaskForChapter(mangaTitle, chapter)` is called
- THEN it SHALL return the matching task, or nil if none exists
- AND `ChapterQueued(mangaTitle, chapter)` SHALL return true if such a task exists

### Requirement: Concurrent Worker Pool
The queue SHALL process chapter tasks as a worker pool: up to `maxDownloadWorkers` (3) tasks run concurrently, FIFO across the queue, with at most one download per site at a time.

#### Scenario: Process queued chapter tasks concurrently
- GIVEN multiple chapter tasks for different sites are in the queue
- WHEN processing starts
- THEN up to three tasks SHALL be executed at the same time
- AND each executing task SHALL belong to a different site (a site runs at most one download at a time)
- AND tasks waiting for earlier chapters of the same site SHALL wait until that site's download completes
- AND processing SHALL continue until all queued tasks are complete
- AND queued tasks that share a site SHALL run in the order they were added

#### Scenario: Concurrent dispatch respects worker limit
- GIVEN the four queued chapters span four different sites
- WHEN the first three have started downloading
- THEN the fourth SHALL remain "queued"
- AND SHALL start as soon as any of the first three completes

### Requirement: Task Cancellation
The queue SHALL support cancelling individual tasks, all tasks, or a single manga's queued chapters, each with immediate status feedback.

#### Scenario: Cancel queued task
- GIVEN a task is in "queued" or "skipped_cf" status (no download is running)
- WHEN `CancelTask` is called with the task ID
- THEN the task SHALL be removed from the queue entirely
- AND a removal callback SHALL be triggered

#### Scenario: Cancel active download
- GIVEN a task is in "downloading" status
- WHEN `CancelTask` is called with the task ID
- THEN the task's status SHALL be set to "cancelled" immediately
- AND the StatusMessage SHALL be set to "Cancelling..."
- AND the UI callback SHALL be notified BEFORE the cancel function unwinds (so the user sees feedback right away)
- THEN the task's cancel function SHALL be called to abort the download

#### Scenario: Cancel all tasks
- GIVEN multiple tasks exist in the queue
- WHEN `CancelAll` is called
- THEN all downloading and waiting_cf tasks SHALL have their status set to "cancelled" and StatusMessage to "Cancelling..." immediately
- AND all queued and skipped_cf tasks SHALL be marked as "cancelled" with StatusMessage "Cancelled by user"
- AND the UI callback SHALL be notified for all tasks BEFORE any cancel functions are invoked
- THEN all cancel functions SHALL be called (after releasing the queue lock to prevent UI freezing)

#### Scenario: Cancel queued chapters only for one manga
- GIVEN a manga title has chapters both in the queue and currently downloading
- WHEN `CancelMangaQueue` is called with the manga title
- THEN every "queued" and "skipped_cf" chapter of that manga SHALL be set to "cancelled" with StatusMessage "Cancelled by user"
- AND the chapter currently "downloading" for that manga SHALL be left untouched (its download SHALL continue)
- AND a chapter of that manga waiting on a Cloudflare challenge (status "waiting_cf") SHALL be left untouched
- AND chapters for other manga titles SHALL be left untouched
- AND completed, failed and already-cancelled tasks SHALL be left untouched
- AND the cancelled chapters SHALL stay in the queue so they can be started again

### Requirement: CF Challenge Handling
The queue SHALL gate CF-protected downloads on a Cloudflare challenge so only a single browser window opens, wait for the user to provide bypass data (up to `cfWaitTimeout`, default 5 minutes), skip CF-protected queued tasks on timeout so downloads that do not need Cloudflare proceed, and resume CF downloads automatically once the data is imported.

#### Scenario: CF challenge gates CF-protected downloads
- GIVEN a task encounters a CF challenge during download
- WHEN the `cf.CfChallengeError` is returned
- THEN the task status SHALL be set to "waiting_cf"
- AND the browser SHALL be opened exactly once for manual challenge solving
- AND while the challenge is unresolved, no other queued task that would require Cloudflare bypass data SHALL start (preventing a new browser window per queued chapter)
- AND queued tasks that do not require Cloudflare SHALL be dispatched normally on the remaining worker slots
- AND the task SHALL remain in the queue for later retry

#### Scenario: CF data received during wait
- GIVEN the queue is gated on a "waiting_cf" task for a domain
- WHEN Cloudflare bypass data for that domain becomes available (imported via the CF dialog)
- THEN the blocked task SHALL be reset to "queued" with its error cleared
- AND queue processing SHALL resume
- AND the download SHALL proceed using the freshly imported bypass data

#### Scenario: CF wait timeout skips protected tasks
- GIVEN the queue is gated on a "waiting_cf" task and no bypass data is provided within `cfWaitTimeout`
- WHEN the timeout elapses
- THEN the task that hit the challenge SHALL remain "waiting_cf"
- AND every other queued task whose site requires Cloudflare bypass data (with none stored for its domain) SHALL be marked "skipped_cf"
- AND queued tasks that do not require Cloudflare SHALL be processed normally
- AND the skipped tasks SHALL stay in the queue so the user can retry them later

#### Scenario: Resume CF tasks after data import
- GIVEN the queue contains "waiting_cf" or "skipped_cf" tasks and the user imports Cloudflare bypass data
- WHEN `ResumeCfTasks` is called
- THEN every blocked task whose domain now has stored bypass data SHALL be reset to "queued" with its error cleared
- AND queue processing SHALL restart automatically
- AND tasks whose domain still has no bypass data SHALL remain blocked

#### Scenario: Retry CF task
- GIVEN a task is in "waiting_cf", "skipped_cf", or "failed" status
- WHEN `RetryTask` is called
- THEN the task status SHALL be reset to "queued"
- AND queue processing SHALL restart

### Requirement: Retry Unfinished Tasks
The queue SHALL support retrying tasks that did not complete, including failed, cancelled, CF-blocked, and CF-skipped tasks.

#### Scenario: Retry failed task
- GIVEN a task is in "failed" status
- WHEN `RetryTask` is called with the task ID
- THEN the task status SHALL be reset to "queued"
- AND the StatusMessage SHALL be set to "Retrying..."
- AND the error SHALL be cleared
- AND queue processing SHALL restart in a goroutine

#### Scenario: Retry cancelled task
- GIVEN a task is in "cancelled" status (e.g., stopped by the user)
- WHEN `RetryTask` is called with the task ID
- THEN the task status SHALL be reset to "queued"
- AND the StatusMessage SHALL be set to "Retrying..."
- AND the error SHALL be cleared
- AND queue processing SHALL restart in a goroutine

#### Scenario: Cannot retry active task
- GIVEN a task is in "downloading" status
- WHEN `RetryTask` is called
- THEN an error SHALL be returned indicating the task cannot be retried in its current state

### Requirement: Clean Up Completed Tasks
The queue SHALL support removing all completed and cancelled tasks.

#### Scenario: Remove non-active tasks
- GIVEN the queue has completed, cancelled, queued, downloading, waiting_cf, and skipped_cf tasks
- WHEN `RemoveCompletedTasks` is called
- THEN all tasks with status "completed" or "cancelled" or "failed" SHALL be removed
- AND tasks with status "queued", "downloading", "waiting_cf", or "skipped_cf" SHALL be kept
- AND removal callbacks SHALL be triggered for each removed task

### Requirement: UI Callbacks
The queue SHALL notify the UI of state changes through registered callbacks.

#### Scenario: Register callbacks
- GIVEN the queue is created
- WHEN `SetCallbacks` is called
- THEN callbacks for onTaskAdded, onTaskUpdated, onTaskRemoved, and onQueueEmpty SHALL be registered
- AND these callbacks SHALL be invoked on corresponding state changes

#### Scenario: Clean up completed tasks
- GIVEN there are completed or cancelled tasks in the queue
- WHEN `RemoveCompletedTasks` is called
- THEN all non-active tasks (not queued, downloading, waiting_cf, or skipped_cf) SHALL be removed
- AND removal callbacks SHALL be triggered for each removed task

### Requirement: Context-Bound Task Execution
Each downloading task SHALL use a cancellable context for aborting in-flight operations.

#### Scenario: Task context creation
- GIVEN a queued chapter task begins processing
- WHEN the executor goroutine creates a cancellable context
- THEN the context SHALL be stored in `task.CancelFunc` for external cancellation
- AND for chapter tasks the context SHALL propagate through `ExecuteChapterDownload(ctx, manga, chapterURL, cbzName, progressCallback)` to all sub-operations (chapter fetching, image downloads, retry sleeps, rate limit waits)
- AND for legacy manga-level tasks the context SHALL propagate through `Manager.Download(ctx)` to all sub-operations
