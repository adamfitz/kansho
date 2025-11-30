package parser

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// RateLimiter manages rate limiting for sequential operations.
// It ensures operations are spaced out by a specified interval.
type RateLimiter struct {
	ticker   *time.Ticker
	interval time.Duration
}

// NewRateLimiter creates a new rate limiter with the specified interval.
// The interval determines the minimum time between operations.
//
// Example usage:
//
//	limiter := parser.NewRateLimiter(1500 * time.Millisecond)
//	defer limiter.Stop()
//
//	for i, url := range urls {
//	    limiter.Wait()
//	    // ... perform rate-limited operation ...
//	}
//
// Parameters:
//   - interval: Time to wait between operations (e.g., 1500 * time.Millisecond)
//
// Returns:
//   - *RateLimiter: A new rate limiter instance
func NewRateLimiter(interval time.Duration) *RateLimiter {
	return &RateLimiter{
		ticker:   time.NewTicker(interval),
		interval: interval,
	}
}

// Wait blocks until the next tick occurs.
// Call this before each rate-limited operation.
func (rl *RateLimiter) Wait() {
	<-rl.ticker.C
}

// Stop stops the rate limiter and releases resources.
// Should be called when the rate limiter is no longer needed.
// Typically used with defer: defer limiter.Stop()
func (rl *RateLimiter) Stop() {
	rl.ticker.Stop()
}

// GetInterval returns the configured interval for this rate limiter.
func (rl *RateLimiter) GetInterval() time.Duration {
	return rl.interval
}

// BackoffConfig contains configuration for exponential backoff retry logic
type BackoffConfig struct {
	// MaxRetries is the maximum number of retry attempts (default: 5)
	MaxRetries int

	// BaseDelay is the initial delay before the first retry (default: 1 second)
	BaseDelay time.Duration

	// MaxDelay is the maximum delay between retries (default: 32 seconds)
	MaxDelay time.Duration

	// Multiplier is the factor by which delay increases each retry (default: 2.0)
	Multiplier float64

	// Jitter adds randomness to prevent thundering herd (default: true)
	// Adds random time between 0 and 1 second to each delay
	Jitter bool

	// InitialTimeout is the timeout for the first attempt (default: 10 seconds)
	InitialTimeout time.Duration

	// TimeoutMultiplier increases timeout on each retry (default: 1.5)
	// Set to 1.0 to keep timeout constant
	TimeoutMultiplier float64

	// MaxTimeout is the maximum timeout for any single attempt (default: 30 seconds)
	MaxTimeout time.Duration
}

// DefaultBackoffConfig returns a BackoffConfig with sensible defaults
// suitable for most HTTP requests and API calls
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		MaxRetries:        8,
		BaseDelay:         1 * time.Second,
		MaxDelay:          32 * time.Second,
		Multiplier:        2.0,
		Jitter:            true,
		InitialTimeout:    10 * time.Second,
		TimeoutMultiplier: 1.5,
		MaxTimeout:        180 * time.Second,
	}
}

