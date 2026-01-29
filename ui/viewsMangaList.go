package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// hoverLabel is a custom label that shows a tooltip on hover
type hoverLabel struct {
	widget.Label
	hovered          bool
	tooltipText      string
	tooltipLabel     *canvas.Text
	tooltipBg        *canvas.Rectangle
	tooltipContainer *fyne.Container
	window           fyne.Window
	hoverTimer       *time.Timer
	overlayShown     bool
}

func newHoverLabel(text, tooltip string, window fyne.Window) *hoverLabel {
	h := &hoverLabel{
		tooltipText: tooltip,
		window:      window,
	}
	h.ExtendBaseWidget(h)
	h.SetText(text)
	h.Truncation = fyne.TextTruncateEllipsis
	return h
}

func (h *hoverLabel) MouseIn(event *desktop.MouseEvent) {
	h.hovered = true
	if h.hoverTimer != nil {
		h.hoverTimer.Stop()
	}

	h.hoverTimer = time.AfterFunc(500*time.Millisecond, func() {
		if h.hovered && h.window != nil {
			abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(h)
			canvasPos := fyne.NewPos(abs.X+event.Position.X, abs.Y+event.Position.Y)
			h.showTooltip(canvasPos)
		}
	})
}

func (h *hoverLabel) MouseOut() {
	h.hovered = false
	if h.hoverTimer != nil {
		h.hoverTimer.Stop()
	}
	h.hideTooltip()
}

func (h *hoverLabel) MouseMoved(event *desktop.MouseEvent) {
	if h.overlayShown {
		abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(h)
		canvasPos := fyne.NewPos(abs.X+event.Position.X, abs.Y+event.Position.Y)
		h.updateTooltipPosition(canvasPos)
	}
}

func (h *hoverLabel) showTooltip(pos fyne.Position) {
	if h.tooltipText == "" {
		return
	}

	h.tooltipLabel = canvas.NewText(h.tooltipText, theme.ForegroundColor())
	h.tooltipLabel.TextSize = 12

	h.tooltipBg = canvas.NewRectangle(theme.BackgroundColor())

	textSize := fyne.MeasureText(h.tooltipText, 12, fyne.TextStyle{})
	padding := float32(8)
	tooltipWidth := textSize.Width + padding*2
	tooltipHeight := textSize.Height + padding*2

	h.tooltipBg.Resize(fyne.NewSize(tooltipWidth, tooltipHeight))
	h.tooltipLabel.Resize(fyne.NewSize(textSize.Width, textSize.Height))

	tooltipX := pos.X + 15
	tooltipY := pos.Y + 15

	canvasSize := h.window.Canvas().Size()
	if tooltipX+tooltipWidth > canvasSize.Width {
		tooltipX = pos.X - tooltipWidth - 5
	}
	if tooltipY+tooltipHeight > canvasSize.Height {
		tooltipY = pos.Y - tooltipHeight - 5
	}

	h.tooltipContainer = container.NewWithoutLayout(h.tooltipBg, h.tooltipLabel)
	h.tooltipBg.Move(fyne.NewPos(tooltipX, tooltipY))
	h.tooltipLabel.Move(fyne.NewPos(tooltipX+padding, tooltipY+padding))

	h.window.Canvas().Overlays().Add(h.tooltipContainer)
	h.overlayShown = true
}

func (h *hoverLabel) updateTooltipPosition(pos fyne.Position) {
	if h.tooltipBg == nil || h.tooltipLabel == nil {
		return
	}

	padding := float32(8)
	tooltipX := pos.X + 15
	tooltipY := pos.Y + 15

	canvasSize := h.window.Canvas().Size()
	tooltipWidth := h.tooltipBg.Size().Width
	tooltipHeight := h.tooltipBg.Size().Height

	if tooltipX+tooltipWidth > canvasSize.Width {
		tooltipX = pos.X - tooltipWidth - 5
	}
	if tooltipY+tooltipHeight > canvasSize.Height {
		tooltipY = pos.Y - tooltipHeight - 5
	}

	h.tooltipBg.Move(fyne.NewPos(tooltipX, tooltipY))
	h.tooltipLabel.Move(fyne.NewPos(tooltipX+padding, tooltipY+padding))
	h.window.Canvas().Refresh(h.tooltipContainer)
}

