package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"kansho/cf"

	"github.com/gocolly/colly"
)

// HTTPClient is a unified HTTP client that handles CF bypass, retries, and decompression
type HTTPClient struct {
	domain      string
	bypassData  *cf.BypassData
	needsCF     bool
	httpClient  *http.Client
	maxRetries  int
	baseTimeout time.Duration

	// DEBUG FLAGS
	DebugSaveHTML     bool
	DebugSaveHTMLPath string
}

// NewHTTPClient creates a new unified HTTP client for a specific domain
func NewHTTPClient(domain string, needsCF bool) (*HTTPClient, error) {
	client := &HTTPClient{
		domain:      domain,
		needsCF:     needsCF,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		maxRetries:  5,
		baseTimeout: 10 * time.Second,
	}

	// Load CF bypass data if needed
	if needsCF {
		data, err := cf.LoadFromFile(domain)
		if err != nil {
			// Don't return error - we'll try without bypass and handle CF challenges
			log.Printf("[HTTPClient] No CF bypass data for %s: %v", domain, err)
		} else {
			// Validate bypass data
			if err := cf.ValidateCookieData(data); err != nil {
				log.Printf("[HTTPClient] CF bypass data invalid: %v", err)
				cf.MarkCookieAsFailed(domain)
			} else {
				client.bypassData = data
				log.Printf("[HTTPClient] ✓ Loaded CF bypass for %s", domain)
			}
		}
	}

	return client, nil
}

// FetchHTML fetches HTML content from a URL with automatic retry and CF handling
func (c *HTTPClient) FetchHTML(ctx context.Context, targetURL string) (string, error) {
	var lastErr error

	for attempt := 0; attempt < c.maxRetries; attempt++ {
		timeout := c.baseTimeout + (time.Duration(attempt) * 5 * time.Second)

		if attempt > 0 {
			log.Printf("[HTTPClient] Retry attempt %d/%d (timeout: %v) for: %s",
				attempt+1, c.maxRetries, timeout, targetURL)
		}

		// Create context with timeout
		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		html, err := c.fetchHTMLAttempt(reqCtx, targetURL)

		// Success!
		if html != "" {
			preview := html
			if len(preview) > 1024 {
				preview = preview[:1024]
			}
			log.Printf("[HTTPClient][DEBUG] HTML preview (%d bytes):\n%s\n---END PREVIEW---",
				len(preview), preview)
		}

		if err == nil {
			if attempt > 0 {
				log.Printf("[HTTPClient] ✓ Success after %d retries", attempt+1)
			}
			return html, nil
		}

		// Check if it's a CF challenge - don't retry, return immediately
		if cfErr, isCfErr := err.(*cf.CfChallengeError); isCfErr {
			log.Printf("[HTTPClient] CF challenge detected, opening browser")
			return "", cfErr
		}

		// Check if it's a timeout
		isTimeout := strings.Contains(err.Error(), "context deadline exceeded") ||
			strings.Contains(err.Error(), "Client.Timeout exceeded")

		lastErr = err

		// If not a timeout, don't retry
		if !isTimeout {
			log.Printf("[HTTPClient] Non-timeout error, not retrying: %v", err)
			return "", err
		}

		log.Printf("[HTTPClient] ⚠️ Timeout on attempt %d/%d: %v", attempt+1, c.maxRetries, err)

		// Exponential backoff before retry
		if attempt < c.maxRetries-1 {
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			log.Printf("[HTTPClient] Waiting %v before retry...", backoff)
			time.Sleep(backoff)
		}
	}

	log.Printf("[HTTPClient] ✗ Failed after %d attempts", c.maxRetries)
	return "", fmt.Errorf("failed after %d retries: %w", c.maxRetries, lastErr)
}

