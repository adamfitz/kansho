package cloudflare

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
	// Parse the target URL to get the domain
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	domain := parsedURL.Hostname()

	// Try to load stored bypass data
	data, err := LoadFromFile(domain)
	if err != nil {
		log.Printf("No bypass data found for domain: %s", domain)
		return nil // Not an error - just means no data exists yet
	}

	// Check if data is expired
	if data.IsExpired(2 * time.Hour) {
		log.Printf("⚠️ Bypass data for %s is expired (captured at: %s)", domain, data.CapturedAt)
		log.Printf("   You may need to re-import fresh data")
		// Continue anyway - expired data might still work
	} else {
		log.Printf("✓ Using bypass data for %s (captured at: %s)", domain, data.CapturedAt)
	}

	log.Printf("  Protection type: %s", data.Type)

	// Apply the appropriate bypass method
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

	// Set User-Agent from captured entropy
	c.UserAgent = data.Entropy.UserAgent
	log.Printf("  Set User-Agent: %s", data.Entropy.UserAgent)

	// Add all cookies to the collector
	log.Printf("  Adding %d cookies:", len(data.AllCookies))
	hasCFClearance := false
	for _, cookie := range data.AllCookies {
		// Convert our Cookie struct to http.Cookie
		httpCookie := &http.Cookie{
			Name:   cookie.Name,
			Value:  cookie.Value,
			Path:   cookie.Path,
			Domain: cookie.Domain,
			Secure: cookie.Secure,
		}

		// Set expiration if provided
		if cookie.ExpirationDate > 0 {
			httpCookie.Expires = time.Unix(int64(cookie.ExpirationDate), 0)
		}

		// Add cookie to collector
		c.SetCookies(targetURL, []*http.Cookie{httpCookie})

		// Log ALL cookies for debugging
		log.Printf("    • %s=%s... (domain: %s)", cookie.Name, cookie.Value[:min(20, len(cookie.Value))], cookie.Domain)

		// Check for critical Cloudflare cookies
		if cookie.Name == "cf_clearance" {
			hasCFClearance = true
			log.Printf("    ✓ Found cf_clearance cookie!")
		}
		if strings.Contains(strings.ToLower(cookie.Name), "cf_") {
			log.Printf("    ✓ Found CF cookie: %s", cookie.Name)
		}
	}

	if !hasCFClearance {
		log.Printf("  ⚠️ WARNING: cf_clearance cookie NOT found in stored data!")
		log.Printf("  ⚠️ This means the challenge may not have been completed when data was captured")
		log.Printf("  ⚠️ You may need to re-capture data AFTER completing the challenge")
	}

	// Set additional headers
	c.OnRequest(func(r *colly.Request) {
		// Accept-Language
		if data.Headers["acceptLanguage"] != "" {
			r.Headers.Set("Accept-Language", data.Headers["acceptLanguage"])
		}

		// Add common browser headers to look more legitimate
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
		r.Headers.Set("Accept-Encoding", "gzip, deflate, br")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "none")
		r.Headers.Set("Sec-Fetch-User", "?1")

		// Chrome/Chromium specific headers
		if strings.Contains(data.Entropy.UserAgent, "Chrome") {
			r.Headers.Set("sec-ch-ua", `"Chromium";v="130", "Not?A_Brand";v="99"`)
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
