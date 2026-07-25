// Package manual provides deterministic fixed and manually advanced clocks.
package manual

import "time"

// Fixed is an immutable wall clock.
type Fixed struct {
	now time.Time
}

// NewFixed returns an immutable clock fixed at now.
func NewFixed(now time.Time) Fixed {
	return Fixed{now: now}
}

// Now returns the fixed timestamp without changing its location.
func (fixed Fixed) Now() time.Time {
	return fixed.now
}

// Since returns the duration from start to the fixed timestamp. If both values
// contain compatible monotonic readings, time.Time.Sub uses them.
func (fixed Fixed) Since(start time.Time) time.Duration {
	return fixed.now.Sub(start)
}

// Measure returns an elapsed measurement that remains zero for a fixed clock.
func (Fixed) Measure() func() time.Duration {
	return func() time.Duration { return 0 }
}
