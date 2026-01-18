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
// - Background: Purple gradient (45° angle)
// - Header: Application title and subtitle (top)
// - Content Area: Two-column layout
//   - Left Column: Manga list (top 50%) and Edit Manga form (bottom 50%)
//   - Right Column: Chapter list (100%)
//
// - Footer: Attribution text (bottom)
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

	// Create the header component
	// Shows application title "鑑賞 kansho" and subtitle
	header := NewHeader()

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
	// Displays chapters for the currently selected manga
	chapterListView := NewChapterListView(state)

	// Assemble the left column
	// This contains the manga list (top) and edit manga form (bottom)
	// Using NewGridWithRows(2, ...) gives each card 50% of the vertical space
	leftColumn := container.NewGridWithRows(2,
		container.NewStack(mangaListView.Card), // Top 50%
		container.NewStack(editMangaView.Card), // Bottom 50%
	)

	// Assemble the main content area
	// This is a two-column layout with equal widths (50% each)
	// Left column contains manga list and edit form
	// Right column contains chapter list
	contentArea := container.NewGridWithColumns(2,
		container.NewPadded(leftColumn),           // Left 50%
		container.NewPadded(chapterListView.Card), // Right 50%
	)

	// Create the footer component
	// Shows attribution text
	footer := NewFooter()

	// Assemble the main layout using border container
	// Border container places items at edges (top, bottom, left, right)
	// and fills the center with remaining space
	mainLayout := container.NewBorder(
		container.NewPadded(header), // Top: Header with padding
		container.NewPadded(footer), // Bottom: Footer with padding
		nil,                         // Left: None
		nil,                         // Right: None
		contentArea,                 // Center: Main content fills remaining space
	)

	// Stack the gradient behind all content
	// container.NewStack layers objects on the z-axis
	// First object (gradient) is at the back, second (mainLayout) is in front
	content := container.NewStack(gradient, mainLayout)

	return content
}
