package ui

import (
	"fmt"
	"log"

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
}

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
		b.refreshPopup()
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
	b.refreshPopup()
	b.popup.Show()
}

// buildPopup constructs the stable pop-up structure: a fixed header (title,
// overall progress + global "Cancel All", and a Close button) above a
// scrollable list of the queue contents.
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

	return container.NewBorder(header, nil, nil, nil, scroll)
}

// refreshPopup rebuilds the pop-up's list from the current queue state,
// grouping tasks by manga with a per-manga "Cancel All" button and a "Retry"
// button on any unfinished task.
func (b *DownloadQueueButton) refreshPopup() {
	queue := config.GetDownloadQueue()
	tasks := queue.GetTasks()

	b.overallLabel.SetText(fmt.Sprintf("Overall: %d manga · %d chapters in queue", len(taskTitles(tasks)), len(tasks)))

	objects := make([]fyne.CanvasObject, 0, len(tasks)+len(tasks)/2)
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
// the queue and get a "Retry" button so the user can re-queue them. The label
// occupies the centre of the row (so it gets the remaining width and truncates
// cleanly instead of wrapping into the Retry button) and the button hugs the
// right edge.
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

	retryButton := widget.NewButtonWithIcon("Retry", theme.MediaReplayIcon(), func() {
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
