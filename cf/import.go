package cf

import (
	"fmt"
	//"log"
	"time"

	"golang.design/x/clipboard"
)

// ImportFromClipboard reads CF bypass data from the clipboard,
// parses it, and saves it to file. Returns the domain on success.
func ImportFromClipboard() (string, error) {
	logCF("ImportFromClipboard: Starting clipboard import")

	// Initialize clipboard
	if err := clipboard.Init(); err != nil {
		logCF("ImportFromClipboard: Failed to initialize clipboard: %v", err)
		LogCFImport("unknown", false, err)
		return "", fmt.Errorf("failed to initialize clipboard: %w", err)
	}

	// Read clipboard contents
	clipboardData := clipboard.Read(clipboard.FmtText)
	if len(clipboardData) == 0 {
		err := fmt.Errorf("clipboard is empty")
		logCF("ImportFromClipboard: %v", err)
		LogCFImport("unknown", false, err)
		return "", err
	}

	jsonData := string(clipboardData)
	logCF("ImportFromClipboard: Read %d bytes from clipboard", len(jsonData))

	// Parse JSON into BypassData
	data, err := ParseCapturedData(jsonData)
	if err != nil {
		logCF("ImportFromClipboard: Failed to parse clipboard data: %v", err)
		LogCFImport("unknown", false, err)
		return "", fmt.Errorf("failed to parse clipboard data: %w", err)
	}

	logCF("ImportFromClipboard: Successfully parsed data for domain=%s", data.Domain)
	logCF("ImportFromClipboard: Protection type=%s", data.Type)
	logCF("ImportFromClipboard: Total cookies=%d", len(data.AllCookies))
	logCF("ImportFromClipboard: Has Turnstile=%v", data.TurnstileToken != "")
	logCF("ImportFromClipboard: User-Agent=%s", data.Entropy.UserAgent)

	if data.CfClearance != "" {
		logCF("ImportFromClipboard: cf_clearance present (%d chars)", len(data.CfClearance))
		logCF("ImportFromClipboard: cf_clearance captured at=%s", data.CfClearanceCapturedAt.Format(time.RFC3339))
		logCF("ImportFromClipboard: cf_clearance URL=%s", data.CfClearanceUrl)
	}

	// Save to file
	if err := SaveToFile(data, data.Domain); err != nil {
		logCF("ImportFromClipboard: Failed to save data: %v", err)
		LogCFImport(data.Domain, false, err)
		return "", fmt.Errorf("failed to save data: %w", err)
	}

	logCF("ImportFromClipboard: Successfully saved bypass data for domain=%s (type=%s)", data.Domain, data.Type)
	LogCFImport(data.Domain, true, nil)

	return data.Domain, nil
}
