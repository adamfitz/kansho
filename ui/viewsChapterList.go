package ui

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"kansho/cf"
	"kansho/parser"
	"kansho/sites"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type ChapterListView struct {
	Card               fyne.CanvasObject
	selectedMangaLabel *widget.Label
	chapterList        *widget.List
	contentContainer   *fyne.Container
	updateButton       *widget.Button
	stopButton         *widget.Button // NEW: Stop button
	progressBar        *widget.ProgressBar
	progressLabel      *widget.Label
	progressContainer  *fyne.Container
	chapters           []string
	state              *KanshoAppState

	// NEW: Context for cancellation
	cancelFunc context.CancelFunc
}

func NewChapterListView(state *KanshoAppState) *ChapterListView {
	view := &ChapterListView{
		state:    state,
		chapters: []string{},
	}

	view.selectedMangaLabel = widget.NewLabel("Select a manga to view chapters")

	// Create Update button
	view.updateButton = widget.NewButton("Update Chapters", func() {
		view.onUpdateButtonClicked()
	})
	view.updateButton.Disable()

	// NEW: Create Stop button (initially disabled)
	view.stopButton = widget.NewButton("Stop Download", func() {
		view.onStopButtonClicked()
	})
	view.stopButton.Disable()

	view.progressBar = widget.NewProgressBar()
	view.progressBar.Min = 0
	view.progressBar.Max = 1

	view.progressLabel = widget.NewLabel("")
	view.progressLabel.Truncation = fyne.TextTruncateEllipsis
	view.progressLabel.Wrapping = fyne.TextWrapWord

	view.progressContainer = container.NewVBox(
		view.progressLabel,
		view.progressBar,
	)
	view.progressContainer.Hide()

	view.chapterList = widget.NewList(
		func() int {
			return len(view.chapters)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			label := item.(*widget.Label)
			label.SetText(view.chapters[id])
		},
	)

	view.contentContainer = container.NewStack(
		widget.NewLabel("Select a manga to view chapters"),
	)

	// NEW: Create button container with both Update and Stop buttons
	buttonContainer := container.NewHBox(
		view.updateButton,
		view.stopButton,
	)

	cardContent := container.NewBorder(
		container.NewVBox(
			NewBoldLabel("Chapter List"),
			NewSeparator(),
		),
		container.NewVBox(
			view.progressContainer,
			NewSeparator(),
			container.NewCenter(buttonContainer), // Changed from just updateButton
		),
		nil,
		nil,
		view.contentContainer,
	)

	view.Card = NewCard(cardContent)

	view.state.RegisterMangaSelectedCallback(func(id int) {
		view.onMangaSelected(id)
	})

	view.state.RegisterMangaDeletedCallback(func(id int) {
		if view.state.SelectedMangaID == -1 {
			view.showNoSelection()
		}
	})

	return view
}

// NEW: Handler for stop button
func (v *ChapterListView) onStopButtonClicked() {
	if v.cancelFunc != nil {
		log.Println("User requested download cancellation")
		v.progressLabel.SetText("Stopping download...")
		v.cancelFunc() // Trigger cancellation
		v.stopButton.Disable()
	}
}

