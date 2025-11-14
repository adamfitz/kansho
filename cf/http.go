package cf

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gocolly/colly"
)

// ApplyToCollector applies stored bypass data to a Colly collector
// Automatically detects and applies the appropriate bypass method
// ApplyToCollector applies stored bypass data to a Colly collector
func ApplyToCollector(c *colly.Collector, targetURL string) error {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	domain := parsedURL.Hostname()

	data, err := LoadFromFile(domain)
	if err != nil {
		log.Printf("No bypass data found for domain: %s", domain)
		return nil
	}

	// Check if cf_clearance cookie is expired (if it exists)
	if data.Type == ProtectionCookie && data.CfClearanceStruct != nil {
		if data.CfClearanceStruct.Expires != nil {
			if time.Now().After(*data.CfClearanceStruct.Expires) {
				log.Printf("⚠️ cf_clearance cookie for %s has EXPIRED (expired at: %s)",
					domain, data.CfClearanceStruct.Expires.Format(time.RFC3339))
				return fmt.Errorf("cf_clearance cookie expired")
			} else {
				expiresIn := time.Until(*data.CfClearanceStruct.Expires)
				log.Printf("✓ Using cf_clearance for %s (valid for: %s)", domain, expiresIn.Round(time.Hour))
			}
		}
	} else {
		// For non-cookie methods or if no expiry info, just log age
		capturedTime, _ := time.Parse(time.RFC3339, data.CapturedAt)
		age := time.Since(capturedTime)
		log.Printf("ℹ️ Using bypass data for %s (captured: %s ago)", domain, age.Round(time.Minute))
	}

	log.Printf("  Protection type: %s", data.Type)

	switch data.Type {
	case ProtectionTurnstile:
		return ApplyTurnstileBypass(c, data, targetURL)
	case ProtectionCookie:
		return ApplyCookieBypass(c, data, targetURL)
	default:
		return fmt.Errorf("unknown protection type: %s", data.Type)
	}
}

// ApplyCookieBypass applies cookie-based bypass (the original method)
func ApplyCookieBypass(c *colly.Collector, data *BypassData, targetURL string) error {
	if !data.HasCookies() {
		return fmt.Errorf("no cookie data available")
	}

	log.Printf("✓ Applying cookie-based bypass for %s", data.Domain)

	// Set User-Agent
	c.UserAgent = data.Entropy.UserAgent
	log.Printf("  Set User-Agent: %s", data.Entropy.UserAgent)

	// CRITICAL: Add cf_clearance from CfClearanceStruct if available
	hasCFClearance := false
	if data.CfClearanceStruct != nil {
		httpCookie := &http.Cookie{
			Name:     data.CfClearanceStruct.Name,
			Value:    data.CfClearanceStruct.Value,
			Path:     data.CfClearanceStruct.Path,
			Domain:   data.CfClearanceStruct.Domain,
			Secure:   data.CfClearanceStruct.Secure,
			HttpOnly: data.CfClearanceStruct.HttpOnly,
		}

		if data.CfClearanceStruct.Expires != nil {
			httpCookie.Expires = *data.CfClearanceStruct.Expires
		}

		c.SetCookies(targetURL, []*http.Cookie{httpCookie})
		hasCFClearance = true
		log.Printf("    ✓ Added cf_clearance from CfClearanceStruct: %s", data.CfClearanceStruct.Value[:min(20, len(data.CfClearanceStruct.Value))])
	}

	// Add remaining cookies from AllCookies
	log.Printf("  Adding %d additional cookies:", len(data.AllCookies))
	for _, cookie := range data.AllCookies {
		// Skip if this is cf_clearance (already added above)
		if cookie.Name == "cf_clearance" {
			continue
		}

		httpCookie := &http.Cookie{
			Name:   cookie.Name,
			Value:  cookie.Value,
			Path:   cookie.Path,
			Domain: cookie.Domain,
			Secure: cookie.Secure,
		}

		if cookie.ExpirationDate > 0 {
			httpCookie.Expires = time.Unix(int64(cookie.ExpirationDate), 0)
		}

		c.SetCookies(targetURL, []*http.Cookie{httpCookie})
		log.Printf("    • %s=%s...", cookie.Name, cookie.Value[:min(20, len(cookie.Value))])
	}

	if !hasCFClearance {
		log.Printf("  ⚠️ WARNING: cf_clearance cookie NOT found!")
		return fmt.Errorf("cf_clearance cookie missing from stored data")
	}

	// Rest of header setup remains the same...
	c.OnRequest(func(r *colly.Request) {
		if data.Headers["acceptLanguage"] != "" {
			r.Headers.Set("Accept-Language", data.Headers["acceptLanguage"])
		}
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Encoding", "gzip, deflate, br")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "none")
		r.Headers.Set("Sec-Fetch-User", "?1")

		if strings.Contains(data.Entropy.UserAgent, "Chrome") {
			r.Headers.Set("sec-ch-ua", `"Chromium";v="142", "Not_A Brand";v="99"`)
			r.Headers.Set("sec-ch-ua-mobile", "?0")
			r.Headers.Set("sec-ch-ua-platform", fmt.Sprintf(`"%s"`, data.Entropy.Platform))
		}
	})

	log.Printf("✓ Cookie bypass applied successfully")
	return nil
}

// Helper function for min (Go 1.21+ has this built-in)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MakeRequest makes an HTTP request with the appropriate bypass method
// This is for use with standard http.Client (not Colly)
func MakeRequest(client *http.Client, targetURL string) (*http.Response, error) {
	// Parse URL to get domain
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	domain := parsedURL.Hostname()

	// Load bypass data
	data, err := LoadFromFile(domain)
	if err != nil {
		return nil, fmt.Errorf("no bypass data found for domain: %s", domain)
	}

	// Use appropriate method
	switch data.Type {
	case ProtectionTurnstile:
		return MakeTurnstileRequest(client, data, targetURL)
	case ProtectionCookie:
		return makeCookieRequest(client, data, targetURL)
	default:
		return nil, fmt.Errorf("unknown protection type: %s", data.Type)
	}
}

// makeCookieRequest makes an HTTP request with cookies
func makeCookieRequest(client *http.Client, data *BypassData, targetURL string) (*http.Response, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, err
	}

	// Set User-Agent
	req.Header.Set("User-Agent", data.Entropy.UserAgent)

	// Add cookies
	for _, cookie := range data.AllCookies {
		req.AddCookie(&http.Cookie{
			Name:   cookie.Name,
			Value:  cookie.Value,
			Path:   cookie.Path,
			Domain: cookie.Domain,
		})
	}

	// Set headers
	req.Header.Set("Accept-Language", data.Headers["acceptLanguage"])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	return client.Do(req)
}
