package cf

import (
	"encoding/json"
	"fmt"
	//"log"
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
		logCF("ParseCfClearanceCookie: Empty input string")
		return nil, fmt.Errorf("cfClearance string is empty")
	}

	logCF("ParseCfClearanceCookie: Parsing cookie string (%d chars)", len(raw))

	parts := strings.Split(raw, ";")
	cookie := &CfClearanceCookie{
		HttpOnly: false,
		Secure:   false,
	}

	for i, part := range parts {
		part = strings.TrimSpace(part)
		if i == 0 {
			if !strings.HasPrefix(part, "cf_clearance=") {
				logCF("ParseCfClearanceCookie: Invalid format - doesn't start with 'cf_clearance='")
				return nil, fmt.Errorf("invalid cf_clearance format")
			}
			cookie.Name = "cf_clearance"
			cookie.Value = strings.TrimPrefix(part, "cf_clearance=")
			logCF("ParseCfClearanceCookie: Extracted value (%d chars)", len(cookie.Value))
			continue
		}

		switch {
		case strings.EqualFold(part, "HttpOnly"):
			cookie.HttpOnly = true
			logCF("ParseCfClearanceCookie: HttpOnly=true")
		case strings.EqualFold(part, "Secure"):
			cookie.Secure = true
			logCF("ParseCfClearanceCookie: Secure=true")
		case strings.EqualFold(part, "Partitioned"):
			logCF("ParseCfClearanceCookie: Partitioned attribute detected")
		case strings.HasPrefix(strings.ToLower(part), "path="):
			cookie.Path = strings.TrimPrefix(part, "Path=")
			logCF("ParseCfClearanceCookie: Path=%s", cookie.Path)
		case strings.HasPrefix(strings.ToLower(part), "domain="):
			cookie.Domain = strings.TrimPrefix(part, "Domain=")
			logCF("ParseCfClearanceCookie: Domain=%s", cookie.Domain)
		case strings.HasPrefix(strings.ToLower(part), "expires="):
			expiresStr := strings.TrimPrefix(part, "Expires=")
			t, err := time.Parse(time.RFC1123, expiresStr)
			if err != nil {
				logCF("ParseCfClearanceCookie: Failed to parse Expires: %v", err)
			} else {
				cookie.Expires = &t
				logCF("ParseCfClearanceCookie: Expires=%s", t.Format(time.RFC3339))
			}
		case strings.HasPrefix(strings.ToLower(part), "samesite="):
			cookie.SameSite = strings.TrimPrefix(part, "SameSite=")
			logCF("ParseCfClearanceCookie: SameSite=%s", cookie.SameSite)
		default:
			logCF("ParseCfClearanceCookie: Unrecognized attribute: %q", part)
		}
	}

	logCF("ParseCfClearanceCookie: Successfully parsed cookie")
	return cookie, nil
}

// ParseCfClearance extracts raw cf_clearance value only
func ParseCfClearance(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("cfClearance string is empty")
	}
	parts := strings.Split(raw, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "cf_clearance=") {
			value := strings.TrimPrefix(part, "cf_clearance=")
			logCF("ParseCfClearance: Extracted value (%d chars)", len(value))
			return value, nil
		}
	}
	logCF("ParseCfClearance: cf_clearance field not found")
	return "", fmt.Errorf("cf_clearance field not found")
}

