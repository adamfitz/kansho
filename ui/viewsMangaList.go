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
	"fyne.io/fyne/v2/widget"
)

// MangaListView represents the manga list card component.
// This view displays all manga bookmarks in a scrollable list.
// Users can click on entries to select them, which updates the chapter list.
//
// The view automatically:
// - Sorts manga alphabetically by title
// - Refreshes when new manga are added
// - Updates when manga are deleted
// - Notifies the app state when a selection is made
type MangaListView struct {
	// Card is the complete UI component ready to be added to the layout
	Card fyne.CanvasObject

	// List is the scrollable list widget showing manga titles
	List         *widget.List
	deleteButton *widget.Button // Button to delete the selected manga
	editButton   *widget.Button // Button to edit the selected manga
	dirButton    *widget.Button // Button to open manga directory

	// Search components
	searchEntry       *widget.Entry
	searchButton      *widget.Button
	clearSearchButton *widget.Button
	searchResults     []int  // Indices of manga that match search
	currentSearchIdx  int    // Current position in search results
	lastSearchTerm    string // Last search term to detect changes

	// Track the currently selected manga index
	selectedIndex int

	// state is a reference to the shared application state
	// This allows the view to access manga data and notify of selection changes
	state *KanshoAppState

	// Reference to the edit manga view for loading manga data
	editMangaView *EditMangaView
}

// NewMangaListView creates a new manga list view component.
// The view displays all manga from the app state and keeps itself synchronized
// with state changes through callbacks.
//
// Parameters:
//   - state: Pointer to the shared application state
//
// Returns:
//   - *MangaListView: A new manga list view with all components initialized
//
// The view registers callbacks to:
// - Refresh the list when manga are added
// - Refresh the list when manga are deleted
func NewMangaListView(state *KanshoAppState) *MangaListView {
	view := &MangaListView{
		state:            state,
		selectedIndex:    -1, // No selection initially
		searchResults:    []int{},
		currentSearchIdx: -1,
		lastSearchTerm:   "",
	}

	// Create the Delete Manga button
	view.deleteButton = widget.NewButton("Delete Manga", func() {
		view.onDeleteButtonClicked()
	})
	view.deleteButton.Disable()

	// Create the Edit Manga button
	view.editButton = widget.NewButton("Edit Manga", func() {
		view.onEditButtonClicked()
	})
	view.editButton.Disable()

	// Create the Manga Dir button
	view.dirButton = widget.NewButton("Manga Dir", func() {
		view.onDirButtonClicked()
	})
	view.dirButton.Disable()

	// Create search components
	view.searchEntry = widget.NewEntry()
	view.searchEntry.SetPlaceHolder("Search manga titles...")
	view.searchEntry.OnSubmitted = func(text string) {
		view.performSearch()
	}

	view.searchButton = widget.NewButton("Search", func() {
		view.performSearch()
	})

	view.clearSearchButton = widget.NewButton("Clear Search", func() {
		view.clearSearch()
	})

	// Sort manga alphabetically by title for consistent display
	sort.Slice(view.state.MangaData.Manga, func(i, j int) bool {
		return view.state.MangaData.Manga[i].Title < view.state.MangaData.Manga[j].Title
	})

	// Create the list widget
	view.List = widget.NewList(
		// Length: Return the number of manga in our data
		func() int {
			return len(view.state.MangaData.Manga)
		},
		// CreateItem: Create a template label that will be reused
		func() fyne.CanvasObject {
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis
			return label
		},
		// UpdateItem: Fill in the label with actual manga data
		func(id widget.ListItemID, item fyne.CanvasObject) {
			label := item.(*widget.Label)
			label.SetText(view.state.MangaData.Manga[id].Title)
		},
	)

	// Set up the selection handler
	view.List.OnSelected = func(id widget.ListItemID) {
		view.selectedIndex = int(id)

		// Enable all buttons since something is now selected
		view.deleteButton.Enable()
		view.editButton.Enable()
		view.dirButton.Enable()

		// Notify the app state that selection changed
		view.state.SelectManga(int(id))
	}

	// Build the card content with search box on same line as header
	cardContent := container.NewBorder(
		// Top: Card title on left, search box on right, separator below
		container.NewVBox(
			container.NewBorder(
				nil,
				nil,
				NewBoldLabel("Manga List"),
				nil,
				view.searchEntry, // Search box fills remaining space on right
			),
			NewSeparator(),
		),
		// Bottom: All buttons in one row
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
		nil, // Left
		nil, // Right
		// Center: The scrollable list fills the remaining space
		view.List,
	)

	// Wrap the content in a card with white background
	view.Card = NewCard(cardContent)

	// Register callback to refresh list when manga are added
	view.state.RegisterMangaAddedCallback(func() {
		view.refresh()
	})

	// Register callback to refresh list when manga are deleted
	view.state.RegisterMangaDeletedCallback(func(id int) {
		view.refresh()
	})

	return view
}

// SetEditMangaView sets the reference to the edit manga view
// This allows the manga list to load manga data into the edit form
func (v *MangaListView) SetEditMangaView(editView *EditMangaView) {
	v.editMangaView = editView
}

