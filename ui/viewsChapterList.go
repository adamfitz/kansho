package ui

import (
	"fmt"
	"log"
	"sort"

	"kansho/parser"
	"kansho/sites"

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
	selectedMangaLabel *widget.Label       // Shows which manga is selected
	chapterList        *widget.List        // List of chapters
	contentContainer   *fyne.Container     // Container that holds the dynamic content
	updateButton       *widget.Button      // Button to update/refresh chapters
	progressBar        *widget.ProgressBar // Progress bar for downloads
	progressLabel      *widget.Label       // Label showing download status
	progressContainer  *fyne.Container     // Container for progress UI

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

	// Create progress bar and label (initially hidden)
	view.progressBar = widget.NewProgressBar()
	view.progressBar.Min = 0
	view.progressBar.Max = 1

	// Create progress label with truncation to prevent card expansion
	view.progressLabel = widget.NewLabel("")
	view.progressLabel.Truncation = fyne.TextTruncateEllipsis // Prevent long manga names from expanding the card
	view.progressLabel.Wrapping = fyne.TextWrapWord           // Wrap if needed

	view.progressContainer = container.NewVBox(
		view.progressLabel,
		view.progressBar,
	)
	view.progressContainer.Hide() // Hidden by default

	// Create the chapter list widget (initially empty)
	view.chapterList = widget.NewList(
		func() int {
			return len(view.chapters)
		},
		func() fyne.CanvasObject {
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis // Add truncation
			return label
		},
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
		// Bottom: Progress bar and Update button
		container.NewVBox(
			view.progressContainer, // Progress section (hidden by default)
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
	log.Printf("Loaded local chapters [%s]", manga.Title)
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
			label := widget.NewLabel("template")
			label.Truncation = fyne.TextTruncateEllipsis // Add truncation
			return label
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

func (v *ChapterListView) defaultChapterList() {
	v.chapters = []string{} // Clear chapters

	// Show "no chapters found" message
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel("No chapters found"),
	}
	v.contentContainer.Refresh()
}

// showNoChapters displays a message when no chapters are found.
func (v *ChapterListView) showNoChapters() {
	v.chapters = []string{} // Clear chapters

	// Show message
	v.contentContainer.Objects = []fyne.CanvasObject{
		widget.NewLabel("No chapters found for this manga"),
	}
	v.contentContainer.Refresh()
}

// onUpdateButtonClicked is called when the user clicks the Update Chapters button.
// This dispatches to the appropriate download function based on the manga's site.
func (v *ChapterListView) onUpdateButtonClicked() {
	// Get the currently selected manga
	manga := v.state.GetSelectedManga()
	if manga == nil {
		return
	}

	// Disable button during download (already on UI thread)
	v.updateButton.Disable()

	// Show progress bar and reset state
	v.progressContainer.Show()
	v.progressBar.SetValue(0)
	v.progressLabel.SetText("Starting download...")

	// Run download in a goroutine to prevent UI freezing
	go func() {
		var totalChapters int
		var err error

		// Dispatch to the correct download function based on manga's site
		switch manga.Site {
		case "mgeko":
			err = sites.MgekoDownloadChapters(manga, func(status string, progress float64, current, total int) {
				totalChapters = total
				// Update progress bar and label safely on main thread
				fyne.Do(func() {
					v.progressLabel.SetText(status)
					v.progressBar.SetValue(progress)
				})
			})

		case "xbato":
			err = sites.XbatoDownloadChapters(manga, func(status string, progress float64, current, total int) {
				totalChapters = total
				// Update progress bar and label safely on main thread
				fyne.Do(func() {
					v.progressLabel.SetText(status)
					v.progressBar.SetValue(progress)
				})
			})

		// Add more sites here as you implement them
		// case "asurascans":
		// 	err = sites.AsurascansDownloadChapters(manga, ...)
		// case "rizzfables":
		// 	err = sites.RizzfablesDownloadChapters(manga, ...)

		default:
			// Site not supported yet
			fyne.Do(func() {
				v.progressContainer.Hide()
				v.updateButton.Enable()
				err := fmt.Errorf("download not supported for site: %s", manga.Site)
				v.progressLabel.SetText(fmt.Sprintf("Error: %v", err))
				dialog.ShowError(err, v.state.Window)
			})
			return
		}

		// Back on main thread for UI updates after download
		fyne.Do(func() {
			// Hide progress bar and re-enable button
			v.progressContainer.Hide()
			v.updateButton.Enable()

			if err != nil {
				// Show inline error message
				v.progressLabel.SetText(fmt.Sprintf("Error: %v", err))

				// Also show a dialog for visibility
				dialog.ShowError(err, v.state.Window)
				return
			}

			// Success case
			if totalChapters > 0 {
				v.progressLabel.SetText("Download complete.")
				dialog.ShowInformation(
					"Download Complete",
					fmt.Sprintf("Successfully downloaded %d chapters for %s", totalChapters, manga.Title),
					v.state.Window,
				)
			}

			// Reload the chapter list after download
			v.onMangaSelected(v.state.SelectedMangaID)
		})
	}()
}
