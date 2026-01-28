package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
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
	// Start with default options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-gpu", true),
	)

	// We’ll load bypass data *before* creating the context if CF is needed
	var bypassData *cf.BypassData
	if needsCF {
		data, err := cf.LoadFromFile(domain)
		if err != nil {
			log.Printf("[Browser:%s] No CF bypass data found", domain)
		} else {
			bypassData = data
			log.Printf("[Browser:%s] ✓ Loaded CF bypass data", domain)

			// CRITICAL: use the same User-Agent that earned the cookie
			if ua := strings.TrimSpace(data.Entropy.UserAgent); ua != "" {
				opts = append(opts, chromedp.UserAgent(ua))
				log.Printf("[Browser:%s] Using captured User-Agent: %s", domain, ua)
			} else {
				log.Printf("[Browser:%s] WARNING: bypass data has empty User-Agent, falling back to default", domain)
				opts = append(opts,
					chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"),
				)
			}
		}
	} else {
		// No CF, just use a sane default UA
		opts = append(opts,
			chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/115.0.0.0 Safari/537.36"),
		)
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)

	session := &BrowserSession{
		ctx:        browserCtx,
		cancel:     func() { cancelBrowser(); cancelAlloc() },
		domain:     domain,
		needsCF:    needsCF,
		bypassData: bypassData,
	}

	return session, nil
}

// normalizeDomain ensures cookie domain is valid for Chromium
func normalizeDomain(d string) string {
	if d == "" {
		return ""
	}
	if !strings.HasPrefix(d, ".") {
		return "." + d
	}
	return d
}

// injectCookies builds and injects cookies into the browser
func (bs *BrowserSession) injectCookies(tasks *[]chromedp.Action) int {
	if bs.bypassData == nil {
		return 0
	}

	injected := 0
	var cookies []*network.CookieParam

	// Inject cf_clearance first
	if bs.bypassData.CfClearanceStruct != nil {
		domain := normalizeDomain(bs.bypassData.CfClearanceStruct.Domain)
		path := bs.bypassData.CfClearanceStruct.Path
		if path == "" {
			path = "/"
		}

		cookie := &network.CookieParam{
			Name:     bs.bypassData.CfClearanceStruct.Name,
			Value:    bs.bypassData.CfClearanceStruct.Value,
			Domain:   domain,
			Path:     path,
			Secure:   bs.bypassData.CfClearanceStruct.Secure,
			HTTPOnly: bs.bypassData.CfClearanceStruct.HttpOnly,
		}

		cookies = append(cookies, cookie)
		injected++
	}

	// Inject all other cookies
	for _, ck := range bs.bypassData.AllCookies {
		if ck.Name == "" {
			continue
		}

		domain := normalizeDomain(ck.Domain)
		path := ck.Path
		if path == "" {
			path = "/"
		}

		cookie := &network.CookieParam{
			Name:   ck.Name,
			Value:  ck.Value,
			Domain: domain,
			Path:   path,
			Secure: ck.Secure,
		}

		cookies = append(cookies, cookie)
		injected++
	}

	// Log injection
	cf.LogCFBrowserAction("InjectCookies", bs.domain, len(cookies), true, nil)

	// Inject into browser
	*tasks = append(*tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.SetCookies(cookies).Do(ctx)
	}))

	return injected
}

// dumpBrowserCookies logs all cookies currently in Chromium
func dumpBrowserCookies(ctx context.Context, domain string) {
	var cookies []*network.Cookie

	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		cookies, err = network.GetCookies().Do(ctx)
		return err
	}))

	if err != nil {
		cf.LogCFError("DumpBrowserCookies", domain, err)
		return
	}

	for _, ck := range cookies {
		cf.LogCFBrowserAction("BrowserCookie", domain, 1, true, nil)
		cf.LogCFError("BrowserCookie",
			domain,
			fmt.Errorf("%s=%s domain=%s path=%s",
				ck.Name, ck.Value, ck.Domain, ck.Path,
			),
		)
	}
}

