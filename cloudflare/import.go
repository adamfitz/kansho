package cloudflare

import (
	"fmt"
	"log"

	"golang.design/x/clipboard"
)

// ImportFromClipboard reads Cloudflare data from clipboard and saves it
// Returns the domain name and any error
func ImportFromClipboard() (string, error) {
	// Initialize clipboard (required once per application)
	if err := clipboard.Init(); err != nil {
		return "", fmt.Errorf("failed to initialize clipboard: %w", err)
	}

	// Read clipboard contents
	clipboardData := clipboard.Read(clipboard.FmtText)
	if len(clipboardData) == 0 {
		return "", fmt.Errorf("clipboard is empty")
	}

	jsonData := string(clipboardData)
	log.Printf("Read %d bytes from clipboard", len(jsonData))

	// Parse the JSON
	data, err := ParseCapturedData(jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to parse clipboard data: %w", err)
	}

	log.Printf("Parsed Cloudflare data for domain: %s", data.Domain)
	log.Printf("  - Cloudflare cookies: %d", len(data.Cookies))
	log.Printf("  - Total cookies: %d", len(data.AllCookies))
	log.Printf("  - User agent: %s", data.Entropy.UserAgent)

	// Save to file
	if err := SaveToFile(data, data.Domain); err != nil {
		return "", fmt.Errorf("failed to save data: %w", err)
	}

	log.Printf("Saved Cloudflare data for domain: %s", data.Domain)

	return data.Domain, nil
}
