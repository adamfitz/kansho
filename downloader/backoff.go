package downloader

import (
	"math"
	"time"
)

// decayBackoff is a per-manga backoff controller that rises while a site keeps
// failing and only trickles back down once downloads work again — instead of
// snapping straight back to the configured base after the first success.
//
// It is borrowed by the chapter-image and chapter-list fetch retry loops and by
// the chapter download retry loop when the site's SiteRetryPolicy has
// DecayBackoff enabled. Each retry attempt still waits exponentially from the
// current effective base (2^attempt * effectiveBase, capped at max); after the
// whole attempt set has exhausted the base doubles (also capped at max); after
// any success it is dropped by one step toward the configured base, like
// draining a queue.
//
// cur is 0 while "at base"; effective() then returns the configured base.
type decayBackoff struct {
	base time.Duration // configured base backoff
	step time.Duration // how far a single success drags the base back down
	max  time.Duration // ceiling for any retry wait and for the base (0 = none)
	cur  time.Duration // current effective base; 0 means "at base"
}

// delay returns the wait for the given 0-based retry attempt within the current
// attempt set. The wait grows exponentially from the effective base and is
// capped at max so a single attempt set can never stall the download.
func (d *decayBackoff) delay(attempt int) time.Duration {
	wait := time.Duration(math.Pow(2, float64(attempt))) * d.effective()
	if d.max > 0 && wait > d.max {
		wait = d.max
	}
	return wait
}

// effective returns the base the next retry waits grow from.
func (d *decayBackoff) effective() time.Duration {
	if d.cur < d.base {
		return d.base
	}
	return d.cur
}

// fail doubles the effective base after a fully-failed attempt set.
func (d *decayBackoff) fail() {
	if d.cur < d.base { // seed from the base on the first failure
		d.cur = d.base
	}
	d.cur *= 2
	if d.max > 0 && d.cur > d.max {
		d.cur = d.max
	}
}

// succeed steps the effective base back down by one step after a successful
// operation. It never goes below the configured base.
func (d *decayBackoff) succeed() {
	if d.cur < d.base {
		return
	}
	d.cur -= d.step
	if d.cur <= d.base {
		d.cur = 0 // back at base
	}
}
