package main

// Refactored main.go
// This file has been simplified to focus only on application initialization.
// All UI layout, components, and state management have been moved to separate packages:
//
// Package structure:
// - models/        : Data structures (Site, SitesConfig, RequiredFields)
// - config/        : Configuration (verification, save and load bookmarks, logging)
// - ui/            : UI state management and theme constants, reusable UI components (cards, header, footer,
// 						manga list, add manga, chapter list)
// - bookmarks/     : Manga data loading (existing package)

import (
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"kansho/config"
	"kansho/ui"
)

func main() {

	// Create a new Fyne application instance
	// This initializes the application and sets up the event loop
	kanshoApp := app.NewWithID("com.backyard.kansho") // must match your AppMetadata.ID

	kanshoMetadata := fyne.AppMetadata{
		ID:      "com.backyard.kansho",
		Name:    "Kansho",
		Version: config.Version,
	}

	app.SetMetadata(kanshoMetadata)

	// Create the main application window
	// The window title "kansho" (鑑賞) means "appreciation" or "viewing" in Japanese
	myWindow := kanshoApp.NewWindow("kansho")

	// -------------------------------------------------------------------------
	// FILE MENU WITH QUIT OPTION
	// -------------------------------------------------------------------------
	// Create File menu with Quit option for graceful application shutdown
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Logs", func() {
			log.Println("[UI] Kansho Logs opened (GUI)")
			ui.ShowLogWindow(kanshoApp)
		}),
		fyne.NewMenuItem("Bookmarks", func() {
			log.Println("[UI] Kansho Boomarks opened (GUI)")
			ui.ShowBookmarksWindow(kanshoApp)
		}),
		// Dont expose to the user, leave the keybind for debugging
		// fyne.NewMenuItem("Configuration", func() {
		// 	log.Println("[UI] Kansho configuration opened (GUI)")
		// 	ui.ShowConfigWindow(kanshoApp)
		// }),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About", func() {
			log.Println("[UI] About dialog opened") // ← Add logging like the others
			ui.ShowAboutDialog(kanshoApp)           // ← Pass kanshoApp like the others
		}),
	)

	// Create main menu bar and add it to the window
	// File is a well known application menu, so by default fyne injects its own Quit menu entry and it is not required
	// to manually configure this (like the other entries above).
	mainMenu := fyne.NewMainMenu(fileMenu, helpMenu)
	myWindow.SetMainMenu(mainMenu)

	// -------------------------------------------------------------------------
	// KEYBOARD SHORTCUT: CTRL+Q TO QUIT
	// -------------------------------------------------------------------------
	// Register Ctrl+Q keyboard shortcut for quick application exit
	// This is a standard shortcut on many platforms
	myWindow.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyQ,
		Modifier: fyne.KeyModifierControl,
	}, func(shortcut fyne.Shortcut) {
		log.Println("[UI] User closed application (ctrl + q)")
		kanshoApp.Quit()
	})
	myWindow.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyL,
		Modifier: fyne.KeyModifierControl,
	}, func(shortcut fyne.Shortcut) {
		log.Println("[UI] Kansho Logs opened (ctrl + l)")
		ui.ShowLogWindow(kanshoApp)
	})
	myWindow.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyB,
		Modifier: fyne.KeyModifierControl,
	}, func(shortcut fyne.Shortcut) {
		log.Println("[UI] Kansho Boomarks opened (ctrl + b)")
		ui.ShowBookmarksWindow(kanshoApp)
	})
	myWindow.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyC,
		Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift, // bitwise OR operator to make both shift and ctrl the modifier keys
	}, func(shortcut fyne.Shortcut) {
		log.Println("[UI] Kansho configuration opened (ctrl + c)")
		ui.ShowConfigWindow(kanshoApp)
	})

	myWindow.SetCloseIntercept(func() {
		log.Println("[UI] User closed application (File menu)")
		kanshoApp.Quit() // allow the app to actually close
	})

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