// ParseCapturedData parses the JSON from clipboard into BypassData struct
func ParseCapturedData(jsonData string) (*BypassData, error) {
	logCF("ParseCapturedData: Starting parse (%d bytes)", len(jsonData))

	var data BypassData

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		logCF("ParseCapturedData: JSON unmarshal failed: %v", err)
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if data.Domain == "" {
		logCF("ParseCapturedData: Domain is empty")
		return nil, fmt.Errorf("domain is empty")
	}
	logCF("ParseCapturedData: Domain=%s", data.Domain)

	data.Type = data.DetermineProtectionType()
	logCF("ParseCapturedData: Protection type=%s", data.Type)

	if data.Type == ProtectionNone {
		logCF("ParseCapturedData: No valid bypass data found")
		return nil, fmt.Errorf("no valid bypass data found (no cookies or turnstile tokens)")
	}

	// Parse cf_clearance into structured cookie
	rawCF := ""
	if v, ok := data.Headers["cfClearance"]; ok && v != "" {
		rawCF = v
		logCF("ParseCapturedData: Found cfClearance in headers")
	}
	if rawCF == "" && data.CfClearance != "" {
		rawCF = data.CfClearance
		logCF("ParseCapturedData: Using top-level cfClearance")
	}

	if rawCF != "" {
		cookie, err := ParseCfClearanceCookie(rawCF)
		if err != nil {
			logCF("ParseCapturedData: Failed to parse cfClearance cookie: %v", err)
		} else {
			data.CfClearanceStruct = cookie
			data.CfClearance = cookie.Value
			logCF("ParseCapturedData: Successfully parsed CfClearanceStruct")
		}
	} else {
		logCF("ParseCapturedData: No cfClearance string found")
	}

	// Parse cfClearanceCapturedAt
	if ts := data.CfClearanceCapturedAt.Format(time.RFC3339Nano); ts != "" {
		t, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			logCF("ParseCapturedData: Failed to parse cfClearanceCapturedAt: %v", err)
		} else {
			data.CfClearanceCapturedAt = t
			logCF("ParseCapturedData: Captured at: %s", t.Format(time.RFC3339))
		}
	}

	logCF("ParseCapturedData: Total cookies=%d, Turnstile=%v",
		len(data.AllCookies), data.TurnstileToken != "")

	return &data, nil
}

