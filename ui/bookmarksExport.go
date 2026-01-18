package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"kansho/config"
	"kansho/parser"
)

// ShowExportBookmarksDialog handles the export of bookmarks.json
func ShowExportBookmarksDialog(kanshoApp fyne.App, window fyne.Window) {
	// Get the bookmarks file path
	configDir, err := parser.ExpandPath("~/.config/kansho")
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to get config directory: %v", err), window)
		return
	}
	bookmarksFilePath := filepath.Join(configDir, "bookmarks.json")

	// Check if bookmarks.json exists
	_, err = os.Stat(bookmarksFilePath)
	if os.IsNotExist(err) {
		dialog.ShowError(fmt.Errorf("bookmarks.json file does not exist"), window)
		return
	}

	// Read and check if the file is empty
	fileContent, err := os.ReadFile(bookmarksFilePath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to read bookmarks file: %v", err), window)
		return
	}

	// Parse the JSON to check if it's empty
	var bookmarksData config.Manga
	if err := json.Unmarshal(fileContent, &bookmarksData); err != nil {
		dialog.ShowError(fmt.Errorf("bookmarks file is corrupted: %v", err), window)
		return
	}

	// Check if bookmarks are empty
	if len(bookmarksData.Manga) == 0 {
		// Ask user if they want to export an empty file
		confirmDialog := dialog.NewConfirm(
			"Empty Bookmarks File",
			"The bookmarks.json file is empty. Do you still want to export it?",
			func(confirmed bool) {
				if confirmed {
					showSaveDialog(window, bookmarksFilePath)
				}
			},
			window,
		)
		confirmDialog.Show()
		return
	}

	// File exists and is not empty, proceed to save dialog
	showSaveDialog(window, bookmarksFilePath)
}

// showSaveDialog displays the save file dialog for exporting bookmarks
func showSaveDialog(window fyne.Window, sourceFilePath string) {
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

		// Read the source file
		sourceContent, err := os.ReadFile(sourceFilePath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to read bookmarks file: %v", err), window)
			return
		}

		// Write to the selected destination
		_, err = writer.Write(sourceContent)
		if err != nil {
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
