package cf

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/gocolly/colly"
)

// ApplyTurnstileBypass applies Turnstile token to Colly collector
// This makes the collector send a POST request with the Turnstile response
func ApplyTurnstileBypass(c *colly.Collector, data *BypassData, targetURL string) error {
	if !data.HasTurnstile() {
		return fmt.Errorf("no Turnstile data available")
	}

	log.Printf("✓ Applying Turnstile bypass for %s", data.Domain)
	log.Printf("  Challenge token: %s", data.ChallengeToken)
	log.Printf("  Form fields: %d", len(data.TurnstileFormData))

	// Set User-Agent from captured entropy
	c.UserAgent = data.Entropy.UserAgent

	// Build form data from captured Turnstile tokens
	formData := url.Values{}
	for key, value := range data.TurnstileFormData {
		if key != "_form_action" { // Skip our internal metadata
			formData.Set(key, value)
		}
		if len(value) > 50 {
			log.Printf("    • %s=%s... (%d chars)", key, value[:50], len(value))
		}
	}

	// Set common headers
	c.OnRequest(func(r *colly.Request) {
		// Set headers to match browser
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
		r.Headers.Set("Accept-Language", data.Headers["acceptLanguage"])
		r.Headers.Set("Cache-Control", "max-age=0")
		r.Headers.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Site", "same-origin")
		r.Headers.Set("Sec-Fetch-User", "?1")
		r.Headers.Set("Upgrade-Insecure-Requests", "1")

		// Chrome-specific headers
		if strings.Contains(data.Entropy.UserAgent, "Chrome") {
			r.Headers.Set("sec-ch-ua", `"Chromium";v="142", "Not_A Brand";v="99"`)
			r.Headers.Set("sec-ch-ua-mobile", "?0")
			r.Headers.Set("sec-ch-ua-platform", fmt.Sprintf(`"%s"`, data.Entropy.Platform))
		}

		// Set referrer to the challenge URL if we have the token
		if data.ChallengeToken != "" {
			refURL := fmt.Sprintf("%s?__cf_chl_tk=%s", targetURL, data.ChallengeToken)
			r.Headers.Set("Referer", refURL)
		}
	})

	log.Printf("✓ Turnstile bypass configured")
	return nil
}

// MakeTurnstileRequest makes a POST request with Turnstile data
// This is a helper for direct HTTP requests (not using Colly)
func MakeTurnstileRequest(client *http.Client, data *BypassData, targetURL string) (*http.Response, error) {
	if !data.HasTurnstile() {
		return nil, fmt.Errorf("no Turnstile data available")
	}

	// Build form data
	formData := url.Values{}
	for key, value := range data.TurnstileFormData {
		if key != "_form_action" {
			formData.Set(key, value)
		}
	}

	// Create POST request with form data
	req, err := http.NewRequest("POST", targetURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}

	// Set headers
	req.Header.Set("User-Agent", data.Entropy.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", data.Headers["acceptLanguage"])
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cache-Control", "max-age=0")

	if data.ChallengeToken != "" {
		refURL := fmt.Sprintf("%s?__cf_chl_tk=%s", targetURL, data.ChallengeToken)
		req.Header.Set("Referer", refURL)
	}

	return client.Do(req)
}

// PostWithTurnstile is a Colly-compatible helper to make POST requests
// Use this after configuring the collector with ApplyTurnstileBypass
func PostWithTurnstile(c *colly.Collector, data *BypassData, targetURL string) error {
	// Build form data
	formData := make(map[string]string)
	for key, value := range data.TurnstileFormData {
		if key != "_form_action" {
			formData[key] = value
		}
	}

	log.Printf("Making POST request with Turnstile data to: %s", targetURL)

	// Make POST request
	return c.Post(targetURL, formData)
}
