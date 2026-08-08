package ui

import (
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"kansho/config"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// DownloadQueueButton is a compact button that summarises the overall download
// progress for ALL downloads in the queue. It does not show per-task progress
// bars. Clicking it opens a modal pop-up window that fills the main Kansho
// window and lists the queued chapters grouped by manga. The list is
// constrained to the pop-up and scrolls when it is too long. The pop-up stays
// live: its contents update as the queue changes. Because it is modal, it must
// be closed with the "Close" button to return to the normal application
// windows.
//
// Chapters that finish downloading are removed from the queue as soon as they
// complete, so a manga title disappears from the queue once all of its queued
// chapters have finished. Chapters that did not finish (failed, cancelled, or
// waiting on a CF challenge) stay in the queue and get a "Retry" button.
//
// The pop-up also offers two cancellation controls:
//   - "Cancel All" (global, in the header) cancels every queued/downloading task
//   - a per-manga "Cancel All" button next to each manga group cancels all
//     active tasks for that manga only
type DownloadQueueButton struct {
	Card   fyne.CanvasObject
	button *widget.Button
	state  *KanshoAppState

	popup        *widget.PopUp
	listBox      *fyne.Container
	overallLabel *widget.Label

	// Footer status bar: a compact live readout of what the current download is
	// doing (downloading, stalled, waiting to retry, packaging the CBZ, ...).
	statusBadge   *widget.Label
	statusMessage *widget.Label

	// Incremental refresh state. A progress event that only touches the currently
	// downloading task updates these widgets in place instead of rebuilding the
	// whole list, which keeps the main thread responsive during downloads.
	activeRowTask     *config.DownloadTask
	activeRowManga    *widget.Label
	activeRowChapter  *widget.Label
	activeRowProgress *widget.ProgressBar

	// Throttle state for full list rebuilds. Structural changes (a task added,
	// removed, or changed status) rebuild the list at most every
	// progressRefreshInterval, coalescing bursts of events into one rebuild.
	lastFullRebuild  time.Time
	pendingRebuild   *time.Timer
	lastStructureKey string
}

// progressRefreshInterval caps how often the pop-up list is rebuilt from
// scratch during downloads. Progress-bar updates at this rate are visually
// smooth while leaving the main thread free for input.
const progressRefreshInterval = 100 * time.Millisecond

// mangaTaskGroup is a group of queue tasks that belong to one manga title.
type mangaTaskGroup struct {
	title string
	tasks []*config.DownloadTask
}

// NewDownloadQueueButton creates the download queue summary button.
func NewDownloadQueueButton(state *KanshoAppState) *DownloadQueueButton {
	b := &DownloadQueueButton{state: state}
	b.button = widget.NewButton("Download Queue", b.showSummary)
	b.Card = b.button
	b.Refresh()
	return b
}

// Refresh updates the button text to reflect the current contents of the queue
// (completed chapters are removed as soon as they finish, so the counts are of
// the remaining work). If the queue pop-up is open it is kept in sync.
func (b *DownloadQueueButton) Refresh() {
	tasks := config.GetDownloadQueue().GetTasks()

	if len(tasks) == 0 {
		b.button.SetText("Download Queue")
	} else {
		b.button.SetText(fmt.Sprintf("Download Queue: %d manga · %d chapters", len(taskTitles(tasks)), len(tasks)))
	}

	if b.popup != nil && b.popup.Visible() {
		if len(tasks) == 0 {
			b.popup.Hide()
			dialog.ShowInformation("Download Queue", "No downloads in queue.", b.state.Window)
			return
		}
		b.refreshPopup(tasks)
	}
}

// taskTitles returns the set of manga titles that have tasks in the queue.
func taskTitles(tasks []*config.DownloadTask) map[string]bool {
	titles := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		titles[task.Manga.Title] = true
	}
	return titles
}

