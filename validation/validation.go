package validation

import (
	"errors"
	"kansho/models"
)

// ValidateAddManga checks that all required fields for the selected site are present.
// It only works with raw values, no Fyne types, so there’s no import cycle.
func ValidateAddManga(
	siteName string,
	title string,
	shortname string,
	url string,
	location string,
	config *models.SitesConfig,
) error {
	if siteName == "" {
		return errors.New("please select a site")
	}

	// Find the site rules
	var rules *models.RequiredFields
	for _, s := range config.Sites {
		if s.Name == siteName {
			rules = &s.RequiredFields
			break
		}
	}
	if rules == nil {
		return errors.New("unknown site: " + siteName)
	}

	// Validate each required field
	if rules.Title && title == "" {
		return errors.New("title is required")
	}
	if rules.URL && url == "" {
		return errors.New("URL is required")
	}
	if rules.Shortname && shortname == "" {
		return errors.New("shortname is required")
	}
	if rules.Location && location == "" {
		return errors.New("location is required")
	}

	return nil
}
