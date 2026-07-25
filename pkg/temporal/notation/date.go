package notation

import (
	"fmt"
	"unicode/utf8"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

// ParseDate decodes one complete bounded civil-date interval.
func ParseDate(value string, format Format, limits temporal.Limits) (dateperiod.Period, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return dateperiod.Period{}, err
	}
	if len(value) > limits.ParseBytes {
		return dateperiod.Period{}, &temporal.LimitError{
			Field: "parse_bytes", Value: len(value), Max: limits.ParseBytes,
		}
	}
	if !utf8.ValidString(value) {
		return dateperiod.Period{}, fmt.Errorf("%w: invalid UTF-8", temporal.ErrParse)
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
		return dateperiod.Period{}, temporal.ErrUnsupported
	}
	if err != nil {
		return dateperiod.Period{}, err
	}

	start, err := calendar.ParseDate(startText)
	if err != nil {
		return dateperiod.Period{}, fmt.Errorf("%w: start: %w", temporal.ErrParse, err)
	}
	end, err := calendar.ParseDate(endText)
	if err != nil {
		return dateperiod.Period{}, fmt.Errorf("%w: end: %w", temporal.ErrParse, err)
	}
	period, err := dateperiod.New(start, end, bounds)
	if err != nil {
		return dateperiod.Period{}, fmt.Errorf("%w: %w", temporal.ErrParse, err)
	}
	return period, nil
}

// FormatDate encodes a bounded civil-date interval without semantic loss.
func FormatDate(period dateperiod.Period, format Format, limits temporal.Limits) (string, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return "", err
	}

	start := period.Start().String()
	end := period.End().String()
	var value string
	switch format {
	case ISO8601:
		if period.Bounds() != temporal.ClosedOpen {
			return "", fmt.Errorf("%w: ISO 8601 start/end does not encode bounds", temporal.ErrUnsupported)
		}
		value = start + "/" + end
	case ISO80000:
		left, right := isoBrackets(period.Bounds())
		value = left + start + "," + end + right
	case Bourbaki:
		left, right := bourbakiBrackets(period.Bounds())
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
