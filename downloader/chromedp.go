package downloader

import (
	"context"
	"fmt"
	"log"
	"time"

	"kansho/cf"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// BrowserSession manages a chromedp browser context with CF bypass support
type BrowserSession struct {
	ctx        context.Context
	cancel     context.CancelFunc
	domain     string
	needsCF    bool
	bypassData *cf.BypassData
}

// NewBrowserSession creates a new browser session with optional CF bypass
func NewBrowserSession(ctx context.Context, domain string, needsCF bool) (*BrowserSession, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-gpu", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)

	session := &BrowserSession{
		ctx:     browserCtx,
		cancel:  func() { cancelBrowser(); cancelAlloc() },
		domain:  domain,
		needsCF: needsCF,
	}

	// Load CF bypass data if needed (no validation, just load if exists)
	if needsCF {
		data, err := cf.LoadFromFile(domain)
		if err != nil {
			log.Printf("[Browser:%s] No CF bypass data found, will attempt without cookies", domain)
		} else {
			session.bypassData = data
			log.Printf("[Browser:%s] ✓ Loaded CF bypass data (will inject on navigation)", domain)
		}
	}

	return session, nil
}

// NavigateAndEvaluate performs navigation, waiting, and JavaScript evaluation in a SINGLE chromedp.Run()
// This is critical - splitting these into separate Run() calls can cause context/state issues
func (bs *BrowserSession) NavigateAndEvaluate(url, waitSelector, javascript string, result interface{}) error {
	timeout := 60 * time.Second // Generous timeout for the entire operation
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	var tasks []chromedp.Action

	// If we have CF bypass data, inject cookies BEFORE navigation
	if bs.bypassData != nil {
		log.Printf("[Browser:%s] Injecting CF cookies before navigation", bs.domain)

		// Build cookie list
		var cookies []*network.CookieParam

		// Add cf_clearance
		if bs.bypassData.CfClearanceStruct != nil {
			cookie := &network.CookieParam{
				Name:     bs.bypassData.CfClearanceStruct.Name,
				Value:    bs.bypassData.CfClearanceStruct.Value,
				Domain:   bs.bypassData.CfClearanceStruct.Domain,
				Path:     bs.bypassData.CfClearanceStruct.Path,
				Secure:   bs.bypassData.CfClearanceStruct.Secure,
				HTTPOnly: bs.bypassData.CfClearanceStruct.HttpOnly,
			}

			cookies = append(cookies, cookie)
			log.Printf("[Browser:%s]   ✓ Added cf_clearance cookie", bs.domain)
		}

		// Add all other cookies
		for _, ck := range bs.bypassData.AllCookies {
			if ck.Name != "" && ck.Name != "cf_clearance" {
				cookie := &network.CookieParam{
					Name:   ck.Name,
					Value:  ck.Value,
					Domain: ck.Domain,
					Path:   ck.Path,
				}
				cookies = append(cookies, cookie)
			}
		}

		log.Printf("[Browser:%s] ✓ Injecting %d cookies", bs.domain, len(cookies))

		// Inject cookies
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookies(cookies).Do(ctx)
		}))
	}

	// Navigate
	tasks = append(tasks, chromedp.Navigate(url))

	// Wait for selector or body
	if waitSelector != "" {
		log.Printf("[Browser:%s] Waiting for selector: %s", bs.domain, waitSelector)
		tasks = append(tasks, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
	} else {
		tasks = append(tasks, chromedp.WaitReady("body"))
	}

	// Evaluate JavaScript
	log.Printf("[Browser:%s] Evaluating JavaScript", bs.domain)
	tasks = append(tasks, chromedp.Evaluate(javascript, result))

	// Execute ALL tasks in a SINGLE chromedp.Run() call
	// This is how the working CLI version does it!
	err := chromedp.Run(ctx, tasks...)
	if err != nil {
		return fmt.Errorf("navigation and evaluation failed: %w", err)
	}

	log.Printf("[Browser:%s] ✓ Navigation and JavaScript evaluation successful", bs.domain)
	return nil
}

