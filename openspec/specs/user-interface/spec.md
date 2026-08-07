# user-interface Specification

## Purpose
Provide a graphical desktop interface for managing manga bookmarks, initiating downloads, and monitoring progress.

## Requirements

### Requirement: Main Layout
The system SHALL display a two-column layout with a gradient background.

#### Scenario: Main layout structure
- GIVEN the application is running
- WHEN the main window is displayed
- THEN a purple gradient background (45 degrees, RGB 115/103/240 to 136/84/208) SHALL fill the window
- AND a header SHALL appear at the top with the application title
- AND a left column SHALL contain the manga list (top 50%) and edit/add manga form (bottom 50%)
- AND a right column SHALL contain the chapter list (100% height)
- AND a footer SHALL appear at the bottom with attribution text

### Requirement: Application State
The system SHALL use a centralized state object to coordinate UI components.

#### Scenario: Centralized state
- GIVEN the application initializes
- WHEN `NewKanshoAppState` is created
- THEN it SHALL load bookmarks from disk
- AND initialize with no manga selected (SelectedMangaID = -1)
- AND provide observer-style callbacks for manga selection, addition, and deletion

#### Scenario: Select manga triggers callbacks
- GIVEN a user clicks on a manga in the list
- WHEN `SelectManga(id)` is called
- THEN the selected manga ID SHALL be updated
- AND all registered OnMangaSelected callbacks SHALL be invoked

#### Scenario: Delete manga
- GIVEN a manga is selected in the list
- WHEN the user clicks "Delete Manga"
- THEN the manga SHALL be removed from the in-memory bookmarks
- AND the updated bookmarks SHALL be persisted to disk immediately
- AND if the deleted manga was selected, the selection SHALL be cleared
- AND all registered OnMangaDeleted callbacks SHALL be invoked

### Requirement: Manga List View
The system SHALL display all bookmarked manga in a scrollable list.

#### Scenario: Display manga list
- GIVEN the user has bookmarked manga titles
- WHEN the manga list view is rendered
- THEN each manga SHALL display its title and site name
- AND clicking a manga SHALL select it and trigger the chapter list update
- AND "Edit Manga" and "Delete Manga" buttons SHALL be available per entry

### Requirement: Add/Edit Manga Form
The system SHALL provide a form for adding new manga or editing existing ones.

#### Scenario: Dynamic form fields
- GIVEN the user selects a site from the dropdown
- WHEN the site selection changes
- THEN the form SHALL dynamically show or hide fields based on the selected site's RequiredFields config
- AND validation SHALL check that all required fields for the selected site are filled

#### Scenario: Add manga to bookmarks
- GIVEN all required fields are filled for the selected site
- WHEN the user clicks "Add Manga"
- THEN the manga SHALL be added to the bookmarks data
- AND persisted to disk immediately
- AND the manga list view SHALL refresh to show the new entry

### Requirement: Chapter List View
The system SHALL display the chapters of the currently selected manga with per-chapter download state and controls.

#### Scenario: Show local chapters for selected manga
- GIVEN a manga is selected in the manga list
- WHEN the chapter list view receives a selection callback
- THEN it SHALL list all locally downloaded CBZ files for that manga
- AND it SHALL list any remote chapters previously fetched for that manga via the "Refresh" button
- AND it SHALL NOT contact the target site
- AND remote chapters SHALL NOT be loaded automatically

#### Scenario: Remote chapters persist across manga switches
- GIVEN the user has clicked "Refresh" for a manga at least once while the application is open
- WHEN the user selects a different manga and then selects the original manga again
- THEN the remote chapters for the original manga SHALL still be listed
- AND remote chapters for a manga that was never refreshed SHALL NOT be listed

#### Scenario: Manually refresh remote chapters
- GIVEN a manga is selected and its local chapters are listed
- WHEN the user clicks the "Refresh" button at the bottom of the chapter pane
- THEN the system SHALL query the target site for the manga's available chapters
- AND SHALL merge new (not yet downloaded) chapters into the list
- AND SHALL cache the fetched chapters for that manga so they persist for the lifetime of the application
- AND new chapters SHALL NOT be downloaded automatically
- AND the "Refresh" button SHALL be disabled while the fetch is in progress

#### Scenario: Chapter list layout
- GIVEN the chapter list is rendered
- WHEN the user views a chapter row
- THEN each row SHALL be split into three panes:
  - left: chapter name
  - middle: a per-chapter progress bar
  - right: a green tick, a download arrow button, and a red circle-with-slash button, right-aligned
- AND a green tick SHALL be shown for chapters already downloaded on disk
- AND the green tick SHALL have no button chrome and no hover highlight
- AND a download arrow button SHALL be shown for chapters that are not downloaded and idle
- AND a "Download All Missing" button and a "Refresh" button SHALL appear at the bottom-right of the chapter pane for the currently selected manga only

#### Scenario: Load feedback
- GIVEN a manga is selected or the user clicks "Refresh"
- WHEN the chapter list is loading chapters from disk or the site
- THEN a custom AJAX-style loading spinner SHALL be displayed at the bottom-left of the chapter pane
- AND the spinner SHALL be a rotating ring of radial ticks with a bright arc sweeping around it
- AND the spinner SHALL be removed once loading completes

#### Scenario: Start a single chapter download
- GIVEN a chapter is not downloaded, idle, and has a download URL
- WHEN the user clicks the download arrow button on its row
- THEN the chapter SHALL be added to the download queue as a single-chapter task
- AND its row SHALL switch to a queued/downloading state with live progress in the middle pane
- AND NO "added to download queue" dialog SHALL be shown