// refresh updates the list to reflect current data.
// This is called automatically when manga are added or deleted.
func (v *MangaListView) refresh() {
	// Re-sort manga alphabetically
	sort.Slice(v.state.MangaData.Manga, func(i, j int) bool {
		return v.state.MangaData.Manga[i].Title < v.state.MangaData.Manga[j].Title
	})

	// Reset selection since indices may have changed
	v.selectedIndex = -1
	v.List.UnselectAll()
	v.deleteButton.Disable()
	v.editButton.Disable()
	v.dirButton.Disable()

	// Clear search results since indices have changed
	v.searchResults = []int{}
	v.currentSearchIdx = -1

	// Tell the list widget to refresh its display
	v.List.Refresh()
}

// onDeleteButtonClicked is called when the user clicks the Delete Manga button.
func (v *MangaListView) onDeleteButtonClicked() {
	// Validate that something is selected
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.state.MangaData.Manga) {
		dialog.ShowInformation(
			"Delete Manga",
			"Please select a manga to delete.",
			v.state.Window,
		)
		return
	}

	// Get the manga title for the confirmation dialog
	mangaTitle := v.state.MangaData.Manga[v.selectedIndex].Title

	// Show confirmation dialog
	dialog.ShowConfirm(
		"Delete Manga",
		"Are you sure you want to delete \""+mangaTitle+"\"?",
		func(confirmed bool) {
			if confirmed {
				// Delete the manga - this will save to disk and trigger callbacks
				v.state.DeleteManga(v.selectedIndex)
			}
		},
		v.state.Window,
	)
}

// onEditButtonClicked is called when the user clicks the Edit Manga button.
func (v *MangaListView) onEditButtonClicked() {
	// Validate that something is selected
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.state.MangaData.Manga) {
		dialog.ShowInformation(
			"Edit Manga",
			"Please select a manga to edit.",
			v.state.Window,
		)
		return
	}

	// Check if we have a reference to the edit manga view
	if v.editMangaView == nil {
		dialog.ShowError(
			fmt.Errorf("edit manga view not initialized"),
			v.state.Window,
		)
		return
	}

	// Load the selected manga into the edit form
	v.editMangaView.LoadMangaForEditing(v.selectedIndex)
}

// onDirButtonClicked is called when the user clicks the Manga Dir button.
// It opens the manga's directory in the system file manager.
func (v *MangaListView) onDirButtonClicked() {
	// Validate that something is selected
	if v.selectedIndex < 0 || v.selectedIndex >= len(v.state.MangaData.Manga) {
		dialog.ShowInformation(
			"Open Manga Directory",
			"Please select a manga to open its directory.",
			v.state.Window,
		)
		return
	}

	// Get the manga location (directory path)
	mangaLocation := v.state.MangaData.Manga[v.selectedIndex].Location

	// Open the directory using OS-specific command
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", mangaLocation).Start()
	case "darwin": // macOS
		err = exec.Command("open", mangaLocation).Start()
	case "windows":
		err = exec.Command("explorer", mangaLocation).Start()
	default:
		dialog.ShowError(
			fmt.Errorf("unsupported operating system: %s", runtime.GOOS),
			v.state.Window,
		)
		return
	}

	if err != nil {
		dialog.ShowError(
			fmt.Errorf("failed to open directory: %v", err),
			v.state.Window,
		)
	}
}

// performSearch searches for manga titles matching the search term
// If the search term hasn't changed, it cycles through existing results
// If the search term is new, it finds all matching manga
func (v *MangaListView) performSearch() {
	searchTerm := strings.TrimSpace(v.searchEntry.Text)

	// If search term is empty, do nothing
	if searchTerm == "" {
		dialog.ShowInformation(
			"Search",
			"Please enter a search term.",
			v.state.Window,
		)
		return
	}

	// Convert to lowercase for case-insensitive search
	searchTermLower := strings.ToLower(searchTerm)

	// Check if this is a new search or cycling through existing results
	if searchTerm != v.lastSearchTerm {
		// New search - find all matching manga
		v.searchResults = []int{}
		for i, manga := range v.state.MangaData.Manga {
			if strings.Contains(strings.ToLower(manga.Title), searchTermLower) {
				v.searchResults = append(v.searchResults, i)
			}
		}

		v.lastSearchTerm = searchTerm
		v.currentSearchIdx = -1

		// If no results found
		if len(v.searchResults) == 0 {
			dialog.ShowInformation(
				"Search",
				fmt.Sprintf("No manga found matching \"%s\".", searchTerm),
				v.state.Window,
			)
			return
		}
	}

	// Cycle to next result
	v.currentSearchIdx++
	if v.currentSearchIdx >= len(v.searchResults) {
		v.currentSearchIdx = 0 // Loop back to first result
	}

	// Select and scroll to the result
	resultIndex := v.searchResults[v.currentSearchIdx]
	v.List.Select(widget.ListItemID(resultIndex))
	v.List.ScrollTo(widget.ListItemID(resultIndex))

	// Update status if multiple results
	if len(v.searchResults) > 1 {
		// Could show a status message, but for now just select the next one
		// The user can see which one is selected in the list
	}
}

// clearSearch clears the search term and resets search state
func (v *MangaListView) clearSearch() {
	v.searchEntry.SetText("")
	v.searchResults = []int{}
	v.currentSearchIdx = -1
	v.lastSearchTerm = ""

	// Unselect current item
	v.List.UnselectAll()
	v.selectedIndex = -1
	v.deleteButton.Disable()
	v.editButton.Disable()
	v.dirButton.Disable()
}
