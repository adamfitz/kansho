# metadata-queue Specification

## Purpose
Serialize CBZ metadata operations so only one chapter archive is opened, checked or rewritten at a time, and expose the live queue through the main status bar.

## Requirements

### Requirement: Single Metadata Worker
All metadata Check and Repair work SHALL run on one process-wide worker queue. A submission SHALL be accepted only while the queue is open, and accepted tasks SHALL execute in FIFO order.

#### Scenario: Serialized metadata access
- GIVEN Check or Repair work is queued for one or more chapters
- WHEN the worker processes the queue
- THEN exactly one task SHALL run at a time
- AND the next task SHALL start only after the current task returns or panics
- AND the queue SHALL recover from a task panic and continue with the next task

#### Scenario: Duplicate submissions rejected
- GIVEN a task with a dedupe key is pending or running
- WHEN another task with the same dedupe key is submitted
- THEN the new submission SHALL be rejected without running
- AND the dedupe key SHALL be released when the original task finishes

#### Scenario: Shutdown
- GIVEN the queue is closed
- WHEN tasks are running or pending
- THEN the running task context SHALL be cancelled
- AND pending tasks SHALL receive a closed-queue error through their error callback
- AND new submissions SHALL be rejected

### Requirement: Status Reporting
The queue SHALL report running count, queued count and an ordered job list (running job first) to its listener whenever the status changes and once immediately when a listener is registered.

#### Scenario: Status bar shows metadata queue state
- GIVEN the listener is wired to the main window status bar
- WHEN metadata tasks are queued, start running, or finish
- THEN the status bar SHALL show `⟳ Metadata: idle` when nothing is pending
- AND `Metadata: N running · M queued` next to an animated bash-style spinner while work is in flight
- AND the readout SHALL sit next to the chapter refresh readout

#### Scenario: Listener thread safety
- GIVEN queue callbacks fire on queue or submitting goroutines
- WHEN the UI receives a status update
- THEN it SHALL marshal the widget update onto the UI thread before applying it

### Requirement: Metadata Queue Dialog
While metadata work is in flight, the status bar's metadata readout SHALL be a click target that opens a dialog listing the running and queued jobs. When idle the readout SHALL be inert and any open dialog SHALL close.

#### Scenario: Job rows
- GIVEN the metadata dialog is open
- WHEN jobs are running or queued
- THEN each row SHALL show the action, manga title and either a chapter, a volume, or a file label
- AND the running job SHALL be listed first
- AND each row SHALL show an animated dot indicator cycling `.`, `..`, `...`

#### Scenario: Live drain
- GIVEN the metadata dialog is open
- WHEN further statuses arrive or the queue becomes idle
- THEN the job list SHALL track jobs starting and finishing
- AND the dialog SHALL close automatically and stop its animation once the queue is idle

#### Scenario: No stacking
- GIVEN the metadata dialog is already open
- WHEN the user clicks the metadata readout again
- THEN no second dialog SHALL be opened

### Requirement: Logging
The queue SHALL log queueing, running, success, failure and closed-queue rejection events with a `[MetadataQueue]` prefix.
