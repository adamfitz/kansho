package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"kansho/config"
)

// ShowImportBookmarksDialog handles the import of bookmarks.json
func ShowImportBookmarksDialog(kanshoApp fyne.App, window fyne.Window) {
	openDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(fmt.Errorf("error opening file dialog: %v", err), window)
			return
		}

		if reader == nil {
			// User cancelled
			return
		}
		defer reader.Close()

		// Read the file content
		fileContent, err := io.ReadAll(reader)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to read file: %v", err), window)
			return
		}

		// Validate JSON
		var importedData config.Manga
		if err := json.Unmarshal(fileContent, &importedData); err != nil {
			dialog.ShowError(fmt.Errorf("invalid JSON file: %v", err), window)
			return
		}

		// Load current bookmarks
		currentBookmarks := config.LoadBookmarks()

		// Process imported bookmarks
		result := processImportedBookmarks(&currentBookmarks, &importedData)

		// Save the merged bookmarks
		if err := config.SaveBookmarks(currentBookmarks); err != nil {
			dialog.ShowError(fmt.Errorf("failed to save bookmarks: %v", err), window)
			return
		}

		// Show import summary
		summaryMsg := fmt.Sprintf(
			"Import completed!\n\n"+
				"Total in imported file: %d\n"+
				"Exact duplicates skipped: %d\n"+
				"Partial duplicates (renamed): %d\n"+
				"New bookmarks added: %d",
			len(importedData.Manga),
			result.ExactDuplicates,
			result.PartialDuplicates,
			result.NewBookmarks,
		)

		dialog.ShowInformation("Import Summary", summaryMsg, window)
	}, window)

	// Set filter for JSON files (also allow all files)
	openDialog.SetFilter(storage.NewExtensionFileFilter([]string{".json", ".txt"}))

	// Set initial directory to user's home
	homePath, err := os.UserHomeDir()
	if err == nil {
		homeURI := storage.NewFileURI(homePath)
		homeDir, err := storage.ListerForURI(homeURI)
		if err == nil {
			openDialog.SetLocation(homeDir)
		}
	}

	openDialog.Resize(fyne.NewSize(900, 700))
	openDialog.Show()
}

// ImportResult holds statistics about the import operation
type ImportResult struct {
	ExactDuplicates   int
	PartialDuplicates int
	NewBookmarks      int
}

// processImportedBookmarks merges imported bookmarks into current bookmarks
func processImportedBookmarks(current *config.Manga, imported *config.Manga) ImportResult {
	result := ImportResult{}

	for _, importedBookmark := range imported.Manga {
		matchType := checkBookmarkMatch(current, &importedBookmark)

		switch matchType {
		case MatchTypeExact:
			// Exact duplicate - skip
			result.ExactDuplicates++

		case MatchTypePartial:
			// Partial match - add with modified title
			modifiedBookmark := importedBookmark
			// Append site name to title to differentiate
			modifiedBookmark.Title = fmt.Sprintf("%s (%s)", importedBookmark.Title, importedBookmark.Site)
			current.Manga = append(current.Manga, modifiedBookmark)
			result.PartialDuplicates++

		case MatchTypeNone:
			// No match - add as new
			current.Manga = append(current.Manga, importedBookmark)
			result.NewBookmarks++
		}
	}

	return result
}

// MatchType represents the type of match found
type MatchType int

const (
	MatchTypeNone MatchType = iota
	MatchTypeExact
	MatchTypePartial
)

// checkBookmarkMatch checks if a bookmark already exists in the current bookmarks
func checkBookmarkMatch(current *config.Manga, bookmark *config.Bookmarks) MatchType {
	for _, existing := range current.Manga {
		// Check for exact match (all fields match)
		if bookmarksEqual(&existing, bookmark) {
			return MatchTypeExact
		}

		// Check for partial match (everything matches except site and/or URL)
		if existing.Title == bookmark.Title &&
			existing.Chapters == bookmark.Chapters &&
			existing.Location == bookmark.Location &&
			existing.Shortname == bookmark.Shortname {
			// Everything matches except potentially site and/or URL
			if existing.Site != bookmark.Site || existing.Url != bookmark.Url {
				return MatchTypePartial
			}
		}
	}

	return MatchTypeNone
}

// bookmarksEqual checks if two bookmarks are exactly equal
func bookmarksEqual(a, b *config.Bookmarks) bool {
	return a.Title == b.Title &&
		a.Url == b.Url &&
		a.Chapters == b.Chapters &&
		a.Location == b.Location &&
		a.Site == b.Site &&
		a.Shortname == b.Shortname
}