func (h *hoverLabel) hideTooltip() {
	if h.overlayShown && h.tooltipContainer != nil {
		h.window.Canvas().Overlays().Remove(h.tooltipContainer)
		h.overlayShown = false
		h.tooltipContainer = nil
		h.tooltipLabel = nil
		h.tooltipBg = nil
	}
}

// MangaListView represents the manga list card component.
type MangaListView struct {
	Card fyne.CanvasObject

	List         *widget.List
	deleteButton *widget.Button
	editButton   *widget.Button
	dirButton    *widget.Button

	searchEntry       *widget.Entry
	searchButton      *widget.Button
	clearSearchButton *widget.Button
	searchResults     []int
	currentSearchIdx  int
	lastSearchTerm    string

	selectedIndex int
	state         *KanshoAppState
	editMangaView *EditMangaView
}

func NewMangaListView(state *KanshoAppState) *MangaListView {
	view := &MangaListView{
		state:            state,
		selectedIndex:    -1,
		searchResults:    []int{},
		currentSearchIdx: -1,
		lastSearchTerm:   "",
	}

	view.deleteButton = widget.NewButton("Delete Manga", func() {
		view.onDeleteButtonClicked()
	})
	view.deleteButton.Disable()

	view.editButton = widget.NewButton("Edit Manga", func() {
		view.onEditButtonClicked()
	})
	view.editButton.Disable()

	view.dirButton = widget.NewButton("Manga Dir", func() {
		view.onDirButtonClicked()
	})
	view.dirButton.Disable()

	view.searchEntry = widget.NewEntry()
	view.searchEntry.SetPlaceHolder("Search manga titles...")
	view.searchEntry.OnSubmitted = func(string) {
		view.performSearch()
	}

	view.searchButton = widget.NewButton("Search", func() {
		view.performSearch()
	})

	view.clearSearchButton = widget.NewButton("Clear Search", func() {
		view.clearSearch()
	})

	sort.Slice(view.state.MangaData.Manga, func(i, j int) bool {
		return view.state.MangaData.Manga[i].Title < view.state.MangaData.Manga[j].Title
	})

	view.List = widget.NewList(
		func() int {
			return len(view.state.MangaData.Manga)
		},
		func() fyne.CanvasObject {
			return newHoverLabel("template", "", view.state.Window)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			hoverLabel := item.(*hoverLabel)
			manga := view.state.MangaData.Manga[id]
			hoverLabel.SetText(manga.Title)
			hoverLabel.tooltipText = fmt.Sprintf("Site: %s", manga.Site)
		},
	)

	view.List.OnSelected = func(id widget.ListItemID) {
		view.selectedIndex = int(id)
		view.deleteButton.Enable()
		view.editButton.Enable()
		view.dirButton.Enable()
		view.state.SelectManga(int(id))
	}

	cardContent := container.NewBorder(
		container.NewVBox(
			container.NewBorder(
				nil,
				nil,
				NewBoldLabel("Manga List"),
				nil,
				view.searchEntry,
			),
			NewSeparator(),
		),
		container.NewVBox(
			NewSeparator(),
			container.NewCenter(
				container.NewHBox(
					view.searchButton,
					view.clearSearchButton,
					view.deleteButton,
					view.editButton,
					view.dirButton,
				),
			),
		),
		nil,
		nil,
		view.List,
	)

	view.Card = NewCard(cardContent)

	view.state.RegisterMangaAddedCallback(func() {
		view.refresh()
	})

	view.state.RegisterMangaDeletedCallback(func(int) {
		view.refresh()
	})

	return view
}

