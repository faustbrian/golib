package notation_test

import (
	"errors"
	"strings"
	"testing"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/notation"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func dailyTime(t *testing.T, value string) timeofday.Time {
	t.Helper()
	parsed, err := timeofday.Parse(value, temporal.Limits{})
	if err != nil {
		t.Fatalf("Parse(%q): %v", value, err)
	}
	return parsed
}

func dailyInterval(t *testing.T, start, end string, bounds temporal.Bounds) timeofday.Interval {
	t.Helper()
	interval, err := timeofday.Between(dailyTime(t, start), dailyTime(t, end), bounds)
	if err != nil {
		t.Fatalf("Between(%q,%q): %v", start, end, err)
	}
	return interval
}

func TestDailyBoundedNotationRoundTripsEveryKindAndBounds(t *testing.T) {
	t.Parallel()

	for _, format := range []notation.Format{notation.ISO80000, notation.Bourbaki} {
		for _, bounds := range temporal.AllBounds() {
			for _, interval := range []timeofday.Interval{
				dailyInterval(t, "08:00", "17:30", bounds),
				dailyInterval(t, "22:00", "02:30", bounds),
			} {
				encoded, err := notation.FormatDailyInterval(interval, format, temporal.Limits{})
				if err != nil {
					t.Fatalf("FormatDailyInterval(%v, %v): %v", interval.Kind(), bounds, err)
				}
				decoded, err := notation.ParseDailyInterval(encoded, format, temporal.Limits{})
				if err != nil || !decoded.Equal(interval) {
					t.Fatalf("round trip %q = %+v, %v; want %+v", encoded, decoded, err, interval)
				}
			}
		}

		for _, interval := range []timeofday.Interval{
			timeofday.FullDay(),
			timeofday.Collapsed(dailyTime(t, "08:00")),
		} {
			encoded, err := notation.FormatDailyInterval(interval, format, temporal.Limits{})
			if err != nil {
				t.Fatalf("FormatDailyInterval(%v): %v", interval.Kind(), err)
			}
			decoded, err := notation.ParseDailyInterval(encoded, format, temporal.Limits{})
			if err != nil || !decoded.Equal(interval) {
				t.Fatalf("special round trip %q = %+v, %v", encoded, decoded, err)
			}
		}
	}
}

func TestDailyISO8601IsStrictlyLossless(t *testing.T) {
	t.Parallel()

	interval := dailyInterval(t, "22:00", "02:30", temporal.ClosedOpen)
	encoded, err := notation.FormatDailyInterval(interval, notation.ISO8601, temporal.Limits{})
	if err != nil || encoded != "22:00/02:30" {
		t.Fatalf("FormatDailyInterval() = %q, %v", encoded, err)
	}
	decoded, err := notation.ParseDailyInterval(encoded, notation.ISO8601, temporal.Limits{})
	if err != nil || !decoded.Equal(interval) {
		t.Fatalf("ParseDailyInterval() = %+v, %v", decoded, err)
	}
	for _, lossy := range []timeofday.Interval{
		dailyInterval(t, "08:00", "09:00", temporal.Closed),
		timeofday.FullDay(),
		timeofday.Collapsed(dailyTime(t, "08:00")),
	} {
		if _, err := notation.FormatDailyInterval(lossy, notation.ISO8601, temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
			t.Fatalf("FormatDailyInterval(lossy %v) error = %v", lossy.Kind(), err)
		}
	}
}

func TestDailyNotationRejectsMalformedAmbiguousAndHostileInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"",
		"[08:00,08:00]",
		"(08:00,08:00]",
		"[24:00,00:00)",
		"[bad,09:00)",
		"[08:00,bad)",
		"[08:00,09:00) trailing",
		"[08:00,09:00,10:00)",
		string([]byte{0xff}),
		strings.Repeat("x", temporal.DefaultLimits().ParseBytes+1),
	} {
		if _, err := notation.ParseDailyInterval(input, notation.ISO80000, temporal.Limits{}); err == nil {
			t.Fatalf("ParseDailyInterval(%q) error = nil", input)
		}
	}
	if _, err := notation.ParseDailyInterval("[08:00,09:00)", notation.Format(255), temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("ParseDailyInterval(format) error = %v", err)
	}
	if _, err := notation.ParseDailyInterval("[08:00,09:00)", notation.ISO80000, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseDailyInterval(limits) error = %v", err)
	}
	if _, err := notation.ParseDailyInterval("[08:00,09:00)", notation.ISO80000, temporal.Limits{ParseBytes: 4}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseDailyInterval(bytes) error = %v", err)
	}
	if _, err := notation.FormatDailyInterval(dailyInterval(t, "08:00", "09:00", temporal.ClosedOpen), notation.Format(255), temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("FormatDailyInterval(format) error = %v", err)
	}
	if _, err := notation.FormatDailyInterval(dailyInterval(t, "08:00", "09:00", temporal.ClosedOpen), notation.ISO80000, temporal.Limits{FormatBytes: 4}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatDailyInterval(bytes) error = %v", err)
	}
	if _, err := notation.FormatDailyInterval(dailyInterval(t, "08:00", "09:00", temporal.ClosedOpen), notation.ISO80000, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatDailyInterval(limits) error = %v", err)
	}
}