// showSummary opens a modal download queue pop-up that fills the main Kansho
// window, creating it on first use. The user must close it to return to the
// normal application windows.
func (b *DownloadQueueButton) showSummary() {
	if len(config.GetDownloadQueue().GetTasks()) == 0 {
		dialog.ShowInformation("Download Queue", "No downloads in queue.", b.state.Window)
		return
	}

	if b.popup == nil {
		b.popup = widget.NewModalPopUp(b.buildPopup(), b.state.Window.Canvas())
	}
	// Fit the pop-up to the whole window so the queue list has room to work.
	b.popup.Resize(b.state.Window.Canvas().Size())
	b.refreshPopup(config.GetDownloadQueue().GetTasks())
	b.popup.Show()
}

// buildPopup constructs the stable pop-up structure: a fixed header (title,
// overall progress + global "Cancel All", and a Close button) above a
// scrollable list of the queue contents, with a live status bar pinned to the
// bottom.
func (b *DownloadQueueButton) buildPopup() fyne.CanvasObject {
	titleLabel := NewBoldLabel("Download Queue")

	closeButton := widget.NewButtonWithIcon("Close", theme.CancelIcon(), func() {
		b.popup.Hide()
	})
	closeButton.Importance = widget.LowImportance
	titleRow := container.NewBorder(nil, nil, nil, closeButton, titleLabel)

	b.overallLabel = widget.NewLabel("")

	// Clear All empties the queue entirely (removing even cancelled/failed
	// entries); Cancel All only marks the active tasks as cancelled. It asks
	// for confirmation first since it discards every entry in the queue.
	clearAllButton := widget.NewButtonWithIcon("Clear All", theme.DeleteIcon(), func() {
		dialog.ShowConfirm(
			"Clear All Downloads",
			"Cancel and clear all downloads from the queue?\nThis cannot be undone.",
			func(confirmed bool) {
				if !confirmed {
					return
				}
				config.GetDownloadQueue().ClearAll()
			},
			b.state.Window,
		)
	})
	clearAllButton.Importance = widget.HighImportance
	cancelAllButton := widget.NewButtonWithIcon("Cancel All", theme.CancelIcon(), func() {
		config.GetDownloadQueue().CancelAll()
	})
	cancelAllButton.Importance = widget.HighImportance

	controls := container.NewHBox(clearAllButton, cancelAllButton)
	overallRow := container.NewBorder(nil, nil, nil, controls, b.overallLabel)

	header := container.NewVBox(titleRow, overallRow, widget.NewSeparator())

	// The scroll container constrains the list to the pop-up's size and shows a
	// scrollbar when the queue is too long to fit.
	b.listBox = container.NewVBox()
	scroll := container.NewVScroll(b.listBox)

	// Footer status bar: a short bold state badge on the left and the current
	// download's status message on the right (truncating so it never wraps).
	b.statusBadge = widget.NewLabel("")
	b.statusBadge.TextStyle = fyne.TextStyle{Bold: true}
	b.statusMessage = widget.NewLabel("")
	b.statusMessage.Truncation = fyne.TextTruncateEllipsis
	footer := container.NewVBox(
		widget.NewSeparator(),
		container.NewBorder(nil, nil, b.statusBadge, nil, b.statusMessage),
	)

	return container.NewBorder(header, footer, nil, nil, scroll)
}

