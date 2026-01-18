package ui

import (
	"fmt"
	"log"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"kansho/config"
	"kansho/models"
	"kansho/sites"
	"kansho/validation"
)

// EditMangaView represents the "Edit Manga" card component.
// This view allows users to add new manga to their library or edit existing ones by:
// 1. Selecting a manga site from a dropdown
// 2. Entering the manga URL (and potentially other fields based on site requirements)
// 3. Selecting a target directory for manga storage
// 4. Clicking the Add Manga button to save a new entry
// 5. OR clicking the Save Manga button to update an existing entry
//
// The form fields shown are dynamic based on the selected site's requirements.
type EditMangaView struct {
	// Card is the complete UI component ready to be added to the layout
	Card fyne.CanvasObject

	// UI components that need to be accessed after creation
	SiteSelect           *widget.Select   // Dropdown for site selection
	Title                *widget.Entry    // Text input for manga name
	UrlEntry             *widget.Entry    // Text input for manga URL
	DirectoryLabel       *widget.Label    // Label showing selected directory
	DirectoryButton      *widget.Button   // Button to open directory picker
	AddButton            *widget.Button   // Button to add new manga
	SaveButton           *widget.Button   // Button to save changes to existing manga
	CancelButton         *widget.Button   // Button to cancel editing
	SelectedDirectoryURI fyne.ListableURI // Stores the selected directory URI

	// state is a reference to the shared application state
	State *KanshoAppState

	// sitesConfig contains all supported manga sites and their requirements
	SitesConfig models.SitesConfig

	// Track if we're in edit mode (editing existing manga vs adding new)
	isEditMode       bool
	editingMangaID   int    // Index of the manga being edited
	originalLocation string // Original directory path before editing
}

// NewEditMangaView creates a new "Edit Manga" view component.
// This form allows users to add new manga or edit existing ones.
//
// Parameters:
//   - state: Pointer to the shared application state
//
// Returns:
//   - *EditMangaView: A new edit manga view with all components initialized
func NewEditMangaView(State *KanshoAppState) *EditMangaView {
	view := &EditMangaView{
		State:          State,
		isEditMode:     false,
		editingMangaID: -1,
	}

	// Load the sites configuration
	view.SitesConfig = sites.LoadSitesConfig()

	// Create a slice of site display names for the dropdown
	siteNames := make([]string, len(view.SitesConfig.Sites))
	for i, site := range view.SitesConfig.Sites {
		siteNames[i] = site.DisplayName
	}

	// Create the site selection dropdown
	view.SiteSelect = widget.NewSelect(siteNames, func(selected string) {
		view.onSiteSelected(selected)
	})
	view.SiteSelect.PlaceHolder = "Site name"

	// Create the title/name input field
	view.Title = widget.NewEntry()
	view.Title.SetPlaceHolder("Full Manga Name")

	// Create the URL input field
	view.UrlEntry = widget.NewEntry()
	view.UrlEntry.SetPlaceHolder("Paste manga URL")

	// Create the directory selection label and button
	view.DirectoryLabel = widget.NewLabel("No directory selected")
	view.DirectoryLabel.Wrapping = fyne.TextTruncate
	view.DirectoryButton = widget.NewButton("Choose Directory...", func() {
		view.onDirectoryButtonClicked()
	})

	// Create the Add Manga button
	view.AddButton = widget.NewButton("Add Manga", func() {
		view.onAddButtonClicked()
	})

	// Create the Save Manga button
	view.SaveButton = widget.NewButton("Save Manga", func() {
		view.onSaveButtonClicked()
	})
	view.SaveButton.Hide() // Hidden by default, shown in edit mode

	// Create the Cancel Edit button
	view.CancelButton = widget.NewButton("Cancel Edit", func() {
		view.clearForm()
	})
	view.CancelButton.Hide() // Hidden by default, shown in edit mode

	// Create the name/title row with label on the left, entry on the right
	nameRow := container.NewBorder(
		nil,
		nil,
		widget.NewLabel("Name:"),
		nil,
		view.Title,
	)

	// Create the site selection row
	siteRow := container.NewBorder(
		nil,
		nil,
		widget.NewLabel("Select Site:"),
		nil,
		view.SiteSelect,
	)

	// Create the URL row
	urlRow := container.NewVBox(
		widget.NewLabel("URL:"),
		view.UrlEntry,
	)

	// Create the directory row
	directoryRow := container.NewVBox(
		widget.NewLabel("Directory:"),
		container.NewBorder(nil, nil, view.DirectoryButton, nil, view.DirectoryLabel),
	)

	// Create container for the buttons, centered
	buttonRow := container.NewCenter(
		container.NewHBox(
			view.AddButton,
			view.SaveButton,
			view.CancelButton,
		),
	)

	// Build the card content
	cardContent := container.NewVBox(
		NewBoldLabel("Edit Manga"),
		NewSeparator(),
		nameRow,
		siteRow,
		urlRow,
		directoryRow,
		NewSeparator(),
		buttonRow,
	)

	// Wrap the content in a card
	view.Card = NewCard(cardContent)

	return view
}

