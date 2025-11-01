package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// NewBoldLabel creates a label with bold text styling.
// This is a convenience function to avoid repeating the style configuration.
//
// Parameters:
//   - text: The text to display in the label
//
// Returns:
//   - *widget.Label: A label widget with bold styling
func NewBoldLabel(text string) *widget.Label {
	return widget.NewLabelWithStyle(
		text,
		fyne.TextAlignLeading, // Left-aligned text
		fyne.TextStyle{Bold: true},
	)
}

// NewSeparator creates a horizontal separator line.
// This is just a thin wrapper around widget.NewSeparator for consistency.
//
// Returns:
//   - *widget.Separator: A horizontal line separator
func NewSeparator() *widget.Separator {
	return widget.NewSeparator()
}
