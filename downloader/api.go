package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"kansho/cf"

	"github.com/gocolly/colly"
)

// APIClient handles API-based extraction using colly for better CF support
type APIClient struct {
	domain    string
	collector *colly.Collector
	needsCF   bool
}

// NewAPIClient creates a new API client for a specific domain
func NewAPIClient(domain string, needsCF bool) (*APIClient, error) {
	collector := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"),
		colly.AllowURLRevisit(),
	)

	collector.SetRequestTimeout(30 * time.Second)

	client := &APIClient{
		domain:    domain,
		collector: collector,
		needsCF:   needsCF,
	}

	// Apply CF bypass if needed
	if needsCF {
		if err := client.applyCFBypass(); err != nil {
			log.Printf("[APIClient] Warning: Could not apply CF bypass: %v", err)
			// Don't fail - continue without bypass
		}
	}

	return client, nil
}

// applyCFBypass applies CF bypass data to the collector
func (c *APIClient) applyCFBypass() error {
	bypassData, err := cf.LoadFromFile(c.domain)
	if err != nil {
		return fmt.Errorf("no CF bypass data: %w", err)
	}

	// Validate bypass data
	if err := cf.ValidateCookieData(bypassData); err != nil {
		cf.MarkCookieAsFailed(c.domain)
		return fmt.Errorf("CF bypass data invalid: %w", err)
	}

	// Apply to collector
	if err := cf.ApplyToCollector(c.collector, "https://"+c.domain); err != nil {
		return fmt.Errorf("failed to apply bypass to collector: %w", err)
	}

	log.Printf("[APIClient] ✓ Applied CF bypass for %s", c.domain)
	return nil
}

// FetchJSON makes an API request and unmarshals the JSON response
func (c *APIClient) FetchJSON(ctx context.Context, url string, result interface{}) error {
	var responseData []byte
	var statusCode int
	var fetchErr error

	c.collector.OnResponse(func(r *colly.Response) {
		statusCode = r.StatusCode
		responseData = r.Body

		// Try to decompress if needed
		if decompressed, err := cf.DecompressResponse(r, "[APIClient]"); err != nil {
			log.Printf("[APIClient] Failed to decompress response: %v", err)
		} else if decompressed {
			responseData = r.Body
		}

		// Check for CF challenge
		isCF, cfInfo, _ := cf.DetectFromColly(r)
		if isCF {
			log.Printf("[APIClient] ⚠️ Cloudflare challenge detected")
			if c.needsCF {
				cf.MarkCookieAsFailed(c.domain)
				cf.DeleteDomain(c.domain)
			}

			challengeURL := cf.GetChallengeURL(cfInfo, url)
			cf.OpenInBrowser(challengeURL)

			fetchErr = &cf.CfChallengeError{
				URL:        challengeURL,
				StatusCode: cfInfo.StatusCode,
				Indicators: cfInfo.Indicators,
			}
		}
	})

	c.collector.OnError(func(r *colly.Response, err error) {
		fetchErr = fmt.Errorf("request failed: %w", err)

		// Check for CF challenge on error
		isCF, cfInfo, _ := cf.DetectFromColly(r)
		if isCF {
			log.Printf("[APIClient] CF challenge detected on error")
			challengeURL := cf.GetChallengeURL(cfInfo, url)
			cf.OpenInBrowser(challengeURL)

			fetchErr = &cf.CfChallengeError{
				URL:        challengeURL,
				StatusCode: cfInfo.StatusCode,
				Indicators: cfInfo.Indicators,
			}
		}
	})

	// Make the request
	if err := c.collector.Visit(url); err != nil {
		return fmt.Errorf("failed to visit URL: %w", err)
	}

	// Wait for async operations
	c.collector.Wait()

	// Check for errors
	if fetchErr != nil {
		return fetchErr
	}

	if statusCode != 200 {
		return fmt.Errorf("API returned status %d: %s", statusCode, string(responseData))
	}

	// Unmarshal JSON
	if err := json.Unmarshal(responseData, result); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// FetchRaw makes an API request and returns the raw response body
func (c *APIClient) FetchRaw(ctx context.Context, url string) ([]byte, error) {
	var responseData []byte
	var statusCode int
	var fetchErr error

	c.collector.OnResponse(func(r *colly.Response) {
		statusCode = r.StatusCode
		responseData = r.Body

		// Try to decompress if needed
		if decompressed, err := cf.DecompressResponse(r, "[APIClient]"); err != nil {
			log.Printf("[APIClient] Failed to decompress response: %v", err)
		} else if decompressed {
			responseData = r.Body
		}

		// Check for CF challenge
		isCF, cfInfo, _ := cf.DetectFromColly(r)
		if isCF {
			log.Printf("[APIClient] ⚠️ Cloudflare challenge detected")
			if c.needsCF {
				cf.MarkCookieAsFailed(c.domain)
				cf.DeleteDomain(c.domain)
			}

			challengeURL := cf.GetChallengeURL(cfInfo, url)
			cf.OpenInBrowser(challengeURL)

			fetchErr = &cf.CfChallengeError{
				URL:        challengeURL,
				StatusCode: cfInfo.StatusCode,
				Indicators: cfInfo.Indicators,
			}
		}
	})

	c.collector.OnError(func(r *colly.Response, err error) {
		fetchErr = fmt.Errorf("request failed: %w", err)

		// Check for CF challenge on error
		isCF, cfInfo, _ := cf.DetectFromColly(r)
		if isCF {
			log.Printf("[APIClient] CF challenge detected on error")
			challengeURL := cf.GetChallengeURL(cfInfo, url)
			cf.OpenInBrowser(challengeURL)

			fetchErr = &cf.CfChallengeError{
				URL:        challengeURL,
				StatusCode: cfInfo.StatusCode,
				Indicators: cfInfo.Indicators,
			}
		}
	})

	// Make the request
	if err := c.collector.Visit(url); err != nil {
		return nil, fmt.Errorf("failed to visit URL: %w", err)
	}

	// Wait for async operations
	c.collector.Wait()

	// Check for errors
	if fetchErr != nil {
		return nil, fetchErr
	}

	if statusCode != 200 {
		return nil, fmt.Errorf("API returned status %d: %s", statusCode, string(responseData))
	}

	return responseData, nil
}
