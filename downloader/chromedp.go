package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
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

	// Wait for body
	tasks = append(tasks, chromedp.WaitReady("body"))

	// Execute navigation
	err := chromedp.Run(ctx, tasks...)
	if err != nil {
		return fmt.Errorf("navigation failed: %w", err)
	}

	// Give CF a moment to process cookies and redirect (if valid)
	time.Sleep(2 * time.Second)

	// Get HTML and check for CF challenge using the PROPER cf.Detectcf() function
	html, htmlErr := bs.GetHTML()
	if htmlErr == nil {
		// Create a fake HTTP response to use with cf.Detectcf()
		fakeResp := &http.Response{
			StatusCode: 200, // We don't have real status from chromedp, assume 200
			Body:       io.NopCloser(bytes.NewReader([]byte(html))),
			Header:     make(http.Header),
		}

		isCF, cfInfo, cfErr := cf.Detectcf(fakeResp)
		if cfErr != nil {
			log.Printf("[Browser:%s] CF detection error: %v", bs.domain, cfErr)
		}

		if isCF {
			log.Printf("[Browser:%s] ⚠️ Cloudflare challenge detected!", bs.domain)

			// Mark stored data as failed if we had any
			if bs.bypassData != nil {
				log.Printf("[Browser:%s] Stored CF bypass data is invalid", bs.domain)
				cf.MarkCookieAsFailed(bs.domain)
				cf.DeleteDomain(bs.domain)
			}

			// Open browser for manual solve
			challengeURL := cf.GetChallengeURL(cfInfo, url)
			if openErr := cf.OpenInBrowser(challengeURL); openErr != nil {
				return fmt.Errorf("CF detected but failed to open browser: %w", openErr)
			}

			return &cf.CfChallengeError{
				URL:        challengeURL,
				StatusCode: cfInfo.StatusCode,
				Indicators: cfInfo.Indicators,
			}
		}
	}

	// No CF challenge, proceed with selector wait and evaluation
	var evalTasks []chromedp.Action

	if waitSelector != "" {
		log.Printf("[Browser:%s] Waiting for selector: %s", bs.domain, waitSelector)
		evalTasks = append(evalTasks, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
	}

	// Evaluate JavaScript
	log.Printf("[Browser:%s] Evaluating JavaScript", bs.domain)
	evalTasks = append(evalTasks, chromedp.Evaluate(javascript, result))

	// Execute evaluation
	err = chromedp.Run(ctx, evalTasks...)
	if err != nil {
		return fmt.Errorf("evaluation failed: %w", err)
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

	// After navigation, check for CF challenge using proper cf.Detectcf()
	html, htmlErr := bs.GetHTML()
	if htmlErr != nil {
		log.Printf("[Browser:%s] Warning: Could not get HTML for CF detection: %v", bs.domain, htmlErr)
		return nil // Don't fail navigation, just warn
	}

	// Create fake HTTP response for CF detection
	fakeResp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte(html))),
		Header:     make(http.Header),
	}

	// Use proper CF detection from cf package
	isCF, cfInfo, cfErr := cf.Detectcf(fakeResp)
	if cfErr != nil {
		log.Printf("[Browser:%s] CF detection error: %v", bs.domain, cfErr)
	}

	if isCF {
		log.Printf("[Browser:%s] ⚠️ Cloudflare challenge detected in page!", bs.domain)

		// Mark stored data as failed if we had any
		if bs.bypassData != nil {
			log.Printf("[Browser:%s] Stored CF bypass data is invalid", bs.domain)
			cf.MarkCookieAsFailed(bs.domain)
			cf.DeleteDomain(bs.domain)
		}

		// Open browser for manual solve
		challengeURL := cf.GetChallengeURL(cfInfo, url)
		if err := cf.OpenInBrowser(challengeURL); err != nil {
			return fmt.Errorf("CF detected but failed to open browser: %w", err)
		}

		return &cf.CfChallengeError{
			URL:        challengeURL,
			StatusCode: cfInfo.StatusCode,
			Indicators: cfInfo.Indicators,
		}
	}

	log.Printf("[Browser:%s] ✓ Navigation successful", bs.domain)
	return nil
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