// UPDATED: onUpdateButtonClicked with context cancellation
func (v *ChapterListView) onUpdateButtonClicked() {
	manga := v.state.GetSelectedManga()
	if manga == nil {
		return
	}

	// Disable update button, enable stop button
	v.updateButton.Disable()
	v.stopButton.Enable()

	v.progressContainer.Show()
	v.progressBar.SetValue(0)
	v.progressLabel.SetText("Starting download...")

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	v.cancelFunc = cancel

	go func() {
		// Ensure we clean up when done
		defer func() {
			fyne.Do(func() {
				v.progressContainer.Hide()
				v.updateButton.Enable()
				v.stopButton.Disable()
				v.cancelFunc = nil
			})
		}()

		var totalChapters int
		var err error

		// Pass context to download functions
		switch manga.Site {
		case "mgeko":
			err = sites.MgekoDownloadChapters(ctx, manga, func(status string, progress float64, current, total int) {
				totalChapters = total
				fyne.Do(func() {
					v.progressLabel.SetText(status)
					v.progressBar.SetValue(progress)
				})
			})

		case "xbato":
			err = sites.XbatoDownloadChapters(ctx, manga, func(status string, progress float64, current, total int) {
				totalChapters = total
				fyne.Do(func() {
					v.progressLabel.SetText(status)
					v.progressBar.SetValue(progress)
				})
			})

		case "rizzfables":
			err = sites.RizzfablesDownloadChapters(ctx, manga, func(status string, progress float64, current, total int) {
				totalChapters = total
				fyne.Do(func() {
					v.progressLabel.SetText(status)
					v.progressBar.SetValue(progress)
				})
			})

		case "manhuaus":
			err = sites.ManhuausDownloadChapters(ctx, manga, func(status string, progress float64, current, total int) {
				totalChapters = total
				fyne.Do(func() {
					v.progressLabel.SetText(status)
					v.progressBar.SetValue(progress)
				})
			})

		default:
			fyne.Do(func() {
				err := fmt.Errorf("download not supported for site: %s", manga.Site)
				v.progressLabel.SetText(fmt.Sprintf("Error: %v", err))
				dialog.ShowError(err, v.state.Window)
			})
			return
		}

		fyne.Do(func() {
			if err != nil {
				// Check if cancelled
				if err == context.Canceled {
					v.progressLabel.SetText("Download stopped by user")
					dialog.ShowInformation("Download Stopped", "Download was cancelled", v.state.Window)
					return
				}

				// Check if CF challenge
				if cfErr, ok := cf.IscfChallenge(err); ok {
					v.progressLabel.SetText("cf challenge detected")
					onSuccess := func() {
						v.progressLabel.SetText("cf data imported. Retrying download...")
						go func() {
							time.Sleep(1 * time.Second)
							fyne.Do(func() {
								v.progressLabel.SetText("Please click 'Update Chapters' again to retry")
							})
						}()
					}
					ShowcfDialog(v.state.Window, cfErr.URL, onSuccess)
					return
				}

				// Regular error
				v.progressLabel.SetText(fmt.Sprintf("Error: %v", err))
				dialog.ShowError(err, v.state.Window)
				return
			}

			// Success
			if totalChapters > 0 {
				v.progressLabel.SetText("Download complete.")
				dialog.ShowInformation(
					"Download Complete",
					fmt.Sprintf("Successfully downloaded %d chapters for %s", totalChapters, manga.Title),
					v.state.Window,
				)
			}

			v.onMangaSelected(v.state.SelectedMangaID)
		})
	}()
}

// Keep other methods unchanged...
func (v *ChapterListView) onMangaSelected(id int) {
	manga := v.state.GetSelectedManga()
	if manga == nil {
		v.showNoSelection()
		return
	}

	v.updateButton.Enable()

	if manga.Location == "" {
		v.defaultChapterList()
		return
	}

	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		dialog.ShowError(err, v.state.Window)
		v.defaultChapterList()
		return
	}

	if len(downloadedChapters) == 0 {
		v.defaultChapterList()
		return
	}

	sort.Strings(downloadedChapters)
	v.updateChapterList(downloadedChapters)
	numLocalChapters := len(downloadedChapters)
	log.Printf("Found %d local chapters [%s]", numLocalChapters, manga.Title)
}

func (v *ChapterListView) updateChapterList(chapters []string) {
	v.chapters = chapters

	if len(chapters) == 0 {
		v.showNoChapters()
		return
	}

	v.chapterList = widget.NewList(
		func() int {
			return len(v.chapters)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			label := item.(*widget.Label)
			if id < len(v.chapters) {
				label.SetText(v.chapters[id])
			}
		},
	)

	v.contentContainer.Objects = []fyne.CanvasObject{v.chapterList}
	v.contentContainer.Refresh()
}

func (v *ChapterListView) showNoSelection() {
	v.chapters = []string{}
	v.updateButton.Disable()
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel("Select a manga to view chapters"),
	}
	v.contentContainer.Refresh()
}

func (v *ChapterListView) defaultChapterList() {
	v.chapters = []string{}
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel("No chapters found"),
	}
	v.contentContainer.Refresh()
}

func (v *ChapterListView) showNoChapters() {
	v.chapters = []string{}
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel("No chapters found for this manga"),
	}
	v.contentContainer.Refresh()
}
