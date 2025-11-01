package ui

import (
	"errors"
	//"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	//"fyne.io/fyne/v2/widget"
	"kansho/models"
)

// ValidateAddManga checks that all required fields for the selected site are present.
// It is nil-safe and will not panic if any widget is not initialized.
func ValidateAddManga(v *AddMangaView) error {
	// Check that a site is selected
	if v.siteSelect == nil || v.siteSelect.Selected == "" {
		return errors.New("please select a site")
	}
	selectedSiteName := v.siteSelect.Selected

	// Find site rules from SitesConfig
	var siteRules *models.RequiredFields
	for _, s := range v.sitesConfig.Sites {
		if s.Name == selectedSiteName {
			siteRules = &s.RequiredFields
			break
		}
	}
	if siteRules == nil {
		return errors.New("unknown site: " + selectedSiteName)
	}

	// Validate each required field safely
	if siteRules.Title {
		if v.Title == nil || v.Title.Text == "" {
			return errors.New("title is required")
		}
	}
	if siteRules.URL {
		if v.urlEntry == nil || v.urlEntry.Text == "" {
			return errors.New("URL is required")
		}
	}
	if siteRules.Shortname {
		if v.shortnameEntry == nil || v.shortnameEntry.Text == "" {
			return errors.New("shortname is required")
		}
	}
	if siteRules.Location {
		if v.selectedDirectoryURI == nil || v.selectedDirectoryURI.String() == "" {
			return errors.New("location is required")
		}
	}

	return nil
}

// Optional helper: Show validation errors in a dialog
func ValidateAddMangaWithDialog(v *AddMangaView) bool {
	if err := ValidateAddManga(v); err != nil {
		if v.state != nil && v.state.Window != nil {
			dialog.ShowError(err, v.state.Window)
		}
		return false
	}
	return true
}
