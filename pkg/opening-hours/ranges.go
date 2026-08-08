package openinghours

import (
	"slices"
	"time"
)

const maxOutputRanges = 8192

// DailyRange is a start-inclusive, end-exclusive interval within one civil
// date. EndAtDayBoundary distinguishes midnight at the end from midnight at
// the start of the date.
type DailyRange struct {
	Start            LocalTime
	End              LocalTime
	EndAtDayBoundary bool
}

// InstantRange is a start-inclusive, end-exclusive absolute interval.
type InstantRange struct {
	Start time.Time
	End   time.Time
}

// EffectiveRanges returns normalized availability fragments for one civil date.
func (s Schedule) EffectiveRanges(date Date) ([]DailyRange, error) {
	if !validDate(date) {
		return nil, newError("effective ranges", CodeInvalidDate)
	}
	if s.data == nil {
		return []DailyRange{}, nil
	}
	segments, _, err := s.effectiveSegments(date)
	if err != nil {
		return nil, err
	}
	if len(segments) > maxOutputRanges {
		return nil, newError("effective ranges", CodeLimitExceeded)
	}
	result := make([]DailyRange, 0, len(segments))
	for _, item := range segments {
		endAtBoundary := item.end == nanosecondsPerDay
		end := item.end
		if endAtBoundary {
			end = 0
		}
		result = append(result, DailyRange{
			Start:            LocalTime{nanosecond: item.start},
			End:              LocalTime{nanosecond: end},
			EndAtDayBoundary: endAtBoundary,
		})
	}

	return result, nil
}

// EffectiveInstantRanges returns clipped absolute availability within a bounded
// interval of at most 366 elapsed days.
func (s Schedule) EffectiveInstantRanges(start, end time.Time) ([]InstantRange, error) {
	if !end.After(start) || end.Sub(start) > maxSearchHorizon {
		return nil, newError("effective instant ranges", CodeInvalidInterval)
	}
	if s.data == nil {
		return []InstantRange{}, nil
	}
	localized := start.In(s.data.location)
	first, _ := NewDate(localized.Year(), localized.Month(), localized.Day())
	localizedEnd := end.Add(-time.Nanosecond).In(s.data.location)
	last, _ := NewDate(localizedEnd.Year(), localizedEnd.Month(), localizedEnd.Day())
	dayCount := first.DaysUntil(last) + 1
	result := make([]InstantRange, 0, 16)
	for step := range dayCount {
		date, _ := addDate(first, step)
		segments, _, err := s.effectiveSegments(date)
		if err != nil {
			return nil, err
		}
		for _, item := range segments {
			// Effective segments are clipped to a valid date, so an opening
			// boundary cannot cross the civil-date domain.
			opening, _ := s.resolveBoundary(date, item.start, true)
			closing, err := s.resolveBoundary(date, item.end, false)
			if err != nil {
				return nil, err
			}
			opening = latestInstant(opening, start)
			closing = earliestInstant(closing, end)
			if opening.Before(closing) {
				result = append(result, InstantRange{Start: opening, End: closing})
				if len(result) > maxOutputRanges {
					return nil, newError("effective instant ranges", CodeLimitExceeded)
				}
			}
		}
	}

	return normalizeInstantRanges(result), nil
}

func normalizeInstantRanges(input []InstantRange) []InstantRange {
	if len(input) == 0 {
		return []InstantRange{}
	}
	result := make([]InstantRange, len(input))
	copy(result, input)
	slices.SortFunc(result, func(left, right InstantRange) int {
		switch comparison := left.Start.Compare(right.Start); comparison {
		case -1, 1:
			return comparison
		default:
			return left.End.Compare(right.End)
		}
	})
	output := result[:1]
	for _, item := range result[1:] {
		last := &output[len(output)-1]
		if !item.Start.After(last.End) {
			if item.End.After(last.End) {
				last.End = item.End
			}
			continue
		}
		output = append(output, item)
	}

	return output
}

func latestInstant(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}

	return right
}

func earliestInstant(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}

	return right
}

// OpenDuration returns elapsed open time within a bounded absolute interval.
func (s Schedule) OpenDuration(start, end time.Time) (time.Duration, error) {
	ranges, err := s.EffectiveInstantRanges(start, end)
	if err != nil {
		return 0, err
	}
	var total time.Duration
	for _, item := range ranges {
		total += item.End.Sub(item.Start)
	}

	return total, nil
}
