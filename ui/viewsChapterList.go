package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// ChapterListView represents the chapter list card component.
// This view displays chapters for the currently selected manga.
// When no manga is selected, it shows a placeholder message.
//
// The view automatically updates when:
// - A manga is selected from the manga list
// - The selected manga's chapters change
// - The selected manga is deleted
type ChapterListView struct {
	// Card is the complete UI component ready to be added to the layout
	Card fyne.CanvasObject

	// contentContainer holds the dynamic content that changes based on selection
	// This could be a chapter list, a "no selection" message, or a loading indicator
	contentContainer *fyne.Container

	// state is a reference to the shared application state
	state *KanshoAppState
}

// NewChapterListView creates a new chapter list view component.
// The view starts empty and updates when a manga is selected.
//
// Parameters:
//   - state: Pointer to the shared application state
//
// Returns:
//   - *ChapterListView: A new chapter list view with all components initialized
//
// The view registers a callback to update when manga selection changes.
func NewChapterListView(state *KanshoAppState) *ChapterListView {
	view := &ChapterListView{
		state: state,
	}

	// Create a container that will hold our dynamic content
	// Start with the "no selection" message
	view.contentContainer = container.NewVBox(
		layout.NewSpacer(),
		widget.NewLabel("Select a manga to view chapters"),
		layout.NewSpacer(),
	)

	// Build the card content with header and dynamic content area
	cardContent := container.NewBorder(
		// Top: Card title and separator
		container.NewVBox(
			NewBoldLabel("Chapter List"),
			NewSeparator(),
		),
		nil, // Bottom
		nil, // Left
		nil, // Right
		// Center: Dynamic content container
		view.contentContainer,
	)

	// Wrap the content in a card
	view.Card = NewCard(cardContent)

	// Register callback to update when manga selection changes
	view.state.RegisterMangaSelectedCallback(func(id int) {
		view.onMangaSelected(id)
	})

	// Register callback to clear view when manga is deleted
	view.state.RegisterMangaDeletedCallback(func(id int) {
		// If the deleted manga was selected, clear the view
		if view.state.SelectedMangaID == -1 {
			view.showNoSelection()
		}
	})

	return view
}

// onMangaSelected is called when the user selects a manga from the list.
// This updates the chapter list to show chapters for the selected manga.
//
// Parameters:
//   - id: The index of the selected manga in the manga list
func (v *ChapterListView) onMangaSelected(id int) {
	// Get the selected manga from the state
	manga := v.state.GetSelectedManga()
	if manga == nil {
		// No valid selection, show placeholder
		v.showNoSelection()
		return
	}

	// TODO: Load chapters for the selected manga
	// For now, we'll show a placeholder message with the manga title
	// In the future, this will:
	// 1. Load chapters from the manga data
	// 2. Display them in a scrollable list
	// 3. Allow marking chapters as read/unread
	// 4. Allow opening chapters in a browser or reader

	v.showChaptersPlaceholder(manga.Title)
}

// showNoSelection displays a message when no manga is selected.
func (v *ChapterListView) showNoSelection() {
	// Clear the container
	v.contentContainer.Objects = nil

	// Add centered placeholder message
	v.contentContainer.Objects = []fyne.CanvasObject{
		layout.NewSpacer(),
		widget.NewLabel("Select a manga to view chapters"),
		layout.NewSpacer(),
	}

	// Refresh the container to update the display
	v.contentContainer.Refresh()
}

// showChaptersPlaceholder shows a placeholder for chapter list functionality.
// This will be replaced with actual chapter loading in the future.
//
// Parameters:
//   - mangaTitle: The title of the selected manga
func (v *ChapterListView) showChaptersPlaceholder(mangaTitle string) {
	// Clear the container
	v.contentContainer.Objects = nil

	// Add placeholder content
	// In the future, this will be replaced with an actual chapter list
	v.contentContainer.Objects = []fyne.CanvasObject{
		layout.NewSpacer(),
		widget.NewLabel("Chapters for: " + mangaTitle),
		widget.NewLabel("Chapter list functionality coming soon..."),
		layout.NewSpacer(),
	}

	// Refresh the container to update the display
	v.contentContainer.Refresh()
}

// showChapterList displays the actual chapter list for a manga.
// This is a stub for future implementation.
//
// Future implementation will:
// - Load chapters from manga data structure
// - Display in a scrollable list with chapter numbers/titles
// - Show read/unread status
// - Allow opening chapters
// - Allow marking as read/unread
func (v *ChapterListView) showChapterList() {
	// TODO: Implement actual chapter list
	// This will be similar to MangaListView but for chapters
	// Will need:
	// - widget.NewList for chapters
	// - Chapter data structure in bookmarks.Manga
	// - Read/unread tracking
	// - Open chapter functionality
}