// refreshPopup reconciles the pop-up with the current queue state.
//
// The status bar updates on every call (it is just two labels, so it stays
// crisp). Full list rebuilds only happen on structural changes — a task added,
// removed, or changed status — and are throttled to progressRefreshInterval so
// a burst of progress events is coalesced into a single rebuild. Pure progress
// ticks on the task that is already on screen are applied to the active row in
// place, so the main thread is never spent rebuilding the whole list per image.
func (b *DownloadQueueButton) refreshPopup(tasks []*config.DownloadTask) {
	b.updateStatusBar(tasks)

	active := activeDownloadTask(tasks)
	key := structureKey(tasks)

	// Nothing structural changed: if the same task is still downloading, nudge
	// its progress bar in place. (The queue stores stable task pointers, so
	// pointer identity reliably means "same task".)
	if key == b.lastStructureKey {
		if active != nil && active == b.activeRowTask {
			b.updateActiveRowInPlace(active)
		}
		return
	}

	// A rebuild is already scheduled; let it fire with fresh state.
	if b.pendingRebuild != nil {
		return
	}

	// Structural change arrived inside the throttle window: coalesce into one
	// scheduled rebuild instead of rebuilding immediately.
	if elapsed := time.Since(b.lastFullRebuild); elapsed < progressRefreshInterval {
		b.pendingRebuild = time.AfterFunc(progressRefreshInterval-elapsed, func() {
			fyne.Do(func() {
				b.pendingRebuild = nil
				if b.popup == nil || !b.popup.Visible() {
					return
				}
				b.refreshPopup(config.GetDownloadQueue().GetTasks())
			})
		})
		return
	}

	b.rebuildPopup(tasks, active)
}

// structureKey returns a stable snapshot of the queue's structural state: the
// ID and status of every task. Progress ticks (Progress / StatusMessage) do not
// change the key, so they are handled incrementally.
func structureKey(tasks []*config.DownloadTask) string {
	var sb strings.Builder
	for _, task := range tasks {
		sb.WriteString(task.ID)
		sb.WriteByte(':')
		sb.WriteString(task.Status)
		sb.WriteByte('|')
	}
	return sb.String()
}

// rebuildPopup rebuilds the pop-up's list from the current queue state,
// grouping tasks by manga with a per-manga "Cancel All" button and a "Start"
// / "Retry" button on any unfinished task. A prominent "Currently Downloading"
// section sits at the top of the list whenever a download is active, showing
// the in-progress chapter next to a live progress bar and a Stop button.
func (b *DownloadQueueButton) rebuildPopup(tasks []*config.DownloadTask, active *config.DownloadTask) {
	b.overallLabel.SetText(fmt.Sprintf("Overall: %d manga · %d chapters in queue", len(taskTitles(tasks)), len(tasks)))

	// Track which task owns the active row, so progress ticks can update it in
	// place. Stale widgets from a previous build are dropped.
	b.activeRowTask = active
	if active == nil {
		b.activeRowManga, b.activeRowChapter, b.activeRowProgress = nil, nil, nil
	}

	objects := make([]fyne.CanvasObject, 0, len(tasks)+len(tasks)/2+4)

	// Prominent "Currently Downloading" section: the left half shows the chapter
	// being downloaded, the right half a live progress bar with a Stop button.
	if active != nil {
		objects = append(objects, NewBoldLabel("Currently Downloading"))
		objects = append(objects, b.activeTaskRow(active))
		objects = append(objects, widget.NewSeparator())
	}

	for _, group := range groupTasksByManga(tasks) {
		mangaTitle := group.title
		groupCancel := widget.NewButtonWithIcon("Cancel All", theme.CancelIcon(), func() {
			config.GetDownloadQueue().CancelMangaTasks(mangaTitle)
		})
		groupCancel.Importance = widget.HighImportance

		objects = append(objects, container.NewBorder(nil, nil, nil, groupCancel, NewBoldLabel(mangaTitle)))
		for _, task := range group.tasks {
			objects = append(objects, b.taskSummaryRow(task))
		}
		objects = append(objects, widget.NewSeparator())
	}

	b.listBox.Objects = objects
	b.listBox.Refresh()

	b.lastStructureKey = structureKey(tasks)
	b.lastFullRebuild = time.Now()
}

// activeDownloadTask returns the single task that is currently downloading, or
// nil if the queue is idle. The queue processes tasks one at a time, so there
// is never more than one actively downloading task.
func activeDownloadTask(tasks []*config.DownloadTask) *config.DownloadTask {
	for _, task := range tasks {
		if task.Status == "downloading" {
			return task
		}
	}
	return nil
}