// RetryWithBackoff executes a function with exponential backoff retry logic.
// This is a generic retry wrapper that handles transient failures.
//
// The function will:
// 1. Try the operation immediately with InitialTimeout
// 2. If it fails, wait with exponential backoff before retrying
// 3. Increase timeout on each retry (if TimeoutMultiplier > 1.0)
// 4. Add jitter to prevent synchronized retries (if enabled)
// 5. Return success immediately if operation succeeds
// 6. Return error after MaxRetries attempts
// 7. IMMEDIATELY stop retrying if error message contains "non-retryable"
//
// Example usage:
//
//	config := parser.DefaultBackoffConfig()
//	config.MaxRetries = 7
//
//	result, err := parser.RetryWithBackoff(ctx, config, "fetch-chapters", func(ctx context.Context, attempt int) (interface{}, error) {
//	    return fetchChapters(ctx, url)
//	})
//
//	if err != nil {
//	    return fmt.Errorf("failed after retries: %w", err)
//	}
//	chapters := result.([]string)
//
// To mark an error as non-retryable, wrap it like:
//
//	return nil, fmt.Errorf("non-retryable: %w", originalError)
//
// Parameters:
//   - ctx: Context for cancellation support
//   - config: BackoffConfig with retry parameters
//   - operationName: Descriptive name for logging (e.g., "fetch-chapters", "download-image")
//   - operation: Function to retry. Receives context and attempt number (0-based).
//     Should return (result, error). Return nil error on success.
//
// Returns:
//   - interface{}: The result from the successful operation
//   - error: Final error if all retries fail, or context cancellation
func RetryWithBackoff(
	ctx context.Context,
	config BackoffConfig,
	operationName string,
	operation func(ctx context.Context, attempt int) (any, error),
) (any, error) {

	// Validate config and apply defaults
	if config.MaxRetries <= 0 {
		config.MaxRetries = 5
	}
	if config.BaseDelay <= 0 {
		config.BaseDelay = 1 * time.Second
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 32 * time.Second
	}
	if config.Multiplier <= 0 {
		config.Multiplier = 2.0
	}
	if config.InitialTimeout <= 0 {
		config.InitialTimeout = 10 * time.Second
	}
	if config.TimeoutMultiplier <= 0 {
		config.TimeoutMultiplier = 1.5
	}
	if config.MaxTimeout <= 0 {
		config.MaxTimeout = 30 * time.Second
	}

	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Check if context is already cancelled
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Calculate timeout for this attempt
		timeout := calculateTimeout(config.InitialTimeout, config.TimeoutMultiplier, config.MaxTimeout, attempt)

		// Create context with timeout for this attempt
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)

		// Execute the operation
		result, err := operation(attemptCtx, attempt)
		cancel() // Clean up context

		// Success! Return immediately
		if err == nil {
			if attempt > 0 {
				// Log success after retries
				fmt.Printf("<%s> ✓ Success after %d retries\n", operationName, attempt)
			}
			return result, nil
		}

		// Store the error
		lastErr = err

		// Check if this is a non-retryable error
		// Non-retryable errors should be wrapped with "non-retryable:" prefix
		if isNonRetryable(err) {
			fmt.Printf("<%s> Non-retryable error detected, stopping immediately: %v\n", operationName, err)
			return nil, err
		}

		// If this was the last attempt, don't sleep
		if attempt >= config.MaxRetries {
			break
		}

		// Calculate delay before next retry
		delay := calculateDelay(config.BaseDelay, config.MaxDelay, config.Multiplier, config.Jitter, attempt)

		fmt.Printf("<%s> ⚠️  Attempt %d/%d failed: %v\n", operationName, attempt+1, config.MaxRetries+1, err)
		fmt.Printf("<%s> Waiting %v before retry...\n", operationName, delay)

		// Wait with context support (allows cancellation during sleep)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			// Continue to next retry
		}
	}

	// All retries exhausted
	fmt.Printf("<%s> ✗ Failed after %d attempts\n", operationName, config.MaxRetries+1)
	return nil, fmt.Errorf("operation failed after %d attempts: %w", config.MaxRetries+1, lastErr)
}

// calculateDelay computes the exponential backoff delay for a given attempt
func calculateDelay(baseDelay, maxDelay time.Duration, multiplier float64, jitter bool, attempt int) time.Duration {
	// Calculate exponential delay: baseDelay * (multiplier ^ attempt)
	delay := float64(baseDelay) * math.Pow(multiplier, float64(attempt))

	// Apply maximum cap
	if time.Duration(delay) > maxDelay {
		delay = float64(maxDelay)
	}

	// Add jitter if enabled (random 0-1000ms)
	if jitter {
		jitterMs := rand.Intn(1000)
		delay += float64(jitterMs * int(time.Millisecond))
	}

	return time.Duration(delay)
}

