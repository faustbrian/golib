package timeofday

import (
	"fmt"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// IntervalKind distinguishes ordinary, circular, collapsed, and full-day
// interval semantics.
type IntervalKind uint8

const (
	// Ordinary has a start before its end on the same local day.
	Ordinary IntervalKind = iota
	// Circular crosses midnight.
	Circular
	// CollapsedKind is the explicit empty daily interval.
	CollapsedKind
	// FullDayKind is the explicit full-day universe.
	FullDayKind
)

// String returns the stable interval-kind name.
func (k IntervalKind) String() string {
	switch k {
	case Ordinary:
		return "Ordinary"
	case Circular:
		return "Circular"
	case CollapsedKind:
		return "Collapsed"
	case FullDayKind:
		return "FullDay"
	default:
		return ""
	}
}

// Interval is an immutable daily interval between local times.
type Interval struct {
	start  Time
	end    Time
	bounds temporal.Bounds
	kind   IntervalKind
}

// Between constructs an ordinary or circular interval. Equal endpoints are
// rejected because callers must choose Collapsed or FullDay explicitly.
func Between(start, end Time, bounds temporal.Bounds) (Interval, error) {
	if !bounds.Valid() {
		return Interval{}, fmt.Errorf("%w: %d", temporal.ErrBounds, bounds)
	}
	comparison := start.Compare(end)
	if comparison == 0 {
		return Interval{}, temporal.ErrInvalidTime
	}

	kind := Ordinary
	if comparison > 0 {
		kind = Circular
	}

	return Interval{start: start, end: end, bounds: bounds, kind: kind}, nil
}

// Collapsed returns an explicit empty interval anchored at value.
func Collapsed(value Time) Interval {
	return Interval{start: value, end: value, bounds: temporal.Open, kind: CollapsedKind}
}

// FullDay returns the explicit closed daily universe from 00:00 through 24:00.
func FullDay() Interval {
	return Interval{
		start:  Midnight(),
		end:    EndOfDay(),
		bounds: temporal.Closed,
		kind:   FullDayKind,
	}
}

// Start returns the local start value.
func (i Interval) Start() Time {
	return i.start
}

// End returns the local end value.
func (i Interval) End() Time {
	return i.end
}

// Bounds returns endpoint inclusion.
func (i Interval) Bounds() temporal.Bounds {
	return i.bounds
}

// WithBounds returns a copy with replaced endpoint inclusion.
func (i Interval) WithBounds(bounds temporal.Bounds) (Interval, error) {
	if !bounds.Valid() {
		return Interval{}, fmt.Errorf("%w: %d", temporal.ErrBounds, bounds)
	}
	if i.kind == CollapsedKind || i.kind == FullDayKind {
		return i, nil
	}

	i.bounds = bounds
	return i, nil
}

// WithStart returns a copy with a replaced start. A collapsed interval moves
// its anchor; a full-day universe cannot replace only one endpoint.
func (i Interval) WithStart(start Time) (Interval, error) {
	switch i.kind {
	case CollapsedKind:
		return Collapsed(start), nil
	case FullDayKind:
		return Interval{}, temporal.ErrUnsupported
	default:
		return Between(start, i.end, i.bounds)
	}
}

// WithEnd returns a copy with a replaced end. A collapsed interval moves its
// anchor; a full-day universe cannot replace only one endpoint.
func (i Interval) WithEnd(end Time) (Interval, error) {
	switch i.kind {
	case CollapsedKind:
		return Collapsed(end), nil
	case FullDayKind:
		return Interval{}, temporal.ErrUnsupported
	default:
		return Between(i.start, end, i.bounds)
	}
}

// Kind returns the explicit interval kind.
func (i Interval) Kind() IntervalKind {
	return i.kind
}

// Duration returns fixed elapsed coverage, independent of boundary inclusion.
func (i Interval) Duration() time.Duration {
	switch i.kind {
	case CollapsedKind:
		return 0
	case FullDayKind:
		return day
	case Circular:
		return day - i.start.offset + i.end.offset
	default:
		return i.end.offset - i.start.offset
	}
}

// Includes reports whether value belongs to the represented daily set.
func (i Interval) Includes(value Time) bool {
	switch i.kind {
	case CollapsedKind:
		return false
	case FullDayKind:
		return value.offset >= 0 && value.offset <= day
	case Circular:
		if value.offset > i.start.offset || value.offset < i.end.offset {
			return true
		}
		return (value.Equal(i.start) && i.bounds.IncludesStart()) ||
			(value.Equal(i.end) && i.bounds.IncludesEnd())
	default:
		if value.Compare(i.start) < 0 || value.Compare(i.end) > 0 {
			return false
		}
		if value.Equal(i.start) && !i.bounds.IncludesStart() {
			return false
		}
		if value.Equal(i.end) && !i.bounds.IncludesEnd() {
			return false
		}
		return true
	}
}

// Equal reports structural equality.
func (i Interval) Equal(other Interval) bool {
	return i.start.Equal(other.start) && i.end.Equal(other.end) &&
		i.bounds == other.bounds && i.kind == other.kind
}

// SetEqual reports represented-set equality.
func (i Interval) SetEqual(other Interval) bool {
	left, _ := NewIntervalSet(temporal.Limits{}, i)
	right, _ := NewIntervalSet(temporal.Limits{}, other)
	return left.Equal(right)
}

// Contains reports whether every member of other belongs to i.
func (i Interval) Contains(other Interval) bool {
	outer, _ := NewIntervalSet(temporal.Limits{}, i)
	inner, _ := NewIntervalSet(temporal.Limits{}, other)
	difference, _ := inner.Subtract(outer)
	return difference.Len() == 0
}

// Overlaps reports whether the represented sets share at least one value.
func (i Interval) Overlaps(other Interval) bool {
	left, _ := NewIntervalSet(temporal.Limits{}, i)
	right, _ := NewIntervalSet(temporal.Limits{}, other)
	intersection, _ := left.Intersect(right)
	return intersection.Len() > 0
}

// Abuts reports whether an endpoint is equal to the other interval's endpoint.
func (i Interval) Abuts(other Interval) bool {
	return i.end.Equal(other.start) || other.end.Equal(i.start)
}

// Shift moves both endpoints around the fixed 24-hour daily universe.
func (i Interval) Shift(by time.Duration) (Interval, error) {
	switch i.kind {
	case FullDayKind:
		return i, nil
	case CollapsedKind:
		value, _ := i.start.Shift(by, Wrap)
		return Collapsed(value), nil
	case Ordinary, Circular:
	}

	start, _ := i.start.Shift(by, Wrap)
	end, _ := i.end.Shift(by, Wrap)
	return Between(start, end, i.bounds)
}

// Expand moves the start backward by before and the end forward by after.
// Coverage at or above one day becomes FullDay; non-positive coverage is
// rejected instead of ambiguously choosing a collapsed anchor.
func (i Interval) Expand(before, after time.Duration) (Interval, error) {
	if before == time.Duration(-1<<63) {
		return Interval{}, temporal.ErrOverflow
	}
	total, err := NewDuration(i.Duration()).Add(NewDuration(before))
	if err != nil {
		return Interval{}, err
	}
	total, err = total.Add(NewDuration(after))
	if err != nil {
		return Interval{}, err
	}
	if total.Value() <= 0 {
		return Interval{}, temporal.ErrEmpty
	}
	if total.Value() >= day {
		return FullDay(), nil
	}
	start, _ := i.start.Shift(-before, Wrap)
	end, _ := i.end.Shift(after, Wrap)
	return Between(start, end, i.bounds)
}

// Intersection returns the normalized represented-set intersection.
func (i Interval) Intersection(other Interval, limits temporal.Limits) (IntervalSet, error) {
	left, err := NewIntervalSet(limits, i)
	if err != nil {
		return IntervalSet{}, err
	}
	right, err := NewIntervalSet(limits, other)
	if err != nil {
		return IntervalSet{}, err
	}
	return left.Intersect(right)
}

// Union returns the normalized represented-set union.
func (i Interval) Union(other Interval, limits temporal.Limits) (IntervalSet, error) {
	return NewIntervalSet(limits, i, other)
}

// Difference returns members of i which are not members of other.
func (i Interval) Difference(other Interval, limits temporal.Limits) (IntervalSet, error) {
	left, err := NewIntervalSet(limits, i)
	if err != nil {
		return IntervalSet{}, err
	}
	right, err := NewIntervalSet(limits, other)
	if err != nil {
		return IntervalSet{}, err
	}
	return left.Subtract(right)
}

// Gap returns the ordinary interval strictly between two non-overlapping
// intervals. Abutting intervals return an explicit collapsed gap.
func (i Interval) Gap(other Interval) (Interval, error) {
	if i.Overlaps(other) {
		return Interval{}, temporal.ErrEmpty
	}
	if i.kind != Ordinary || other.kind != Ordinary {
		return Interval{}, temporal.ErrUnsupported
	}

	left, right := i, other
	if other.end.Compare(i.start) <= 0 {
		left, right = other, i
	}
	if left.end.Equal(right.start) {
		return Collapsed(left.end), nil
	}
	return Between(
		left.end,
		right.start,
		dailyBounds(!left.bounds.IncludesEnd(), !right.bounds.IncludesStart()),
	)
}

// Split partitions the interval into fixed-duration pieces while assigning
// every internal seam to exactly one piece.
func (i Interval) Split(step time.Duration, limits temporal.Limits) ([]Interval, error) {
	if step <= 0 {
		return nil, temporal.ErrStep
	}
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if i.kind == CollapsedKind {
		return nil, nil
	}

	count := int(i.Duration() / step)
	if i.Duration()%step != 0 {
		count++
	}
	if count > limits.Steps {
		return nil, &temporal.LimitError{Field: "steps", Value: count, Max: limits.Steps}
	}
	result := make([]Interval, 0, count)
	for elapsed := time.Duration(0); elapsed < i.Duration(); {
		next := elapsed + step
		if next < elapsed || next > i.Duration() {
			next = i.Duration()
		}
		start := i.timeAt(elapsed)
		end := i.timeAt(next)
		includeStart := elapsed > 0 || i.bounds.IncludesStart()
		includeEnd := next == i.Duration() && i.bounds.IncludesEnd()
		part, _ := Between(start, end, dailyBounds(includeStart, includeEnd))
		result = append(result, part)
		elapsed = next
	}
	return result, nil
}

// Steps returns fixed-duration positions belonging to the interval, moving
// forward from its start and wrapping at midnight when necessary.
func (i Interval) Steps(step time.Duration, limits temporal.Limits) ([]Time, error) {
	if step <= 0 {
		return nil, temporal.ErrStep
	}
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if i.kind == CollapsedKind {
		return nil, nil
	}

	first := time.Duration(0)
	if !i.bounds.IncludesStart() {
		first = step
	}
	result := make([]Time, 0, min(int(i.Duration()/step)+1, limits.Steps))
	for elapsed := first; elapsed < i.Duration() || elapsed == i.Duration() && i.bounds.IncludesEnd(); elapsed += step {
		if len(result) == limits.Steps {
			return nil, &temporal.LimitError{Field: "steps", Value: len(result) + 1, Max: limits.Steps}
		}
		result = append(result, i.timeAt(elapsed))
		if elapsed > i.Duration()-step {
			break
		}
	}
	return result, nil
}

func (i Interval) timeAt(elapsed time.Duration) Time {
	offset := i.start.offset + elapsed
	if offset == day {
		if i.kind == Circular {
			return Midnight()
		}
		return EndOfDay()
	}
	if offset > day {
		offset %= day
	}
	return timeFromOffset(offset)
}

func (i Interval) segments() []dailySegment {
	switch i.kind {
	case CollapsedKind:
		return nil
	case FullDayKind:
		return []dailySegment{{start: 0, end: day, includeStart: true, includeEnd: true}}
	case Circular:
		return []dailySegment{
			{start: 0, end: i.end.offset, includeStart: true, includeEnd: i.bounds.IncludesEnd()},
			{start: i.start.offset, end: day, includeStart: i.bounds.IncludesStart(), includeEnd: true},
		}
	default:
		return []dailySegment{{
			start:        i.start.offset,
			end:          i.end.offset,
			includeStart: i.bounds.IncludesStart(),
			includeEnd:   i.bounds.IncludesEnd(),
		}}
	}
}