// LoadMangaForEditing loads an existing manga's data into the form for editing
func (v *EditMangaView) LoadMangaForEditing(mangaID int) {
	if mangaID < 0 || mangaID >= len(v.State.MangaData.Manga) {
		return
	}

	manga := v.State.MangaData.Manga[mangaID]

	// Set edit mode
	v.isEditMode = true
	v.editingMangaID = mangaID
	v.originalLocation = manga.Location

	// Load the manga data into the form
	v.Title.SetText(manga.Title)
	v.SiteSelect.SetSelected(manga.Site)
	v.UrlEntry.SetText(manga.Url)
	v.DirectoryLabel.SetText(manga.Location)

	// Parse the location to set the directory URI
	// Location format is typically: /path/to/directory/MangaName
	fileURI := storage.NewFileURI(manga.Location)
	listableURI, err := storage.ListerForURI(fileURI)
	if err != nil {
		log.Printf("[EditManga] Warning: Could not create ListableURI for %s: %v", manga.Location, err)
		// Still set the label even if we can't get a listable URI
	} else {
		v.SelectedDirectoryURI = listableURI
	}

	// Show Save button and Cancel button, hide Add button
	v.AddButton.Hide()
	v.SaveButton.Show()
	v.CancelButton.Show()

	log.Printf("[EditManga] Loaded manga for editing: %s (ID: %d)", manga.Title, mangaID)
}

// ClearForm resets the form to add mode
func (v *EditMangaView) clearForm() {
	v.Title.SetText("")
	v.UrlEntry.SetText("")
	v.DirectoryLabel.SetText("No directory selected")
	v.SelectedDirectoryURI = nil
	v.SiteSelect.ClearSelected()

	// Reset to add mode
	v.isEditMode = false
	v.editingMangaID = -1
	v.originalLocation = ""

	// Show Add button, hide Save and Cancel buttons
	v.SaveButton.Hide()
	v.CancelButton.Hide()
	v.AddButton.Show()
}

// onDirectoryButtonClicked opens a folder selection dialog.
func (v *EditMangaView) onDirectoryButtonClicked() {
	folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, v.State.Window)
			return
		}

		if uri == nil {
			return
		}

		v.SelectedDirectoryURI = uri
		v.DirectoryLabel.SetText(uri.Path())
	}, v.State.Window)

	homePath, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Failed to get home directory: %v", err)
	} else {
		homeURI := storage.NewFileURI(homePath)
		homeDir, err := storage.ListerForURI(homeURI)
		if err != nil {
			log.Printf("Failed to get ListableURI for %s: %v", homePath, err)
		} else {
			folderDialog.SetLocation(homeDir)
		}
	}

	folderDialog.Resize(fyne.NewSize(900, 700))
	folderDialog.Show()
}

// onSiteSelected is called when the user selects a site from the dropdown.
func (v *EditMangaView) onSiteSelected(selected string) {
	var selectedSite models.Site
	for _, site := range v.SitesConfig.Sites {
		if site.DisplayName == selected {
			selectedSite = site
			break
		}
	}

	log.Printf("Selected site: %s\n", selectedSite.Name)
}