// mangaTitleMaxRunes is the fixed length the manga title is truncated to in the
// status bar, so long titles stay readable at a glance.
const mangaTitleMaxRunes = 30

// updateStatusBar refreshes the footer status bar with what the current download
// is doing right now. The badge shows the site being downloaded from plus a
// small state glyph; the message shows the (truncated) manga title followed by
// the latest status message. This is driven by the progress callback, which
// emits an update for every phase of an image download, so the bar never sits
// stale during a stalled download.
func (b *DownloadQueueButton) updateStatusBar(tasks []*config.DownloadTask) {
	active := activeDownloadTask(tasks)
	if active == nil {
		b.statusBadge.SetText("⏸ Idle")
		if len(tasks) == 0 {
			b.statusMessage.SetText("No downloads in queue")
		} else {
			b.statusMessage.SetText(fmt.Sprintf("%d chapters queued, waiting for a free slot", len(tasks)))
		}
		return
	}

	if site := siteNameForTask(active); site != "" {
		b.statusBadge.SetText(fmt.Sprintf("%s %s", site, statusEmojiForMessage(active.StatusMessage)))
	} else {
		b.statusBadge.SetText(statusEmojiForMessage(active.StatusMessage))
	}

	if title := truncateMangaTitle(active.Manga.Title, mangaTitleMaxRunes); title != "" {
		b.statusMessage.SetText(fmt.Sprintf("%s — %s", title, active.StatusMessage))
	} else {
		b.statusMessage.SetText(active.StatusMessage)
	}
}

// siteNameForTask returns the human-readable site name for a download task,
// falling back to the URL hostname when the bookmark has no site set.
func siteNameForTask(task *config.DownloadTask) string {
	if task == nil {
		return ""
	}
	if task.Manga.Site != "" {
		return task.Manga.Site
	}
	if task.Manga.Url != "" {
		if host, err := url.Parse(task.Manga.Url); err == nil && host.Hostname() != "" {
			return strings.TrimPrefix(host.Hostname(), "www.")
		}
	}
	return ""
}

// statusEmojiForMessage returns a small state glyph derived from the current
// download's status message.
func statusEmojiForMessage(msg string) string {
	switch {
	case strings.Contains(msg, "STALLED"):
		return "⚠"
	case strings.Contains(msg, "retry"):
		return "⏳"
	case strings.Contains(msg, "Creating CBZ"):
		return "📦"
	case strings.Contains(msg, "Starting download"), strings.Contains(msg, "Found "):
		return "▶"
	default:
		return "⬇"
	}
}

// truncateMangaTitle truncates a (possibly very long) manga title to at most
// maxRunes runes, appending an ellipsis.
func truncateMangaTitle(title string, maxRunes int) string {
	runes := []rune(title)
	if len(runes) <= maxRunes {
		return title
	}
	if maxRunes <= 1 {
		return "…"
	}
	return string(runes[:maxRunes-1]) + "…"
}

// activeTaskRow renders the currently downloading task split in half: the left
// pane shows the chapter being downloaded (manga title above, chapter name
// below), the right pane shows a live progress bar with a Stop button that
// cancels the download.
func (b *DownloadQueueButton) activeTaskRow(task *config.DownloadTask) fyne.CanvasObject {
	mangaLabel := widget.NewLabel(task.Manga.Title)
	mangaLabel.Truncation = fyne.TextTruncateEllipsis
	mangaLabel.TextStyle = fyne.TextStyle{Bold: true}

	chapterLabel := widget.NewLabel(task.Chapter)
	chapterLabel.Truncation = fyne.TextTruncateEllipsis

	leftPane := container.NewVBox(mangaLabel, chapterLabel)

	progress := widget.NewProgressBar()
	progress.SetValue(task.Progress)

	stopButton := widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), func() {
		if err := config.GetDownloadQueue().CancelTask(task.ID); err != nil {
			dialog.ShowError(err, b.state.Window)
			return
		}
		log.Printf("[UI] Stopped task %s (%s - %s)", task.ID, task.Manga.Title, task.Chapter)
	})
	stopButton.Importance = widget.HighImportance

	rightPane := container.NewVBox(progress, container.NewBorder(nil, nil, nil, stopButton, nil))

	// Keep the widgets so later progress ticks can update them in place without
	// rebuilding the whole list.
	b.activeRowManga = mangaLabel
	b.activeRowChapter = chapterLabel
	b.activeRowProgress = progress

	return container.NewGridWithColumns(2, leftPane, rightPane)
}

