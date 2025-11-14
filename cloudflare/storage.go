package cloudflare

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CapturedData represents the JSON structure from the browser extension
type CapturedData struct {
	CapturedAt string            `json:"capturedAt"`
	URL        string            `json:"url"`
	Domain     string            `json:"domain"`
	Cookies    []Cookie          `json:"cookies"`
	AllCookies []Cookie          `json:"allCookies"`
	Entropy    Entropy           `json:"entropy"`
	Headers    map[string]string `json:"headers"`
}

// Cookie represents a browser cookie
type Cookie struct {
	Name           string  `json:"name"`
	Value          string  `json:"value"`
	Domain         string  `json:"domain"`
	Path           string  `json:"path"`
	Secure         bool    `json:"secure"`
	HTTPOnly       bool    `json:"httpOnly"`
	SameSite       string  `json:"sameSite"`
	ExpirationDate float64 `json:"expirationDate"` // Unix timestamp
}

// Entropy represents browser fingerprint data
type Entropy struct {
	UserAgent           string           `json:"userAgent"`
	Language            string           `json:"language"`
	Languages           []string         `json:"languages"`
	Platform            string           `json:"platform"`
	HardwareConcurrency int              `json:"hardwareConcurrency"`
	DeviceMemory        float64          `json:"deviceMemory"`
	ScreenResolution    ScreenResolution `json:"screenResolution"`
	Timezone            string           `json:"timezone"`
	TimezoneOffset      int              `json:"timezoneOffset"`
	WebGL               *WebGLInfo       `json:"webgl"`
}

// ScreenResolution represents screen properties
type ScreenResolution struct {
	Width      int `json:"width"`
	Height     int `json:"height"`
	ColorDepth int `json:"colorDepth"`
	PixelDepth int `json:"pixelDepth"`
}

// WebGLInfo represents GPU information
type WebGLInfo struct {
	Vendor   string `json:"vendor"`
	Renderer string `json:"renderer"`
}

// ParseCapturedData parses the JSON from clipboard into CapturedData struct
func ParseCapturedData(jsonData string) (*CapturedData, error) {
	var data CapturedData

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate required fields
	if data.Domain == "" {
		return nil, fmt.Errorf("domain is empty")
	}

	if len(data.Cookies) == 0 && len(data.AllCookies) == 0 {
		return nil, fmt.Errorf("no cookies found in captured data")
	}

	return &data, nil
}

// SaveToFile saves the captured data to a JSON file
// The file is saved in the user's config directory
func SaveToFile(data *CapturedData, domain string) error {
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
func LoadFromFile(domain string) (*CapturedData, error) {
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
	var data CapturedData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &data, nil
}

// IsExpired checks if the captured data is too old
// Cloudflare tokens typically expire after a few hours
func (c *CapturedData) IsExpired(maxAge time.Duration) bool {
	capturedTime, err := time.Parse(time.RFC3339, c.CapturedAt)
	if err != nil {
		return true // If we can't parse the time, consider it expired
	}

	return time.Since(capturedTime) > maxAge
}

// GetCookieString returns all cookies formatted as a Cookie header value
// e.g., "cookie1=value1; cookie2=value2"
func (c *CapturedData) GetCookieString() string {
	var cookieStrings []string

	// Use all cookies, not just Cloudflare ones
	// Some sites require additional session cookies
	for _, cookie := range c.AllCookies {
		cookieStrings = append(cookieStrings, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
	}

	return fmt.Sprintf("%s", cookieStrings)
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
