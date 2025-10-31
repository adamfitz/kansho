package models

// RequiredFields defines which input fields are required when adding manga from a specific site.
// This allows the UI to dynamically show/hide input fields based on what each site needs.
// For example, some sites might require a shortname while others don't.
type RequiredFields struct {
	URL       bool `json:"url"`       // Whether the manga URL is required
	Shortname bool `json:"shortname"` // Whether a short identifier is required
	Title     bool `json:"title"`     // Whether the manga title is required
	Location  bool `json:"location"`  // Whether a location/path is required
}

// Site represents a manga source website configuration.
// Each site has different requirements for what data is needed to track manga.
// The DisplayName is shown to users, while Name is used internally.
type Site struct {
	Name           string         `json:"name"`            // Internal identifier (e.g., "mangadex")
	DisplayName    string         `json:"display_name"`    // User-facing name (e.g., "MangaDex")
	RequiredFields RequiredFields `json:"required_fields"` // Which fields this site requires
}

// SitesConfig represents the root structure of the sites.json configuration file.
// This file contains all supported manga sites and their requirements.
type SitesConfig struct {
	Sites []Site `json:"sites"` // Array of all configured manga sites
}
