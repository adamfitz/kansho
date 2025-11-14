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

// AddMangaView represents the "Manage Manga" card component.
// This view allows users to add new manga to their library by:
// 1. Selecting a manga site from a dropdown
// 2. Entering the manga URL (and potentially other fields based on site requirements)
// 3. Selecting a target directory for manga storage
// 4. Clicking the Add Manga button to save
//
// The form fields shown are dynamic based on the selected site's requirements.
// For example, some sites might need a shortname while others don't.
type AddMangaView struct {
	// Card is the complete UI component ready to be added to the layout
	Card fyne.CanvasObject

	// UI components that need to be accessed after creation
	SiteSelect           *widget.Select   // Dropdown for site selection
	Title                *widget.Entry    // Text input for manga name
	ShortnameEntry       *widget.Entry    // Text input for manga shortname
	UrlEntry             *widget.Entry    // Text input for manga URL
	DirectoryLabel       *widget.Label    // Label showing selected directory
	DirectoryButton      *widget.Button   // Button to open directory picker
	AddButton            *widget.Button   // Button to add the manga
	SelectedDirectoryURI fyne.ListableURI // Stores the selected directory URI

	// Container for dynamic fields that may be shown/hidden
	shortnameContainer *fyne.Container

	// state is a reference to the shared application state
	State *KanshoAppState

	// sitesConfig contains all supported manga sites and their requirements
	SitesConfig models.SitesConfig
}

// NewAddMangaView creates a new "Manage Manga" view component.
// This form allows users to add new manga to their library.
//
// Parameters:
//   - state: Pointer to the shared application state
//
// Returns:
//   - *AddMangaView: A new add manga view with all components initialized
//
// The view loads site configurations and creates a dynamic form that adapts
// to the requirements of the selected manga site.
func NewAddMangaView(State *KanshoAppState) *AddMangaView {
	view := &AddMangaView{
		State: State,
	}

	// Load the sites configuration
	// This tells us which manga sites are supported and what data they need
	view.SitesConfig = sites.LoadSitesConfig()

	// Create a slice of site display names for the dropdown
	// Extract just the user-facing names from the config
	siteNames := make([]string, len(view.SitesConfig.Sites))
	for i, site := range view.SitesConfig.Sites {
		siteNames[i] = site.DisplayName
	}

	// Create the site selection dropdown
	view.SiteSelect = widget.NewSelect(siteNames, func(selected string) {
		// This callback is triggered when the user selects a site
		view.onSiteSelected(selected)
	})
	view.SiteSelect.PlaceHolder = "Site name"

	// Create the title/name input field
	view.Title = widget.NewEntry()
	view.Title.SetPlaceHolder("Full Manga Name")

	// Create the shortname input field
	view.ShortnameEntry = widget.NewEntry()
	view.ShortnameEntry.SetPlaceHolder("Short Name value")

	// Create the URL input field
	view.UrlEntry = widget.NewEntry()
	view.UrlEntry.SetPlaceHolder("Paste manga URL")

	// Create the directory selection label and button
	view.DirectoryLabel = widget.NewLabel("No directory selected")
	view.DirectoryLabel.Wrapping = fyne.TextTruncate // Truncate long paths with ellipsis
	view.DirectoryButton = widget.NewButton("Choose Directory...", func() {
		view.onDirectoryButtonClicked()
	})

	// Create the Add Manga button
	view.AddButton = widget.NewButton("Add Manga", func() {
		view.onAddButtonClicked()
	})

	// Create the name/title row with label on the left, entry on the right
	nameRow := container.NewBorder(
		nil,                      // Top
		nil,                      // Bottom
		widget.NewLabel("Name:"), // Left - the label
		nil,                      // Right
		view.Title,               // Center - the entry field (fills remaining space)
	)

	// Create the first row with Select Site and Short Name fields
	// Using NewGridWithColumns to get equal-width columns
	firstRow := container.NewGridWithColumns(2,
		container.NewVBox(
			widget.NewLabel("Select Site:"),
			view.SiteSelect,
		),
		container.NewVBox(
			widget.NewLabel("Short Name:"),
			view.ShortnameEntry,
		),
	)

	// Create the second row with URL field (spans full width)
	secondRow := container.NewVBox(
		widget.NewLabel("URL:"),
		view.UrlEntry,
	)

	// Create the third row with Directory field (button + label)
	// Use a horizontal box to place the button and label side by side
	directoryRow := container.NewVBox(
		widget.NewLabel("Directory:"),
		container.NewBorder(nil, nil, view.DirectoryButton, nil, view.DirectoryLabel),
	)

	// Create container for the button, centered
	buttonRow := container.NewCenter(view.AddButton)

	// Build the card content with horizontal layout
	cardContent := container.NewVBox(
		NewBoldLabel("Manage Manga"),
		NewSeparator(),
		nameRow,
		firstRow,
		secondRow,
		directoryRow,
		NewSeparator(),
		buttonRow,
	)

	// Wrap the content in a card
	view.Card = NewCard(cardContent)

	return view
}

