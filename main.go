package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"image/color"
)

// Bookmark represents a manga bookmark with a title and URL
// This struct stores the essential information for tracking manga
type Bookmark struct {
	Title string // The display name of the manga
	URL   string // The web address where the manga can be accessed
}

// LoadBookmarks returns a slice of mock bookmark data
// In a real application, this would load from a database or file
func LoadBookmarks() []Bookmark {
	return []Bookmark{
		{Title: "Job Change Log", URL: "https://example.com/job"},
		{Title: "Absolute dominion", URL: "https://example.com/absolute"},
	}
}

// createCard wraps content in a white card-like container
// This creates a visual card effect with a white background and padding
// Parameters:
//   - content: The fyne.CanvasObject to be displayed inside the card
// Returns:
//   - A fyne.CanvasObject that looks like a card with white background
func createCard(content fyne.CanvasObject) fyne.CanvasObject {
	// Create a white rectangle as the card background
	bg := canvas.NewRectangle(color.RGBA{R: 255, G: 255, B: 255, A: 255})
	// Set minimum size to ensure the card is visible
	bg.SetMinSize(fyne.NewSize(100, 100))
	
	// Stack the background behind the padded content
	// container.NewStack layers objects on top of each other
	// container.NewPadded adds padding around the content
	return container.NewStack(bg, container.NewPadded(content))
}

