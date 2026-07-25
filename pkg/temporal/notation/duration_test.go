package notation_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/notation"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestFixedISODurationParsing(t *testing.T) {
	t.Parallel()

	tests := map[string]time.Duration{
		"PT0S":                         0,
		"PT5M":                         5 * time.Minute,
		"PT1H2M3S":                     time.Hour + 2*time.Minute + 3*time.Second,
		"PT1.123456789S":               time.Second + 123_456_789,
		"P2D":                          48 * time.Hour,
		"P1W2DT3H":                     9*24*time.Hour + 3*time.Hour,
		"-P1WT30M":                     -(7*24*time.Hour + 30*time.Minute),
		"P106751DT23H47M16.854775807S": time.Duration(1<<63 - 1),
	}
	for input, want := range tests {
		got, err := notation.ParseDuration(input, temporal.Limits{})
		if err != nil || got.Value() != want {
			t.Fatalf("ParseDuration(%q) = %v, %v; want %v", input, got.Value(), err, want)
		}
	}
}

func TestFixedISODurationRejectsCalendarAndMalformedComponents(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"",
		"P",
		"PT",
		"P1Y",
		"P1M",
		"PT1H1H",
		"PT1M1H",
		"PT1.S",
		"PT1.1234567890S",
		"PT-1H",
		"P1DT trailing",
		"P999999999999999999999D",
		"P999999999W",
		"P106751DT24H",
		"-P106751DT23H47M16.854775809S",
		string([]byte{0xff}),
	} {
		if _, err := notation.ParseDuration(input, temporal.Limits{}); err == nil {
			t.Fatalf("ParseDuration(%q) error = nil", input)
		}
	}
	if _, err := notation.ParseDuration("PT1.123S", temporal.Limits{Precision: 2}); !errors.Is(err, temporal.ErrPrecision) {
		t.Fatalf("ParseDuration(precision) error = %v", err)
	}
	if _, err := notation.ParseDuration("PT1S", temporal.Limits{ParseBytes: 3}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseDuration(limit) error = %v", err)
	}
	if _, err := notation.ParseDuration("PT1S", temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseDuration(invalid limits) error = %v", err)
	}
}

func TestFixedISODurationFormatRoundTrips(t *testing.T) {
	t.Parallel()

	for _, raw := range []time.Duration{
		0,
		time.Nanosecond,
		time.Hour + 2*time.Minute + 3*time.Second + 400*time.Millisecond,
		-(49*time.Hour + 5*time.Minute),
		time.Duration(1<<63 - 1),
		time.Duration(-1 << 63),
	} {
		value := timeofday.NewDuration(raw)
		encoded, err := notation.FormatDuration(value, temporal.Limits{})
		if err != nil {
			t.Fatalf("FormatDuration(%v): %v", raw, err)
		}
		decoded, err := notation.ParseDuration(encoded, temporal.Limits{})
		if err != nil || decoded != value {
			t.Fatalf("round trip %v via %q = %v, %v", raw, encoded, decoded.Value(), err)
		}
	}
}

func TestFixedISODurationFormatHonorsOutputLimit(t *testing.T) {
	t.Parallel()

	if _, err := notation.FormatDuration(
		timeofday.NewDuration(time.Hour),
		temporal.Limits{FormatBytes: 3},
	); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatDuration(limit) error = %v", err)
	}
	if _, err := notation.FormatDuration(
		timeofday.NewDuration(time.Hour),
		temporal.Limits{Precision: 10},
	); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatDuration(invalid limits) error = %v", err)
	}
}
