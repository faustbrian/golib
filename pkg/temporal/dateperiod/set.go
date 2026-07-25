package dateperiod

import (
	"sort"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

// Set is an immutable normalized collection of disjoint civil-date periods.
type Set struct {
	periods []Period
	limits  temporal.Limits
}

// NewSet copies and normalizes periods into closed included-date ranges.
func NewSet(limits temporal.Limits, periods ...Period) (Set, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return Set{}, err
	}
	if len(periods) > limits.InputPeriods {
		return Set{}, dateLimitError("input_periods", len(periods), limits.InputPeriods)
	}

	working := make([]Period, 0, len(periods))
	for _, period := range periods {
		if canonical, ok := period.canonical(); ok {
			working = append(working, canonical)
		}
	}
	sort.SliceStable(working, func(i, j int) bool {
		comparison := compareDate(working[i].start, working[j].start)
		if comparison != 0 {
			return comparison < 0
		}
		return compareDate(working[i].end, working[j].end) < 0
	})

	normalized := make([]Period, 0, len(working))
	for _, period := range working {
		last := len(normalized) - 1
		if last >= 0 && normalized[last].end.DaysUntil(period.start) <= 1 {
			if compareDate(period.end, normalized[last].end) > 0 {
				normalized[last].end = period.end
			}
			continue
		}
		if len(normalized) == limits.OutputPeriods {
			return Set{}, dateLimitError("output_periods", len(normalized)+1, limits.OutputPeriods)
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

// Equal reports represented-set equality.
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

// TotalDays returns the number of represented civil dates.
func (s Set) TotalDays() int {
	total := 0
	for _, period := range s.periods {
		total += period.Days()
	}
	return total
}

// Includes reports whether any normalized period contains date.
func (s Set) Includes(date calendar.Date) bool {
	index := sort.Search(len(s.periods), func(index int) bool {
		return compareDate(s.periods[index].end, date) >= 0
	})
	return index < len(s.periods) && s.periods[index].Includes(date)
}

// Span returns the smallest closed period containing the set.
func (s Set) Span() (Period, bool) {
	if len(s.periods) == 0 {
		return Period{}, false
	}
	return Period{
		start:  s.periods[0].start,
		end:    s.periods[len(s.periods)-1].end,
		bounds: temporal.Closed,
	}, true
}

// Gaps returns exact closed missing-date ranges between normalized periods.
func (s Set) Gaps() []Period {
	if len(s.periods) < 2 {
		return nil
	}
	result := make([]Period, 0, len(s.periods)-1)
	for index := 1; index < len(s.periods); index++ {
		start, _ := s.periods[index-1].end.AddDays(1)
		end, _ := s.periods[index].start.AddDays(-1)
		result = append(result, Period{start: start, end: end, bounds: temporal.Closed})
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

// Intersect returns common represented dates in O(n+m) time.
func (s Set) Intersect(other Set) (Set, error) {
	limits := s.effectiveLimits()
	result := make([]Period, 0, min(len(s.periods), len(other.periods)))
	for left, right := 0, 0; left < len(s.periods) && right < len(other.periods); {
		if intersection, ok := s.periods[left].intersect(other.periods[right]); ok {
			if len(result) == limits.OutputPeriods {
				return Set{}, dateLimitError("output_periods", len(result)+1, limits.OutputPeriods)
			}
			result = append(result, intersection)
		}
		switch compareDate(s.periods[left].end, other.periods[right].end) {
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

// Subtract returns dates in s which are not in other.
func (s Set) Subtract(other Set) (Set, error) {
	limits := s.effectiveLimits()
	result := s.Periods()
	for _, removed := range other.periods {
		next := make([]Period, 0, len(result)+1)
		for _, period := range result {
			fragments := period.subtract(removed)
			if len(next)+len(fragments) > limits.OutputPeriods {
				return Set{}, dateLimitError("output_periods", len(next)+len(fragments), limits.OutputPeriods)
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

func dateLimitError(field string, value, maximum int) error {
	return &temporal.LimitError{Field: field, Value: value, Max: maximum}
}