// SaveToFile saves the captured data to a JSON file
func SaveToFile(data *BypassData, domain string) error {
	logCF("SaveToFile: Saving bypass data for domain=%s", domain)

	configDir, err := os.UserConfigDir()
	if err != nil {
		logCF("SaveToFile: Failed to get config directory: %v", err)
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	cfDir := filepath.Join(configDir, "kansho", "cf")
	if err := os.MkdirAll(cfDir, 0755); err != nil {
		logCF("SaveToFile: Failed to create directory: %v", err)
		return fmt.Errorf("failed to create directory: %w", err)
	}

	filename := filepath.Join(cfDir, fmt.Sprintf("%s.json", domain))
	logCF("SaveToFile: Target file=%s", filename)

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		logCF("SaveToFile: JSON marshal failed: %v", err)
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	if err := os.WriteFile(filename, jsonData, 0644); err != nil {
		logCF("SaveToFile: File write failed: %v", err)
		return fmt.Errorf("failed to write file: %w", err)
	}

	logCF("SaveToFile: Successfully saved (%d bytes)", len(jsonData))
	LogCFCookieData(domain, data)
	return nil
}

// LoadFromFile loads captured data for a specific domain
func LoadFromFile(domain string) (*BypassData, error) {
	logCF("LoadFromFile: Loading bypass data for domain=%s", domain)

	configDir, err := os.UserConfigDir()
	if err != nil {
		logCF("LoadFromFile: Failed to get config directory: %v", err)
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	filename := filepath.Join(configDir, "kansho", "cf", fmt.Sprintf("%s.json", domain))

	if _, err := os.Stat(filename); os.IsNotExist(err) {
		logCF("LoadFromFile: No data file found for domain=%s", domain)
		return nil, fmt.Errorf("no cf data found for domain: %s", domain)
	}

	jsonData, err := os.ReadFile(filename)
	if err != nil {
		logCF("LoadFromFile: File read failed: %v", err)
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var data BypassData
	if err := json.Unmarshal(jsonData, &data); err != nil {
		logCF("LoadFromFile: JSON unmarshal failed: %v", err)
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	logCF("LoadFromFile: Successfully loaded (%d bytes)", len(jsonData))
	LogCFCookieData(domain, &data)

	return &data, nil
}

// ValidateCookieData checks if stored cookie data is still usable
func ValidateCookieData(data *BypassData) error {
	logCF("ValidateCookieData: Starting validation")

	validationErrors := []string{}

	if data == nil {
		err := "bypass data is nil"
		validationErrors = append(validationErrors, err)
		LogCFValidation("unknown", false, validationErrors)
		return fmt.Errorf(err)
	}

	// Check if data is too old (24 hours max age)
	if data.CapturedAt != "" {
		capturedTime, err := time.Parse(time.RFC3339, data.CapturedAt)
		if err == nil {
			age := time.Since(capturedTime)
			if age > 24*time.Hour {
				errMsg := fmt.Sprintf("bypass data is too old: %v (max: 24h)", age.Round(time.Minute))
				validationErrors = append(validationErrors, errMsg)
				LogCFValidation(data.Domain, false, validationErrors)
				return fmt.Errorf(errMsg)
			}
			logCF("ValidateCookieData: Bypass data age: %v (OK)", age.Round(time.Minute))
		}
	}

	// Validate cf_clearance cookie specifically
	if data.CfClearanceStruct != nil {
		// Check expiry
		if data.CfClearanceStruct.Expires != nil {
			now := time.Now()
			if now.After(*data.CfClearanceStruct.Expires) {
				errMsg := fmt.Sprintf("cf_clearance cookie has expired at %v",
					data.CfClearanceStruct.Expires.Format(time.RFC3339))
				validationErrors = append(validationErrors, errMsg)
				LogCFValidation(data.Domain, false, validationErrors)
				return fmt.Errorf(errMsg)
			}

			// Warn if expiring soon (within 1 hour)
			timeLeft := time.Until(*data.CfClearanceStruct.Expires)
			if timeLeft < time.Hour {
				logCF("ValidateCookieData: ⚠️  cf_clearance expires soon: %v", timeLeft.Round(time.Minute))
			} else {
				logCF("ValidateCookieData: cf_clearance valid for: %v", timeLeft.Round(time.Hour))
			}
		}

		// Validate cookie value format
		if data.CfClearanceStruct.Value == "" {
			errMsg := "cf_clearance value is empty"
			validationErrors = append(validationErrors, errMsg)
			LogCFValidation(data.Domain, false, validationErrors)
			return fmt.Errorf(errMsg)
		}

		// Check domain matches
		if data.CfClearanceStruct.Domain == "" {
			errMsg := "cf_clearance domain is empty"
			validationErrors = append(validationErrors, errMsg)
			LogCFValidation(data.Domain, false, validationErrors)
			return fmt.Errorf(errMsg)
		}

		logCF("ValidateCookieData: cf_clearance cookie structure: OK")
	} else {
		errMsg := "no cf_clearance cookie structure found"
		validationErrors = append(validationErrors, errMsg)
		LogCFValidation(data.Domain, false, validationErrors)
		return fmt.Errorf(errMsg)
	}

	// Check if cookie recently failed (within last 5 minutes)
	if failedAt, ok := data.Headers["_failed_at"]; ok {
		failTime, err := time.Parse(time.RFC3339, failedAt)
		if err == nil {
			timeSinceFail := time.Since(failTime)
			if timeSinceFail < 5*time.Minute {
				errMsg := fmt.Sprintf("cookie failed recently (%v ago), needs manual re-capture",
					timeSinceFail.Round(time.Second))
				validationErrors = append(validationErrors, errMsg)
				LogCFValidation(data.Domain, false, validationErrors)
				return fmt.Errorf(errMsg)
			}
			logCF("ValidateCookieData: Previous failure was %v ago (OK to retry)",
				timeSinceFail.Round(time.Minute))
		}
	}

	logCF("ValidateCookieData: ✓ All validation checks passed")
	LogCFValidation(data.Domain, true, nil)
	return nil
}

// MarkCookieAsFailed marks a cookie as having failed
func MarkCookieAsFailed(domain string) error {
	logCF("MarkCookieAsFailed: Marking cookie as failed for domain=%s", domain)

	data, err := LoadFromFile(domain)
	if err != nil {
		logCF("MarkCookieAsFailed: Failed to load data: %v", err)
		return err
	}

	// Add a failure marker to the data
	failTime := time.Now().Format(time.RFC3339)
	data.Headers["_failed_at"] = failTime
	logCF("MarkCookieAsFailed: Set failure time=%s", failTime)

	// Re-save with failure marker
	return SaveToFile(data, domain)
}

// ListStoredDomains returns a list of all domains that have stored CF data
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

	logCF("ListStoredDomains: Found %d stored domains", len(domains))
	return domains, nil
}

// DeleteDomain removes stored CF data for a specific domain
func DeleteDomain(domain string) error {
	logCF("DeleteDomain: Deleting data for domain=%s", domain)

	configDir, err := os.UserConfigDir()
	if err != nil {
		logCF("DeleteDomain: Failed to get config directory: %v", err)
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	filename := filepath.Join(configDir, "kansho", "cf", fmt.Sprintf("%s.json", domain))

	if err := os.Remove(filename); err != nil {
		if os.IsNotExist(err) {
			logCF("DeleteDomain: No data found for domain=%s", domain)
			return fmt.Errorf("no data found for domain: %s", domain)
		}
		logCF("DeleteDomain: Failed to delete file: %v", err)
		return fmt.Errorf("failed to delete file: %w", err)
	}

	logCF("DeleteDomain: Successfully deleted data for domain=%s", domain)
	return nil
}