// onDirectoryButtonClicked opens a folder selection dialog.
// When the user selects a directory, it updates the label and stores the URI.
func (v *AddMangaView) onDirectoryButtonClicked() {
	// Create a folder open dialog
	folderDialog := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			// Error occurred while opening the dialog
			dialog.ShowError(err, v.State.Window)
			return
		}

		if uri == nil {
			// User cancelled the dialog
			return
		}

		// Store the selected directory URI
		v.SelectedDirectoryURI = uri

		// Update the label to show the selected path
		// uri.Path() gives the full filesystem path
		v.DirectoryLabel.SetText(uri.Path())
	}, v.State.Window)

	// Set the dialog to start at user's home directory
	// You can also set a different starting location if desired
	// Get the user's home directory
	homePath, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Failed to get home directory: %v", err)
	} else {
		// Create a File URI for the home directory
		homeURI := storage.NewFileURI(homePath)

		// Get a ListableURI for the folder dialog
		homeDir, err := storage.ListerForURI(homeURI)
		if err != nil {
			log.Printf("Failed to get ListableURI for %s: %v", homePath, err)
		} else {
			folderDialog.SetLocation(homeDir) // no type assertion needed
		}
	}

	// Show the dialog
	folderDialog.Show()
}

// onSiteSelected is called when the user selects a site from the dropdown.
// This shows/hides input fields based on the site's requirements.
//
// Parameters:
//   - selected: The display name of the selected site
func (v *AddMangaView) onSiteSelected(selected string) {
	// Find the selected site in our configuration
	var selectedSite models.Site
	for _, site := range v.SitesConfig.Sites {
		if site.DisplayName == selected {
			selectedSite = site
			break
		}
	}

	// Log the site requirements (for debugging)
	log.Printf("Selected site: %s\n", selectedSite.Name)
	// fmt.Printf("Required fields - URL: %v, Shortname: %v, Title: %v, Location: %v\n",
	// 	selectedSite.RequiredFields.URL,
	// 	selectedSite.RequiredFields.Shortname,
	// 	selectedSite.RequiredFields.Title,
	// 	selectedSite.RequiredFields.Location)

	// TODO: Implement dynamic field visibility
	// Show/hide shortname field based on selectedSite.RequiredFields.Shortname
	// Show/hide title field based on selectedSite.RequiredFields.Title
	// Show/hide location field based on selectedSite.RequiredFields.Location
}

// onAddButtonClicked is called when the user clicks the Add Manga button.
// This validates the input and adds the manga to the library.
func (v *AddMangaView) onAddButtonClicked() {
	selectedSite := v.SiteSelect.Selected

	// Get the field values safely
	title := ""
	if v.Title != nil {
		title = v.Title.Text
	}

	shortname := ""
	if v.ShortnameEntry != nil {
		shortname = v.ShortnameEntry.Text
	}

	url := ""
	if v.UrlEntry != nil {
		url = v.UrlEntry.Text
	}

	location := ""
	if v.SelectedDirectoryURI != nil {
		// removes the file:// from the beginning of the directory (linux only?)
		cleanedDirectory := strings.ReplaceAll(v.SelectedDirectoryURI.String(), "file://", "")
		location = fmt.Sprintf("%s/%s", cleanedDirectory, title)
	}

	// Validate the input using the validation package
	err := validation.ValidateAddManga(selectedSite, title, shortname, url, location, &v.SitesConfig)
	if err != nil {
		if v.State != nil && v.State.Window != nil {
			dialog.ShowError(err, v.State.Window) // UI-specific
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
		Shortname: shortname,
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
	if shortname != "" {
		successMsg += fmt.Sprintf("\nShort Name: %s", shortname)
	}

	dialog.ShowInformation("Success", successMsg, v.State.Window)

	// Clear the form
	v.clearForm()
}

// clearForm resets all input fields to their default state.
// This is called after successfully adding a manga.
func (v *AddMangaView) clearForm() {
	v.Title.SetText("")
	v.UrlEntry.SetText("")
	v.ShortnameEntry.SetText("")
	v.DirectoryLabel.SetText("No directory selected")
	v.SelectedDirectoryURI = nil
	// Note: We don't clear the site selection as users often add
	// multiple manga from the same site
}
