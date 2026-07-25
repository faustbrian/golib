// Package notation provides strict codecs for temporal interval notation.
package notation

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

// Format identifies a supported interval notation.
type Format uint8

const (
	// FormatInvalid is the safe zero value.
	FormatInvalid Format = iota
	// ISO8601 uses start/end form and implies closed-open bounds.
	ISO8601
	// ISO80000 uses conventional inclusive and exclusive brackets.
	ISO80000
	// Bourbaki uses inward and outward square brackets.
	Bourbaki
)

// ParseInstant decodes one complete bounded instant interval.
func ParseInstant(value string, format Format, limits temporal.Limits) (instant.Period, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return instant.Period{}, err
	}
	if len(value) > limits.ParseBytes {
		return instant.Period{}, &temporal.LimitError{
			Field: "parse_bytes",
			Value: len(value),
			Max:   limits.ParseBytes,
		}
	}
	if !utf8.ValidString(value) {
		return instant.Period{}, fmt.Errorf("%w: invalid UTF-8", temporal.ErrParse)
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
		return instant.Period{}, temporal.ErrUnsupported
	}
	if err != nil {
		return instant.Period{}, err
	}

	start, err := parseInstant(startText, limits.Precision)
	if err != nil {
		return instant.Period{}, fmt.Errorf("%w: start: %w", temporal.ErrParse, err)
	}
	end, err := parseInstant(endText, limits.Precision)
	if err != nil {
		return instant.Period{}, fmt.Errorf("%w: end: %w", temporal.ErrParse, err)
	}

	period, err := instant.New(start, end, bounds)
	if err != nil {
		return instant.Period{}, fmt.Errorf("%w: %w", temporal.ErrParse, err)
	}

	return period, nil
}

// FormatInstant encodes a bounded instant interval without semantic loss.
func FormatInstant(period instant.Period, format Format, limits temporal.Limits) (string, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return "", err
	}

	start := period.Start().Format(time.RFC3339Nano)
	end := period.End().Format(time.RFC3339Nano)
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
			Field: "format_bytes",
			Value: len(value),
			Max:   limits.FormatBytes,
		}
	}

	return value, nil
}

func splitBounded(value string, bourbaki bool) (string, string, temporal.Bounds, error) {
	if len(value) < 5 {
		return "", "", temporal.ClosedOpen, temporal.ErrParse
	}

	left := value[0]
	right := value[len(value)-1]
	includeStart, includeEnd, ok := bracketInclusion(left, right, bourbaki)
	if !ok {
		return "", "", temporal.ClosedOpen, temporal.ErrBounds
	}
	start, end, err := splitExactly(value[1:len(value)-1], ',')
	if err != nil {
		return "", "", temporal.ClosedOpen, err
	}

	return start, end, notationBounds(includeStart, includeEnd), nil
}

func splitExactly(value string, separator byte) (string, string, error) {
	index := strings.IndexByte(value, separator)
	if index <= 0 || index == len(value)-1 || strings.IndexByte(value[index+1:], separator) >= 0 {
		return "", "", temporal.ErrParse
	}

	return value[:index], value[index+1:], nil
}

func bracketInclusion(left, right byte, bourbaki bool) (bool, bool, bool) {
	if bourbaki {
		if (left != '[' && left != ']') || (right != '[' && right != ']') {
			return false, false, false
		}
		return left == '[', right == ']', true
	}

	if (left != '[' && left != '(') || (right != ']' && right != ')') {
		return false, false, false
	}
	return left == '[', right == ']', true
}

func notationBounds(includeStart, includeEnd bool) temporal.Bounds {
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

func isoBrackets(bounds temporal.Bounds) (string, string) {
	left := "("
	right := ")"
	if bounds.IncludesStart() {
		left = "["
	}
	if bounds.IncludesEnd() {
		right = "]"
	}

	return left, right
}

func bourbakiBrackets(bounds temporal.Bounds) (string, string) {
	left := "]"
	right := "["
	if bounds.IncludesStart() {
		left = "["
	}
	if bounds.IncludesEnd() {
		right = "]"
	}

	return left, right
}

func parseInstant(value string, precision int) (time.Time, error) {
	if fractionDigits(value) > precision {
		return time.Time{}, temporal.ErrPrecision
	}

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}

	return parsed, nil
}

func fractionDigits(value string) int {
	dot := strings.IndexByte(value, '.')
	if dot < 0 {
		return 0
	}

	digits := 0
	for index := dot + 1; index < len(value) && value[index] >= '0' && value[index] <= '9'; index++ {
		digits++
	}

	return digits
}
