package instant

import (
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// Duration returns the fixed elapsed duration from start to end. It reports
// ErrOverflow when the span cannot be represented by time.Duration.
func (p Period) Duration() (time.Duration, error) {
	duration := p.end.Sub(p.start)
	if !p.start.Add(duration).Equal(p.end) {
		return 0, temporal.ErrOverflow
	}

	return duration, nil
}

// CompareDuration compares fixed elapsed endpoint spans and returns -1, 0,
// or 1. Boundary inclusion does not change elapsed duration.
func (p Period) CompareDuration(other Period) (int, error) {
	left, err := p.Duration()
	if err != nil {
		return 0, err
	}
	right, err := other.Duration()
	if err != nil {
		return 0, err
	}
	switch {
	case left < right:
		return -1, nil
	case left > right:
		return 1, nil
	default:
		return 0, nil
	}
}

// WithStart returns a period with start replacing the current start.
func (p Period) WithStart(start time.Time) (Period, error) {
	return New(start, p.end, p.bounds)
}

// WithEnd returns a period with end replacing the current end.
func (p Period) WithEnd(end time.Time) (Period, error) {
	return New(p.start, end, p.bounds)
}

// WithBounds returns a period with bounds replacing the current bounds.
func (p Period) WithBounds(bounds temporal.Bounds) (Period, error) {
	return New(p.start, p.end, bounds)
}

// WithDurationAfterStart keeps the start and replaces the end using a
// non-negative fixed elapsed duration.
func (p Period) WithDurationAfterStart(duration time.Duration) (Period, error) {
	if duration < 0 {
		return Period{}, temporal.ErrStep
	}
	return New(p.start, p.start.Add(duration), p.bounds)
}

// WithDurationBeforeEnd keeps the end and replaces the start using a
// non-negative fixed elapsed duration.
func (p Period) WithDurationBeforeEnd(duration time.Duration) (Period, error) {
	if duration < 0 {
		return Period{}, temporal.ErrStep
	}
	return New(p.end.Add(-duration), p.end, p.bounds)
}

// MoveStart shifts only the start by a fixed elapsed duration.
func (p Period) MoveStart(duration time.Duration) (Period, error) {
	return New(p.start.Add(duration), p.end, p.bounds)
}

// MoveEnd shifts only the end by a fixed elapsed duration.
func (p Period) MoveEnd(duration time.Duration) (Period, error) {
	return New(p.start, p.end.Add(duration), p.bounds)
}

// Move shifts both endpoints by a fixed elapsed duration.
func (p Period) Move(duration time.Duration) Period {
	return Period{
		start:  p.start.Add(duration),
		end:    p.end.Add(duration),
		bounds: p.bounds,
	}
}

// Expand moves the start backward and end forward. A negative duration shrinks
// the period. Shrinking past equal endpoints is rejected as reversed.
func (p Period) Expand(duration time.Duration) (Period, error) {
	if duration == time.Duration(-1<<63) {
		return Period{}, temporal.ErrOverflow
	}

	return New(p.start.Add(-duration), p.end.Add(duration), p.bounds)
}

// Equal reports structural equality, including endpoint locations and bounds.
// Monotonic readings never participate because construction strips them.
//
//lint:ignore QF1009 structural equality deliberately includes location identity
func (p Period) Equal(other Period) bool {
	return p.start == other.start && p.end == other.end && p.bounds == other.bounds
}

// SetEqual reports whether two periods represent exactly the same set.
func (p Period) SetEqual(other Period) bool {
	if p.IsEmpty() || other.IsEmpty() {
		return p.IsEmpty() && other.IsEmpty()
	}

	return p.start.Equal(other.start) &&
		p.end.Equal(other.end) &&
		p.bounds == other.bounds
}

// Intersect returns the exact non-empty intersection with other.
func (p Period) Intersect(other Period) (Period, bool) {
	if p.IsEmpty() || other.IsEmpty() {
		return Period{}, false
	}

	start, includeStart := laterStart(p, other)
	end, includeEnd := earlierEnd(p, other)
	comparison := start.Compare(end)
	if comparison > 0 || (comparison == 0 && (!includeStart || !includeEnd)) {
		return Period{}, false
	}

	return Period{start: start, end: end, bounds: boundsFrom(includeStart, includeEnd)}, true
}

// Subtract returns the exact fragments of p which are not members of other.
// Results are ordered and contain at most two periods.
func (p Period) Subtract(other Period) []Period {
	intersection, ok := p.Intersect(other)
	if !ok {
		return []Period{p}
	}
	if intersection.SetEqual(p) {
		return nil
	}

	result := make([]Period, 0, 2)
	if p.start.Before(intersection.start) ||
		(p.start.Equal(intersection.start) && p.bounds.IncludesStart() && !intersection.bounds.IncludesStart()) {
		result = append(result, Period{
			start:  p.start,
			end:    intersection.start,
			bounds: boundsFrom(p.bounds.IncludesStart(), !intersection.bounds.IncludesStart()),
		})
	}

	if intersection.end.Before(p.end) ||
		(intersection.end.Equal(p.end) && p.bounds.IncludesEnd() && !intersection.bounds.IncludesEnd()) {
		result = append(result, Period{
			start:  intersection.end,
			end:    p.end,
			bounds: boundsFrom(!intersection.bounds.IncludesEnd(), p.bounds.IncludesEnd()),
		})
	}

	return result
}

// Difference is the exact set difference and is an alias for Subtract.
func (p Period) Difference(other Period) []Period { return p.Subtract(other) }

// Union returns the normalized exact union. Disjoint periods remain distinct.
func (p Period) Union(other Period, limits temporal.Limits) (Set, error) {
	return NewSet(limits, p, other)
}

// Merge returns the convex hull of p and other. Unlike Union, Merge includes
// any gap between disjoint periods.
func (p Period) Merge(other Period) Period {
	start, includeStart := earlierStartForHull(p, other)
	end, includeEnd := laterEndForHull(p, other)
	return Period{start: start, end: end, bounds: boundsFrom(includeStart, includeEnd)}
}

// Gap returns the exact non-empty set between disjoint periods. Adjacent
// periods leave a singleton gap only when both exclude the shared endpoint.
func (p Period) Gap(other Period) (Period, bool) {
	if p.IsEmpty() || other.IsEmpty() {
		return Period{}, false
	}
	if _, ok := p.Intersect(other); ok {
		return Period{}, false
	}

	left, right := p, other
	if left.start.After(right.start) {
		left, right = right, left
	}

	if left.end.Equal(right.start) &&
		(left.bounds.IncludesEnd() || right.bounds.IncludesStart()) {
		return Period{}, false
	}

	return Period{
		start:  left.end,
		end:    right.start,
		bounds: boundsFrom(!left.bounds.IncludesEnd(), !right.bounds.IncludesStart()),
	}, true
}

func laterStart(a, b Period) (time.Time, bool) {
	switch a.start.Compare(b.start) {
	case -1:
		return b.start, b.bounds.IncludesStart()
	case 1:
		return a.start, a.bounds.IncludesStart()
	default:
		return a.start, a.bounds.IncludesStart() && b.bounds.IncludesStart()
	}
}

func earlierEnd(a, b Period) (time.Time, bool) {
	switch a.end.Compare(b.end) {
	case -1:
		return a.end, a.bounds.IncludesEnd()
	case 1:
		return b.end, b.bounds.IncludesEnd()
	default:
		return a.end, a.bounds.IncludesEnd() && b.bounds.IncludesEnd()
	}
}

func earlierStartForHull(a, b Period) (time.Time, bool) {
	switch a.start.Compare(b.start) {
	case -1:
		return a.start, a.bounds.IncludesStart()
	case 1:
		return b.start, b.bounds.IncludesStart()
	default:
		return a.start, a.bounds.IncludesStart() || b.bounds.IncludesStart()
	}
}

func laterEndForHull(a, b Period) (time.Time, bool) {
	switch a.end.Compare(b.end) {
	case -1:
		return b.end, b.bounds.IncludesEnd()
	case 1:
		return a.end, a.bounds.IncludesEnd()
	default:
		return a.end, a.bounds.IncludesEnd() || b.bounds.IncludesEnd()
	}
}

func boundsFrom(includeStart, includeEnd bool) temporal.Bounds {
	switch {
	case includeStart && includeEnd:
		return temporal.Closed
	case includeStart:
		return temporal.ClosedOpen
	case includeEnd:
		return temporal.OpenClosed
	default:
		return temporal.Open
	}
}
