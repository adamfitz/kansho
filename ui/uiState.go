package ui

import (
	"kansho/bookmarks"
	"kansho/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// KanshoAppState holds the shared state for the entire application.
// This centralized state allows different UI components to communicate with each other.
// For example, when a user selects a manga in the list, the chapter view can be updated.
//
// The state uses callbacks to notify components when data changes, following an
// observer pattern. This keeps components loosely coupled while allowing them to react
// to changes.
type KanshoAppState struct {
	// Window is the main application window, needed for showing dialogs
	Window fyne.Window

	// MangaData contains all loaded manga bookmarks
	MangaData bookmarks.Manga

	// SitesConfig contains configuration for all supported manga sites
	SitesConfig models.SitesConfig

	// SelectedMangaID is the index of the currently selected manga in the list
	// -1 indicates no selection
	SelectedMangaID int

	// Callbacks for notifying components when state changes
	// These allow views to update themselves when relevant data changes

	// OnMangaSelected is called when a user selects a manga from the list
	// The callback receives the ID of the selected manga
	OnMangaSelected []func(id int)

	// OnMangaAdded is called when a new manga is successfully added
	// This allows the manga list to refresh and show the new entry
	OnMangaAdded []func()

	// OnMangaDeleted is called when a manga is removed
	// This allows the list to refresh and remove the deleted entry
	OnMangaDeleted []func(id int)
}

// NewKanshoAppState creates and initializes a new application state.
// This should be called once at application startup.
//
// Parameters:
//   - window: The main application window
//
// Returns:
//   - *KanshoAppState: A new state instance with initialized data
func NewKanshoAppState(window fyne.Window) *KanshoAppState {
	return &KanshoAppState{
		Window:          window,
		MangaData:       bookmarks.LoadBookmarks(),
		SitesConfig:     models.SitesConfig{}, // Will be loaded by config package
		SelectedMangaID: -1,                   // No selection initially
		OnMangaSelected: make([]func(int), 0),
		OnMangaAdded:    make([]func(), 0),
		OnMangaDeleted:  make([]func(int), 0),
	}
}

// SelectManga updates the selected manga and notifies all registered callbacks.
// This is called when a user clicks on a manga in the list.
//
// Parameters:
//   - id: The index of the selected manga in the MangaData.Manga slice
func (s *KanshoAppState) SelectManga(id int) {
	s.SelectedMangaID = id

	// Trigger all registered callbacks
	for _, callback := range s.OnMangaSelected {
		callback(id)
	}
}

// AddManga adds a new manga to the bookmarks and notifies callbacks.
// In the future, this will save to disk/database.
//
// Parameters:
//   - manga: The manga bookmark to add
//
// TODO: Implement actual persistence (save to file/database)
func (s *KanshoAppState) AddManga(manga bookmarks.Bookmarks) {
	// Add the manga to our in-memory data
	s.MangaData.Manga = append(s.MangaData.Manga, manga)

	// Save to disk immediately
	err := bookmarks.SaveBookmarks(s.MangaData)
	if err != nil {
		// Handle error - maybe show a dialog to the user
		dialog.ShowError(err, s.Window)
	}

	// Notify all registered callbacks that a manga was added
	for _, callback := range s.OnMangaAdded {
		callback()
	}
}

// DeleteManga removes a manga from the bookmarks and notifies callbacks.
// In the future, this will update the disk/database.
//
// Parameters:
//   - id: The index of the manga to delete
//
// TODO: Implement actual persistence (save to file/database)
func (s *KanshoAppState) DeleteManga(id int) {
	// Validate the ID is within bounds
	if id < 0 || id >= len(s.MangaData.Manga) {
		return
	}

	// Remove the manga from the slice
	s.MangaData.Manga = append(s.MangaData.Manga[:id], s.MangaData.Manga[id+1:]...)

	// TODO: Save to disk/database here
	// bookmarks.SaveBookmarks(s.MangaData)

	// If the deleted manga was selected, clear the selection
	if s.SelectedMangaID == id {
		s.SelectedMangaID = -1
	} else if s.SelectedMangaID > id {
		// If a manga before the selected one was deleted, adjust the index
		s.SelectedMangaID--
	}

	// Notify all registered callbacks that a manga was deleted
	for _, callback := range s.OnMangaDeleted {
		callback(id)
	}
}

// GetSelectedManga returns the currently selected manga, or nil if none is selected.
//
// Returns:
//   - *bookmarks.Bookmarks: Pointer to the selected manga, or nil if no selection
func (s *KanshoAppState) GetSelectedManga() *bookmarks.Bookmarks {
	if s.SelectedMangaID < 0 || s.SelectedMangaID >= len(s.MangaData.Manga) {
		return nil
	}
	return &s.MangaData.Manga[s.SelectedMangaID]
}

// RegisterMangaSelectedCallback registers a callback to be called when manga selection changes.
// Multiple callbacks can be registered and will all be called in order.
//
// Parameters:
//   - callback: Function to call when a manga is selected
func (s *KanshoAppState) RegisterMangaSelectedCallback(callback func(int)) {
	s.OnMangaSelected = append(s.OnMangaSelected, callback)
}

// RegisterMangaAddedCallback registers a callback to be called when a manga is added.
// Multiple callbacks can be registered and will all be called in order.
//
// Parameters:
//   - callback: Function to call when a manga is added
func (s *KanshoAppState) RegisterMangaAddedCallback(callback func()) {
	s.OnMangaAdded = append(s.OnMangaAdded, callback)
}

// RegisterMangaDeletedCallback registers a callback to be called when a manga is deleted.
// Multiple callbacks can be registered and will all be called in order.
//
// Parameters:
//   - callback: Function to call when a manga is deleted
func (s *KanshoAppState) RegisterMangaDeletedCallback(callback func(int)) {
	s.OnMangaDeleted = append(s.OnMangaDeleted, callback)
}