// onAddButtonClicked is called when the user clicks the Add Manga button.
func (v *EditMangaView) onAddButtonClicked() {
	selectedSite := v.SiteSelect.Selected

	title := ""
	if v.Title != nil {
		title = v.Title.Text
	}

	url := ""
	if v.UrlEntry != nil {
		url = v.UrlEntry.Text
	}

	location := ""
	if v.SelectedDirectoryURI != nil {
		cleanedDirectory := strings.ReplaceAll(v.SelectedDirectoryURI.String(), "file://", "")
		location = fmt.Sprintf("%s/%s", cleanedDirectory, title)
	}

	// Validate the input
	err := validation.ValidateAddManga(selectedSite, title, "", url, location, &v.SitesConfig)
	if err != nil {
		if v.State != nil && v.State.Window != nil {
			dialog.ShowError(err, v.State.Window)
		}
		return
	}

	// Create the directory for the manga
	err = os.MkdirAll(location, 0755)
	if err != nil {
		if v.State != nil && v.State.Window != nil {
			dialog.ShowError(
				fmt.Errorf("failed to create manga directory: %v", err),
				v.State.Window,
			)
		}
		return
	}

	// Create the new manga entry
	newManga := config.Bookmarks{
		Title:     title,
		Shortname: "", // Removed shortname
		Url:       url,
		Site:      selectedSite,
		Location:  location,
	}

	// Add to app state
	v.State.AddManga(newManga)

	// Show success dialog
	successMsg := fmt.Sprintf(
		"Manga added successfully!\n\nTitle: %s\nSite: %s\nURL: %s\nDirectory: %s",
		title, selectedSite, url, location,
	)

	dialog.ShowInformation("Success", successMsg, v.State.Window)

	// Clear the form
	v.clearForm()
}

// onSaveButtonClicked is called when the user clicks the Save Manga button.
func (v *EditMangaView) onSaveButtonClicked() {
	if !v.isEditMode || v.editingMangaID < 0 || v.editingMangaID >= len(v.State.MangaData.Manga) {
		dialog.ShowError(fmt.Errorf("no manga loaded for editing"), v.State.Window)
		return
	}

	selectedSite := v.SiteSelect.Selected
	title := v.Title.Text
	url := v.UrlEntry.Text

	// Get the new location
	newLocation := ""
	if v.SelectedDirectoryURI != nil {
		newLocation = v.SelectedDirectoryURI.Path()
	} else if v.DirectoryLabel.Text != "No directory selected" {
		newLocation = v.DirectoryLabel.Text
	}

	// Validate the input
	err := validation.ValidateAddManga(selectedSite, title, "", url, newLocation, &v.SitesConfig)
	if err != nil {
		dialog.ShowError(err, v.State.Window)
		return
	}

	// Check if directory location changed
	if v.originalLocation != newLocation && v.originalLocation != "" {
		// Verify the original directory exists
		if _, err := os.Stat(v.originalLocation); err == nil {
			// Rename the directory
			err = os.Rename(v.originalLocation, newLocation)
			if err != nil {
				dialog.ShowError(
					fmt.Errorf("failed to rename directory from %s to %s: %v",
						v.originalLocation, newLocation, err),
					v.State.Window,
				)
				return
			}
			log.Printf("[EditManga] Renamed directory: %s -> %s", v.originalLocation, newLocation)
		} else {
			// Original directory doesn't exist, create new one
			err = os.MkdirAll(newLocation, 0755)
			if err != nil {
				dialog.ShowError(
					fmt.Errorf("failed to create manga directory: %v", err),
					v.State.Window,
				)
				return
			}
			log.Printf("[EditManga] Created new directory: %s", newLocation)
		}
	} else if newLocation != "" {
		// Ensure directory exists
		err = os.MkdirAll(newLocation, 0755)
		if err != nil {
			dialog.ShowError(
				fmt.Errorf("failed to create manga directory: %v", err),
				v.State.Window,
			)
			return
		}
	}

	// Update the manga entry
	v.State.MangaData.Manga[v.editingMangaID].Title = title
	v.State.MangaData.Manga[v.editingMangaID].Site = selectedSite
	v.State.MangaData.Manga[v.editingMangaID].Url = url
	v.State.MangaData.Manga[v.editingMangaID].Location = newLocation
	v.State.MangaData.Manga[v.editingMangaID].Shortname = "" // Remove shortname

	// Save to disk
	err = config.SaveBookmarks(v.State.MangaData)
	if err != nil {
		dialog.ShowError(
			fmt.Errorf("failed to save bookmarks: %v", err),
			v.State.Window,
		)
		return
	}

	// Show success dialog
	successMsg := fmt.Sprintf(
		"Manga updated successfully!\n\nTitle: %s\nSite: %s\nURL: %s\nDirectory: %s",
		title, selectedSite, url, newLocation,
	)

	dialog.ShowInformation("Success", successMsg, v.State.Window)

	// Trigger refresh callbacks
	for _, callback := range v.State.OnMangaAdded {
		callback()
	}

	// Clear the form
	v.clearForm()
}
