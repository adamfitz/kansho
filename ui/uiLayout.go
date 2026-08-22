package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// BuildMainLayout constructs the complete application UI layout.
// This is the main entry point for creating the user interface.
// It assembles all components (header, footer, cards) into a cohesive layout
// with a gradient background.
//
// The layout structure is:
//   - Background: Purple gradient (45° angle)
//   - Header: Application title and subtitle (top) with the download queue
//     summary button on the right
//   - Content Area: Two-column layout
//   - Left Column: Manga list (fills the column; Edit Manga form unfolds on demand)
//   - Right Column: Chapter list (100%)
//
// - Status bar: Selected manga's download state (bottom)
//
// Parameters:
//   - window: The main application window (needed for dialogs and state)
//
// Returns:
//   - fyne.CanvasObject: The complete UI layout ready to be set as window content
func BuildMainLayout(window fyne.Window) fyne.CanvasObject {
	// Initialize the application state
	// This centralized state allows all UI components to communicate
	state := NewKanshoAppState(window)

	// Create the gradient background
	// This creates a smooth transition from light purple to dark purple
	gradient := canvas.NewLinearGradient(
		GradientStartColor, // Light purple (top-left)
		GradientEndColor,   // Dark purple (bottom-right)
		GradientAngle,      // 45 degree angle
	)

	// Download queue summary button (top-right of the header)
	// Shows overall progress across all chapters in the download queue
	downloadQueueButton := NewDownloadQueueButton(state)

	// Create the header component
	// Shows application title "鑑賞 kansho" and subtitle, with the download
	// queue summary button on the right
	header := NewHeader(downloadQueueButton.Card)

	// Create the three main view components
	// Each view is self-contained and manages its own state through callbacks

	// Manga List View (top-left card)
	// Displays all manga bookmarks in a scrollable list
	mangaListView := NewMangaListView(state)

	// Edit Manga View (bottom-left card)
	// Form for adding new manga to the library or editing existing ones
	editMangaView := NewEditMangaView(state)

	// Connect the manga list view to the edit manga view
	// This allows the "Edit Manga" button to load data into the form
	mangaListView.SetEditMangaView(editMangaView)

	// Chapter List View (right card)
	// Displays chapters for the currently selected manga with per-chapter
	// progress bars and download controls
	chapterListView := NewChapterListView(state, downloadQueueButton)

	// Status bar (bottom of the window, above the footer)
	// Mirrors the download queue page's status bar: it shows the selected
	// manga's download site and downloaded chapter count; after a chapter list
	// refresh it also shows how many chapters are not downloaded. The chapter
	// list view keeps it up to date.
	statusBar := NewMainStatusBar()
	chapterListView.SetStatusBar(statusBar)

	// Assemble the left column
	// The manga list fills the whole column by default; the edit manga form
	// stays folded until the user opens it via "Add Manga" or "Edit Manga".
	// Using a Border layout with the edit form as the bottom border, Fyne skips
	// hidden border children, so the list takes the full column height while
	// the edit panel is folded.
	editPanel := container.NewStack(editMangaView.Card)
	editPanel.Hide()

	leftColumn := container.NewBorder(
		nil,
		editPanel,
		nil,
		nil,
		container.NewStack(mangaListView.Card), // Fills full column while folded
	)

	// Track whether the edit panel is currently shown so the "Add Manga" /
	// "Collapse" header button can act as a toggle. Fyne does not re-layout a
	// container when a child's visibility changes, so refresh the border
	// explicitly to expand/shrink the list accordingly.
	editVisible := false
	setEditVisible := func(visible bool) {
		editVisible = visible
		if visible {
			editPanel.Show()
			mangaListView.SetAddButtonText("Collapse")
		} else {
			editPanel.Hide()
			mangaListView.SetAddButtonText("Add Manga")
		}
		leftColumn.Refresh()
	}

	// The header button toggles the panel: opening it resets the form to add
	// mode, closing it folds the panel back down.
	mangaListView.SetToggleEditHandler(func() {
		if editVisible {
			setEditVisible(false)
		} else {
			editMangaView.PrepareForNewManga()
			setEditVisible(true)
		}
	})

	// "Edit Manga" in the list always opens the panel with the selection.
	mangaListView.SetUnfoldEditHandler(func() {
		setEditVisible(true)
	})

	// Assemble the main content area
	// This is a two-column layout with equal widths (50% each)
	// Left column contains manga list and edit form
	// Right column contains chapter list
	contentArea := container.NewGridWithColumns(2,
		container.NewPadded(leftColumn),           // Left 50%
		container.NewPadded(chapterListView.Card), // Right 50%
	)

	// Assemble the main layout using border container
	// Border container places items at edges (top, bottom, left, right)
	// and fills the center with remaining space
	// The layout mirrors the download queue page: header on top, status bar at
	// the bottom, and no footer.
	mainLayout := container.NewBorder(
		container.NewPadded(header),        // Top: Header with padding
		container.NewPadded(statusBar.Bar), // Bottom: Status bar with padding
		nil,                                // Left: None
		nil,                                // Right: None
		contentArea,                        // Center: Main content fills remaining space
	)

	// Stack the gradient behind all content
	// container.NewStack layers objects on the z-axis
	// First object (gradient) is at the back, second (mainLayout) is in front
	content := container.NewStack(gradient, mainLayout)

	return content
}
