package ui

import (
	"fmt"
	"image/color"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"kansho/mangadex"
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

// Filter option labels shared by the status dropdown and the type radio group.
const (
	filterAll       = "All"
	statusOngoing   = "Ongoing"
	statusCompleted = "Completed"
	statusHiatus    = "Hiatus"
	statusCancelled = "Cancelled"
)

var statusFilterOptions = []string{filterAll, statusOngoing, statusCompleted, statusHiatus, statusCancelled}
var typeFilterOptions = []string{filterAll, "Manga", "Webtoon"}

// MangaListView represents the manga list card component.
type MangaListView struct {
	Card fyne.CanvasObject

	List           *widget.List
	addMangaButton *widget.Button
	deleteButton   *widget.Button
	editButton     *widget.Button
	dirButton      *widget.Button
	siteButton     *widget.Button

	statusSelect *widget.Select
	typeRadio    *widget.RadioGroup
	// filterIndices maps each visible list row to its index in
	// state.MangaData.Manga, so the list can show a filtered subset while the
	// selection buttons keep working on the full bookmark slice.
	filterIndices []int

	searchEntry       *widget.Entry
	searchButton      *widget.Button
	clearSearchButton *widget.Button
	searchResults     []int
	currentSearchIdx  int
	lastSearchTerm    string

	selectedIndex int
	state         *KanshoAppState
	editMangaView *EditMangaView

	infoDisplayHandler func(info *mangadex.MangaInfo, bookmarkTitle string)
	onCountChanged     func(int)

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

	view.statusSelect = widget.NewSelect(statusFilterOptions, func(string) {
		view.applyFilter()
	})
	view.statusSelect.SetSelected(filterAll)

	view.typeRadio = widget.NewRadioGroup(typeFilterOptions, func(string) {
		view.applyFilter()
	})
	view.typeRadio.Horizontal = true
	view.typeRadio.SetSelected(filterAll)

	sort.Slice(view.state.MangaData.Manga, func(i, j int) bool {
		return view.state.MangaData.Manga[i].Title < view.state.MangaData.Manga[j].Title
	})

	view.List = widget.NewList(
		func() int {
			return len(view.filterIndices)
		},
		func() fyne.CanvasObject {
			// Each row is a border layout: the title label fills the row and a
			// plain (i) info icon sits on the right-hand side, mirroring the
			// per-chapter action icons.
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis
			infoButton := newIconButton(mangaInfoIconResource)
			gutter := canvas.NewRectangle(color.Transparent)
			gutter.SetMinSize(fyne.NewSize(theme.ScrollBarSize(), 1))
			return container.NewBorder(nil, nil, nil, container.NewHBox(infoButton, gutter), label)
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			if int(id) < 0 || int(id) >= len(view.filterIndices) {
				return
			}
			mangaIndex := view.filterIndices[id]
			row := item.(*fyne.Container)
			label := row.Objects[0].(*widget.Label)
			controls := row.Objects[1].(*fyne.Container)
			infoButton := controls.Objects[0].(*iconButton)
			label.SetText(view.state.MangaData.Manga[mangaIndex].Title)
			infoButton.OnTapped = func() {
				view.onInfoButtonClicked(mangaIndex)
			}
		},
	)

	view.List.OnSelected = func(id widget.ListItemID) {
		if int(id) < 0 || int(id) >= len(view.filterIndices) {
			return
		}
		mangaIndex := view.filterIndices[id]
		view.selectedIndex = mangaIndex
		view.deleteButton.Enable()
		view.editButton.Enable()
		view.dirButton.Enable()
		view.siteButton.Enable()
		view.state.SelectManga(mangaIndex)
	}

	listTheme := mangaListScrollTheme{Theme: theme.Current()}
	listWithPersistentScrollbar := container.NewThemeOverride(view.List, listTheme)
	cardContent := container.NewBorder(
		container.NewVBox(
			container.NewHBox(
				widget.NewLabel("Status:"),
				view.statusSelect,
				widget.NewLabel("Type:"),
				view.typeRadio,
			),
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
		listWithPersistentScrollbar,
	)

	view.Card = NewCard(cardContent)

	view.state.RegisterMangaAddedCallback(func() {
		view.refresh()
	})

	view.state.RegisterMangaDeletedCallback(func(int) {
		view.refresh()
	})

	view.refresh()

	return view
}

type mangaListScrollTheme struct {
	fyne.Theme
}

func (t mangaListScrollTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameScrollBarSmall {
		return t.Theme.Size(theme.SizeNameScrollBar)
	}
	return t.Theme.Size(name)
}

func (v *MangaListView) SetEditMangaView(editView *EditMangaView) {
	v.editMangaView = editView
}

func (v *MangaListView) SetCountChangedHandler(handler func(int)) {
	v.onCountChanged = handler
	if handler != nil {
		handler(len(v.filterIndices))
	}
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

	v.searchResults = []int{}
	v.currentSearchIdx = -1

	v.applyFilter()
}

// applyFilter rebuilds the visible row mapping from the current status and type
// filters, then refreshes the list and the displayed count. The status filter is
// resolved from the local MangaDex records, so it only queries the database when
// a specific status is selected.
func (v *MangaListView) applyFilter() {
	// The filter widgets fire their change callback while they are being
	// configured in the constructor, before the list exists.
	if v.List == nil {
		return
	}

	status := v.statusSelect.Selected
	kind := v.typeRadio.Selected

	var statuses []string
	if status != "" && status != filterAll {
		if store, err := mangadex.GetStore(); err == nil {
			statuses, _ = store.BookmarkStatuses(v.state.MangaData.Manga)
		}
	}

	v.filterIndices = make([]int, 0, len(v.state.MangaData.Manga))
	for i, manga := range v.state.MangaData.Manga {
		if !matchesTypeFilter(manga.Type, kind) {
			continue
		}
		if status != "" && status != filterAll {
			current := ""
			if i < len(statuses) {
				current = prettifyStatus(statuses[i])
			}
			if !strings.EqualFold(current, status) {
				continue
			}
		}
		v.filterIndices = append(v.filterIndices, i)
	}

	v.selectedIndex = -1
	v.List.UnselectAll()
	v.disableSelectionButtons()
	v.List.Refresh()
	if v.onCountChanged != nil {
		v.onCountChanged(len(v.filterIndices))
	}
}

// matchesTypeFilter reports whether a bookmark type passes the type radio
// filter. An empty or unknown type is only shown by the "All" option.
func matchesTypeFilter(bookmarkType, filter string) bool {
	if filter == "" || filter == filterAll {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(bookmarkType), filter)
}

func (v *MangaListView) disableSelectionButtons() {
	v.deleteButton.Disable()
	v.editButton.Disable()
	v.dirButton.Disable()
	v.siteButton.Disable()
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
		for row, mangaIndex := range v.filterIndices {
			if strings.Contains(strings.ToLower(v.state.MangaData.Manga[mangaIndex].Title), searchTermLower) {
				v.searchResults = append(v.searchResults, row)
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
	if strings.TrimSpace(mangaURL) == "" {
		dialog.ShowInformation("Open Site", "This Local title has no website URL.", v.state.Window)
		return
	}
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

// onInfoButtonClicked opens the MangaDex title information for the manga at the
// given list index, either from the local database or through the lookup
// dialog when no local record exists yet. The information is rendered in the
// chapter list pane via the display handler.
func (v *MangaListView) onInfoButtonClicked(mangaIndex int) {
	if mangaIndex < 0 || mangaIndex >= len(v.state.MangaData.Manga) {
		return
	}
	if v.infoDisplayHandler == nil {
		return
	}
	manga := v.state.MangaData.Manga[mangaIndex]
	ShowMangaTitleInfo(manga, v.state.Window, v.infoDisplayHandler)
}

// SetInfoDisplayHandler registers the callback that renders a MangaDex title
// record. It is wired to the chapter list pane so title info is displayed in
// the main window instead of a separate dialog window.
func (v *MangaListView) SetInfoDisplayHandler(fn func(info *mangadex.MangaInfo, bookmarkTitle string)) {
	v.infoDisplayHandler = fn
}
