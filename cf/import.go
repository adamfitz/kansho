package cf

import (
	"fmt"
	"log"

	"golang.design/x/clipboard"
)

// ImportFromClipboard reads cf bypass data from the clipboard,
// parses it, and saves it to file. Returns the domain on success.
func ImportFromClipboard() (string, error) {
	// Initialize clipboard
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

	// Parse JSON into BypassData
	data, err := ParseCapturedData(jsonData)
	if err != nil {
		return "", fmt.Errorf("failed to parse clipboard data: %w", err)
	}

	log.Printf("Parsed bypass data for domain: %s", data.Domain)
	log.Printf("  - Protection type: %s", data.Type)

	if len(data.Cookies) > 0 {
		log.Printf("  - cf cookies: %d", len(data.Cookies))
		log.Printf("  - Total cookies: %d", len(data.AllCookies))
	}

	if data.TurnstileToken != "" {
		log.Printf("  - Turnstile token present")
	}

	log.Printf("  - User agent: %s", data.Entropy.UserAgent)
	log.Printf("  - cfClearance: %s", data.CfClearance)
	log.Printf("  - cfClearanceCapturedAt: %s", data.CfClearanceCapturedAt)
	log.Printf("  - cfClearanceUrl: %s", data.CfClearanceUrl)

	// Save to file
	if err := SaveToFile(data, data.Domain); err != nil {
		return "", fmt.Errorf("failed to save data: %w", err)
	}

	log.Printf("Saved bypass data for domain: %s (type: %s)", data.Domain, data.Type)
	return data.Domain, nil
}
