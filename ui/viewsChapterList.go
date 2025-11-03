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

	// IMPORTANT: Use NewBorder instead of NewVBox so the list can expand
	view.contentContainer = container.NewBorder(
		// Top: The selected manga label
		container.NewVBox(
			view.selectedMangaLabel,
			NewSeparator(),
		),
		nil, // Bottom
		nil, // Left
		nil, // Right
		// Center: Empty for now, will be populated with list or messages
		widget.NewLabel(""),
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

	// Update the label to show which manga is selected
	v.selectedMangaLabel.SetText("Chapters for: " + manga.Title)
	log.Printf("loading local chapters for: %s", manga.Title)

	// Enable the update button since a manga is now selected
	v.updateButton.Enable()

	// Check if manga location is valid
	if manga.Location == "" {
		v.showPlaceholder()
		return
	}

	// Load chapters from disk using the manga's location
	downloadedChapters, err := parser.LocalChapterList(manga.Location)
	if err != nil {
		// Handle error - maybe the directory doesn't exist yet
		dialog.ShowError(err, v.state.Window)
		v.showPlaceholder() // Show placeholder if loading failed
		return
	}

	// Check if any chapters were found
	if len(downloadedChapters) == 0 {
		// No chapters downloaded yet
		v.showPlaceholder()
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
		v.showNoChapters()
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

	// Rebuild the content container with Border layout
	newContent := container.NewBorder(
		// Top: manga label and separator
		container.NewVBox(
			v.selectedMangaLabel,
			NewSeparator(),
		),
		nil, // Bottom
		nil, // Left
		nil, // Right
		// Center: The list (will expand to fill)
		v.chapterList,
	)

	// Replace the entire content container
	v.contentContainer.Objects = []fyne.CanvasObject{newContent}
	v.contentContainer.Refresh()
}

// showNoSelection displays a message when no manga is selected.
func (v *ChapterListView) showNoSelection() {
	v.selectedMangaLabel.SetText("Select a manga to view chapters")
	v.chapters = []string{} // Clear chapters

	// Disable the update button since no manga is selected
	v.updateButton.Disable()

	// Rebuild with just the label
	newContent := container.NewBorder(
		container.NewVBox(
			v.selectedMangaLabel,
		),
		nil, nil, nil,
		widget.NewLabel(""),
	)

	v.contentContainer.Objects = []fyne.CanvasObject{newContent}
	v.contentContainer.Refresh()
}

func (v *ChapterListView) showPlaceholder() {
	v.chapters = []string{} // Clear chapters

	// Rebuild with label and placeholder
	newContent := container.NewBorder(
		container.NewVBox(
			v.selectedMangaLabel,
			NewSeparator(),
		),
		nil, nil, nil,
		widget.NewLabel("Chapter list functionality coming soon..."),
	)

	v.contentContainer.Objects = []fyne.CanvasObject{newContent}
	v.contentContainer.Refresh()
}

// showNoChapters displays a message when no chapters are found.
func (v *ChapterListView) showNoChapters() {
	v.chapters = []string{} // Clear chapters

	// Rebuild with label and "no chapters" message
	newContent := container.NewBorder(
		container.NewVBox(
			v.selectedMangaLabel,
			NewSeparator(),
		),
		nil, nil, nil,
		widget.NewLabel("No chapters found for this manga"),
	)

	v.contentContainer.Objects = []fyne.CanvasObject{newContent}
	v.contentContainer.Refresh()
}

// onUpdateButtonClicked is called when the user clicks the Update Chapters button.
// This reloads the chapter list from disk for the currently selected manga.
func (v *ChapterListView) onUpdateButtonClicked() {
	// Get the currently selected manga
	manga := v.state.GetSelectedManga()
	if manga == nil {
		// No manga selected (shouldn't happen since button is disabled)
		return
	}

	// TODO: YOUR UPDATE CODE GOES HERE
	// Reload chapters from disk
	// Example:
	// chapters := YourLoadChaptersFunction(manga.Location)
	// v.updateChapterList(chapters)

	// For now, just refresh the placeholder
	v.showPlaceholder()

	// WHEN YOU IMPLEMENT YOUR CHAPTER LOADING:
	// Replace the v.showPlaceholder() line above with:
	//    loadedChapters := LoadChaptersFromDisk(manga.Location)
	//    v.updateChapterList(loadedChapters)
}
