package sites

import (
	"embed"
	"encoding/json"
	"fmt"
	"strconv"
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

// LoadSitesConfig loads the manga site configuration from the embedded sites.json
// This configuration determines which manga sites are supported and what information
// is required when adding manga from each site
//
// Returns:
//   - models.SitesConfig: The loaded configuration, or an empty config if loading fails
//
// The sites.json file is embedded into the binary at compile time
func LoadSitesConfig() models.SitesConfig {
	// Get the embedded sites.json content
	byteValues, err := GetEmbeddedSitesJSON()
	if err != nil {
		fmt.Printf("error loading embedded sites config: %v\n", err)
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

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
