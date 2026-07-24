// Package openinghourstemporal provides lossless adapters for
// temporal/timeofday values.
package openinghourstemporal

import (
	"errors"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

// ErrLossyMapping reports an interval whose state or bounds cannot be
// represented without changing semantics.
var ErrLossyMapping = errors.New("openinghourstemporal: lossy mapping")

// RangeFromInterval converts an ordinary or circular start-inclusive,
// end-exclusive interval.
func RangeFromInterval(interval timeofday.Interval) (openinghours.Range, error) {
	if interval.Bounds() != temporal.ClosedOpen ||
		(interval.Kind() != timeofday.Ordinary && interval.Kind() != timeofday.Circular) {
		return openinghours.Range{}, ErrLossyMapping
	}
	start, err := localTime(interval.Start(), false)
	if err != nil {
		return openinghours.Range{}, err
	}
	end, _ := localTime(interval.End(), true)
	return openinghours.NewRange(start, end)
}

// IntervalFromRange converts a range while making fractional precision
// explicit. Digits must exactly represent both endpoints.
func IntervalFromRange(value openinghours.Range, digits int) (timeofday.Interval, error) {
	start, err := temporalTime(value.Start(), digits)
	if err != nil {
		return timeofday.Interval{}, err
	}
	end, err := temporalTime(value.End(), digits)
	if err != nil {
		return timeofday.Interval{}, err
	}
	return timeofday.Between(start, end, temporal.ClosedOpen)
}

// RuleFromIntervals converts an explicitly bounded interval collection. An
// empty or single collapsed interval maps to closed; full day must stand alone.
func RuleFromIntervals(intervals []timeofday.Interval,
	policy openinghours.OverlapPolicy,
) (openinghours.DayRule, error) {
	if len(intervals) == 0 {
		return openinghours.Closed(), nil
	}
	if len(intervals) == 1 {
		switch intervals[0].Kind() {
		case timeofday.FullDayKind:
			return openinghours.OpenAllDay(), nil
		case timeofday.CollapsedKind:
			return openinghours.Closed(), nil
		case timeofday.Ordinary, timeofday.Circular:
		}
	}
	ranges := make([]openinghours.Range, 0, len(intervals))
	for _, interval := range intervals {
		if interval.Kind() == timeofday.FullDayKind || interval.Kind() == timeofday.CollapsedKind {
			return openinghours.DayRule{}, ErrLossyMapping
		}
		converted, err := RangeFromInterval(interval)
		if err != nil {
			return openinghours.DayRule{}, err
		}
		ranges = append(ranges, converted)
	}
	return openinghours.OpenRanges(ranges, policy)
}

func localTime(value timeofday.Time, end bool) (openinghours.LocalTime, error) {
	if value.IsEndBoundary() {
		if end {
			return openinghours.LocalTime{}, nil
		}
		return openinghours.LocalTime{}, ErrLossyMapping
	}
	hour, minute, second, nanosecond := value.Components()
	return openinghours.NewLocalTime(hour, minute, second, nanosecond)
}

func temporalTime(value openinghours.LocalTime, digits int) (timeofday.Time, error) {
	return timeofday.New(
		value.Hour(), value.Minute(), value.Second(), value.Nanosecond(), digits,
	)
}
