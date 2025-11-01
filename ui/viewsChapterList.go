package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ChapterListView represents the chapter list card component.
// This view displays all chapters for the currently selected manga.
// When a manga is selected from the manga list, this view loads and displays
// all downloaded chapters from disk.
type ChapterListView struct {
	// Card is the complete UI component ready to be added to the layout
	Card fyne.CanvasObject

	// UI components
	selectedMangaLabel *widget.Label   // Shows which manga is selected
	chapterList        *widget.List    // List of chapters
	contentContainer   *fyne.Container // Container that holds the dynamic content

	// Data
	chapters []string // Store chapter names/paths

	// state is a reference to the shared application state
	state *KanshoAppState
}

// NewChapterListView creates a new chapter list view component.
// This view displays chapters for the selected manga and updates
// automatically when manga selection changes.
//
// Parameters:
//   - state: Pointer to the shared application state
//
// Returns:
//   - *ChapterListView: A new chapter list view with all components initialized
func NewChapterListView(state *KanshoAppState) *ChapterListView {
	view := &ChapterListView{
		state:    state,
		chapters: []string{}, // Start with empty chapter list
	}

	// Create the label showing which manga is selected
	view.selectedMangaLabel = widget.NewLabel("Select a manga to view chapters")

	// Create the chapter list widget (initially empty)
	view.chapterList = widget.NewList(
		// Length: Return the number of chapters
		func() int {
			return len(view.chapters)
		},
		// CreateItem: Create a template label
		func() fyne.CanvasObject {
			return widget.NewLabel("template")
		},
		// UpdateItem: Fill in the label with chapter data
		func(id widget.ListItemID, item fyne.CanvasObject) {
			label := item.(*widget.Label)
			label.SetText(view.chapters[id])
		},
	)

	// Create a container for dynamic content
	// Start with just the "select a manga" label
	view.contentContainer = container.NewVBox(
		view.selectedMangaLabel,
	)

	// Build the card content
	cardContent := container.NewBorder(
		// Top: Header
		container.NewVBox(
			NewBoldLabel("Chapter List"),
			NewSeparator(),
		),
		nil, // Bottom
		nil, // Left
		nil, // Right
		// Center: Dynamic content (will show list or status message)
		view.contentContainer,
	)

	// Wrap the content in a card
	view.Card = NewCard(cardContent)

	// Register callback for when manga selection changes
	// This is where your chapter loading code will run
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

// onMangaSelected is called when a manga is selected from the manga list.
// This is where you'll add your code to load chapters from disk.
//
// Parameters:
//   - id: The index of the selected manga in the MangaData.Manga slice
func (v *ChapterListView) onMangaSelected(id int) {
	// Get the selected manga
	manga := v.state.GetSelectedManga()
	if manga == nil {
		// No valid selection, show placeholder
		v.showNoSelection()
		return
	}

	// Update the label to show which manga is selected
	v.selectedMangaLabel.SetText("Chapters for: " + manga.Title)

	// TODO: YOUR CODE GOES HERE
	// Load chapters from disk using the manga's location
	// Example:
	// chapters := YourLoadChaptersFunction(manga.Location)
	// v.updateChapterList(chapters)

	// For now, show placeholder message
	v.showPlaceholder()

	// WHEN YOU IMPLEMENT YOUR CHAPTER LOADING:
	// 1. Remove the v.showPlaceholder() line above
	// 2. Call your function to get chapters from disk
	// 3. Call v.updateChapterList(chapters) with your loaded chapters
	// 4. Example:
	//
	//    loadedChapters := LoadChaptersFromDisk(manga.Location)
	//    v.updateChapterList(loadedChapters)
}

// updateChapterList updates the view with a new list of chapters.
// Call this method after loading chapters from disk.
//
// Parameters:
//   - chapters: Slice of chapter names/paths to display
func (v *ChapterListView) updateChapterList(chapters []string) {
	// Store the chapters
	v.chapters = chapters

	if len(chapters) == 0 {
		// No chapters found
		v.showNoChapters()
		return
	}

	// Replace the content container to show the chapter list
	v.contentContainer.Objects = []fyne.CanvasObject{
		v.selectedMangaLabel,
		NewSeparator(),
		v.chapterList,
	}
	v.contentContainer.Refresh()

	// Refresh the list to display new data
	v.chapterList.Refresh()
}

// showNoSelection displays a message when no manga is selected.
func (v *ChapterListView) showNoSelection() {
	v.selectedMangaLabel.SetText("Select a manga to view chapters")
	v.chapters = []string{} // Clear chapters

	// Show just the label
	v.contentContainer.Objects = []fyne.CanvasObject{
		v.selectedMangaLabel,
	}
	v.contentContainer.Refresh()
}

// showPlaceholder shows a placeholder message for "coming soon" functionality.
func (v *ChapterListView) showPlaceholder() {
	v.chapters = []string{} // Clear chapters

	// Show label and placeholder message
	v.contentContainer.Objects = []fyne.CanvasObject{
		v.selectedMangaLabel,
		NewSeparator(),
		widget.NewLabel("Chapter list functionality coming soon..."),
	}
	v.contentContainer.Refresh()
}

// showNoChapters displays a message when no chapters are found.
func (v *ChapterListView) showNoChapters() {
	v.chapters = []string{} // Clear chapters

	// Show label and "no chapters" message
	v.contentContainer.Objects = []fyne.CanvasObject{
		v.selectedMangaLabel,
		NewSeparator(),
		widget.NewLabel("No chapters found for this manga"),
	}
	v.contentContainer.Refresh()
}
