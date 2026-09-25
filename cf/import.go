package cf

import (
	"fmt"
	"strings"
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

	return ImportFromClipboardString(string(clipboardData))
}

// ImportFromClipboardString parses CF bypass data from a JSON string and saves
// it to file. It rejects any cf_clearance whose domain does not match the
// domain the data was captured on, so a token stolen from another site can
// never be written to this site's file.
func ImportFromClipboardString(jsonData string) (string, error) {
	logCF("ImportFromClipboardString: Starting import (%d bytes)", len(jsonData))

	// Parse JSON into BypassData
	data, err := ParseCapturedData(jsonData)
	if err != nil {
		logCF("ImportFromClipboardString: Failed to parse clipboard data: %v", err)
		LogCFImport("unknown", false, err)
		return "", fmt.Errorf("failed to parse clipboard data: %w", err)
	}

	logCF("ImportFromClipboardString: Successfully parsed data for domain=%s", data.Domain)
	logCF("ImportFromClipboardString: Protection type=%s", data.Type)
	logCF("ImportFromClipboardString: Total cookies=%d", len(data.AllCookies))
	logCF("ImportFromClipboardString: Has Turnstile=%v", data.TurnstileToken != "")
	logCF("ImportFromClipboardString: User-Agent=%s", data.Entropy.UserAgent)

	if data.CfClearance != "" {
		logCF("ImportFromClipboardString: cf_clearance present (%d chars)", len(data.CfClearance))
		logCF("ImportFromClipboardString: cf_clearance captured at=%s", data.CfClearanceCapturedAt.Format(time.RFC3339))
		logCF("ImportFromClipboardString: cf_clearance URL=%s", data.CfClearanceUrl)
	}

	// Reject a cf_clearance issued for a different site. Cloudflare
	// cryptographically binds tokens to the domain they were issued for, so a
	// mismatched token is guaranteed to fail and must never be written to
	// this site's file.
	if err := ValidateImportData(data); err != nil {
		logCF("ImportFromClipboardString: ✗ %v", err)
		LogCFImport(data.Domain, false, err)
		return "", err
	}

	// Save to file
	if err := SaveToFile(data, data.Domain); err != nil {
		logCF("ImportFromClipboardString: Failed to save data: %v", err)
		LogCFImport(data.Domain, false, err)
		return "", fmt.Errorf("failed to save data: %w", err)
	}

	logCF("ImportFromClipboardString: Successfully saved bypass data for domain=%s (type=%s)", data.Domain, data.Type)
	LogCFImport(data.Domain, true, nil)

	return data.Domain, nil
}

// ValidateImportData checks parsed bypass data before it is written to disk.
// It rejects a cf_clearance issued for a site that differs from the domain the
// data was captured on, so wrong-domain tokens can never be persisted.
func ValidateImportData(data *BypassData) error {
	if data == nil {
		return fmt.Errorf("bypass data is nil")
	}
	if data.CfClearanceStruct != nil && data.CfClearanceStruct.Domain != "" &&
		!ClearanceDomainMatches(data.CfClearanceStruct.Domain, data.Domain) {
		return fmt.Errorf(
			"cf_clearance domain mismatch: cookie is for %q but data was captured on %q — "+
				"you must solve the CF challenge on %s, not on another tab",
			strings.TrimPrefix(data.CfClearanceStruct.Domain, "."),
			strings.TrimPrefix(data.Domain, "."),
			strings.TrimPrefix(data.Domain, "."),
		)
	}
	return nil
}
