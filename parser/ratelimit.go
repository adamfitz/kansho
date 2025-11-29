package parser

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
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
		MaxRetries:        5,
		BaseDelay:         1 * time.Second,
		MaxDelay:          32 * time.Second,
		Multiplier:        2.0,
		Jitter:            true,
		InitialTimeout:    10 * time.Second,
		TimeoutMultiplier: 1.5,
		MaxTimeout:        30 * time.Second,
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
	operation func(ctx context.Context, attempt int) (interface{}, error),
) (interface{}, error) {

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

		// Check if this is a retryable error
		// For now, we retry on all errors, but you could add logic here
		// to distinguish between retryable (timeouts, 5xx) and non-retryable (4xx) errors

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
