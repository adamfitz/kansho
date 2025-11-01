package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// NewCard wraps content in a card-like container with a white background.
// Cards provide visual separation between different sections of the UI and
// create a clean, modern appearance against the gradient background.
//
// The card uses a stacked layout where:
// 1. A white rectangle forms the background
// 2. The content is placed on top with padding
//
// Parameters:
//   - content: The fyne.CanvasObject to be displayed inside the card
//
// Returns:
//   - fyne.CanvasObject: A card container with white background and padded content
//
// Example usage:
//
//	label := widget.NewLabel("Hello")
//	card := components.NewCard(label)
func NewCard(content fyne.CanvasObject) fyne.CanvasObject {
	// Create a white rectangle as the card background
	// This provides contrast against the purple gradient
	bg := canvas.NewRectangle(CardBackgroundColor)

	// Set minimum size to ensure the card is always visible
	// even when content is very small
	bg.SetMinSize(fyne.NewSize(CardMinWidth, CardMinHeight))

	// Stack layers the background behind the padded content
	// container.NewStack places objects on top of each other (z-axis layering)
	// container.NewPadded adds uniform padding around all sides of the content
	return container.NewStack(bg, container.NewPadded(content))
}

// NewCardWithHeader creates a card with a title header and separator.
// This is a convenience function for the common pattern of cards with titles.
//
// Parameters:
//   - title: The text to display in the card header
//   - content: The main content to display below the header
//
// Returns:
//   - fyne.CanvasObject: A card with header, separator, and content
//
// Example usage:
//
//	list := widget.NewList(...)
//	card := components.NewCardWithHeader("My List", list)
func NewCardWithHeader(title string, content fyne.CanvasObject) fyne.CanvasObject {
	// Create header with title and separator
	header := container.NewVBox(
		// Bold label for the card title
		NewBoldLabel(title),
		// Horizontal line to separate header from content
		NewSeparator(),
	)

	// Use border layout to place header at top and content below
	cardContent := container.NewBorder(
		header,  // Top border
		nil,     // Bottom border
		nil,     // Left border
		nil,     // Right border
		content, // Center content (fills remaining space)
	)

	// Wrap in card styling
	return NewCard(cardContent)
}
