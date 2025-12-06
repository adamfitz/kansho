package sites

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"kansho/models"
)

//go:embed sites.json
var embeddedFS embed.FS

// GetEmbeddedSitesJSON returns the raw content of the embedded sites.json file
// This allows other packages to access the embedded configuration
func GetEmbeddedSitesJSON() ([]byte, error) {
	return embeddedFS.ReadFile("sites.json")
}

// LoadSitesConfig loads the manga site configuration from config/sites.json.
// This configuration determines which manga sites are supported and what information
// is required when adding manga from each site.
//
// Returns:
//   - models.SitesConfig: The loaded configuration, or an empty config if loading fails
//
// File location: ./config/sites.json
// The function logs any errors to stdout but does not halt execution.
func LoadSitesConfig() models.SitesConfig {
	// Define the path to the sites configuration file
	sitesLocation := "./sites/sites.json"

	// Open the configuration file
	file, err := os.Open(sitesLocation)
	if err != nil {
		// Log the error but continue with empty config
		// This allows the app to start even if the config file is missing
		fmt.Printf("error loading sites config file: %v\n", err)
		return models.SitesConfig{}
	}
	defer file.Close()

	// Read the entire file content into memory
	byteValues, err := io.ReadAll(file)
	if err != nil {
		fmt.Printf("error reading sites config file: %v\n", err)
		return models.SitesConfig{}
	}

	// Parse the JSON content into our SitesConfig struct
	var sitesConfig models.SitesConfig
	if err := json.Unmarshal(byteValues, &sitesConfig); err != nil {
		fmt.Printf("error unmarshalling sites config: %v\n", err)
		return models.SitesConfig{}
	}

	return sitesConfig
}

// extractChapterNumber extracts the numeric chapter number from filenames like "ch001.cbz" or "ch091.2.cbz"
func extractChapterNumber(filename string) int {
	// Remove .cbz extension
	name := strings.TrimSuffix(filename, ".cbz")

	// Remove "ch" prefix
	name = strings.TrimPrefix(name, "ch")

	// Split on dots to get main chapter number
	parts := strings.Split(name, ".")
	if len(parts) == 0 {
		return 0
	}

	// Parse the first part as the chapter number
	var chapterNum int
	fmt.Sscanf(parts[0], "%d", &chapterNum)
	return chapterNum
}
