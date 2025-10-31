package ui

import (
	"sort"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	//"fyne.io/fyne/v2/layout"
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
	List *widget.List

	// state is a reference to the shared application state
	// This allows the view to access manga data and notify of selection changes
	state *KanshoAppState
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
		state: state,
	}

	// Sort manga alphabetically by title for consistent display
	// This makes it easier for users to find specific manga
	sort.Slice(view.state.MangaData.Manga, func(i, j int) bool {
		return view.state.MangaData.Manga[i].Title < view.state.MangaData.Manga[j].Title
	})

	// Create the list widget
	// widget.NewList uses three callbacks:
	// 1. Length function - tells the list how many items exist
	// 2. CreateItem function - creates a template for list items (called once)
	// 3. UpdateItem function - populates each item with actual data (called for each visible item)
	view.List = widget.NewList(
		// Length: Return the number of manga in our data
		func() int {
			return len(view.state.MangaData.Manga)
		},
		// CreateItem: Create a template label that will be reused
		// This is called once to create the item template
		func() fyne.CanvasObject {
			return widget.NewLabel("template")
		},
		// UpdateItem: Fill in the label with actual manga data
		// This is called for each visible list item
		// id is the index in the manga array
		func(id widget.ListItemID, item fyne.CanvasObject) {
			// Cast the generic CanvasObject back to a Label
			label := item.(*widget.Label)
			// Set the label text to the manga title
			label.SetText(view.state.MangaData.Manga[id].Title)
		},
	)

	// Set up the selection handler
	// This is called when a user clicks on a list item
	view.List.OnSelected = func(id widget.ListItemID) {
		// Notify the app state that selection changed
		// This will trigger callbacks in other views (like the chapter list)
		view.state.SelectManga(int(id))
	}

	// Build the card content with header and list
	cardContent := container.NewBorder(
		// Top: Card title and separator
		container.NewVBox(
			NewBoldLabel("Manga List"),
			NewSeparator(),
		),
		nil, // Bottom
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

// refresh updates the list to reflect current data.
// This is called automatically when manga are added or deleted.
// It re-sorts the manga and tells the list widget to redraw.
func (v *MangaListView) refresh() {
	// Re-sort manga alphabetically
	sort.Slice(v.state.MangaData.Manga, func(i, j int) bool {
		return v.state.MangaData.Manga[i].Title < v.state.MangaData.Manga[j].Title
	})

	// Tell the list widget to refresh its display
	// This causes UpdateItem to be called again for all visible items
	v.List.Refresh()
}
