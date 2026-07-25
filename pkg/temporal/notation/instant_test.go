package notation_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	"github.com/faustbrian/golib/pkg/temporal/notation"
)

func notationPeriod(t *testing.T, bounds temporal.Bounds) instant.Period {
	t.Helper()

	period, err := instant.New(
		time.Date(2026, time.January, 2, 3, 4, 5, 123_456_789, time.FixedZone("EET", 2*60*60)),
		time.Date(2026, time.January, 3, 4, 5, 6, 987_654_321, time.FixedZone("EET", 2*60*60)),
		bounds,
	)
	if err != nil {
		t.Fatalf("instant.New(): %v", err)
	}

	return period
}

func TestBoundedNotationRoundTripsEveryBoundsMode(t *testing.T) {
	t.Parallel()

	for _, format := range []notation.Format{notation.ISO80000, notation.Bourbaki} {
		for _, bounds := range temporal.AllBounds() {
			period := notationPeriod(t, bounds)
			encoded, err := notation.FormatInstant(period, format, temporal.Limits{})
			if err != nil {
				t.Fatalf("FormatInstant(%v, %v): %v", bounds, format, err)
			}
			decoded, err := notation.ParseInstant(encoded, format, temporal.Limits{})
			if err != nil {
				t.Fatalf("ParseInstant(%q, %v): %v", encoded, format, err)
			}
			if !decoded.SetEqual(period) || decoded.Bounds() != bounds {
				t.Fatalf("round trip = %+v, want %+v", decoded, period)
			}
		}
	}
}

func TestBracketSemanticsAreExact(t *testing.T) {
	t.Parallel()

	start := "2026-01-02T03:04:05Z"
	end := "2026-01-03T03:04:05Z"
	tests := map[string]struct {
		format notation.Format
		input  string
		bounds temporal.Bounds
	}{
		"iso closed open":      {notation.ISO80000, "[" + start + "," + end + ")", temporal.ClosedOpen},
		"iso open closed":      {notation.ISO80000, "(" + start + "," + end + "]", temporal.OpenClosed},
		"bourbaki closed open": {notation.Bourbaki, "[" + start + "," + end + "[", temporal.ClosedOpen},
		"bourbaki open closed": {notation.Bourbaki, "]" + start + "," + end + "]", temporal.OpenClosed},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			period, err := notation.ParseInstant(test.input, test.format, temporal.Limits{})
			if err != nil || period.Bounds() != test.bounds {
				t.Fatalf("ParseInstant() = %+v, %v", period, err)
			}
		})
	}
}

func TestISO8601RoundTripAndLosslessBoundPolicy(t *testing.T) {
	t.Parallel()

	period := notationPeriod(t, temporal.ClosedOpen)
	encoded, err := notation.FormatInstant(period, notation.ISO8601, temporal.Limits{})
	if err != nil {
		t.Fatalf("FormatInstant(): %v", err)
	}
	decoded, err := notation.ParseInstant(encoded, notation.ISO8601, temporal.Limits{})
	if err != nil || !decoded.SetEqual(period) {
		t.Fatalf("ParseInstant(%q) = %+v, %v", encoded, decoded, err)
	}

	if _, err := notation.FormatInstant(
		notationPeriod(t, temporal.Closed),
		notation.ISO8601,
		temporal.Limits{},
	); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("lossy FormatInstant() error = %v", err)
	}
}

func TestInstantNotationRejectsHostileAndAmbiguousInput(t *testing.T) {
	t.Parallel()

	valid := "[2026-01-02T03:04:05Z,2026-01-03T03:04:05Z)"
	tests := []string{
		"",
		"[bad,2026-01-03T03:04:05Z)",
		"[2026-01-02T03:04:05Z,bad)",
		"[2026-01-03T03:04:05Z,2026-01-02T03:04:05Z)",
		valid + " trailing",
		"[2026-01-02T03:04:05Z,2026-01-03T03:04:05Z,2026-01-04T03:04:05Z)",
		"[2026-01-02T03:04:05.1234567890Z,2026-01-03T03:04:05Z)",
		string([]byte{0xff, 0xfe}),
		strings.Repeat("x", temporal.DefaultLimits().ParseBytes+1),
	}
	for _, input := range tests {
		if _, err := notation.ParseInstant(input, notation.ISO80000, temporal.Limits{}); err == nil {
			t.Fatalf("ParseInstant(%q) error = nil", input)
		}
	}
}

func TestBourbakiRejectsForeignBrackets(t *testing.T) {
	t.Parallel()

	if _, err := notation.ParseInstant(
		"(2026-01-02T03:04:05Z,2026-01-03T03:04:05Z)",
		notation.Bourbaki,
		temporal.Limits{},
	); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("ParseInstant(brackets) error = %v", err)
	}
}

func TestInstantNotationHonorsCustomLimitsAndUnknownFormats(t *testing.T) {
	t.Parallel()

	input := "[2026-01-02T03:04:05Z,2026-01-03T03:04:05Z)"
	if _, err := notation.ParseInstant(input, notation.ISO80000, temporal.Limits{ParseBytes: 8}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseInstant(limit) error = %v", err)
	}
	if _, err := notation.ParseInstant(input, notation.Format(255), temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("ParseInstant(format) error = %v", err)
	}
	if _, err := notation.FormatInstant(notationPeriod(t, temporal.ClosedOpen), notation.Format(255), temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("FormatInstant(format) error = %v", err)
	}
	if _, err := notation.FormatInstant(notationPeriod(t, temporal.ClosedOpen), notation.ISO80000, temporal.Limits{FormatBytes: 8}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatInstant(limit) error = %v", err)
	}
	if _, err := notation.ParseInstant(input, notation.ISO80000, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseInstant(invalid limits) error = %v", err)
	}
	if _, err := notation.FormatInstant(
		notationPeriod(t, temporal.ClosedOpen),
		notation.ISO80000,
		temporal.Limits{Precision: 10},
	); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatInstant(invalid limits) error = %v", err)
	}
}
