package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"kansho/bookmarks"
	"kansho/config"
	"kansho/models"
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
	siteSelect           *widget.Select   // Dropdown for site selection
	shortnameEntry       *widget.Entry    // Text input for manga shortname
	urlEntry             *widget.Entry    // Text input for manga URL
	directoryLabel       *widget.Label    // Label showing selected directory
	directoryButton      *widget.Button   // Button to open directory picker
	addButton            *widget.Button   // Button to add the manga
	selectedDirectoryURI fyne.ListableURI // Stores the selected directory URI

	// Container for dynamic fields that may be shown/hidden
	shortnameContainer *fyne.Container

	// state is a reference to the shared application state
	state *KanshoAppState

	// sitesConfig contains all supported manga sites and their requirements
	sitesConfig models.SitesConfig
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
func NewAddMangaView(state *KanshoAppState) *AddMangaView {
	view := &AddMangaView{
		state: state,
	}

	// Load the sites configuration
	// This tells us which manga sites are supported and what data they need
	view.sitesConfig = config.LoadSitesConfig()

	// Create a slice of site display names for the dropdown
	// Extract just the user-facing names from the config
	siteNames := make([]string, len(view.sitesConfig.Sites))
	for i, site := range view.sitesConfig.Sites {
		siteNames[i] = site.DisplayName
	}

	// Create the site selection dropdown
	view.siteSelect = widget.NewSelect(siteNames, func(selected string) {
		// This callback is triggered when the user selects a site
		view.onSiteSelected(selected)
	})
	view.siteSelect.PlaceHolder = "Site name"

	// Create the shortname input field
	view.shortnameEntry = widget.NewEntry()
	view.shortnameEntry.SetPlaceHolder("Short Name value")

	// Create the URL input field
	view.urlEntry = widget.NewEntry()
	view.urlEntry.SetPlaceHolder("Paste manga URL")

	// Create the directory selection label and button
	view.directoryLabel = widget.NewLabel("No directory selected")
	view.directoryLabel.Wrapping = fyne.TextTruncate // Truncate long paths with ellipsis
	view.directoryButton = widget.NewButton("Choose Directory...", func() {
		view.onDirectoryButtonClicked()
	})

	// Create the Add Manga button
	view.addButton = widget.NewButton("Add Manga", func() {
		view.onAddButtonClicked()
	})

	// Create the first row with Select Site and Short Name fields
	// Using NewGridWithColumns to get equal-width columns
	firstRow := container.NewGridWithColumns(2,
		container.NewVBox(
			widget.NewLabel("Select Site:"),
			view.siteSelect,
		),
		container.NewVBox(
			widget.NewLabel("Short Name:"),
			view.shortnameEntry,
		),
	)

	// Create the second row with URL field (spans full width)
	secondRow := container.NewVBox(
		widget.NewLabel("URL:"),
		view.urlEntry,
	)

	// Create the third row with Directory field (button + label)
	// Use a horizontal box to place the button and label side by side
	directoryRow := container.NewVBox(
		widget.NewLabel("Directory:"),
		container.NewBorder(nil, nil, view.directoryButton, nil, view.directoryLabel),
	)

	// Create container for the button, centered
	buttonRow := container.NewCenter(view.addButton)

	// Build the card content with horizontal layout
	cardContent := container.NewVBox(
		NewBoldLabel("Manage Manga"),
		NewSeparator(),
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
			dialog.ShowError(err, v.state.Window)
			return
		}

		if uri == nil {
			// User cancelled the dialog
			return
		}

		// Store the selected directory URI
		v.selectedDirectoryURI = uri

		// Update the label to show the selected path
		// uri.Path() gives the full filesystem path
		v.directoryLabel.SetText(uri.Path())
	}, v.state.Window)

	// Set the dialog to start at user's home directory
	// You can also set a different starting location if desired
	homeDir, err := storage.ListerForURI(storage.NewFileURI("~"))
	if err == nil {
		if listable, ok := homeDir.(fyne.ListableURI); ok {
			folderDialog.SetLocation(listable)
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
	for _, site := range v.sitesConfig.Sites {
		if site.DisplayName == selected {
			selectedSite = site
			break
		}
	}

	// Log the site requirements (for debugging)
	fmt.Printf("Selected site: %s\n", selectedSite.Name)
	fmt.Printf("Required fields - URL: %v, Shortname: %v, Title: %v, Location: %v\n",
		selectedSite.RequiredFields.URL,
		selectedSite.RequiredFields.Shortname,
		selectedSite.RequiredFields.Title,
		selectedSite.RequiredFields.Location)

	// TODO: Implement dynamic field visibility
	// Show/hide shortname field based on selectedSite.RequiredFields.Shortname
	// Show/hide title field based on selectedSite.RequiredFields.Title
	// Show/hide location field based on selectedSite.RequiredFields.Location
}

// onAddButtonClicked is called when the user clicks the Add Manga button.
// This validates the input and adds the manga to the library.
func (v *AddMangaView) onAddButtonClicked() {
	// Get the entered values
	url := v.urlEntry.Text
	shortname := v.shortnameEntry.Text

	// Validate that a site is selected
	if v.siteSelect.Selected == "" {
		dialog.ShowInformation(
			"Add Manga",
			"Please select a site first.",
			v.state.Window,
		)
		return
	}

	// Validate that the URL is not empty
	if url == "" {
		dialog.ShowInformation(
			"Add Manga",
			"Please provide Manga URL.",
			v.state.Window,
		)
		return
	}

	// Validate that a directory has been selected
	if v.selectedDirectoryURI == nil {
		dialog.ShowInformation(
			"Add Manga",
			"Please select a target directory.",
			v.state.Window,
		)
		return
	}

	// TODO: Implement proper validation based on site requirements
	// TODO: Extract manga title from URL or require user input
	// TODO: Create subdirectory based on manga name under target directory

	// For now, create a basic manga entry
	// Use shortname as title if provided, otherwise use default
	title := "New Manga"
	if shortname != "" {
		title = shortname
	}

	newManga := bookmarks.Bookmarks{
		Title: title,
		Url:   url,
		Site:  v.siteSelect.Selected,
		// TODO: Store the directory path in your bookmarks structure
		// You'll need to add a Location/Directory field to bookmarks.Bookmarks
	}

	// Add the manga to the app state
	// This will trigger callbacks that refresh the manga list
	v.state.AddManga(newManga)

	// Show success dialog
	successMsg := fmt.Sprintf("Manga added successfully!\n\nSite: %s\nURL: %s\nDirectory: %s",
		v.siteSelect.Selected, url, v.selectedDirectoryURI.Path())
	if shortname != "" {
		successMsg += fmt.Sprintf("\nShort Name: %s", shortname)
	}

	dialog.ShowInformation(
		"Success",
		successMsg,
		v.state.Window,
	)

	// Clear the form after successful addition
	v.clearForm()
}

// clearForm resets all input fields to their default state.
// This is called after successfully adding a manga.
func (v *AddMangaView) clearForm() {
	v.urlEntry.SetText("")
	v.shortnameEntry.SetText("")
	v.directoryLabel.SetText("No directory selected")
	v.selectedDirectoryURI = nil
	// Note: We don't clear the site selection as users often add
	// multiple manga from the same site
}
