package downloader

import (
	"context"
	"fmt"
	"log"
	"net/url"

	"kansho/cf"
)

// RequestExecutor decides the best method to fetch content (HTTP vs Browser)
// and handles the execution with appropriate retries and fallback
type RequestExecutor struct {
	domain     string
	httpClient *HTTPClient
	needsCF    bool
}

// NewRequestExecutor creates a new request executor
func NewRequestExecutor(targetURL string, needsCF bool) (*RequestExecutor, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	domain := parsedURL.Hostname()

	// Create HTTP client
	httpClient, err := NewHTTPClient(domain, needsCF)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &RequestExecutor{
		domain:     domain,
		httpClient: httpClient,
		needsCF:    needsCF,
	}, nil
}

// FetchHTML fetches HTML with automatic HTTP→Browser fallback
func (e *RequestExecutor) FetchHTML(ctx context.Context, targetURL string, waitSelector string) (string, error) {
	log.Printf("[Executor] Fetching: %s", targetURL)

	// Try HTTP first (fast and efficient)
	html, err := e.httpClient.FetchHTML(ctx, targetURL)

	// Success!
	if err == nil {
		log.Printf("[Executor] ✓ HTTP fetch successful")
		return html, nil
	}

	// Check if it's a CF challenge
	if cfErr, isCfErr := err.(*cf.CfChallengeError); isCfErr {
		log.Printf("[Executor] CF challenge detected - needs manual solve")
		return "", cfErr
	}

	// HTTP failed with a non-CF error - try browser fallback
	log.Printf("[Executor] HTTP failed (%v), trying browser fallback...", err)

	return e.fetchWithBrowser(ctx, targetURL, waitSelector)
}

// fetchWithBrowser falls back to browser-based fetching
func (e *RequestExecutor) fetchWithBrowser(ctx context.Context, targetURL string, waitSelector string) (string, error) {
	log.Printf("[Executor] Starting browser fetch for: %s", targetURL)

	session, err := NewBrowserSession(ctx, e.domain, e.needsCF)
	if err != nil {
		return "", fmt.Errorf("failed to create browser session: %w", err)
	}
	defer session.Close()

	if err := session.Navigate(targetURL, waitSelector); err != nil {
		return "", fmt.Errorf("browser navigation failed: %w", err)
	}

	html, err := session.GetHTML()
	if err != nil {
		return "", fmt.Errorf("failed to get HTML from browser: %w", err)
	}

	log.Printf("[Executor] ✓ Browser fetch successful")
	return html, nil
}

// GetHTTPClient returns the underlying HTTP client for advanced usage
func (e *RequestExecutor) GetHTTPClient() *HTTPClient {
	return e.httpClient
}