// fetchHTMLAttempt performs a single HTTP request attempt
func (c *HTTPClient) fetchHTMLAttempt(ctx context.Context, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Apply CF bypass headers/cookies if available
	if c.bypassData != nil {
		c.applyCFBypass(req, targetURL)
	} else {
		// Use generic browser headers
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/114.0.0.0 Safari/537.36")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Decompress if needed
	contentEncoding := resp.Header.Get("Content-Encoding")
	decompressed, wasCompressed, err := cf.DecompressResponseBody(bodyBytes, contentEncoding)
	if err != nil {
		return "", fmt.Errorf("failed to decompress response: %w", err)
	}

	if wasCompressed {
		log.Printf("[HTTPClient] ✓ Decompressed response: %d → %d bytes", len(bodyBytes), len(decompressed))
		bodyBytes = decompressed
	}

	// DEBUG: Decompressed preview only
	decPreview := bodyBytes
	if len(decPreview) > 1024 {
		decPreview = decPreview[:1024]
	}
	log.Printf("\n[HTTPClient][DEBUG] DECOMPRESSED RESPONSE (%d bytes):\n%s\n--- END DECOMPRESSED PREVIEW ---\n",
		len(decPreview), string(decPreview))

	// OPTIONAL: Save full HTML to file
	if c.DebugSaveHTML && c.DebugSaveHTMLPath != "" {
		err := os.WriteFile(c.DebugSaveHTMLPath, bodyBytes, 0644)
		if err != nil {
			log.Printf("[HTTPClient][DEBUG] Failed to save HTML: %v", err)
		} else {
			log.Printf("[HTTPClient][DEBUG] Saved full HTML to %s", c.DebugSaveHTMLPath)
		}
	}

	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Check for CF challenge
	isCF, cfInfo, err := cf.Detectcf(resp)
	if err != nil {
		return "", fmt.Errorf("CF detection error: %w", err)
	}

	if isCF {
		log.Printf("[HTTPClient] ⚠️ Cloudflare challenge detected!")

		// Mark stored data as failed if we had any
		if c.bypassData != nil {
			cf.MarkCookieAsFailed(c.domain)
			cf.DeleteDomain(c.domain)
		}

		// Open browser for manual solve
		challengeURL := cf.GetChallengeURL(cfInfo, targetURL)
		if err := cf.OpenInBrowser(challengeURL); err != nil {
			return "", fmt.Errorf("CF detected but failed to open browser: %w", err)
		}

		return "", &cf.CfChallengeError{
			URL:        challengeURL,
			StatusCode: cfInfo.StatusCode,
			Indicators: cfInfo.Indicators,
		}
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return string(bodyBytes), nil
}

// applyCFBypass applies CF bypass data to an HTTP request
func (c *HTTPClient) applyCFBypass(req *http.Request, targetURL string) {
	// Set User-Agent
	req.Header.Set("User-Agent", c.bypassData.Entropy.UserAgent)

	// Add cf_clearance cookie if available
	if c.bypassData.CfClearanceStruct != nil {
		cookie := &http.Cookie{
			Name:     c.bypassData.CfClearanceStruct.Name,
			Value:    c.bypassData.CfClearanceStruct.Value,
			Domain:   c.bypassData.CfClearanceStruct.Domain,
			Path:     c.bypassData.CfClearanceStruct.Path,
			Secure:   c.bypassData.CfClearanceStruct.Secure,
			HttpOnly: c.bypassData.CfClearanceStruct.HttpOnly,
		}
		req.AddCookie(cookie)
	}

	// Add all other cookies
	for _, ck := range c.bypassData.AllCookies {
		if ck.Name != "" && ck.Name != "cf_clearance" {
			req.AddCookie(&http.Cookie{
				Name:   ck.Name,
				Value:  ck.Value,
				Domain: ck.Domain,
				Path:   ck.Path,
			})
		}
	}

	// Set browser-like headers
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Accept-Language", c.bypassData.Headers["acceptLanguage"])
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")

	// Chrome-specific headers
	if strings.Contains(c.bypassData.Entropy.UserAgent, "Chrome") {
		req.Header.Set("sec-ch-ua", `"Chromium";v="142", "Not_A Brand";v="99"`)
		req.Header.Set("sec-ch-ua-mobile", "?0")
		req.Header.Set("sec-ch-ua-platform", fmt.Sprintf(`"%s"`, c.bypassData.Entropy.Platform))
	}
}

// CreateCollyCollector creates a Colly collector with CF bypass applied
func (c *HTTPClient) CreateCollyCollector() *colly.Collector {
	collector := colly.NewCollector(
		colly.AllowURLRevisit(),
	)

	// Set timeout
	collector.SetRequestTimeout(30 * time.Second)

	// Apply CF bypass if available
	if c.bypassData != nil {
		// Set User-Agent
		collector.UserAgent = c.bypassData.Entropy.UserAgent

		// Add cookies
		var cookies []*http.Cookie

		if c.bypassData.CfClearanceStruct != nil {
			cookies = append(cookies, &http.Cookie{
				Name:     c.bypassData.CfClearanceStruct.Name,
				Value:    c.bypassData.CfClearanceStruct.Value,
				Domain:   c.bypassData.CfClearanceStruct.Domain,
				Path:     c.bypassData.CfClearanceStruct.Path,
				Secure:   c.bypassData.CfClearanceStruct.Secure,
				HttpOnly: c.bypassData.CfClearanceStruct.HttpOnly,
			})
		}

		for _, ck := range c.bypassData.AllCookies {
			if ck.Name != "" && ck.Name != "cf_clearance" {
				cookies = append(cookies, &http.Cookie{
					Name:   ck.Name,
					Value:  ck.Value,
					Domain: ck.Domain,
					Path:   ck.Path,
				})
			}
		}

		// Apply cookies on every request
		collector.OnRequest(func(r *colly.Request) {
			for _, cookie := range cookies {
				r.Headers.Set("Cookie", fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
			}

			// Set browser-like headers
			r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
			r.Headers.Set("Accept-Encoding", "gzip, deflate, br")
			r.Headers.Set("Accept-Language", c.bypassData.Headers["acceptLanguage"])
			r.Headers.Set("Connection", "keep-alive")
			r.Headers.Set("Upgrade-Insecure-Requests", "1")
			r.Headers.Set("Sec-Fetch-Dest", "document")
			r.Headers.Set("Sec-Fetch-Mode", "navigate")
			r.Headers.Set("Sec-Fetch-Site", "none")
			r.Headers.Set("Sec-Fetch-User", "?1")

			if strings.Contains(c.bypassData.Entropy.UserAgent, "Chrome") {
				r.Headers.Set("sec-ch-ua", `"Chromium";v="142", "Not_A Brand";v="99"`)
				r.Headers.Set("sec-ch-ua-mobile", "?0")
				r.Headers.Set("sec-ch-ua-platform", fmt.Sprintf(`"%s"`, c.bypassData.Entropy.Platform))
			}
		})

		log.Printf("[HTTPClient] ✓ Created Colly collector with CF bypass")
	} else {
		collector.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"
	}

	// Add automatic decompression
	collector.OnResponse(func(r *colly.Response) {
		if _, err := cf.DecompressResponse(r, "[HTTPClient]"); err != nil {
			log.Printf("[HTTPClient] Failed to decompress: %v", err)
		}
	})

	return collector
}

// GetDomain returns the domain this client is configured for
func (c *HTTPClient) GetDomain() string {
	return c.domain
}
