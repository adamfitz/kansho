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