// Navigate navigates to a URL and waits for page load with CF cookie injection
func (bs *BrowserSession) Navigate(url string, waitSelector string) error {
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	var tasks []chromedp.Action

	// If we have CF bypass data, inject cookies BEFORE navigation
	if bs.bypassData != nil {
		log.Printf("[Browser:%s] Injecting CF cookies before navigation", bs.domain)

		// Build cookie list
		var cookies []*network.CookieParam

		// Add cf_clearance
		if bs.bypassData.CfClearanceStruct != nil {
			cookie := &network.CookieParam{
				Name:     bs.bypassData.CfClearanceStruct.Name,
				Value:    bs.bypassData.CfClearanceStruct.Value,
				Domain:   bs.bypassData.CfClearanceStruct.Domain,
				Path:     bs.bypassData.CfClearanceStruct.Path,
				Secure:   bs.bypassData.CfClearanceStruct.Secure,
				HTTPOnly: bs.bypassData.CfClearanceStruct.HttpOnly,
			}

			cookies = append(cookies, cookie)
			log.Printf("[Browser:%s]   ✓ Added cf_clearance cookie", bs.domain)
		}

		// Add all other cookies
		for _, ck := range bs.bypassData.AllCookies {
			if ck.Name != "" && ck.Name != "cf_clearance" {
				cookie := &network.CookieParam{
					Name:   ck.Name,
					Value:  ck.Value,
					Domain: ck.Domain,
					Path:   ck.Path,
				}
				cookies = append(cookies, cookie)
			}
		}

		log.Printf("[Browser:%s] ✓ Injecting %d cookies", bs.domain, len(cookies))

		// Inject cookies
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			return network.SetCookies(cookies).Do(ctx)
		}))
	}

	// Navigate
	tasks = append(tasks, chromedp.Navigate(url))

	// Wait for selector or body
	if waitSelector != "" {
		tasks = append(tasks, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
	} else {
		tasks = append(tasks, chromedp.WaitReady("body"))
	}

	err := chromedp.Run(ctx, tasks...)
	if err != nil {
		return fmt.Errorf("navigation failed: %w", err)
	}

	// After navigation, check for CF challenge
	html, htmlErr := bs.GetHTML()
	if htmlErr != nil {
		log.Printf("[Browser:%s] Warning: Could not get HTML for CF detection: %v", bs.domain, htmlErr)
		return nil // Don't fail navigation, just warn
	}

	// Check for CF challenge in the HTML
	if isCF := detectCFInHTML(html); isCF {
		log.Printf("[Browser:%s] ⚠️ Cloudflare challenge detected in page!", bs.domain)

		// Mark stored data as failed if we had any
		if bs.bypassData != nil {
			log.Printf("[Browser:%s] Stored CF bypass data is invalid", bs.domain)
			cf.MarkCookieAsFailed(bs.domain)
			cf.DeleteDomain(bs.domain)
		}

		// Open browser for manual solve
		if err := cf.OpenInBrowser(url); err != nil {
			return fmt.Errorf("CF detected but failed to open browser: %w", err)
		}

		return &cf.CfChallengeError{
			URL:        url,
			StatusCode: 0, // We don't have HTTP status from chromedp
			Indicators: []string{"cf_challenge_detected_in_html"},
		}
	}

	log.Printf("[Browser:%s] ✓ Navigation successful", bs.domain)
	return nil
}

// detectCFInHTML checks if HTML contains Cloudflare challenge indicators
func detectCFInHTML(html string) bool {
	indicators := []string{
		"cf-browser-verification",
		"cf_captcha_kind",
		"cf-challenge-running",
		"Checking your browser",
		"Just a moment",
		"ray_id",
		"cf-error-details",
	}

	for _, indicator := range indicators {
		if len(html) > 0 && containsString(html, indicator) {
			return true
		}
	}

	return false
}

// containsString is a simple substring check
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Evaluate runs JavaScript and returns the result
func (bs *BrowserSession) Evaluate(js string, res interface{}) error {
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	return chromedp.Run(ctx, chromedp.Evaluate(js, res))
}

// GetHTML returns the page HTML
func (bs *BrowserSession) GetHTML() (string, error) {
	timeout := 10 * time.Second
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	var html string
	err := chromedp.Run(ctx, chromedp.OuterHTML("html", &html))
	return html, err
}

// Close closes the browser session
func (bs *BrowserSession) Close() {
	if bs.cancel != nil {
		bs.cancel()
	}
}

// FetchHTML fetches a URL using chromedp and returns the HTML
// This is the main function sites should use through the downloader
func FetchHTML(ctx context.Context, url, domain string, needsCF bool, waitSelector string) (string, error) {
	session, err := NewBrowserSession(ctx, domain, needsCF)
	if err != nil {
		return "", fmt.Errorf("failed to create browser session: %w", err)
	}
	defer session.Close()

	if err := session.Navigate(url, waitSelector); err != nil {
		return "", err
	}

	html, err := session.GetHTML()
	if err != nil {
		return "", fmt.Errorf("failed to get HTML: %w", err)
	}

	return html, nil
}
