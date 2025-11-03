package ui

import (
	"log"
	"sort"

	"kansho/parser"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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
	updateButton       *widget.Button  // Button to update/refresh chapters

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

	// Create the Update Chapters button
	view.updateButton = widget.NewButton("Update Chapters", func() {
		view.onUpdateButtonClicked()
	})
	// Initially disable the update button since no manga is selected
	view.updateButton.Disable()

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

	// IMPORTANT: Use NewStack so content can be fully replaced and expanded
	view.contentContainer = container.NewStack(
		widget.NewLabel("Select a manga to view chapters"),
	)

	// Build the card content
	cardContent := container.NewBorder(
		// Top: Header
		container.NewVBox(
			NewBoldLabel("Chapter List"),
			NewSeparator(),
		),
		// Bottom: Update button
		container.NewVBox(
			NewSeparator(),
			container.NewCenter(view.updateButton),
		),
		nil, // Left
		nil, // Right
		// Center: The content container (will expand to fill space)
		view.contentContainer,
	)

	// Wrap the content in a card
	view.Card = NewCard(cardContent)

	// Register callback for when manga selection changes
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

	log.Printf("loading local chapters for: %s", manga.Title)

	// Enable the update button since a manga is now selected
	v.updateButton.Enable()

	// Check if manga location is valid
	if manga.Location == "" {
		v.defaultChapterList()
		return
	}

	// Load chapters from disk using the manga's location
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		// Handle error - maybe the directory doesn't exist yet
		dialog.ShowError(err, v.state.Window)
		v.defaultChapterList() // Show placeholder if loading failed
		return
	}

	// Check if any chapters were found
	if len(downloadedChapters) == 0 {
		// No chapters downloaded yet
		v.defaultChapterList()
		return
	}

	// Sort chapters alphabetically (optional but recommended)
	sort.Strings(downloadedChapters)

	// Update the chapter list with the downloaded chapters
	v.updateChapterList(downloadedChapters)
	log.Printf("finished loading %s local chapters", manga.Title)
}

// updateChapterList updates the view with a new list of chapters.
// Call this method after loading chapters from disk.
//
// Parameters:
//   - chapters: Slice of chapter names to display
func (v *ChapterListView) updateChapterList(chapters []string) {
	// Store the chapters
	v.chapters = chapters

	if len(chapters) == 0 {
		// No chapters found
		v.defaultChapterList()
		return
	}

	// Recreate the chapter list widget completely with new data
	v.chapterList = widget.NewList(
		func() int {
			return len(v.chapters)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("template")
		},
		func(id widget.ListItemID, item fyne.CanvasObject) {
			label := item.(*widget.Label)
			if id < len(v.chapters) {
				label.SetText(v.chapters[id])
			}
		},
	)

	// Replace the entire content container with just the list
	v.contentContainer.Objects = []fyne.CanvasObject{v.chapterList}
	v.contentContainer.Refresh()
}

// showNoSelection displays a message when no manga is selected.
func (v *ChapterListView) showNoSelection() {
	v.chapters = []string{} // Clear chapters

	// Disable the update button since no manga is selected
	v.updateButton.Disable()

	// Show placeholder message
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel("Select a manga to view chapters"),
	}
	v.contentContainer.Refresh()
}

// default message when no local chaptes aer found
func (v *ChapterListView) defaultChapterList() {
	v.chapters = []string{} // Clear chapters

	// Show "no chapters found" message
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel("No chapters found"),
	}
	v.contentContainer.Refresh()
}

// Called when the user clicks the Update Chapters button.
// This rstarts the check for new chapter of the existing manag entry
func (v *ChapterListView) onUpdateButtonClicked() {
	// Get the currently selected manga
	manga := v.state.GetSelectedManga()
	if manga == nil {
		// No manga selected (shouldn't happen since button is disabled)
		return
	}

	// For now, just refresh the placeholder
	v.defaultChapterList()
}