// calculateTimeout computes the timeout for a given attempt
func calculateTimeout(initialTimeout time.Duration, multiplier float64, maxTimeout time.Duration, attempt int) time.Duration {
	// If multiplier is 1.0, keep timeout constant
	if multiplier == 1.0 {
		return initialTimeout
	}

	// Calculate increased timeout: initialTimeout * (multiplier ^ attempt)
	timeout := float64(initialTimeout) * math.Pow(multiplier, float64(attempt))

	// Apply maximum cap
	if time.Duration(timeout) > maxTimeout {
		return maxTimeout
	}

	return time.Duration(timeout)
}

// IsTimeoutError checks if an error is a timeout error
// Useful for distinguishing retryable from non-retryable errors
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return contains(errStr, "context deadline exceeded") ||
		contains(errStr, "Client.Timeout exceeded") ||
		contains(errStr, "timeout") ||
		contains(errStr, "timed out")
}

// ChromedpWaitConfig configures how long to wait for JavaScript execution
type ChromedpWaitConfig struct {
	// InitialWait is the delay after navigation starts (default: 3s)
	InitialWait time.Duration

	// HydrationWait is time for React/framework hydration (default: 5s)
	HydrationWait time.Duration

	// DataFetchWait is time for async data fetching (default: 7s)
	DataFetchWait time.Duration

	// FinalBufferWait is final buffer for lazy content (default: 5s)
	FinalBufferWait time.Duration

	// TotalTimeout is maximum time for entire operation (default: 90s)
	TotalTimeout time.Duration

	// ScrollToBottom forces page scroll to trigger lazy loading (default: false)
	ScrollToBottom bool

	// ScrollDelay is time to wait after scrolling (default: 2s)
	ScrollDelay time.Duration

	// MinimumHTMLSize is minimum expected HTML size in bytes (default: 0 = no check)
	MinimumHTMLSize int

	// WaitForSelector waits for a specific CSS selector before continuing (default: "")
	WaitForSelector string
}

// DefaultChromedpWaitConfig returns sensible defaults for modern JS-heavy sites
func DefaultChromedpWaitConfig() ChromedpWaitConfig {
	return ChromedpWaitConfig{
		InitialWait:     3 * time.Second,
		HydrationWait:   5 * time.Second,
		DataFetchWait:   7 * time.Second,
		FinalBufferWait: 5 * time.Second,
		TotalTimeout:    90 * time.Second,
		ScrollToBottom:  false,
		ScrollDelay:     2 * time.Second,
		MinimumHTMLSize: 0,
		WaitForSelector: "",
	}
}

// AsuraChromedpWaitConfig returns config optimized for Asura Scans (Next.js/React heavy site)
func AsuraChromedpWaitConfig() ChromedpWaitConfig {
	return ChromedpWaitConfig{
		InitialWait:     5 * time.Second,   // Extra time for Cloudflare + page load
		HydrationWait:   8 * time.Second,   // React/Next.js needs longer to hydrate
		DataFetchWait:   7 * time.Second,   // API calls and data fetching
		FinalBufferWait: 5 * time.Second,   // Images and lazy content
		TotalTimeout:    120 * time.Second, // Longer timeout for CF + large pages
		ScrollToBottom:  true,              // CRITICAL: Trigger lazy loading
		ScrollDelay:     3 * time.Second,   // Wait after scroll for images to load
		MinimumHTMLSize: 500000,            // 500KB minimum (Next.js SSR is huge)
		WaitForSelector: "",                // Optional: could wait for specific content
	}
}

// TotalWaitTime returns the sum of all wait periods (including scroll if enabled)
func (c ChromedpWaitConfig) TotalWaitTime() time.Duration {
	total := c.InitialWait + c.HydrationWait + c.DataFetchWait + c.FinalBufferWait
	if c.ScrollToBottom {
		total += c.ScrollDelay
	}
	return total
}

