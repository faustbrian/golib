package timeofday

import (
	"iter"
	"sort"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

type dailySegment struct {
	start        time.Duration
	end          time.Duration
	includeStart bool
	includeEnd   bool
}

// IntervalSet is an immutable normalized collection of disjoint linear daily
// segments. Circular inputs are split at midnight for stable ordering.
type IntervalSet struct {
	segments []dailySegment
	limits   temporal.Limits
}

// NewIntervalSet constructs a normalized daily interval set.
func NewIntervalSet(limits temporal.Limits, intervals ...Interval) (IntervalSet, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return IntervalSet{}, err
	}
	if len(intervals) > limits.InputPeriods {
		return IntervalSet{}, &temporal.LimitError{
			Field: "input_periods",
			Value: len(intervals),
			Max:   limits.InputPeriods,
		}
	}

	segments := make([]dailySegment, 0, len(intervals))
	for _, interval := range intervals {
		segments = append(segments, interval.segments()...)
	}

	return newIntervalSetFromSegments(limits, segments)
}

// Len returns the number of normalized linear segments.
func (s IntervalSet) Len() int {
	return len(s.segments)
}

// Intervals returns a copied stable linear representation of the set.
func (s IntervalSet) Intervals() []Interval {
	result := make([]Interval, len(s.segments))
	for index, segment := range s.segments {
		if segment.start == 0 && segment.end == day && segment.includeStart && segment.includeEnd {
			result[index] = FullDay()
			continue
		}
		result[index] = Interval{
			start:  timeFromOffset(segment.start),
			end:    timeFromOffset(segment.end),
			bounds: dailyBounds(segment.includeStart, segment.includeEnd),
			kind:   Ordinary,
		}
	}

	return result
}

// Equal reports represented-set equality between normalized sets.
func (s IntervalSet) Equal(other IntervalSet) bool {
	if len(s.segments) != len(other.segments) {
		return false
	}
	for index := range s.segments {
		if s.segments[index] != other.segments[index] {
			return false
		}
	}

	return true
}

// Includes reports whether any normalized segment includes value.
func (s IntervalSet) Includes(value Time) bool {
	index := sort.Search(len(s.segments), func(index int) bool {
		return s.segments[index].end >= value.offset
	})
	return index < len(s.segments) && segmentIncludes(s.segments[index], value.offset)
}

// Duration returns the total fixed coverage of the normalized daily set.
func (s IntervalSet) Duration() time.Duration {
	var total time.Duration
	for _, segment := range s.segments {
		total += segment.end - segment.start
	}
	return total
}

// Search returns the normalized segment containing value.
func (s IntervalSet) Search(value Time) (int, bool) {
	index := sort.Search(len(s.segments), func(index int) bool {
		return s.segments[index].end >= value.offset
	})
	return index, index < len(s.segments) && segmentIncludes(s.segments[index], value.offset)
}

// All returns a stable iterator over copied normalized intervals.
func (s IntervalSet) All() iter.Seq[Interval] {
	intervals := s.Intervals()
	return func(yield func(Interval) bool) {
		for _, interval := range intervals {
			if !yield(interval) {
				return
			}
		}
	}
}

// Union returns the normalized union with other.
func (s IntervalSet) Union(other IntervalSet) (IntervalSet, error) {
	segments := make([]dailySegment, 0, len(s.segments)+len(other.segments))
	segments = append(segments, s.segments...)
	segments = append(segments, other.segments...)
	return newIntervalSetFromSegments(s.effectiveLimits(), segments)
}

// Intersect returns the normalized common daily members in O(n+m) time.
func (s IntervalSet) Intersect(other IntervalSet) (IntervalSet, error) {
	limits := s.effectiveLimits()
	result := make([]dailySegment, 0, min(len(s.segments), len(other.segments)))

	for left, right := 0, 0; left < len(s.segments) && right < len(other.segments); {
		if intersection, ok := intersectDaily(s.segments[left], other.segments[right]); ok {
			if len(result) == limits.OutputPeriods {
				return IntervalSet{}, dailyLimitError(len(result)+1, limits.OutputPeriods)
			}
			result = append(result, intersection)
		}
		switch {
		case s.segments[left].end < other.segments[right].end:
			left++
		case s.segments[left].end > other.segments[right].end:
			right++
		default:
			left++
			right++
		}
	}

	return newIntervalSetFromSegments(limits, result)
}

// Subtract returns members of s which are not members of other.
func (s IntervalSet) Subtract(other IntervalSet) (IntervalSet, error) {
	limits := s.effectiveLimits()
	result := append([]dailySegment(nil), s.segments...)
	for _, removed := range other.segments {
		next := make([]dailySegment, 0, len(result)+1)
		for _, segment := range result {
			fragments := subtractDaily(segment, removed)
			if len(next)+len(fragments) > limits.OutputPeriods {
				return IntervalSet{}, dailyLimitError(len(next)+len(fragments), limits.OutputPeriods)
			}
			next = append(next, fragments...)
		}
		result = next
	}

	return newIntervalSetFromSegments(limits, result)
}