func main() {
	// Create a new Fyne application instance
	myApp := app.New()
	
	// Create the main window with title "kansho"
	myWindow := myApp.NewWindow("kansho")
	
	// Set the initial window size to 1200x800 pixels
	myWindow.Resize(fyne.NewSize(1200, 800))

	// Create a linear gradient background transitioning from purple to darker purple
	// The gradient angle is 45 degrees
	gradient := canvas.NewLinearGradient(
		color.RGBA{R: 115, G: 103, B: 240, A: 255}, // Light purple
		color.RGBA{R: 136, G: 84, B: 208, A: 255},  // Darker purple
		45, // Angle in degrees
	)

	// === HEADER SECTION ===
	// Create the main title text with Japanese characters and romanization
	titleText := canvas.NewText("鑑賞 kansho", color.White)
	titleText.TextSize = 48 // Large font size for prominence
	titleText.TextStyle = fyne.TextStyle{Bold: true} // Bold text
	titleText.Alignment = fyne.TextAlignCenter // Center alignment

	// Create the subtitle describing the application
	subtitleText := canvas.NewText("A cross-platform desktop application built with Go and fyne", color.White)
	subtitleText.TextSize = 16 // Smaller font for subtitle
	subtitleText.Alignment = fyne.TextAlignCenter // Center alignment

	// Combine title and subtitle in a vertical box with spacing
	header := container.NewVBox(
		titleText,
		subtitleText,
		layout.NewSpacer(), // Add space below header
	)

	// === MANGA LIST CARD ===
	// Create a vertical box to hold the list of manga bookmarks
	mangaListBox := container.NewVBox()
	
	// Populate the manga list with bookmarks loaded from LoadBookmarks()
	for _, b := range LoadBookmarks() {
		// Add each bookmark title as a label to the list
		mangaListBox.Add(widget.NewLabel(b.Title))
	}
	
	// Create the content for the Manga List card
	mangaListContent := container.NewVBox(
		// Card title in bold
		widget.NewLabelWithStyle("Manga List", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(), // Horizontal line separator
		mangaListBox, // The actual list of manga
	)
	// Wrap the content in a white card
	mangaCard := createCard(mangaListContent)

	// === ADD MANGA URL CARD ===
	// Create a text entry field for users to paste manga URLs
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("Paste manga URL") // Placeholder text when empty
	
	// Create a button that adds the URL when clicked
	addButton := widget.NewButton("ADD URL", func() {
		// Get the text from the URL entry field
		url := urlEntry.Text
		
		// Validate that the URL is not empty
		if url == "" {
			// Show an information dialog if URL is empty
			dialog.ShowInformation("Add Manga", "Please provide Manga URL.", myWindow)
			return
		}
		
		// Show success dialog with the added URL
		dialog.ShowInformation("Success", fmt.Sprintf("Manga URL added successfully!\n\n%s", url), myWindow)
		
		// Clear the entry field after adding
		urlEntry.SetText("")
		
		// TODO: In the future, this will actually save the manga to a database/file
		// and refresh the manga list to show the new entry
	})

	// Create the content for the Add Manga URL card
	urlContent := container.NewVBox(
		// Card title in bold
		widget.NewLabelWithStyle("Add Manga URL", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(), // Horizontal line separator
		layout.NewSpacer(), // Push content down
		widget.NewLabel("Enter URL:"), // Label for the input field
		urlEntry, // The text entry widget
		addButton, // The add button
	)
	// Wrap the content in a white card
	urlCard := createCard(urlContent)

	// === CHAPTER LIST CARD ===
	// Create the content for the Chapter List card (currently empty)
	chapterListContent := container.NewVBox(
		// Card title in bold
		widget.NewLabelWithStyle("Chapter List", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewSeparator(), // Horizontal line separator
		layout.NewSpacer(), // Fill remaining space
	)
	// Wrap the content in a white card
	chapterCard := createCard(chapterListContent)

	// === LAYOUT ASSEMBLY ===
	// TO CHANGE THE VERTICAL SIZE DISTRIBUTION OF THE LEFT CARDS:
	// The current NewVBox sizes cards based on content. 
	// Replace the leftColumn line below with one of these options:

	// CURRENT - Content-based sizing (Manga List gets minimal space, Add URL gets more)
	//leftColumn := container.NewVBox(
	//	mangaCard, // Top card - sizes to content
	//	urlCard,   // Bottom card - sizes to content
	//)
	
	// OPTION 1 - Equal 50/50 split (uncomment to use)
	//leftColumn := container.NewGridWithRows(2, mangaCard, urlCard)
	
	// OPTION 2 - User-adjustable split with draggable divider (uncomment to use)
	// leftColumn := container.NewVSplit(mangaCard, urlCard)
	// leftColumn.SetOffset(0.5) // Start at 50/50

	// OPTION 3 - Make both cards expand to fill space equally
	leftColumn := container.NewGridWithRows(2, 
	     container.NewMax(mangaCard),  // 50% 
	     container.NewMax(urlCard))     // 50%

	// === MAIN CONTENT AREA ===
	// TO CHANGE THE HORIZONTAL WIDTH DISTRIBUTION (left cards vs chapter list):
	// The current NewBorder layout doesn't give equal widths.
	// Replace the contentArea line below with one of these options:

	// CURRENT - Left column takes minimum space, chapter card fills the rest
	//contentArea := container.NewBorder(
	//	nil, nil, // No top or bottom borders
	//	container.NewPadded(leftColumn), // Left side with padding
	//	nil, // No right border
	//	container.NewPadded(chapterCard), // Center fills with chapter card
	//)
	
	// OPTION 1 - Equal 50/50 width split (uncomment to use)
	contentArea := container.NewGridWithColumns(2, 
	     container.NewPadded(leftColumn),
	     container.NewPadded(chapterCard))
	
	// OPTION 2 - User-adjustable horizontal split with draggable divider (uncomment to use)
	// contentArea := container.NewHSplit(
	//     container.NewPadded(leftColumn),
	//     container.NewPadded(chapterCard))
	// contentArea.SetOffset(0.5) // Start at 50/50, user can drag to adjust

	// === FOOTER SECTION ===
	// Create footer text with heart emoji
	footerText := canvas.NewText("Built with ❤️ using fyne and Go", color.White)
	footerText.TextSize = 14 // Small font for footer
	footerText.Alignment = fyne.TextAlignCenter // Center alignment

	// === MAIN LAYOUT ===
	// Create the main layout using a border container
	// Header at top, footer at bottom, content in center
	mainLayout := container.NewBorder(
		container.NewPadded(header), // Top: Header with padding
		container.NewPadded(footerText), // Bottom: Footer with padding
		nil, nil, // No left or right borders
		contentArea, // Center: Main content area
	)

	// Stack the gradient background behind all content
	// container.NewStack layers objects, so gradient is behind mainLayout
	content := container.NewStack(gradient, mainLayout)

	// Set the window content to our complete layout
	myWindow.SetContent(content)
	
	// Show the window and start the application event loop
	// This blocks until the window is closed
	myWindow.ShowAndRun()
}