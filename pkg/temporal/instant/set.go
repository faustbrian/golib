package instant

import (
	"sort"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// Set is an immutable, normalized collection of disjoint instant periods.
// Its canonical order is stable and all slice boundaries are copied.
type Set struct {
	periods []Period
	limits  temporal.Limits
}

// NewSet validates, copies, sorts, and normalizes periods. Empty periods are
// discarded and mergeable periods are combined.
func NewSet(limits temporal.Limits, periods ...Period) (Set, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return Set{}, err
	}
	if len(periods) > limits.InputPeriods {
		return Set{}, &temporal.LimitError{
			Field: "input_periods",
			Value: len(periods),
			Max:   limits.InputPeriods,
		}
	}

	working := make([]Period, 0, len(periods))
	for _, period := range periods {
		if !period.IsEmpty() {
			working = append(working, period)
		}
	}

	sort.SliceStable(working, func(i, j int) bool {
		return lessPeriod(working[i], working[j])
	})

	normalized := make([]Period, 0, len(working))
	for _, period := range working {
		last := len(normalized) - 1
		if last >= 0 && mergeable(normalized[last], period) {
			normalized[last] = mergePeriods(normalized[last], period)
			continue
		}

		if len(normalized) == limits.OutputPeriods {
			return Set{}, &temporal.LimitError{
				Field: "output_periods",
				Value: len(normalized) + 1,
				Max:   limits.OutputPeriods,
			}
		}
		normalized = append(normalized, period)
	}

	return Set{periods: normalized, limits: limits}, nil
}

// Len returns the number of normalized periods.
func (s Set) Len() int {
	return len(s.periods)
}

// Periods returns a copy of the normalized periods.
func (s Set) Periods() []Period {
	result := make([]Period, len(s.periods))
	copy(result, s.periods)

	return result
}

// Equal reports represented-set equality between normalized sets.
func (s Set) Equal(other Set) bool {
	if len(s.periods) != len(other.periods) {
		return false
	}
	for index := range s.periods {
		if !s.periods[index].SetEqual(other.periods[index]) {
			return false
		}
	}

	return true
}

// Span returns the smallest period containing all members of the set.
func (s Set) Span() (Period, bool) {
	if len(s.periods) == 0 {
		return Period{}, false
	}

	first := s.periods[0]
	last := s.periods[len(s.periods)-1]
	return Period{
		start: first.start,
		end:   last.end,
		bounds: boundsFrom(
			first.bounds.IncludesStart(),
			last.bounds.IncludesEnd(),
		),
	}, true
}

// TotalDuration returns the sum of covered elapsed durations.
func (s Set) TotalDuration() (time.Duration, error) {
	var total time.Duration
	for _, period := range s.periods {
		duration, err := period.Duration()
		if err != nil {
			return 0, err
		}
		if duration > 0 && total > time.Duration(1<<63-1)-duration {
			return 0, temporal.ErrOverflow
		}
		total += duration
	}

	return total, nil
}

// Includes reports whether any normalized period includes value.
func (s Set) Includes(value time.Time) bool {
	index := sort.Search(len(s.periods), func(index int) bool {
		return !s.periods[index].end.Before(value)
	})

	return index < len(s.periods) && s.periods[index].Includes(value)
}

// Gaps returns the exact gaps between adjacent normalized periods.
func (s Set) Gaps() []Period {
	if len(s.periods) < 2 {
		return nil
	}

	result := make([]Period, 0, len(s.periods)-1)
	for index := 1; index < len(s.periods); index++ {
		if gap, ok := s.periods[index-1].Gap(s.periods[index]); ok {
			result = append(result, gap)
		}
	}

	return result
}

// Union returns the normalized union with other.
func (s Set) Union(other Set) (Set, error) {
	combined := make([]Period, 0, len(s.periods)+len(other.periods))
	combined = append(combined, s.periods...)
	combined = append(combined, other.periods...)

	return NewSet(s.effectiveLimits(), combined...)
}

// Intersect returns the normalized intersection with other in O(n+m) time.
func (s Set) Intersect(other Set) (Set, error) {
	limits := s.effectiveLimits()
	result := make([]Period, 0, min(len(s.periods), len(other.periods)))

	for left, right := 0, 0; left < len(s.periods) && right < len(other.periods); {
		if intersection, ok := s.periods[left].Intersect(other.periods[right]); ok {
			if len(result) == limits.OutputPeriods {
				return Set{}, &temporal.LimitError{
					Field: "output_periods",
					Value: len(result) + 1,
					Max:   limits.OutputPeriods,
				}
			}
			result = append(result, intersection)
		}

		switch s.periods[left].end.Compare(other.periods[right].end) {
		case -1:
			left++
		case 1:
			right++
		default:
			left++
			right++
		}
	}

	return NewSet(limits, result...)
}

// Subtract returns all members of s which are not members of other.
func (s Set) Subtract(other Set) (Set, error) {
	limits := s.effectiveLimits()
	result := s.Periods()

	for _, removed := range other.periods {
		next := make([]Period, 0, len(result)+1)
		for _, period := range result {
			fragments := period.Subtract(removed)
			if len(next)+len(fragments) > limits.OutputPeriods {
				return Set{}, &temporal.LimitError{
					Field: "output_periods",
					Value: len(next) + len(fragments),
					Max:   limits.OutputPeriods,
				}
			}
			next = append(next, fragments...)
		}
		result = next
	}

	return NewSet(limits, result...)
}

func (s Set) effectiveLimits() temporal.Limits {
	return s.limits.Resolve()
}

func lessPeriod(left, right Period) bool {
	if comparison := left.start.Compare(right.start); comparison != 0 {
		return comparison < 0
	}
	if left.bounds.IncludesStart() != right.bounds.IncludesStart() {
		return left.bounds.IncludesStart()
	}
	if comparison := left.end.Compare(right.end); comparison != 0 {
		return comparison < 0
	}

	return left.bounds.IncludesEnd() && !right.bounds.IncludesEnd()
}

func mergeable(left, right Period) bool {
	if right.start.Before(left.end) {
		return true
	}

	return right.start.Equal(left.end) &&
		(left.bounds.IncludesEnd() || right.bounds.IncludesStart())
}

func mergePeriods(left, right Period) Period {
	includeStart := left.bounds.IncludesStart()
	if left.start.Equal(right.start) {
		includeStart = includeStart || right.bounds.IncludesStart()
	}

	end := left.end
	includeEnd := left.bounds.IncludesEnd()
	switch left.end.Compare(right.end) {
	case -1:
		end = right.end
		includeEnd = right.bounds.IncludesEnd()
	case 0:
		includeEnd = includeEnd || right.bounds.IncludesEnd()
	}

	return Period{
		start:  left.start,
		end:    end,
		bounds: boundsFrom(includeStart, includeEnd),
	}
}
