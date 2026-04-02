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

		partLower := strings.ToLower(part)
		switch {
		case partLower == "httponly":
			cookie.HttpOnly = true
			logCF("ParseCfClearanceCookie: HttpOnly=true")
		case partLower == "secure":
			cookie.Secure = true
			logCF("ParseCfClearanceCookie: Secure=true")
		case partLower == "partitioned":
			logCF("ParseCfClearanceCookie: Partitioned attribute detected")
		case strings.HasPrefix(partLower, "path="):
			// Use the lowercased version to strip the key, preserving value casing
			cookie.Path = part[len("path="):]
			logCF("ParseCfClearanceCookie: Path=%s", cookie.Path)
		case strings.HasPrefix(partLower, "domain="):
			cookie.Domain = part[len("domain="):]
			logCF("ParseCfClearanceCookie: Domain=%s", cookie.Domain)
		case strings.HasPrefix(partLower, "expires="):
			expiresStr := part[len("expires="):]
			// Try multiple date formats Cloudflare uses
			var t time.Time
			var err error
			for _, layout := range []string{time.RFC1123, "Mon, 02-Jan-2006 15:04:05 MST", time.RFC850} {
				t, err = time.Parse(layout, expiresStr)
				if err == nil {
					break
				}
			}
			if err != nil {
				logCF("ParseCfClearanceCookie: Failed to parse Expires %q: %v", expiresStr, err)
			} else {
				cookie.Expires = &t
				logCF("ParseCfClearanceCookie: Expires=%s", t.Format(time.RFC3339))
			}
		case strings.HasPrefix(partLower, "samesite="):
			cookie.SameSite = part[len("samesite="):]
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

// ValidateCookieData performs structural validation only — it checks that the
// bypass data is present and structurally correct, but does NOT reject data
// based on timestamps, expiry dates, or previous failures.
//
// Expiry/staleness is determined empirically: the data is used in the actual
// request, and only marked as failed if the request itself triggers a CF
// challenge. This avoids false-positive rejections caused by inaccurate
// clock-based expiry values.
//
// targetDomain is the domain you intend to use the bypass for (e.g. "asuracomic.net").
// Pass an empty string to skip the domain match check (backwards-compatible).
func ValidateCookieData(data *BypassData, targetDomain ...string) error {
	logCF("ValidateCookieData: Starting structural validation")

	validationErrors := []string{}

	if data == nil {
		err := "bypass data is nil"
		validationErrors = append(validationErrors, err)
		LogCFValidation("unknown", false, validationErrors)
		return fmt.Errorf("%s", err)
	}

	// Log age for informational purposes only — not used to reject data.
	if data.CapturedAt != "" {
		if capturedTime, err := time.Parse(time.RFC3339, data.CapturedAt); err == nil {
			logCF("ValidateCookieData: Bypass data age: %v (informational only — not used for rejection)",
				time.Since(capturedTime).Round(time.Minute))
		}
	}

	// Validate cf_clearance structure
	if data.CfClearanceStruct != nil {
		// Log expiry for informational purposes only — not used to reject data.
		if data.CfClearanceStruct.Expires != nil {
			timeLeft := time.Until(*data.CfClearanceStruct.Expires)
			if timeLeft < 0 {
				logCF("ValidateCookieData: cf_clearance expiry timestamp is in the past (%v ago) — attempting anyway",
					(-timeLeft).Round(time.Minute))
			} else {
				logCF("ValidateCookieData: cf_clearance expiry: %v remaining (informational only)",
					timeLeft.Round(time.Hour))
			}
		}

		// Structural check: value must not be empty
		if data.CfClearanceStruct.Value == "" {
			errMsg := "cf_clearance value is empty"
			validationErrors = append(validationErrors, errMsg)
			LogCFValidation(data.Domain, false, validationErrors)
			return fmt.Errorf("%s", errMsg)
		}

		// Structural check: domain must be present
		if data.CfClearanceStruct.Domain == "" {
			errMsg := "cf_clearance domain is empty"
			validationErrors = append(validationErrors, errMsg)
			LogCFValidation(data.Domain, false, validationErrors)
			return fmt.Errorf("%s", errMsg)
		}

		// Domain mismatch check — a token for site A will be cryptographically
		// rejected by site B, so this is a hard structural error, not a staleness check.
		//
		// We accept two valid cases:
		//   1. Exact match:   cookie domain "example.com"  for target "example.com"
		//   2. Parent domain: cookie domain "example.com"  for target "www.example.com"
		//      (standard browser cookie scoping — a cookie issued for the apex domain
		//       is sent by the browser for all subdomains including www)
		//
		// We reject:
		//   - cookie domain "other.com" for target "example.com"  (completely different site)
		//   - cookie domain "sub.example.com" for target "example.com" (subdomain can't cover apex)
		if len(targetDomain) > 0 && targetDomain[0] != "" {
			target := targetDomain[0]
			cookieDomain := strings.TrimPrefix(data.CfClearanceStruct.Domain, ".")
			targetClean := strings.TrimPrefix(target, ".")

			exactMatch := cookieDomain == targetClean
			// Parent domain match: cookie is for "example.com", target is "sub.example.com"
			parentMatch := strings.HasSuffix(targetClean, "."+cookieDomain)

			if !exactMatch && !parentMatch {
				errMsg := fmt.Sprintf(
					"cf_clearance domain mismatch: cookie is for %q but target is %q — "+
						"you must solve the CF challenge on %s, not on another tab",
					cookieDomain, targetClean, targetClean,
				)
				logCF("ValidateCookieData: ⚠️  %s", errMsg)
				validationErrors = append(validationErrors, errMsg)
				LogCFValidation(data.Domain, false, validationErrors)
				return fmt.Errorf("%s", errMsg)
			}
			if parentMatch {
				logCF("ValidateCookieData: cf_clearance parent domain %q covers target %q: OK", cookieDomain, targetClean)
			} else {
				logCF("ValidateCookieData: cf_clearance domain matches target (%s): OK", targetClean)
			}
		}

		logCF("ValidateCookieData: ✓ cf_clearance structure OK")
	} else {
		errMsg := "no cf_clearance cookie structure found"
		validationErrors = append(validationErrors, errMsg)
		LogCFValidation(data.Domain, false, validationErrors)
		return fmt.Errorf("%s", errMsg)
	}

	// Log previous failure marker for informational purposes — not used to reject data.
	// The caller already cleared this by writing fresh data to disk; if the old marker
	// is still present it simply means the previous attempt failed, which we already know.
	if failedAt, ok := data.Headers["_failed_at"]; ok {
		if failTime, err := time.Parse(time.RFC3339, failedAt); err == nil {
			logCF("ValidateCookieData: Previous failure recorded %v ago (informational — attempting anyway)",
				time.Since(failTime).Round(time.Minute))
		}
	}

	logCF("ValidateCookieData: ✓ All structural checks passed")
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