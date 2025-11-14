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

// ApplyToCollector applies stored Cloudflare data to a Colly collector
// This sets cookies and headers to bypass Cloudflare protection
func ApplyToCollector(c *colly.Collector, targetURL string) error {
	// Parse the target URL to get the domain
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return fmt.Errorf("failed to parse URL: %w", err)
	}

	domain := parsedURL.Hostname()

	// Try to load stored Cloudflare data
	data, err := LoadFromFile(domain)
	if err != nil {
		// No stored data - this is OK, just means we haven't imported yet
		log.Printf("No Cloudflare data found for domain: %s", domain)
		return nil
	}

	// Check if data is expired (Cloudflare tokens typically last a few hours)
	if data.IsExpired(2 * time.Hour) {
		log.Printf("⚠️ Cloudflare data for %s is expired (captured at: %s)", domain, data.CapturedAt)
		log.Printf("   You may need to re-import fresh data")
		// Continue anyway - expired data might still work
	} else {
		log.Printf("✓ Using Cloudflare data for %s (captured at: %s)", domain, data.CapturedAt)
	}

	// Set User-Agent from captured entropy
	c.UserAgent = data.Entropy.UserAgent
	log.Printf("  Set User-Agent: %s", data.Entropy.UserAgent)

	// Add all cookies to the collector
	log.Printf("  Adding %d cookies", len(data.AllCookies))
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

		// Log Cloudflare-specific cookies
		if strings.Contains(strings.ToLower(cookie.Name), "cf_") ||
			strings.Contains(strings.ToLower(cookie.Name), "clearance") {
			log.Printf("    → CF Cookie: %s=%s...", cookie.Name, cookie.Value[:min(20, len(cookie.Value))])
		}
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

	log.Printf("✓ Cloudflare data applied successfully")
	return nil
}

// Helper function for min (Go 1.21+ has this built-in)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ApplyToHTTPClient applies stored Cloudflare data to a standard http.Client
// This is useful if you're not using Colly
func ApplyToHTTPClient(client *http.Client, targetURL string) (*http.Request, error) {
	// Parse the target URL to get the domain
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	domain := parsedURL.Hostname()

	// Try to load stored Cloudflare data
	data, err := LoadFromFile(domain)
	if err != nil {
		// No stored data
		return nil, fmt.Errorf("no Cloudflare data found for domain: %s", domain)
	}

	// Create request
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

	return req, nil
}