// RetryWithChromedpWaits executes a chromedp operation with proper JS wait times and retry logic.
// This is specifically for operations that use chromedp and need to wait for JavaScript execution.
//
// The function handles:
// 1. Creating chromedp context with proper timeout
// 2. Adding progressive waits for JS execution
// 3. Retrying on failure with exponential backoff
// 4. Content validation (HTML size, script tags)
// 5. INCREASING WAIT TIMES ON EACH RETRY (critical for slow JS execution)
// 6. Optional scrolling to trigger lazy loading
// 7. Optional waiting for specific selectors
//
// Example usage:
//
//	config := parser.DefaultBackoffConfig()
//	waitConfig := parser.AsuraChromedpWaitConfig() // Use site-specific config
//
//	result, err := parser.RetryWithChromedpWaits(
//		ctx,
//		config,
//		waitConfig,
//		"fetch-images",
//		chapterURL,
//		func(html string) (interface{}, error) {
//			// Parse HTML and extract data
//			images := extractImagesFromHTML(html)
//			if len(images) == 0 {
//				return nil, fmt.Errorf("no images found")
//			}
//			return images, nil
//		},
//	)
//
// Parameters:
//   - ctx: Context for cancellation
//   - backoffConfig: Retry configuration (retries, delays, timeouts)
//   - waitConfig: JavaScript wait time configuration (BASE values that scale up)
//   - operationName: Descriptive name for logging
//   - url: URL to navigate to
//   - parseHTML: Function that receives HTML and extracts data
//
// Returns:
//   - interface{}: Extracted data from successful parse
//   - error: Error if all retries fail
func RetryWithChromedpWaits(
	ctx context.Context,
	backoffConfig BackoffConfig,
	waitConfig ChromedpWaitConfig,
	operationName string,
	url string,
	parseHTML func(html string) (interface{}, error),
) (interface{}, error) {

	// Validate wait config
	if waitConfig.TotalTimeout <= 0 {
		waitConfig.TotalTimeout = 90 * time.Second
	}
	if waitConfig.ScrollDelay <= 0 {
		waitConfig.ScrollDelay = 2 * time.Second
	}

	result, err := RetryWithBackoff(ctx, backoffConfig, operationName, func(ctx context.Context, attempt int) (interface{}, error) {
		// CRITICAL: Scale wait times based on attempt number
		// Each retry waits LONGER for JavaScript to execute
		scaleFactor := 1.0 + (float64(attempt) * 0.5) // 1.0x, 1.5x, 2.0x, 2.5x...

		scaledInitial := time.Duration(float64(waitConfig.InitialWait) * scaleFactor)
		scaledHydration := time.Duration(float64(waitConfig.HydrationWait) * scaleFactor)
		scaledDataFetch := time.Duration(float64(waitConfig.DataFetchWait) * scaleFactor)
		scaledBuffer := time.Duration(float64(waitConfig.FinalBufferWait) * scaleFactor)
		scaledScrollDelay := time.Duration(float64(waitConfig.ScrollDelay) * scaleFactor)

		totalWait := scaledInitial + scaledHydration + scaledDataFetch + scaledBuffer
		if waitConfig.ScrollToBottom {
			totalWait += scaledScrollDelay
		}

		// Scale timeout too
		scaledTimeout := time.Duration(float64(waitConfig.TotalTimeout) * scaleFactor)
		if scaledTimeout > 180*time.Second {
			scaledTimeout = 180 * time.Second // Cap at 3 minutes
		}

		fmt.Printf("<%s> Attempt %d: Waiting %.0fs for JS (scale: %.1fx)\n",
			operationName, attempt+1, totalWait.Seconds(), scaleFactor)
		fmt.Printf("<%s> Wait breakdown: initial=%v hydration=%v fetch=%v buffer=%v scroll=%v\n",
			operationName, scaledInitial, scaledHydration, scaledDataFetch, scaledBuffer, scaledScrollDelay)

		// Create chromedp context with scaled timeout
		chromedpCtx, chromedpCancel := context.WithTimeout(context.Background(), scaledTimeout)
		defer chromedpCancel()

		browserCtx, browserCancel := chromedp.NewContext(chromedpCtx)
		defer browserCancel()

		var html string
		startNav := time.Now()

		// Build action list dynamically based on config
		actions := []chromedp.Action{
			chromedp.Navigate(url),
			chromedp.WaitReady("body"),
			chromedp.Sleep(scaledInitial),
		}

		// Wait for specific selector if configured
		if waitConfig.WaitForSelector != "" {
			fmt.Printf("<%s> Waiting for selector: %s\n", operationName, waitConfig.WaitForSelector)
			actions = append(actions, chromedp.WaitVisible(waitConfig.WaitForSelector))
		}

		// Progressive waits for JS execution
		actions = append(actions,
			chromedp.Sleep(scaledHydration),
			chromedp.Sleep(scaledDataFetch),
		)

		// Scroll to bottom if enabled (triggers lazy loading)
		if waitConfig.ScrollToBottom {
			fmt.Printf("<%s> Scrolling to bottom to trigger lazy loading\n", operationName)
			actions = append(actions,
				chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight);`, nil),
				chromedp.Sleep(scaledScrollDelay),
				// Scroll back to top to capture everything
				chromedp.Evaluate(`window.scrollTo(0, 0);`, nil),
				chromedp.Sleep(500*time.Millisecond),
			)
		}

		// Final buffer and get HTML
		actions = append(actions,
			chromedp.Sleep(scaledBuffer),
			chromedp.OuterHTML("html", &html),
		)

		// Execute all actions
		err := chromedp.Run(browserCtx, actions...)

		if err != nil {
			return nil, fmt.Errorf("chromedp navigation failed: %w", err)
		}

		elapsedNav := time.Since(startNav)
		fmt.Printf("<%s> Navigation complete in %s. HTML length: %d\n", operationName, elapsedNav, len(html))

		// Validate minimum HTML size if configured
		if waitConfig.MinimumHTMLSize > 0 && len(html) < waitConfig.MinimumHTMLSize {
			return nil, fmt.Errorf("page content too short (%d bytes < %d minimum), likely incomplete load",
				len(html), waitConfig.MinimumHTMLSize)
		}

		// Generic validation: ensure page has content
		if len(html) < 1000 {
			return nil, fmt.Errorf("page content too short (%d bytes), likely incomplete load", len(html))
		}

		// Count script tags as health check
		scriptCount := countScriptTags(html)
		fmt.Printf("<%s> Found %d script tags\n", operationName, scriptCount)

		if scriptCount == 0 {
			return nil, fmt.Errorf("no script tags found, page may not have loaded correctly")
		}

		// Parse HTML using provided function
		return parseHTML(html)
	})

	return result, err
}

// countScriptTags counts the number of <script> tags in HTML
func countScriptTags(html string) int {
	count := 0
	searchStr := "<script"
	idx := 0
	for {
		pos := strings.Index(html[idx:], searchStr)
		if pos == -1 {
			break
		}
		count++
		idx += pos + len(searchStr)
	}
	return count
}

// isNonRetryable checks if an error should stop retries immediately
// Returns true if the error message contains indicators that retrying would be futile
//
// This includes:
// - Errors containing "non-retryable" in the message
// - Cloudflare challenge errors (cf_challenge_opened)
// - Any error wrapped with these patterns
func isNonRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check the error message for known non-retryable patterns
	errStr := err.Error()

	// CF challenge errors should not be retried
	if contains(errStr, "cf_challenge_opened") {
		return true
	}

	// Explicit non-retryable marker
	if contains(errStr, "non-retryable") {
		return true
	}

	// Check if the underlying error is a CF challenge
	// This handles wrapped errors like fmt.Errorf("...: %w", cfErr)
	if contains(errStr, "cf challenge") || contains(errStr, "CF challenge") {
		return true
	}

	return false
}

// contains checks if a string contains a substring (case-sensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
