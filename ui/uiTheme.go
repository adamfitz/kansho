package ui

import (
	"image/color"
)

// Theme constants define the visual appearance of the application.
// By centralizing these values, we ensure consistency across all UI components
// and make it easy to update the look and feel of the entire application.

// Color palette for the application
var (
	// GradientStartColor is the lighter purple used at the start of the background gradient
	GradientStartColor = color.RGBA{R: 115, G: 103, B: 240, A: 255}

	// GradientEndColor is the darker purple used at the end of the background gradient
	GradientEndColor = color.RGBA{R: 136, G: 84, B: 208, A: 255}

	// CardBackgroundColor is the white color used for card backgrounds
	CardBackgroundColor = color.RGBA{R: 255, G: 255, B: 255, A: 255}

	// TextColorLight is used for text on dark backgrounds (like the gradient)
	TextColorLight = color.White
)

// Text size constants for consistent typography
const (
	// TitleTextSize is used for the main application title
	TitleTextSize = 48

	// SubtitleTextSize is used for descriptive text below titles
	SubtitleTextSize = 16

	// FooterTextSize is used for footer text
	FooterTextSize = 14

	// CardTitleTextSize can be used for card headers (currently using default bold labels)
	CardTitleTextSize = 16
)

// Layout constants
const (
	// GradientAngle defines the angle of the background gradient in degrees
	GradientAngle = 45

	// CardMinWidth is the minimum width for card components
	CardMinWidth = 100

	// CardMinHeight is the minimum height for card components
	CardMinHeight = 100

	// DefaultWindowWidth is the initial width of the application window
	DefaultWindowWidth = 1200

	// DefaultWindowHeight is the initial height of the application window
	DefaultWindowHeight = 800
)