// NavigateAndEvaluate performs navigation, waiting, and JS evaluation
func (bs *BrowserSession) NavigateAndEvaluate(url, waitSelector, javascript string, result interface{}) error {
	timeout := 60 * time.Second
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	var tasks []chromedp.Action

	// Inject cookies
	injected := bs.injectCookies(&tasks)

	// Navigate
	tasks = append(tasks, chromedp.Navigate(url))
	tasks = append(tasks, chromedp.WaitReady("body"))

	// Execute navigation
	err := chromedp.Run(ctx, tasks...)
	if err != nil {
		cf.LogCFError("NavigateAndEvaluate-Navigation", bs.domain, err)
		return fmt.Errorf("navigation failed: %w", err)
	}

	// Dump cookies after navigation
	dumpBrowserCookies(ctx, bs.domain)

	// CF detection
	html, htmlErr := bs.GetHTML()
	if htmlErr == nil {
		fakeResp := &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader([]byte(html))),
			Header:     make(http.Header),
		}

		isCF, cfInfo, cfErr := cf.Detectcf(fakeResp)
		if cfErr != nil {
			cf.LogCFError("DetectCF", bs.domain, cfErr)
		}

		if isCF {
			cf.LogCFBrowserAction("CFChallengeDetected", url, injected, false, nil)

			if bs.bypassData != nil {
				cf.MarkCookieAsFailed(bs.domain)
				cf.DeleteDomain(bs.domain)
			}

			challengeURL := cf.GetChallengeURL(cfInfo, url)
			cf.OpenInBrowser(challengeURL)

			return &cf.CfChallengeError{
				URL:        challengeURL,
				StatusCode: cfInfo.StatusCode,
				Indicators: cfInfo.Indicators,
			}
		}
	}

	// Evaluate JS
	var evalTasks []chromedp.Action
	if waitSelector != "" {
		evalTasks = append(evalTasks, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
	}
	evalTasks = append(evalTasks, chromedp.Evaluate(javascript, result))

	err = chromedp.Run(ctx, evalTasks...)
	if err != nil {
		cf.LogCFError("NavigateAndEvaluate-Eval", bs.domain, err)
		return fmt.Errorf("evaluation failed: %w", err)
	}

	return nil
}

// Navigate navigates to a URL and waits for page load
func (bs *BrowserSession) Navigate(url string, waitSelector string) error {
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	var tasks []chromedp.Action

	// Inject cookies
	injected := bs.injectCookies(&tasks)

	// Navigate
	tasks = append(tasks, chromedp.Navigate(url))

	if waitSelector != "" {
		tasks = append(tasks, chromedp.WaitVisible(waitSelector, chromedp.ByQuery))
	} else {
		tasks = append(tasks, chromedp.WaitReady("body"))
	}

	err := chromedp.Run(ctx, tasks...)
	if err != nil {
		cf.LogCFError("Navigate-Navigation", bs.domain, err)
		return fmt.Errorf("navigation failed: %w", err)
	}

	// Dump cookies after navigation
	dumpBrowserCookies(ctx, bs.domain)

	// CF detection
	html, htmlErr := bs.GetHTML()
	if htmlErr != nil {
		cf.LogCFError("Navigate-GetHTML", bs.domain, htmlErr)
		return nil
	}

	fakeResp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader([]byte(html))),
		Header:     make(http.Header),
	}

	isCF, cfInfo, cfErr := cf.Detectcf(fakeResp)
	if cfErr != nil {
		cf.LogCFError("Navigate-DetectCF", bs.domain, cfErr)
	}

	if isCF {
		cf.LogCFBrowserAction("CFChallengeDetected", url, injected, false, nil)

		if bs.bypassData != nil {
			cf.MarkCookieAsFailed(bs.domain)
			cf.DeleteDomain(bs.domain)
		}

		challengeURL := cf.GetChallengeURL(cfInfo, url)
		cf.OpenInBrowser(challengeURL)

		return &cf.CfChallengeError{
			URL:        challengeURL,
			StatusCode: cfInfo.StatusCode,
			Indicators: cfInfo.Indicators,
		}
	}

	return nil
}

// Evaluate runs JavaScript and returns the result
func (bs *BrowserSession) Evaluate(js string, res interface{}) error {
	timeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	err := chromedp.Run(ctx, chromedp.Evaluate(js, res))
	if err != nil {
		cf.LogCFError("Evaluate", bs.domain, err)
	}
	return err
}

// GetHTML returns the page HTML
func (bs *BrowserSession) GetHTML() (string, error) {
	timeout := 10 * time.Second
	ctx, cancel := context.WithTimeout(bs.ctx, timeout)
	defer cancel()

	var html string
	err := chromedp.Run(ctx, chromedp.OuterHTML("html", &html))
	if err != nil {
		cf.LogCFError("GetHTML", bs.domain, err)
	}
	return html, err
}

// Close closes the browser session
func (bs *BrowserSession) Close() {
	if bs.cancel != nil {
		bs.cancel()
	}
}

// FetchHTML fetches a URL using chromedp and returns the HTML
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
