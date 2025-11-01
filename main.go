package main

// Refactored main.go
// This file has been simplified to focus only on application initialization.
// All UI layout, components, and state management have been moved to separate packages:
//
// Package structure:
// - models/        : Data structures (Site, SitesConfig, RequiredFields)
// - config/        : Configuration loading (sites.json)
// - ui/            : UI state management and theme constants
// - ui/components/ : Reusable UI components (cards, header, footer)
// - ui/views/      : View components (manga list, add manga, chapter list)
// - bookmarks/     : Manga data loading (existing package)

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"kansho/ui"
)

func main() {
	// Create a new Fyne application instance
	// This initializes the application and sets up the event loop
	kanshoApp := app.NewWithID("com.backyard.kansho") // must match your AppMetadata.ID

	kanshoMetadata := fyne.AppMetadata{
		ID:      "com.backyard.kansho",
		Name:    "Kansho",
		Version: "0.0.1",
		Build:   1,
		Icon:    bundled,
	}

	app.SetMetadata(kanshoMetadata)

	// Create the main application window
	// The window title "kansho" (鑑賞) means "appreciation" or "viewing" in Japanese
	myWindow := kanshoApp.NewWindow("kansho")

	// Set the initial window size
	// Users can resize the window, but this provides a good starting size
	myWindow.Resize(fyne.NewSize(ui.DefaultWindowWidth, ui.DefaultWindowHeight))

	// Build the complete UI layout
	// This creates all components, sets up state management, and assembles the layout
	// The ui.BuildMainLayout function handles all the complexity of:
	// - Creating the gradient background
	// - Initializing the application state
	// - Creating all view components (manga list, add manga, chapter list)
	// - Setting up callbacks for inter-component communication
	// - Assembling everything into a cohesive layout
	content := ui.BuildMainLayout(myWindow)

	// Set the window content to our complete layout
	myWindow.SetContent(content)

	// Show the window and start the application event loop
	// This call blocks until the window is closed by the user
	// The event loop handles:
	// - User input (clicks, keyboard)
	// - Window events (resize, close)
	// - Redraws and updates
	myWindow.ShowAndRun()
}
