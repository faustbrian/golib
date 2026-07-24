package notation

import (
	"fmt"
	"unicode/utf8"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

// ParseDailyInterval decodes one complete ordinary, circular, collapsed, or
// full-day local-time interval.
func ParseDailyInterval(value string, format Format, limits temporal.Limits) (timeofday.Interval, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return timeofday.Interval{}, err
	}
	if len(value) > limits.ParseBytes {
		return timeofday.Interval{}, &temporal.LimitError{
			Field: "parse_bytes", Value: len(value), Max: limits.ParseBytes,
		}
	}
	if !utf8.ValidString(value) {
		return timeofday.Interval{}, fmt.Errorf("%w: invalid UTF-8", temporal.ErrParse)
	}

	var startText, endText string
	var bounds temporal.Bounds
	var err error
	switch format {
	case ISO8601:
		startText, endText, err = splitExactly(value, '/')
		bounds = temporal.ClosedOpen
	case ISO80000:
		startText, endText, bounds, err = splitBounded(value, false)
	case Bourbaki:
		startText, endText, bounds, err = splitBounded(value, true)
	default:
		return timeofday.Interval{}, temporal.ErrUnsupported
	}
	if err != nil {
		return timeofday.Interval{}, err
	}

	start, err := timeofday.Parse(startText, limits)
	if err != nil {
		return timeofday.Interval{}, fmt.Errorf("%w: start: %w", temporal.ErrParse, err)
	}
	end, err := timeofday.Parse(endText, limits)
	if err != nil {
		return timeofday.Interval{}, fmt.Errorf("%w: end: %w", temporal.ErrParse, err)
	}

	if start.Equal(timeofday.Midnight()) && end.IsEndBoundary() && bounds == temporal.Closed {
		return timeofday.FullDay(), nil
	}
	if start.Equal(end) {
		if bounds == temporal.Open {
			return timeofday.Collapsed(start), nil
		}
		return timeofday.Interval{}, temporal.ErrInvalidTime
	}
	if start.IsEndBoundary() && end.Equal(timeofday.Midnight()) {
		return timeofday.Interval{}, temporal.ErrInvalidTime
	}

	interval, _ := timeofday.Between(start, end, bounds)
	return interval, nil
}

// FormatDailyInterval encodes a daily interval without semantic loss.
func FormatDailyInterval(interval timeofday.Interval, format Format, limits temporal.Limits) (string, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return "", err
	}

	start := interval.Start().String()
	end := interval.End().String()
	var value string
	switch format {
	case ISO8601:
		if interval.Bounds() != temporal.ClosedOpen ||
			interval.Kind() == timeofday.CollapsedKind || interval.Kind() == timeofday.FullDayKind {
			return "", fmt.Errorf("%w: ISO 8601 start/end does not encode daily interval kind or bounds", temporal.ErrUnsupported)
		}
		value = start + "/" + end
	case ISO80000:
		left, right := isoBrackets(interval.Bounds())
		value = left + start + "," + end + right
	case Bourbaki:
		left, right := bourbakiBrackets(interval.Bounds())
		value = left + start + "," + end + right
	default:
		return "", temporal.ErrUnsupported
	}

	if len(value) > limits.FormatBytes {
		return "", &temporal.LimitError{
			Field: "format_bytes", Value: len(value), Max: limits.FormatBytes,
		}
	}
	return value, nil
}