// Complement returns the set difference from the explicit closed full-day
// universe.
func (s IntervalSet) Complement() (IntervalSet, error) {
	universe := IntervalSet{
		segments: FullDay().segments(),
		limits:   s.effectiveLimits(),
	}
	return universe.Subtract(s)
}

// Gaps returns the complement against the explicit full-day universe.
func (s IntervalSet) Gaps() (IntervalSet, error) { return s.Complement() }

func newIntervalSetFromSegments(limits temporal.Limits, segments []dailySegment) (IntervalSet, error) {
	sort.SliceStable(segments, func(i, j int) bool {
		if segments[i].start != segments[j].start {
			return segments[i].start < segments[j].start
		}
		if segments[i].includeStart != segments[j].includeStart {
			return segments[i].includeStart
		}
		if segments[i].end != segments[j].end {
			return segments[i].end < segments[j].end
		}
		return segments[i].includeEnd && !segments[j].includeEnd
	})

	normalized := make([]dailySegment, 0, len(segments))
	for _, segment := range segments {
		if segmentEmpty(segment) {
			continue
		}
		last := len(normalized) - 1
		if last >= 0 && dailyMergeable(normalized[last], segment) {
			normalized[last] = mergeDaily(normalized[last], segment)
			continue
		}
		if len(normalized) == limits.OutputPeriods {
			return IntervalSet{}, dailyLimitError(len(normalized)+1, limits.OutputPeriods)
		}
		normalized = append(normalized, segment)
	}

	return IntervalSet{segments: normalized, limits: limits}, nil
}

func (s IntervalSet) effectiveLimits() temporal.Limits {
	return s.limits.Resolve()
}

func segmentIncludes(segment dailySegment, value time.Duration) bool {
	if value < segment.start || value > segment.end {
		return false
	}
	if value == segment.start && !segment.includeStart {
		return false
	}
	if value == segment.end && !segment.includeEnd {
		return false
	}
	return true
}

func segmentEmpty(segment dailySegment) bool {
	return segment.start > segment.end ||
		(segment.start == segment.end && (!segment.includeStart || !segment.includeEnd))
}

func dailyMergeable(left, right dailySegment) bool {
	return right.start < left.end ||
		(right.start == left.end && (left.includeEnd || right.includeStart))
}

func mergeDaily(left, right dailySegment) dailySegment {
	if left.start == right.start {
		left.includeStart = left.includeStart || right.includeStart
	}
	if right.end > left.end {
		left.end = right.end
		left.includeEnd = right.includeEnd
	} else if right.end == left.end {
		left.includeEnd = left.includeEnd || right.includeEnd
	}
	return left
}

func intersectDaily(left, right dailySegment) (dailySegment, bool) {
	result := dailySegment{}
	switch {
	case left.start > right.start:
		result.start, result.includeStart = left.start, left.includeStart
	case left.start < right.start:
		result.start, result.includeStart = right.start, right.includeStart
	default:
		result.start = left.start
		result.includeStart = left.includeStart && right.includeStart
	}
	switch {
	case left.end < right.end:
		result.end, result.includeEnd = left.end, left.includeEnd
	case left.end > right.end:
		result.end, result.includeEnd = right.end, right.includeEnd
	default:
		result.end = left.end
		result.includeEnd = left.includeEnd && right.includeEnd
	}

	return result, !segmentEmpty(result)
}

func subtractDaily(value, removed dailySegment) []dailySegment {
	intersection, ok := intersectDaily(value, removed)
	if !ok {
		return []dailySegment{value}
	}
	if intersection == value {
		return nil
	}

	result := make([]dailySegment, 0, 2)
	if value.start < intersection.start ||
		value.start == intersection.start && value.includeStart && !intersection.includeStart {
		result = append(result, dailySegment{
			start:        value.start,
			end:          intersection.start,
			includeStart: value.includeStart,
			includeEnd:   !intersection.includeStart,
		})
	}
	if intersection.end < value.end ||
		intersection.end == value.end && value.includeEnd && !intersection.includeEnd {
		result = append(result, dailySegment{
			start:        intersection.end,
			end:          value.end,
			includeStart: !intersection.includeEnd,
			includeEnd:   value.includeEnd,
		})
	}
	return result
}

func timeFromOffset(offset time.Duration) Time {
	if offset == day {
		return EndOfDay()
	}
	return Time{offset: offset, fractionalDigits: 9, hasSeconds: true}
}

func dailyBounds(includeStart, includeEnd bool) temporal.Bounds {
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

func dailyLimitError(value, maximum int) error {
	return &temporal.LimitError{Field: "output_periods", Value: value, Max: maximum}
}