// updateActiveRowInPlace refreshes the currently displayed "Currently
// Downloading" row without rebuilding the list, for pure progress ticks.
func (b *DownloadQueueButton) updateActiveRowInPlace(task *config.DownloadTask) {
	if b.activeRowProgress == nil {
		return
	}
	b.activeRowManga.SetText(task.Manga.Title)
	b.activeRowChapter.SetText(task.Chapter)
	b.activeRowProgress.SetValue(task.Progress)
}

// groupTasksByManga groups the given tasks by manga title, preserving order.
func groupTasksByManga(tasks []*config.DownloadTask) []*mangaTaskGroup {
	groups := make([]*mangaTaskGroup, 0)
	byTitle := make(map[string]*mangaTaskGroup)
	for _, task := range tasks {
		group := byTitle[task.Manga.Title]
		if group == nil {
			group = &mangaTaskGroup{title: task.Manga.Title}
			byTitle[task.Manga.Title] = group
			groups = append(groups, group)
		}
		group.tasks = append(group.tasks, task)
	}
	return groups
}

// taskSummaryRow returns a one-line summary of a queue task. Tasks that did not
// finish downloading (failed, cancelled, or waiting on a CF challenge) stay in
// the queue and get an action button so the user can re-queue them: "Start" for
// a download the user stopped, "Retry" for a failed or CF-blocked one. The
// label occupies the centre of the row (so it gets the remaining width and
// truncates cleanly instead of wrapping into the button) and the button hugs
// the right edge.
func (b *DownloadQueueButton) taskSummaryRow(task *config.DownloadTask) fyne.CanvasObject {
	status := task.Status
	if task.Chapter != "" {
		status = fmt.Sprintf("%s (%s)", task.Status, task.Chapter)
	}
	label := widget.NewLabel(fmt.Sprintf("%s %s — %s", getStatusIcon(task.Status), task.Manga.Title, status))
	label.Truncation = fyne.TextTruncateEllipsis

	if !isRetryableTaskStatus(task.Status) {
		return label
	}

	actionLabel := "Retry"
	actionIcon := theme.MediaReplayIcon()
	if task.Status == "cancelled" {
		actionLabel = "Start"
		actionIcon = theme.MediaPlayIcon()
	}

	retryButton := widget.NewButtonWithIcon(actionLabel, actionIcon, func() {
		if err := config.GetDownloadQueue().RetryTask(task.ID); err != nil {
			dialog.ShowError(err, b.state.Window)
			return
		}
		log.Printf("[UI] Retried task %s (%s - %s)", task.ID, task.Manga.Title, task.Chapter)
	})
	retryButton.Importance = widget.LowImportance

	return container.NewBorder(nil, nil, nil, retryButton, label)
}

// isRetryableTaskStatus returns true if a task was queued for download but did
// not finish, so it can be retried.
func isRetryableTaskStatus(status string) bool {
	return status == "failed" || status == "cancelled" || status == "waiting_cf"
}

// getStatusIcon returns a unicode icon for a task status.
func getStatusIcon(status string) string {
	switch status {
	case "queued":
		return "⏳"
	case "downloading":
		return "⬇️"
	case "waiting_cf":
		return "🔒"
	case "completed":
		return "✅"
	case "cancelled":
		return "🚫"
	case "failed":
		return "❌"
	default:
		return "❓"
	}
}
