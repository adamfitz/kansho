package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// NewHeader creates the application header with title and subtitle.
// The header displays the Japanese name "鑑賞" (kansho) meaning "appreciation/viewing"
// along with a subtitle describing the application.
//
// The header includes:
// - Large, bold title with Japanese characters and romanization
// - Smaller subtitle with application description
// - An optional right-side object (e.g. the download queue summary button)
//
// Parameters:
//   - rightObject: An optional fyne.CanvasObject placed on the right of the
//     header. Pass nil to render the header without a right-side object.
//
// Returns:
//   - fyne.CanvasObject: A container with the formatted header content
func NewHeader(rightObject fyne.CanvasObject) fyne.CanvasObject {
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
	titleAndSubtitle := container.NewVBox(
		titleText,
		subtitleText,
	)

	var right fyne.CanvasObject
	if rightObject != nil {
		right = container.NewPadded(rightObject)
	}

	// Border layout places the optional right object at the right edge while
	// keeping the title/subtitle centered in the remaining space
	header := container.NewBorder(
		nil,
		nil,
		nil,
		right,
		titleAndSubtitle,
	)

	return header
}