func (v *MangaListView) SetEditMangaView(editView *EditMangaView) {
	v.editMangaView = editView
}

func (v *MangaListView) refresh() {
	sort.Slice(v.state.MangaData.Manga, func(i, j int) bool {
		return v.state.MangaData.Manga[i].Title < v.state.MangaData.Manga[j].Title
	})

	v.selectedIndex = -1
	v.List.UnselectAll()
	v.deleteButton.Disable()
	v.editButton.Disable()
	v.dirButton.Disable()

	v.searchResults = []int{}
	v.currentSearchIdx = -1

	v.List.Refresh()
}

func (v *MangaListView) onDeleteButtonClicked() {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.state.MangaData.Manga) {
		dialog.ShowInformation("Delete Manga", "Please select a manga to delete.", v.state.Window)
		return
	}

	mangaTitle := v.state.MangaData.Manga[v.selectedIndex].Title

	dialog.ShowConfirm(
		"Delete Manga",
		"Are you sure you want to delete \""+mangaTitle+"\"?",
		func(confirmed bool) {
			if confirmed {
				v.state.DeleteManga(v.selectedIndex)
			}
		},
		v.state.Window,
	)
}

func (v *MangaListView) onEditButtonClicked() {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.state.MangaData.Manga) {
		dialog.ShowInformation("Edit Manga", "Please select a manga to edit.", v.state.Window)
		return
	}

	if v.editMangaView == nil {
		dialog.ShowError(fmt.Errorf("edit manga view not initialized"), v.state.Window)
		return
	}

	v.editMangaView.LoadMangaForEditing(v.selectedIndex)
}

func (v *MangaListView) onDirButtonClicked() {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.state.MangaData.Manga) {
		dialog.ShowInformation("Open Manga Directory", "Please select a manga to open its directory.", v.state.Window)
		return
	}

	mangaLocation := v.state.MangaData.Manga[v.selectedIndex].Location

	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", mangaLocation).Start()
	case "darwin":
		err = exec.Command("open", mangaLocation).Start()
	case "windows":
		err = exec.Command("explorer", mangaLocation).Start()
	default:
		dialog.ShowError(fmt.Errorf("unsupported operating system: %s", runtime.GOOS), v.state.Window)
		return
	}

	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to open directory: %v", err), v.state.Window)
	}
}

func (v *MangaListView) performSearch() {
	searchTerm := strings.TrimSpace(v.searchEntry.Text)
	if searchTerm == "" {
		dialog.ShowInformation("Search", "Please enter a search term.", v.state.Window)
		return
	}

	searchTermLower := strings.ToLower(searchTerm)

	if searchTerm != v.lastSearchTerm {
		v.searchResults = []int{}
		for i, manga := range v.state.MangaData.Manga {
			if strings.Contains(strings.ToLower(manga.Title), searchTermLower) {
				v.searchResults = append(v.searchResults, i)
			}
		}

		v.lastSearchTerm = searchTerm
		v.currentSearchIdx = -1

		if len(v.searchResults) == 0 {
			dialog.ShowInformation("Search", fmt.Sprintf("No manga found matching \"%s\".", searchTerm), v.state.Window)
			return
		}
	}

	v.currentSearchIdx++
	if v.currentSearchIdx >= len(v.searchResults) {
		v.currentSearchIdx = 0
	}

	resultIndex := v.searchResults[v.currentSearchIdx]
	v.List.Select(widget.ListItemID(resultIndex))
	v.List.ScrollTo(widget.ListItemID(resultIndex))
}

func (v *MangaListView) clearSearch() {
	v.searchEntry.SetText("")
	v.searchResults = []int{}
	v.currentSearchIdx = -1
	v.lastSearchTerm = ""

	v.List.UnselectAll()
	v.selectedIndex = -1
	v.deleteButton.Disable()
	v.editButton.Disable()
	v.dirButton.Disable()
}
