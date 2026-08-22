package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	//"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// enterButton is a button that also activates when the Enter key is pressed
// while it is focused. Fyne's default button only reacts to Space, so a custom
// type is needed so the user can dismiss dialogs with Enter.
type enterButton struct {
	widget.Button
}

// TypedKey triggers the button action on both Enter and Space.
func (b *enterButton) TypedKey(ev *fyne.KeyEvent) {
	if ev.Name == fyne.KeySpace || ev.Name == fyne.KeyReturn {
		b.Tapped(nil)
	}
}

// MangaListView represents the manga list card component.
type MangaListView struct {
	Card fyne.CanvasObject

	List           *widget.List
	addMangaButton *widget.Button
	deleteButton   *widget.Button
	editButton     *widget.Button
	dirButton      *widget.Button
	siteButton     *widget.Button

	searchEntry       *widget.Entry
	searchButton      *widget.Button
	clearSearchButton *widget.Button
	searchResults     []int
	currentSearchIdx  int
	lastSearchTerm    string

	selectedIndex int
	state         *KanshoAppState
	editMangaView *EditMangaView

	onUnfoldEdit func()
	onToggleEdit func()
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

	view.dirButton = widget.NewButton("Directory", func() {
		view.onDirButtonClicked()
	})
	view.dirButton.Disable()

	view.siteButton = widget.NewButton("Website", func() {
		view.onSiteButtonClicked()
	})
	view.siteButton.Disable()

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

	view.addMangaButton = widget.NewButton("Add Manga", func() {
		if view.onToggleEdit != nil {
			view.onToggleEdit()
		}
	})

	sort.Slice(view.state.MangaData.Manga, func(i, j int) bool {
		return view.state.MangaData.Manga[i].Title < view.state.MangaData.Manga[j].Title
	})

	view.List = widget.NewList(
		func() int {
			return len(view.state.MangaData.Manga)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			label := item.(*widget.Label)
			label.SetText(view.state.MangaData.Manga[id].Title)
		},
	)

	view.List.OnSelected = func(id widget.ListItemID) {
		view.selectedIndex = int(id)
		view.deleteButton.Enable()
		view.editButton.Enable()
		view.dirButton.Enable()
		view.siteButton.Enable()
		view.state.SelectManga(int(id))
	}

	cardContent := container.NewBorder(
		container.NewVBox(
			container.NewBorder(
				nil,
				nil,
				NewBoldLabel("Manga List"),
				view.addMangaButton,
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
					view.siteButton,
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

// SetUnfoldEditHandler registers a callback invoked when the user asks to
// open the edit manga panel (via the "Edit Manga" button).
func (v *MangaListView) SetUnfoldEditHandler(fn func()) {
	v.onUnfoldEdit = fn
}

// SetToggleEditHandler registers a callback that toggles the edit manga panel
// open/closed, invoked by the "Add Manga" / "Collapse" header button.
func (v *MangaListView) SetToggleEditHandler(fn func()) {
	v.onToggleEdit = fn
}

// SetAddButtonText updates the label of the add/collapse toggle button to
// reflect the current state of the edit manga panel.
func (v *MangaListView) SetAddButtonText(text string) {
	v.addMangaButton.SetText(text)
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
	v.siteButton.Disable()

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

	if v.onUnfoldEdit != nil {
		v.onUnfoldEdit()
	}
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
			v.showNoSearchResults(searchTerm)
			return
		}
	}

	if len(v.searchResults) == 0 {
		return
	}
	v.currentSearchIdx++
	if v.currentSearchIdx >= len(v.searchResults) {
		v.currentSearchIdx = 0
	}

	resultIndex := v.searchResults[v.currentSearchIdx]
	v.List.Select(widget.ListItemID(resultIndex))
	v.List.ScrollTo(widget.ListItemID(resultIndex))
}

// showNoSearchResults shows a dialog when a search term matches nothing. The OK
// button is focused so the user can dismiss it with Enter, and focus returns to
// the search box afterwards.
func (v *MangaListView) showNoSearchResults(searchTerm string) {
	okButton := &enterButton{}
	okButton.ExtendBaseWidget(okButton)
	okButton.Button.Text = "OK"
	okButton.Button.Importance = widget.HighImportance

	content := container.NewVBox(
		widget.NewLabel(fmt.Sprintf("No manga found matching \"%s\".", searchTerm)),
	)

	dlg := dialog.NewCustom("Search", "", content, v.state.Window)
	dlg.SetButtons([]fyne.CanvasObject{okButton})

	okButton.Button.OnTapped = func() {
		dlg.Hide()
	}
	dlg.SetOnClosed(func() {
		v.state.Window.Canvas().Focus(v.searchEntry)
	})

	dlg.Show()

	// Focus the OK button so pressing Enter dismisses the dialog.
	v.state.Window.Canvas().Focus(okButton)
}

func (v *MangaListView) onSiteButtonClicked() {
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.state.MangaData.Manga) {
		dialog.ShowInformation("Open Site", "Select a manga from the list to open the site.", v.state.Window)
		return
	}

	mangaURL := v.state.MangaData.Manga[v.selectedIndex].Url
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", mangaURL).Start()
	case "darwin":
		err = exec.Command("open", mangaURL).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", mangaURL).Start()
	default:
		dialog.ShowError(fmt.Errorf("unsupported operating system: %s", runtime.GOOS), v.state.Window)
		return
	}

	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to open URL: %v", err), v.state.Window)
	}
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
	v.siteButton.Disable()
}
