package ui

import (
	"fmt"
	"log"

	"kansho/config"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type DownloadQueueView struct {
	Card              fyne.CanvasObject
	taskList          *widget.List
	contentContainer  *fyne.Container
	cancelButton      *widget.Button
	cancelAllButton   *widget.Button
	clearButton       *widget.Button
	chapterListButton *widget.Button
	state             *KanshoAppState
	tasks             []*config.DownloadTask
	selectedTaskID    string
	onViewToggle      func() // Callback to toggle back to chapter list
}

func NewDownloadQueueView(state *KanshoAppState) *DownloadQueueView {
	view := &DownloadQueueView{
		state: state,
		tasks: []*config.DownloadTask{},
	}

	// Cancel Download button (initially disabled)
	view.cancelButton = widget.NewButton("Cancel Download", func() {
		view.onCancelDownload()
	})
	view.cancelButton.Disable()

	// Cancel All button
	view.cancelAllButton = widget.NewButton("Cancel All", func() {
		view.onCancelAll()
	})

	// Clear Completed button
	view.clearButton = widget.NewButton("Clear Completed", func() {
		view.onClearCompleted()
	})

	// Chapter List button (to toggle back)
	view.chapterListButton = widget.NewButton("Chapter List", func() {
		if view.onViewToggle != nil {
			view.onViewToggle()
		}
	})

	// Create task list
	view.taskList = widget.NewList(
		func() int {
			return len(view.tasks)
		},
		func() fyne.CanvasObject {
			titleLabel := widget.NewLabel("Manga Title")
			titleLabel.TextStyle.Bold = true

			statusLabel := widget.NewLabel("Status message")
			statusLabel.Truncation = fyne.TextTruncateEllipsis

			progressBar := widget.NewProgressBar()
			progressBar.Min = 0
			progressBar.Max = 1

			return container.NewVBox(
				titleLabel,
				statusLabel,
				progressBar,
			)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if id >= len(view.tasks) {
				return
			}

			task := view.tasks[id]
			vbox := item.(*fyne.Container)

			titleLabel := vbox.Objects[0].(*widget.Label)
			statusLabel := vbox.Objects[1].(*widget.Label)
			progressBar := vbox.Objects[2].(*widget.ProgressBar)

			// Format title with status indicator
			statusIcon := view.getStatusIcon(task.Status)
			titleLabel.SetText(fmt.Sprintf("%s %s", statusIcon, task.Manga.Title))

			// Set status message
			statusLabel.SetText(task.StatusMessage)

			// Set progress
			progressBar.SetValue(task.Progress)
		},
	)

	// Handle selection
	view.taskList.OnSelected = func(id widget.ListItemID) {
		if id < len(view.tasks) {
			view.selectedTaskID = view.tasks[id].ID
			task := view.tasks[id]

			// Enable cancel button only if task is queued or downloading
			if task.Status == "queued" || task.Status == "downloading" {
				view.cancelButton.Enable()
			} else {
				view.cancelButton.Disable()
			}
		}
	}

	view.taskList.OnUnselected = func(id widget.ListItemID) {
		view.selectedTaskID = ""
		view.cancelButton.Disable()
	}

	view.contentContainer = container.NewStack(
		widget.NewLabel("No downloads in queue"),
	)

	buttonContainer := container.NewHBox(
		view.cancelButton,
		view.cancelAllButton,
		view.clearButton,
		view.chapterListButton,
	)

	cardContent := container.NewBorder(
		container.NewVBox(
			NewBoldLabel("Download Queue"),
			NewSeparator(),
		),
		container.NewVBox(
			NewSeparator(),
			container.NewCenter(buttonContainer),
		),
		nil,
		nil,
		view.contentContainer,
	)

	view.Card = NewCard(cardContent)

	// Register queue callbacks
	queue := config.GetDownloadQueue()
	queue.SetCallbacks(
		func(task *config.DownloadTask) {
			fyne.Do(func() {
				view.refreshTaskList()
			})
		},
		func(task *config.DownloadTask) {
			fyne.Do(func() {
				view.refreshTaskList()
			})
		},
		func(taskID string) {
			fyne.Do(func() {
				view.refreshTaskList()
			})
		},
		func() {
			fyne.Do(func() {
				view.refreshTaskList()
			})
		},
	)

	// Initial load
	view.refreshTaskList()

	return view
}

// SetViewToggleCallback sets the callback for the Chapter List button
func (v *DownloadQueueView) SetViewToggleCallback(callback func()) {
	v.onViewToggle = callback
}

func (v *DownloadQueueView) getStatusIcon(status string) string {
	switch status {
	case "queued":
		return "⏳"
	case "downloading":
		return "⬇️"
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

func (v *DownloadQueueView) onCancelDownload() {
	if v.selectedTaskID == "" {
		return
	}

	queue := config.GetDownloadQueue()
	err := queue.CancelTask(v.selectedTaskID)
	if err != nil {
		dialog.ShowError(err, v.state.Window)
		return
	}

	log.Printf("[UI] Cancelled task: %s", v.selectedTaskID)
	v.selectedTaskID = ""
	v.cancelButton.Disable()
	v.refreshTaskList()
}

func (v *DownloadQueueView) onCancelAll() {
	dialog.ShowConfirm(
		"Cancel All Downloads",
		"Are you sure you want to cancel all downloads?",
		func(confirmed bool) {
			if confirmed {
				queue := config.GetDownloadQueue()
				queue.CancelAll()
				log.Println("[UI] Cancelled all tasks")
				v.refreshTaskList()
			}
		},
		v.state.Window,
	)
}

func (v *DownloadQueueView) onClearCompleted() {
	queue := config.GetDownloadQueue()
	queue.RemoveCompletedTasks()
	log.Println("[UI] Cleared completed tasks")
	v.refreshTaskList()
}

func (v *DownloadQueueView) refreshTaskList() {
	queue := config.GetDownloadQueue()
	v.tasks = queue.GetTasks()

	if len(v.tasks) == 0 {
		v.contentContainer.Objects = []fyne.CanvasObject{
			widget.NewLabel("No downloads in queue"),
		}
	} else {
		v.contentContainer.Objects = []fyne.CanvasObject{v.taskList}
	}

	v.contentContainer.Refresh()

	// Refresh the list if it's being displayed
	if len(v.tasks) > 0 {
		v.taskList.Refresh()
	}
}
