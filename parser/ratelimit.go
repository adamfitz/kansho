package parser

import (
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
