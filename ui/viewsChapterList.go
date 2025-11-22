package ui

import (
	"fmt"
	"log"
	"sort"

	"kansho/config"
	"kansho/parser"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type ChapterListView struct {
	Card                fyne.CanvasObject
	selectedMangaLabel  *widget.Label
	chapterList         *widget.List
	contentContainer    *fyne.Container
	queueDownloadButton *widget.Button
	viewToggleButton    *widget.Button
	state               *KanshoAppState
	chapters            []string

	// View management
	downloadQueueView *DownloadQueueView
	showingQueue      bool
	mainContainer     *fyne.Container
}

func NewChapterListView(state *KanshoAppState) *ChapterListView {
	view := &ChapterListView{
		state:        state,
		chapters:     []string{},
		showingQueue: false,
	}

	view.selectedMangaLabel = widget.NewLabel("Select a manga to view chapters")

	// Queue Download button - adds current manga to download queue
	view.queueDownloadButton = widget.NewButton("Queue Download", func() {
		view.onQueueDownloadClicked()
	})
	view.queueDownloadButton.Disable()

	// View Toggle button - switches between chapter list and download queue
	view.viewToggleButton = widget.NewButton("Download Queue", func() {
		view.toggleView()
	})

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

	// Create button container
	buttonContainer := container.NewHBox(
		view.queueDownloadButton,
		view.viewToggleButton,
	)

	// Chapter list card content
	chapterCardContent := container.NewBorder(
		container.NewVBox(
			NewBoldLabel("Chapter List"),
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

	// Create download queue view
	view.downloadQueueView = NewDownloadQueueView(state)

	// Set the toggle callback so Download Queue can switch back
	view.downloadQueueView.SetViewToggleCallback(func() {
		view.toggleView() // This will switch back to chapter list
	})

	// Main container that will swap between views
	view.mainContainer = container.NewStack(NewCard(chapterCardContent))
	view.Card = view.mainContainer

	// Register callbacks
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

func (v *ChapterListView) toggleView() {
	v.showingQueue = !v.showingQueue

	if v.showingQueue {
		// Show download queue
		v.mainContainer.Objects = []fyne.CanvasObject{v.downloadQueueView.Card}
		v.viewToggleButton.SetText("Chapter List")
		log.Println("[UI] Switched to Download Queue view")
	} else {
		// Show chapter list
		chapterCardContent := v.buildChapterListCard()
		v.mainContainer.Objects = []fyne.CanvasObject{NewCard(chapterCardContent)}
		v.viewToggleButton.SetText("Download Queue")
		log.Println("[UI] Switched to Chapter List view")
	}

	v.mainContainer.Refresh()
}

func (v *ChapterListView) buildChapterListCard() *fyne.Container {
	buttonContainer := container.NewHBox(
		v.queueDownloadButton,
		v.viewToggleButton,
	)

	return container.NewBorder(
		container.NewVBox(
			NewBoldLabel("Chapter List"),
			NewSeparator(),
		),
		container.NewVBox(
			NewSeparator(),
			container.NewCenter(buttonContainer),
		),
		nil,
		nil,
		v.contentContainer,
	)
}

func (v *ChapterListView) onQueueDownloadClicked() {
	manga := v.state.GetSelectedManga()
	if manga == nil {
		dialog.ShowError(fmt.Errorf("no manga selected"), v.state.Window)
		return
	}

	queue := config.GetDownloadQueue()
	task, err := queue.AddTask(manga)
	if err != nil {
		dialog.ShowError(err, v.state.Window)
		return
	}

	log.Printf("[UI] Added '%s' to download queue (ID: %s)", manga.Title, task.ID)

	dialog.ShowInformation(
		"Added to Queue",
		fmt.Sprintf("'%s' has been added to the download queue", manga.Title),
		v.state.Window,
	)
}

func (v *ChapterListView) onMangaSelected(id int) {
	manga := v.state.GetSelectedManga()
	if manga == nil {
		v.showNoSelection()
		return
	}

	v.queueDownloadButton.Enable()

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
	v.queueDownloadButton.Disable()
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
