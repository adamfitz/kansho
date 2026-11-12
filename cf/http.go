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

	// Log info about the bypass data we're about to use.
	// We do NOT validate expiry or staleness here — the request result determines
	// whether the data is still good. Pre-flight time-based rejections cause false
	// positives when clock-derived expiry values are inaccurate.
	if data.Type == ProtectionCookie && data.CfClearanceStruct != nil {
		// Structural check only — domain mismatch is a hard error (wrong site token)
		if err := ValidateCookieData(data, domain); err != nil {
			log.Printf("⚠️ CF bypass data for %s is structurally invalid: %v", domain, err)
			return fmt.Errorf("cf bypass structurally invalid: %w", err)
		}
		if data.CfClearanceStruct.Expires != nil {
			expiresIn := time.Until(*data.CfClearanceStruct.Expires)
			if expiresIn < 0 {
				log.Printf("ℹ️ cf_clearance for %s has a past expiry timestamp (attempting anyway)", domain)
			} else {
				log.Printf("✓ Using cf_clearance for %s (expiry timestamp: %s remaining)", domain, expiresIn.Round(time.Hour))
			}
		}
	} else {
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
	// cf_clearance is exported on its own (partitioned cookies are invisible to
	// chrome.cookies.getAll), so accept data that carries only the structured
	// clearance with no additional cookies.
	if !data.HasCookies() && data.CfClearanceStruct == nil {
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
		// Skip cf_clearance only when the structured copy was already added.
		if cookie.Name == "cf_clearance" && hasCFClearance {
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
		if cookie.Name == "cf_clearance" {
			hasCFClearance = true
			log.Printf("    ✓ Added cf_clearance from allCookies: %s", cookie.Value[:min(20, len(cookie.Value))])
		} else {
			log.Printf("    • %s=%s...", cookie.Name, cookie.Value[:min(20, len(cookie.Value))])
		}
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

	// Add the structured cf_clearance first (clearance-only captures have no
	// entry in AllCookies).
	hasCFClearance := false
	if data.CfClearanceStruct != nil {
		req.AddCookie(&http.Cookie{
			Name:   data.CfClearanceStruct.Name,
			Value:  data.CfClearanceStruct.Value,
			Path:   data.CfClearanceStruct.Path,
			Domain: data.CfClearanceStruct.Domain,
		})
		hasCFClearance = true
	}

	// Add cookies
	for _, cookie := range data.AllCookies {
		if cookie.Name == "cf_clearance" && hasCFClearance {
			continue
		}
		req.AddCookie(&http.Cookie{
			Name:   cookie.Name,
			Value:  cookie.Value,
			Path:   cookie.Path,
			Domain: cookie.Domain,
		})
		if cookie.Name == "cf_clearance" {
			hasCFClearance = true
		}
	}

	// Set headers
	req.Header.Set("Accept-Language", data.Headers["acceptLanguage"])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	return client.Do(req)
}
