package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"kansho/bookmarks"
	"kansho/config"
	"kansho/models"
)

// AddMangaView represents the "Add Manga URL" card component.
// This view allows users to add new manga to their library by:
// 1. Selecting a manga site from a dropdown
// 2. Entering the manga URL (and potentially other fields based on site requirements)
// 3. Clicking the ADD URL button to save
//
// The form fields shown are dynamic based on the selected site's requirements.
// For example, some sites might need a shortname while others don't.
type AddMangaView struct {
	// Card is the complete UI component ready to be added to the layout
	Card fyne.CanvasObject

	// UI components that need to be accessed after creation
	siteSelect *widget.Select // Dropdown for site selection
	urlEntry   *widget.Entry  // Text input for manga URL
	addButton  *widget.Button // Button to add the manga

	// state is a reference to the shared application state
	state *KanshoAppState

	// sitesConfig contains all supported manga sites and their requirements
	sitesConfig models.SitesConfig
}

// NewAddMangaView creates a new "Add Manga URL" view component.
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
	view.siteSelect.PlaceHolder = "Select a site..."

	// Create the URL input field
	view.urlEntry = widget.NewEntry()
	view.urlEntry.SetPlaceHolder("Paste manga URL")

	// Create the ADD URL button
	view.addButton = widget.NewButton("ADD URL", func() {
		view.onAddButtonClicked()
	})

	// Build the card content
	// NewVBox arranges items vertically with spacing
	cardContent := container.NewVBox(
		NewBoldLabel("Add Manga URL"),
		NewSeparator(),
		layout.NewSpacer(),              // Push content down from top
		widget.NewLabel("Select Site:"), // Label for dropdown
		view.siteSelect,                 // Site dropdown
		widget.NewLabel("Enter URL:"),   // Label for URL field
		view.urlEntry,                   // URL input field
		view.addButton,                  // Add button
	)

	// Wrap the content in a card
	view.Card = NewCard(cardContent)

	return view
}

// onSiteSelected is called when the user selects a site from the dropdown.
// In the future, this will show/hide input fields based on the site's requirements.
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
	// In the future, this information will be used to dynamically show/hide form fields
	fmt.Printf("Selected site: %s\n", selectedSite.Name)
	fmt.Printf("Required fields - URL: %v, Shortname: %v, Title: %v, Location: %v\n",
		selectedSite.RequiredFields.URL,
		selectedSite.RequiredFields.Shortname,
		selectedSite.RequiredFields.Title,
		selectedSite.RequiredFields.Location)

	// TODO: Implement dynamic form fields
	// Future implementation:
	// - Show/hide shortname field based on selectedSite.RequiredFields.Shortname
	// - Show/hide title field based on selectedSite.RequiredFields.Title
	// - Show/hide location field based on selectedSite.RequiredFields.Location
	// - Update validation logic in onAddButtonClicked
}

// onAddButtonClicked is called when the user clicks the ADD URL button.
// This validates the input and adds the manga to the library.
func (v *AddMangaView) onAddButtonClicked() {
	// Get the entered URL
	url := v.urlEntry.Text

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
	// In the future, this will validate based on site requirements
	if url == "" {
		dialog.ShowInformation(
			"Add Manga",
			"Please provide Manga URL.",
			v.state.Window,
		)
		return
	}

	// TODO: Implement proper validation based on site requirements
	// TODO: Extract manga title from URL or require user input
	// TODO: Handle shortname and location fields when implemented

	// For now, create a basic manga entry
	// In the future, this will be more sophisticated and use site-specific parsing
	newManga := bookmarks.Bookmarks{
		Title: "New Manga", // TODO: Get real title
		Url:   url,
		Site:  v.siteSelect.Selected,
	}

	// Add the manga to the app state
	// This will trigger callbacks that refresh the manga list
	v.state.AddManga(newManga)

	// Show success dialog
	dialog.ShowInformation(
		"Success",
		fmt.Sprintf("Manga URL added successfully!\n\nSite: %s\nURL: %s",
			v.siteSelect.Selected, url),
		v.state.Window,
	)

	// Clear the form after successful addition
	v.clearForm()
}

// clearForm resets all input fields to their default state.
// This is called after successfully adding a manga.
func (v *AddMangaView) clearForm() {
	v.urlEntry.SetText("")
	// Note: We don't clear the site selection as users often add
	// multiple manga from the same site
}
