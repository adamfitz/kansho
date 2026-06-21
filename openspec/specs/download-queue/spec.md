# download-queue Specification

## Purpose
Provide a FIFO download queue that manages multiple manga download tasks with lifecycle tracking, cancellation, and retry support.

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

#### Scenario: Add task to queue
- GIVEN the queue is empty
- WHEN a manga bookmark is added as a task
- THEN a task with a unique ID SHALL be created using `fmt.Sprintf("%s-%d", manga.Shortname, len(q.tasks))`
- AND a value copy of the manga data SHALL be stored (not a pointer) to prevent external mutation
- AND a callback SHALL notify UI of the new task
- AND queue processing SHALL start automatically in a goroutine

#### Scenario: Duplicate manga rejected
- GIVEN a manga is already in the queue
- WHEN the same manga title is added again
- THEN the operation SHALL return an error indicating the manga is already queued

### Requirement: FIFO Processing
The queue SHALL process tasks in first-in-first-out order.

#### Scenario: Process queued tasks sequentially
- GIVEN multiple tasks are in the queue
- WHEN processing starts
- THEN tasks SHALL be executed in the order they were added
- AND only one task SHALL be processed at a time
- AND processing SHALL continue until all queued tasks are complete

### Requirement: Task Cancellation
The queue SHALL support cancelling individual tasks or all tasks with immediate status feedback.

#### Scenario: Cancel queued task
- GIVEN a task is in "queued" status
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
- THEN all downloading tasks SHALL have their status set to "cancelled" and StatusMessage to "Cancelling..." immediately
- AND all queued tasks SHALL be marked as "cancelled" with StatusMessage "Cancelled by user"
- AND the UI callback SHALL be notified for all tasks BEFORE any cancel functions are invoked
- THEN all cancel functions SHALL be called (after releasing the queue lock to prevent UI freezing)

### Requirement: CF Challenge Handling
The queue SHALL detect CF challenges and pause affected tasks for manual resolution.

#### Scenario: CF challenge detected
- GIVEN a task encounters a CF challenge during download
- WHEN the `cf.CfChallengeError` is returned
- THEN the task status SHALL be set to "waiting_cf"
- AND the browser SHALL be opened for manual challenge solving
- AND the task SHALL remain in the queue for later retry

#### Scenario: Retry CF task
- GIVEN a task is in "waiting_cf" or "failed" status
- WHEN `RetryTask` is called
- THEN the task status SHALL be reset to "queued"
- AND queue processing SHALL restart

### Requirement: Retry Failed Tasks
The queue SHALL support retrying failed tasks.

#### Scenario: Retry failed task
- GIVEN a task is in "failed" status
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
- GIVEN the queue has completed, cancelled, queued, downloading, and waiting_cf tasks
- WHEN `RemoveCompletedTasks` is called
- THEN all tasks with status "completed" or "cancelled" or "failed" SHALL be removed
- AND tasks with status "queued", "downloading", or "waiting_cf" SHALL be kept
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
- THEN all non-active tasks (not queued, downloading, or waiting_cf) SHALL be removed
- AND removal callbacks SHALL be triggered for each removed task

### Requirement: Context-Bound Task Execution
Each downloading task SHALL use a cancellable context for aborting in-flight operations.

#### Scenario: Task context creation
- GIVEN a queued task begins processing
- WHEN the executor goroutine creates a cancellable context
- THEN the context SHALL be stored in `task.CancelFunc` for external cancellation
- AND the context SHALL propagate through `Manager.Download(ctx)` to all sub-operations (chapter fetching, image downloads, retry sleeps, rate limit waits)
