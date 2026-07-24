package instant

import (
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// Point constructs a singleton at value.
func Point(value time.Time) Period {
	value = stripMonotonic(value)
	return Period{start: value, end: value, bounds: temporal.Closed}
}

// After constructs a period beginning at start with a fixed elapsed duration.
func After(start time.Time, duration time.Duration, bounds temporal.Bounds) (Period, error) {
	if duration < 0 {
		return Period{}, temporal.ErrStep
	}

	return New(start, start.Add(duration), bounds)
}

// Before constructs a period ending at end with a fixed elapsed duration.
func Before(end time.Time, duration time.Duration, bounds temporal.Bounds) (Period, error) {
	if duration < 0 {
		return Period{}, temporal.ErrStep
	}

	return New(end.Add(-duration), end, bounds)
}

// Around constructs a period extending radius before and after center.
func Around(center time.Time, radius time.Duration, bounds temporal.Bounds) (Period, error) {
	if radius < 0 {
		return Period{}, temporal.ErrStep
	}

	return New(center.Add(-radius), center.Add(radius), bounds)
}
