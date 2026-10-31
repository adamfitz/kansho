package ui

import (
	"encoding/json"
	"fmt"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"kansho/config"
	"kansho/mangadex"
)

// ShowExportBookmarksDialog exports the bookmarks stored in the database to a
// user-selected JSON file.
func ShowExportBookmarksDialog(kanshoApp fyne.App, window fyne.Window) {
	store, err := mangadex.GetStore()
	if err != nil {
		dialog.ShowError(fmt.Errorf("cannot open Kansho database: %v", err), window)
		return
	}

	bookmarksData, err := store.LoadBookmarks()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to load bookmarks: %v", err), window)
		return
	}

	// Check if bookmarks are empty
	if len(bookmarksData.Manga) == 0 {
		// Ask user if they want to export an empty file
		confirmDialog := dialog.NewConfirm(
			"Empty Bookmarks",
			"The bookmark database is empty. Do you still want to export it?",
			func(confirmed bool) {
				if confirmed {
					showBookmarkSaveDialog(window, bookmarksData)
				}
			},
			window,
		)
		confirmDialog.Show()
		return
	}

	// Bookmarks exist, proceed to save dialog
	showBookmarkSaveDialog(window, bookmarksData)
}

// showBookmarkSaveDialog displays the save file dialog for exporting bookmarks.
func showBookmarkSaveDialog(window fyne.Window, bookmarksData config.Manga) {
	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("error opening save dialog: %v", err), window)
			return
		}

		if writer == nil {
			// User cancelled
			return
		}
		defer writer.Close()

		sourceContent, err := json.MarshalIndent(bookmarksData, "", "  ")
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to encode bookmarks: %v", err), window)
			return
		}

		// Write to the selected destination
		if _, err = writer.Write(sourceContent); err != nil {
			dialog.ShowError(fmt.Errorf("failed to write bookmarks file: %v", err), window)
			return
		}

		dialog.ShowInformation("Success", "Bookmarks exported successfully!", window)
	}, window)

	// Set default filename
	saveDialog.SetFileName("bookmarks.json")

	// Set initial directory to user's home
	homePath, err := os.UserHomeDir()
	if err == nil {
		homeURI := storage.NewFileURI(homePath)
		homeDir, err := storage.ListerForURI(homeURI)
		if err == nil {
			saveDialog.SetLocation(homeDir)
		}
	}

	saveDialog.Resize(fyne.NewSize(900, 700))
	saveDialog.Show()
}
