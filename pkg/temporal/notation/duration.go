package notation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

var fixedDurationPattern = regexp.MustCompile(
	`^(-)?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)(?:\.(\d+))?S)?)?$`,
)

// ParseDuration strictly decodes a fixed elapsed ISO 8601 duration. Years and
// months are not part of this grammar because they require a civil reference.
func ParseDuration(value string, limits temporal.Limits) (timeofday.Duration, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return timeofday.Duration{}, err
	}
	if len(value) > limits.ParseBytes {
		return timeofday.Duration{}, &temporal.LimitError{
			Field: "parse_bytes", Value: len(value), Max: limits.ParseBytes,
		}
	}
	if !utf8.ValidString(value) {
		return timeofday.Duration{}, temporal.ErrParse
	}
	matches := fixedDurationPattern.FindStringSubmatch(value)
	if matches == nil || noDurationComponents(matches) ||
		(strings.Contains(value, "T") && matches[4] == "" && matches[5] == "" && matches[6] == "") {
		return timeofday.Duration{}, temporal.ErrParse
	}
	if len(matches[7]) > limits.Precision {
		return timeofday.Duration{}, temporal.ErrPrecision
	}

	negative := matches[1] == "-"
	result := timeofday.ZeroDuration()
	components := [...]struct {
		text string
		unit time.Duration
	}{
		{matches[2], 7 * 24 * time.Hour},
		{matches[3], 24 * time.Hour},
		{matches[4], time.Hour},
		{matches[5], time.Minute},
		{matches[6], time.Second},
	}
	for _, component := range components {
		if component.text == "" {
			continue
		}
		count, err := strconv.ParseInt(component.text, 10, 64)
		if err != nil || int64(int(count)) != count {
			return timeofday.Duration{}, temporal.ErrOverflow
		}
		part, err := timeofday.NewDuration(component.unit).Multiply(int(count))
		if err != nil {
			return timeofday.Duration{}, err
		}
		if negative {
			part, _ = part.Negate()
		}
		result, err = result.Add(part)
		if err != nil {
			return timeofday.Duration{}, err
		}
	}

	if matches[7] != "" {
		fraction, _ := strconv.ParseInt(matches[7], 10, 64)
		for index := len(matches[7]); index < 9; index++ {
			fraction *= 10
		}
		part := timeofday.NewDuration(time.Duration(fraction))
		var err error
		if negative {
			part, _ = part.Negate()
		}
		result, err = result.Add(part)
		if err != nil {
			return timeofday.Duration{}, err
		}
	}

	return result, nil
}

// FormatDuration returns canonical fixed elapsed ISO 8601 notation.
func FormatDuration(value timeofday.Duration, limits temporal.Limits) (string, error) {
	limits = limits.Resolve()
	if err := limits.Validate(); err != nil {
		return "", err
	}

	raw := value.Value()
	negative := raw < 0
	magnitude := uint64(raw) // #nosec G115 -- normalized as an unsigned magnitude below.
	if negative {
		magnitude = uint64(-(raw + 1)) + 1 // #nosec G115 -- negated raw+1 is non-negative.
	}

	dayNanos := uint64(24 * time.Hour)
	hourNanos := uint64(time.Hour)
	minuteNanos := uint64(time.Minute)
	secondNanos := uint64(time.Second)
	days := magnitude / dayNanos
	magnitude %= dayNanos
	hours := magnitude / hourNanos
	magnitude %= hourNanos
	minutes := magnitude / minuteNanos
	magnitude %= minuteNanos
	seconds := magnitude / secondNanos
	fraction := magnitude % secondNanos

	var builder strings.Builder
	if negative {
		builder.WriteByte('-')
	}
	builder.WriteByte('P')
	if days > 0 {
		fmt.Fprintf(&builder, "%dD", days)
	}
	if hours > 0 || minutes > 0 || seconds > 0 || fraction > 0 || days == 0 {
		builder.WriteByte('T')
		if hours > 0 {
			fmt.Fprintf(&builder, "%dH", hours)
		}
		if minutes > 0 {
			fmt.Fprintf(&builder, "%dM", minutes)
		}
		if seconds > 0 || fraction > 0 || hours == 0 && minutes == 0 {
			fmt.Fprintf(&builder, "%d", seconds)
			if fraction > 0 {
				fractionText := strings.TrimRight(fmt.Sprintf("%09d", fraction), "0")
				builder.WriteByte('.')
				builder.WriteString(fractionText)
			}
			builder.WriteByte('S')
		}
	}

	encoded := builder.String()
	if len(encoded) > limits.FormatBytes {
		return "", &temporal.LimitError{
			Field: "format_bytes", Value: len(encoded), Max: limits.FormatBytes,
		}
	}
	return encoded, nil
}

func noDurationComponents(matches []string) bool {
	for index := 2; index <= 6; index++ {
		if matches[index] != "" {
			return false
		}
	}
	return true
}
