package cf

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CfClearanceCookie represents a structured cf_clearance cookie
type CfClearanceCookie struct {
	Name     string     `json:"name"`
	Value    string     `json:"value"`
	Domain   string     `json:"domain,omitempty"`
	Path     string     `json:"path,omitempty"`
	Expires  *time.Time `json:"expires,omitempty"`
	HttpOnly bool       `json:"httpOnly"`
	Secure   bool       `json:"secure"`
	SameSite string     `json:"sameSite,omitempty"`
}

// ParseCfClearanceCookie parses the raw cf_clearance string into a structured cookie
func ParseCfClearanceCookie(raw string) (*CfClearanceCookie, error) {
	if raw == "" {
		return nil, fmt.Errorf("cfClearance string is empty")
	}

	log.Printf("DEBUG raw cf_clearance: %q", raw)

	parts := strings.Split(raw, ";")
	cookie := &CfClearanceCookie{
		HttpOnly: false,
		Secure:   false,
	}

	for i, part := range parts {
		part = strings.TrimSpace(part)
		if i == 0 {
			if !strings.HasPrefix(part, "cf_clearance=") {
				return nil, fmt.Errorf("invalid cf_clearance format")
			}
			cookie.Name = "cf_clearance"
			cookie.Value = strings.TrimPrefix(part, "cf_clearance=")
			continue
		}

		switch {
		case strings.EqualFold(part, "HttpOnly"):
			cookie.HttpOnly = true
		case strings.EqualFold(part, "Secure"):
			cookie.Secure = true
		case strings.EqualFold(part, "Partitioned"):
			// Partitioned is informational only; you could store it if desired
		case strings.HasPrefix(strings.ToLower(part), "path="):
			cookie.Path = strings.TrimPrefix(part, "Path=")
		case strings.HasPrefix(strings.ToLower(part), "domain="):
			cookie.Domain = strings.TrimPrefix(part, "Domain=")
		case strings.HasPrefix(strings.ToLower(part), "expires="):
			expiresStr := strings.TrimPrefix(part, "Expires=")
			t, err := time.Parse(time.RFC1123, expiresStr)
			if err != nil {
				log.Printf("warning: failed to parse cookie Expires: %v", err)
			} else {
				cookie.Expires = &t
			}
		case strings.HasPrefix(strings.ToLower(part), "samesite="):
			cookie.SameSite = strings.TrimPrefix(part, "SameSite=")
		default:
			log.Printf("DEBUG unrecognized cf_clearance attribute: %q", part)
		}
	}

	log.Printf("DEBUG parsed cookie: %+v", cookie)
	return cookie, nil
}

// --- convenience helper: extracts raw cf_clearance value only ---
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
// ParseCapturedData parses the JSON from clipboard into BypassData struct
func ParseCapturedData(jsonData string) (*BypassData, error) {
	var data BypassData

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if data.Domain == "" {
		return nil, fmt.Errorf("domain is empty")
	}

	data.Type = data.DetermineProtectionType()
	if data.Type == ProtectionNone {
		return nil, fmt.Errorf("no valid bypass data found (no cookies or turnstile tokens)")
	}

	// --- parse cf_clearance into structured cookie ---
	rawCF := ""
	if v, ok := data.Headers["cfClearance"]; ok && v != "" {
		rawCF = v
	}
	if rawCF == "" && data.CfClearance != "" { // fix: also check top-level
		rawCF = data.CfClearance
	}

	if rawCF != "" {
		cookie, err := ParseCfClearanceCookie(rawCF)
		if err != nil {
			log.Printf("warning: failed to parse cfClearance: %v", err)
		} else {
			data.CfClearanceStruct = cookie
			data.CfClearance = cookie.Value // convenience field
			log.Printf("DEBUG parsed CfClearanceStruct: %+v", cookie)
		}
	} else {
		log.Printf("DEBUG no cfClearance string found to parse")
	}

	// parse cfClearanceCapturedAt
	if ts := data.CfClearanceCapturedAt.Format(time.RFC3339Nano); ts != "" {
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			log.Printf("warning: failed to parse cfClearanceCapturedAt: %v", err)
		} else {
			data.CfClearanceCapturedAt = t
		}
	}

	// cfClearanceUrl is already top-level
	if data.CfClearanceUrl != "" {
		// already set
	}

	// --- DEBUG: show everything before saving ---
	log.Printf("DEBUG BEFORE SAVE:")
	log.Printf("  - Domain: %s", data.Domain)
	log.Printf("  - Type: %s", data.Type)
	log.Printf("  - Total cookies: %d", len(data.AllCookies))
	log.Printf("  - Turnstile token: %v", data.TurnstileToken != "")
	log.Printf("  - User agent: %s", data.Entropy.UserAgent)
	log.Printf("  - cfClearance string: %s", data.CfClearance)
	if data.CfClearanceStruct != nil {
		log.Printf("  - CfClearanceStruct: %+v", *data.CfClearanceStruct)
	} else {
		log.Printf("  - CfClearanceStruct: <nil>")
	}
	log.Printf("  - cfClearanceCapturedAt: %s", data.CfClearanceCapturedAt)
	log.Printf("  - cfClearanceUrl: %s", data.CfClearanceUrl)

	return &data, nil
}

// SaveToFile saves the captured data to a JSON file
func SaveToFile(data *BypassData, domain string) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	cfDir := filepath.Join(configDir, "kansho", "cf")
	if err := os.MkdirAll(cfDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	filename := filepath.Join(cfDir, fmt.Sprintf("%s.json", domain))

	// --- DEBUG: Just before writing file ---
	log.Printf("DEBUG BEFORE SAVE: data.CfClearanceStruct = %+v", data.CfClearanceStruct)
	log.Printf("DEBUG BEFORE SAVE: data.CfClearance = %q", data.CfClearance)

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	// --- DEBUG: JSON content that will be written ---
	log.Printf("DEBUG JSON TO SAVE:\n%s", string(jsonData))

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

	filename := filepath.Join(configDir, "kansho", "cf", fmt.Sprintf("%s.json", domain))

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return nil, fmt.Errorf("no cf data found for domain: %s", domain)
	}

	jsonData, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var data BypassData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &data, nil
}

// ListStoredDomains returns a list of all domains that have stored cf data
func ListStoredDomains() ([]string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	cfDir := filepath.Join(configDir, "kansho", "cf")

	if _, err := os.Stat(cfDir); os.IsNotExist(err) {
		return []string{}, nil
	}

	entries, err := os.ReadDir(cfDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var domains []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			domain := entry.Name()[:len(entry.Name())-5]
			domains = append(domains, domain)
		}
	}

	return domains, nil
}

// DeleteDomain removes stored cf data for a specific domain
func DeleteDomain(domain string) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	filename := filepath.Join(configDir, "kansho", "cf", fmt.Sprintf("%s.json", domain))

	if err := os.Remove(filename); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no data found for domain: %s", domain)
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}