#### Scenario: Cancel a chapter download
- GIVEN a chapter is queued or currently downloading
- WHEN the user clicks the red circle-with-slash button on its row
- THEN the corresponding queue task SHALL be cancelled
- AND the row SHALL return to its previous (not downloaded) state

#### Scenario: Delete a downloaded chapter
- GIVEN a chapter is downloaded on disk
- WHEN the user clicks the red circle-with-slash button on its row
- THEN an OK/Cancel confirmation dialog SHALL be shown
- AND the local CBZ file SHALL be deleted only if the user confirms
- AND the chapter list SHALL reload from disk after deletion

#### Scenario: Download all missing chapters
- GIVEN a manga is selected
- WHEN the user clicks "Download All Missing"
- THEN a single-chapter queue task SHALL be created for every chapter that is not downloaded and not already queued
- AND NO "added to download queue" dialog SHALL be shown

#### Scenario: Cloudflare challenge in chapter list
- GIVEN the remote chapter list refresh encounters a CF challenge
- WHEN the challenge is detected
- THEN the refresh SHALL stop
- AND a CF dialog SHALL be shown so the user can solve the challenge and import cf data
- AND on success the chapter list SHALL be reloaded

### Requirement: Download Queue Button
The system SHALL display an overall download progress summary instead of a dedicated queue screen.

#### Scenario: Download queue summary button
- GIVEN there are active or queued chapter downloads
- WHEN the header is rendered
- THEN a "Download Queue" button SHALL be shown in the header
- AND its text SHALL summarize overall progress as: number of manga titles, completed/total chapters, and overall percentage
- AND when there are no tasks it SHALL read "Download Queue"

#### Scenario: Download queue summary pop-up
- GIVEN the user clicks the download queue button
- WHEN the queue is not empty
- THEN a modal pop-up window (not a dialog) SHALL open
- AND it SHALL fill the size of the main Kansho window
- AND it SHALL block interaction with the application windows beneath it, so it must be closed with the "Close" button to return to them
- AND it SHALL group the tasks by manga title, each group showing a per-manga "Cancel All" button
- AND it SHALL show the overall manga and chapter counts remaining in the queue
- AND it SHALL list each manga title, the chapter currently being processed for it, and its status
- AND each task row SHALL show its chapter name on a single line, truncating with an ellipsis if it does not fit
- AND when a chapter is actively downloading, a prominent "Currently Downloading" section SHALL be shown at the top of the pop-up
- AND that section SHALL be split in two halves: the left half SHALL show the chapter currently being downloaded, and the right half SHALL show a live progress bar for that chapter next to a "Stop" button
- AND clicking "Stop" SHALL cancel the active download while leaving the task in the queue so it can be started again
- AND the remaining queue tasks SHALL be listed below the "Currently Downloading" section
- AND the list SHALL be constrained to the pop-up size and SHALL show a scrollbar when the queue is too long to fit
- AND a global "Cancel All" button SHALL be shown in the pop-up header that cancels every queued/downloading task
- AND a "Clear All" button SHALL be shown in the pop-up header that removes every task from the queue entirely (cancelling any active downloads), leaving nothing to retry
- AND clicking "Clear All" SHALL prompt the user with an OK/Cancel confirmation dialog before any tasks are cleared
- AND the pop-up SHALL stay live, updating as the queue changes
- AND it SHALL close automatically when the queue becomes empty

#### Scenario: Remove finished chapters from the queue
- GIVEN a chapter task finishes downloading successfully
- WHEN the download completes
- THEN the task SHALL be removed from the queue immediately
- AND the manga title SHALL be removed from the queue once all of its queued chapters have finished successfully

#### Scenario: Start or retry unfinished chapters
- GIVEN a chapter task did not finish downloading (failed, cancelled, or waiting on a CF challenge)
- WHEN the task remains in the queue
- THEN the task SHALL stay in the queue
- AND a "Start" button SHALL be shown next to a chapter the user stopped or cancelled
- AND a "Retry" button SHALL be shown next to a chapter that failed or is waiting on a CF challenge
- AND clicking "Start" or "Retry" SHALL re-queue the chapter for download

#### Scenario: Cancel all downloads for a manga
- GIVEN the download queue pop-up is open
- WHEN the user clicks a manga group's "Cancel All" button
- THEN every queued or downloading task for that manga title SHALL be cancelled
- AND tasks for other manga SHALL be left untouched
- AND the cancelled tasks SHALL remain in the queue with a "Start" button

#### Scenario: Empty download queue
- GIVEN the queue is empty
- WHEN the user clicks the download queue button
- THEN an informational dialog SHALL be shown stating that there are no downloads in queue

### Requirement: Keyboard Shortcuts and Menus
The system SHALL provide menus and keyboard shortcuts for common operations.

#### Scenario: Menu items
- GIVEN the application menu is visible
- WHEN the user opens the File menu
- THEN "Logs" SHALL open the log display window
- WHEN the user opens the Bookmarks menu
- THEN "Bookmarks" SHALL open the bookmarks window
- AND "Export Bookmarks" SHALL open a save dialog
- AND "Import Bookmarks" SHALL open a file picker
- WHEN the user opens the Help menu
- THEN "About" SHALL show an about dialog with version information

#### Scenario: Config window
- GIVEN the user presses Ctrl+Shift+C
- WHEN the config window opens
- THEN it SHALL display application configuration including version, git commit, build time, and rlv (rlv companion tool) version
