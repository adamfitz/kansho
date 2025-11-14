package cloudflare

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- helper to parse cf_clearance from raw cookie string ---
func ParseCfClearance(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("cfClearance string is empty")
	}
	parts := strings.Split(raw, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "cf_clearance=") {
			return strings.TrimPrefix(part, "cf_clearance="), nil
		}
	}
	return "", fmt.Errorf("cf_clearance field not found")
}

// ParseCapturedData parses the JSON from clipboard into BypassData struct
// ParseCapturedData parses JSON from clipboard into BypassData
func ParseCapturedData(jsonData string) (*BypassData, error) {
	var data BypassData

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate required fields
	if data.Domain == "" {
		return nil, fmt.Errorf("domain is empty")
	}

	// Determine protection type
	data.Type = data.DetermineProtectionType()
	if data.Type == ProtectionNone {
		return nil, fmt.Errorf("no valid bypass data found (no cookies or turnstile tokens)")
	}

	// --- Cloudflare fields ---
	// cfClearance
	if rawCF, ok := data.Headers["cfClearance"]; ok && rawCF != "" {
		data.CfClearanceRaw = rawCF
		cfVal, err := ParseCfClearance(rawCF)
		if err != nil {
			log.Printf("warning: failed to parse cfClearance: %v", err)
		} else {
			data.CfClearance = cfVal
		}
	}

	// cfClearanceCapturedAt
	if ts, ok := data.Headers["cfClearanceCapturedAt"]; ok && ts != "" {
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			log.Printf("warning: failed to parse cfClearanceCapturedAt: %v", err)
		} else {
			data.CfClearanceCapturedAt = t
		}
	}

	// cfClearanceUrl
	if url, ok := data.Headers["cfClearanceUrl"]; ok && url != "" {
		data.CfClearanceUrl = url
	}

	return &data, nil
}

// SaveToFile saves the captured data to a JSON file
func SaveToFile(data *BypassData, domain string) error {
	// Get config directory (e.g., ~/.config/kansho/ on Linux)
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	// Create kansho/cloudflare directory
	cfDir := filepath.Join(configDir, "kansho", "cloudflare")
	if err := os.MkdirAll(cfDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create filename based on domain
	// e.g., www.mgeko.cc.json
	filename := filepath.Join(cfDir, fmt.Sprintf("%s.json", domain))

	// Marshal to JSON with indentation
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// LoadFromFile loads captured data for a specific domain
func LoadFromFile(domain string) (*BypassData, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	filename := filepath.Join(configDir, "kansho", "cloudflare", fmt.Sprintf("%s.json", domain))

	// Check if file exists
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, fmt.Errorf("no cloudflare data found for domain: %s", domain)
	}

	// Read file
	jsonData, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Parse JSON
	var data BypassData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &data, nil
}

// ListStoredDomains returns a list of all domains that have stored Cloudflare data
func ListStoredDomains() ([]string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	cfDir := filepath.Join(configDir, "kansho", "cloudflare")

	// Check if directory exists
	if _, err := os.Stat(cfDir); os.IsNotExist(err) {
		return []string{}, nil // No domains stored yet
	}

	// Read directory
	entries, err := os.ReadDir(cfDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var domains []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			// Remove .json extension to get domain name
			domain := entry.Name()[:len(entry.Name())-5]
			domains = append(domains, domain)
		}
	}

	return domains, nil
}

// DeleteDomain removes stored Cloudflare data for a specific domain
func DeleteDomain(domain string) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	filename := filepath.Join(configDir, "kansho", "cloudflare", fmt.Sprintf("%s.json", domain))

	if err := os.Remove(filename); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no data found for domain: %s", domain)
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}
