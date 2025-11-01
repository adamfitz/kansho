package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// NewFooter creates the application footer with attribution text.
// The footer displays a simple message about the technologies used to build the app.
//
// Returns:
//   - fyne.CanvasObject: A centered text label for the footer
func NewFooter() fyne.CanvasObject {
	footerText := canvas.NewText(" ", TextColorLight)
	footerText.TextSize = FooterTextSize
	footerText.Alignment = fyne.TextAlignCenter

	return footerText
}
