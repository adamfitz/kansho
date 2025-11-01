package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// NewHeader creates the application header with title and subtitle.
// The header displays the Japanese name "鑑賞" (kansho) meaning "appreciation/viewing"
// along with a subtitle describing the application.
//
// The header includes:
// - Large, bold title with Japanese characters and romanization
// - Smaller subtitle with application description
// - Spacer at the bottom for layout purposes
//
// Returns:
//   - fyne.CanvasObject: A container with the formatted header content
func NewHeader() fyne.CanvasObject {
	// Create the main title text with Japanese characters and romanization
	// "鑑賞" means "appreciation" or "viewing" in Japanese
	titleText := canvas.NewText("鑑賞 kansho", TextColorLight)
	titleText.TextSize = TitleTextSize               // Large font (48pt)
	titleText.TextStyle = fyne.TextStyle{Bold: true} // Bold for emphasis
	titleText.Alignment = fyne.TextAlignCenter       // Centered

	// Create the subtitle describing the application
	subtitleText := canvas.NewText(
		"Built with Go and fyne",
		TextColorLight,
	)
	subtitleText.TextSize = SubtitleTextSize      // Smaller font (16pt)
	subtitleText.Alignment = fyne.TextAlignCenter // Centered to match title

	// Combine title and subtitle in a vertical box with spacing
	// layout.NewSpacer() adds flexible space that pushes content apart
	header := container.NewVBox(
		titleText,
		subtitleText,
		layout.NewSpacer(), // Add space below header to separate from content
	)

	return header
}
