package models

// RequiredFields defines which input fields are required when adding manga from a specific site.
// This allows the UI to dynamically show/hide input fields based on what each site needs.
// For example, some sites might require a shortname while others don't.
type RequiredFields struct {
	URL       bool `yaml:"url"`       // Whether the manga URL is required
	Shortname bool `yaml:"shortname"` // Whether a short identifier is required
	Title     bool `yaml:"title"`     // Whether the manga title is required
	Location  bool `yaml:"location"`  // Whether a location/path is required
}

// Site represents a manga source website configuration.
// Each site has different requirements for what data is needed to track manga.
// The DisplayName is shown to users, while Name is used internally.
type Site struct {
	Name           string         `yaml:"name"`            // Internal identifier (e.g., "mangadex")
	DisplayName    string         `yaml:"display_name"`    // User-facing name (e.g., "MangaDex")
	RequiredFields RequiredFields `yaml:"required_fields"` // Which fields this site requires
}

// SitesConfig represents the root structure of the sites.yml configuration file.
// This file contains all supported manga sites and their requirements.
type SitesConfig struct {
	Sites []Site `yaml:"sites"` // Array of all configured manga sites
}
