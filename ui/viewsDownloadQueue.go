package ui

import (
	"fmt"
	"log"

	"kansho/cf"
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
	retryButton       *widget.Button
	cancelAllButton   *widget.Button
	clearButton       *widget.Button
	chapterListButton *widget.Button
	state             *KanshoAppState
	tasks             []*config.DownloadTask
	selectedTaskID    string
	onViewToggle      func()
	cfDialogShown     map[string]bool
}

func NewDownloadQueueView(state *KanshoAppState) *DownloadQueueView {
	view := &DownloadQueueView{
		state:         state,
		tasks:         []*config.DownloadTask{},
		cfDialogShown: make(map[string]bool),
	}

	view.cancelButton = widget.NewButton("Cancel Download", func() {
		view.onCancelDownload()
	})
	view.cancelButton.Disable()

	view.retryButton = widget.NewButton("Retry", func() {
		view.onRetryDownload()
	})
	view.retryButton.Disable()

	view.cancelAllButton = widget.NewButton("Cancel All", func() {
		view.onCancelAll()
	})

	view.clearButton = widget.NewButton("Clear Completed", func() {
		view.onClearCompleted()
	})

	view.chapterListButton = widget.NewButton("Chapter List", func() {
		if view.onViewToggle != nil {
			view.onViewToggle()
		}
	})

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

			statusIcon := view.getStatusIcon(task.Status)
			titleLabel.SetText(fmt.Sprintf("%s %s", statusIcon, task.Manga.Title))
			statusLabel.SetText(task.StatusMessage)
			progressBar.SetValue(task.Progress)
		},
	)

	view.taskList.OnSelected = func(id widget.ListItemID) {
		if id < len(view.tasks) {
			view.selectedTaskID = view.tasks[id].ID
			task := view.tasks[id]

			switch task.Status {
			case "queued", "downloading":
				view.cancelButton.Enable()
				view.retryButton.Disable()
			case "waiting_cf", "failed":
				view.cancelButton.Disable()
				view.retryButton.Enable()
			default:
				view.cancelButton.Disable()
				view.retryButton.Disable()
			}
		}
	}

	view.taskList.OnUnselected = func(id widget.ListItemID) {
		view.selectedTaskID = ""
		view.cancelButton.Disable()
		view.retryButton.Disable()
	}

	view.contentContainer = container.NewStack(
		widget.NewLabel("No downloads in queue"),
	)

	buttonContainer := container.NewHBox(
		view.cancelButton,
		view.retryButton,
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

	queue := config.GetDownloadQueue()
	queue.SetCallbacks(
		func(task *config.DownloadTask) {
			fyne.Do(func() {
				view.refreshTaskList()
			})
		},
		func(task *config.DownloadTask) {
			fyne.Do(func() {
				if task.Status == "waiting_cf" && !view.cfDialogShown[task.ID] {
					view.showCFDialog(task)
					view.cfDialogShown[task.ID] = true
				}
				view.refreshTaskList()
			})
		},
		func(taskID string) {
			fyne.Do(func() {
				delete(view.cfDialogShown, taskID)
				view.refreshTaskList()
			})
		},
		func() {
			fyne.Do(func() {
				view.refreshTaskList()
			})
		},
	)

	view.refreshTaskList()
	return view
}

func (v *DownloadQueueView) SetViewToggleCallback(callback func()) {
	v.onViewToggle = callback
}

func (v *DownloadQueueView) getStatusIcon(status string) string {
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

func (v *DownloadQueueView) showCFDialog(task *config.DownloadTask) {
	log.Printf("[UI] showCFDialog called for task: %s", task.ID)
	log.Printf("[UI] task.Error type: %T", task.Error)
	log.Printf("[UI] task.Error value: %v", task.Error)

	cfErr, ok := task.Error.(*cf.CfChallengeError)
	if !ok {
		log.Printf("[UI] ERROR: task.Error is NOT a *cf.CfChallengeError!")
		return
	}

	log.Printf("[UI] Showing CF dialog for URL: %s", cfErr.URL)
	ShowcfDialog(v.state.Window, cfErr.URL, func() {
		queue := config.GetDownloadQueue()
		delete(v.cfDialogShown, task.ID)
		if err := queue.RetryTask(task.ID); err != nil {
			dialog.ShowError(fmt.Errorf("failed to retry: %w", err), v.state.Window)
		}
	})
	log.Printf("[UI] CF dialog should be visible now")
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
	v.retryButton.Disable()
	v.refreshTaskList()
}

func (v *DownloadQueueView) onRetryDownload() {
	if v.selectedTaskID == "" {
		return
	}

	delete(v.cfDialogShown, v.selectedTaskID)

	queue := config.GetDownloadQueue()
	err := queue.RetryTask(v.selectedTaskID)
	if err != nil {
		dialog.ShowError(err, v.state.Window)
		return
	}

	log.Printf("[UI] Retrying task: %s", v.selectedTaskID)
	v.selectedTaskID = ""
	v.cancelButton.Disable()
	v.retryButton.Disable()
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

	currentTasks := queue.GetTasks()
	currentIDs := make(map[string]bool)
	for _, task := range currentTasks {
		currentIDs[task.ID] = true
	}

	for id := range v.cfDialogShown {
		if !currentIDs[id] {
			delete(v.cfDialogShown, id)
		}
	}

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

	if len(v.tasks) > 0 {
		v.taskList.Refresh()
	}
}
