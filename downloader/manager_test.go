package downloader

import (
	"testing"
	"time"
)

// TestAdaptiveTimeoutPolicy verifies the grow-on-failure / reset-on-success
// deadline policy used for image download attempts.
func TestAdaptiveTimeoutPolicy(t *testing.T) {
	a := adaptiveTimeout{base: 2 * time.Minute, step: 10 * time.Second, max: 30 * time.Minute}

	// First attempt uses the base.
	if got := a.next(); got != 2*time.Minute {
		t.Fatalf("first next() = %v, want base 2m", got)
	}

	// Each failure grows the deadline by one step.
	want := []time.Duration{2*time.Minute + 10*time.Second, 2*time.Minute + 20*time.Second, 2*time.Minute + 30*time.Second}
	for i, w := range want {
		a.fail()
		if got := a.next(); got != w {
			t.Fatalf("after %d failures next() = %v, want %v", i+1, got, w)
		}
	}

	// Growth is capped at max even after many more failures.
	for i := 0; i < 200; i++ {
		a.fail()
	}
	if got := a.next(); got != a.max {
		t.Fatalf("next() = %v after saturating failures, want cap %v", got, a.max)
	}

	// A success resets to the base.
	a.succeed()
	if got := a.next(); got != a.base {
		t.Fatalf("next() = %v after success, want reset to base %v", got, a.base)
	}
}

// TestDecayBackoffPolicy verifies the grow-on-failure / step-down-on-success
// backoff policy: unlike adaptiveTimeout it does not snap back to the base
// after a single success, but decrements like a draining queue.
func TestDecayBackoffPolicy(t *testing.T) {
	d := decayBackoff{base: 2 * time.Second, step: 2 * time.Second, max: 60 * time.Second}

	// While at base, an attempt uses the configured base.
	if got := d.delay(0); got != 2*time.Second {
		t.Fatalf("delay(0) = %v, want base 2s", got)
	}
	if got := d.delay(1); got != 4*time.Second {
		t.Fatalf("delay(1) = %v, want 2*base = 4s", got)
	}

	// Each fully-failed attempt set doubles the effective base.
	want := []time.Duration{4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second}
	for i, w := range want {
		d.fail()
		if got := d.effective(); got != w {
			t.Fatalf("after %d fails effective() = %v, want %v", i+1, got, w)
		}
	}

	// A single success only steps the base back down one rung.
	d.succeed()
	if got := d.effective(); got != 58*time.Second {
		t.Fatalf("effective() = %v after one success, want 58s (single step, not a reset)", got)
	}

	// It never drops below the configured base.
	for i := 0; i < 500; i++ {
		d.succeed()
	}
	if got := d.effective(); got != d.base {
		t.Fatalf("effective() = %v after many successes, want base %v", got, d.base)
	}
}

// TestDecayBackoffDelayCap verifies that a single retry wait is capped at max,
// so an exponential attempt count can never stall a chapter fetch for hours.
func TestDecayBackoffDelayCap(t *testing.T) {
	d := decayBackoff{base: 2 * time.Second, step: 2 * time.Second, max: 60 * time.Second}

	// Saturate the base at the ceiling.
	for i := 0; i < 100; i++ {
		d.fail()
	}
	if got := d.effective(); got != 60*time.Second {
		t.Fatalf("effective() = %v, want ceiling 60s", got)
	}

	// Even a late attempt never exceeds the ceiling.
	for attempt := 0; attempt < 20; attempt++ {
		if got := d.delay(attempt); got > 60*time.Second {
			t.Fatalf("delay(%d) = %v, exceeds max 60s", attempt, got)
		}
	}
}
