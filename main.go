package main

// Refactored main.go
// This file focuses on application initialization.
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

	_ "embed" // required for go:embed

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"

	"kansho/config"
	"kansho/ui"
)

//go:embed packaging/kansho.png
var iconBytes []byte

func main() {

	// Create a new Fyne application instance
	kanshoApp := app.NewWithID("com.backyard.kansho") // must match your AppMetadata.ID

	kanshoMetadata := fyne.AppMetadata{
		ID:      "com.backyard.kansho",
		Name:    "Kansho",
		Version: config.Version,
	}

	app.SetMetadata(kanshoMetadata)

	// Create the main application window
	myWindow := kanshoApp.NewWindow("kansho")

	// -------------------------
	// Set title bar & taskbar icon
	// -------------------------
	appIcon := fyne.NewStaticResource("kansho.png", iconBytes)
	myWindow.SetIcon(appIcon)

	// -------------------------------------------------------------------------
	// FILE MENU WITH QUIT OPTION
	// -------------------------------------------------------------------------
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Logs", func() {
			log.Println("[UI] Kansho Logs opened (GUI)")
			ui.ShowLogWindow(kanshoApp)
		}),
	)

	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About", func() {
			log.Println("[UI] About dialog opened")
			ui.ShowAboutDialog(kanshoApp)
		}),
	)

	bookmarksMenu := fyne.NewMenu("Bookmarks",
		fyne.NewMenuItem("Bookmarks", func() {
			log.Println("[UI] Kansho Bookmarks opened (GUI)")
			ui.ShowBookmarksWindow(kanshoApp)
		}),
		fyne.NewMenuItem("Export Bookmarks", func() {
			log.Println("[UI] Export Bookmarks triggered (GUI)")
			ui.ShowExportBookmarksDialog(kanshoApp, myWindow)
		}),
		fyne.NewMenuItem("Import Bookmarks", func() {
			log.Println("[UI] Import Bookmarks triggered (GUI)")
			ui.ShowImportBookmarksDialog(kanshoApp, myWindow)
		}),
	)

	mainMenu := fyne.NewMainMenu(fileMenu, bookmarksMenu, helpMenu)
	myWindow.SetMainMenu(mainMenu)

	// -------------------------------------------------------------------------
	// KEYBOARD SHORTCUTS
	// -------------------------------------------------------------------------
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
		log.Println("[UI] Kansho Bookmarks opened (ctrl + b)")
		ui.ShowBookmarksWindow(kanshoApp)
	})
	myWindow.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName:  fyne.KeyC,
		Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift,
	}, func(shortcut fyne.Shortcut) {
		log.Println("[UI] Kansho configuration opened (ctrl + c)")
		ui.ShowConfigWindow(kanshoApp)
	})

	myWindow.SetCloseIntercept(func() {
		log.Println("[UI] User closed application (File menu)")
		kanshoApp.Quit()
	})

	// Set initial window size
	myWindow.Resize(fyne.NewSize(ui.DefaultWindowWidth, ui.DefaultWindowHeight))

	// Build the complete UI layout
	content := ui.BuildMainLayout(myWindow)
	myWindow.SetContent(content)

	// Show the window and run the event loop
	myWindow.ShowAndRun()
}
