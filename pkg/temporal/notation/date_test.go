package notation_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/notation"
)

func civilPeriod(t *testing.T, bounds temporal.Bounds) dateperiod.Period {
	t.Helper()
	period, err := dateperiod.New(
		calendar.MustDate(2026, time.January, 2),
		calendar.MustDate(2026, time.March, 4),
		bounds,
	)
	if err != nil {
		t.Fatalf("dateperiod.New(): %v", err)
	}
	return period
}

func TestDateNotationRoundTripsEveryBoundsMode(t *testing.T) {
	t.Parallel()

	for _, format := range []notation.Format{notation.ISO80000, notation.Bourbaki} {
		for _, bounds := range temporal.AllBounds() {
			period := civilPeriod(t, bounds)
			encoded, err := notation.FormatDate(period, format, temporal.Limits{})
			if err != nil {
				t.Fatalf("FormatDate(%v, %v): %v", bounds, format, err)
			}
			decoded, err := notation.ParseDate(encoded, format, temporal.Limits{})
			if err != nil || decoded.Start() != period.Start() || decoded.End() != period.End() || decoded.Bounds() != bounds {
				t.Fatalf("round trip %q = %+v, %v", encoded, decoded, err)
			}
		}
	}
}

func TestDateISO8601UsesCanonicalOperationalBounds(t *testing.T) {
	t.Parallel()

	period := civilPeriod(t, temporal.ClosedOpen)
	encoded, err := notation.FormatDate(period, notation.ISO8601, temporal.Limits{})
	if err != nil || encoded != "2026-01-02/2026-03-04" {
		t.Fatalf("FormatDate() = %q, %v", encoded, err)
	}
	decoded, err := notation.ParseDate(encoded, notation.ISO8601, temporal.Limits{})
	if err != nil || decoded.Bounds() != temporal.ClosedOpen {
		t.Fatalf("ParseDate() = %+v, %v", decoded, err)
	}
	if _, err := notation.FormatDate(civilPeriod(t, temporal.Closed), notation.ISO8601, temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("FormatDate(lossy) error = %v", err)
	}
}

func TestDateNotationRejectsMalformedAndBoundedInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"",
		"[bad,2026-01-02)",
		"[2026-01-02,bad)",
		"[2026-03-04,2026-01-02)",
		"[2026-01-02,2026-03-04) trailing",
		"[2026-01-02,2026-02-03,2026-03-04)",
		string([]byte{0xff}),
		strings.Repeat("x", temporal.DefaultLimits().ParseBytes+1),
	} {
		if _, err := notation.ParseDate(input, notation.ISO80000, temporal.Limits{}); err == nil {
			t.Fatalf("ParseDate(%q) error = nil", input)
		}
	}
	if _, err := notation.ParseDate("[2026-01-02,2026-03-04)", notation.Format(255), temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("ParseDate(format) error = %v", err)
	}
	if _, err := notation.ParseDate("[2026-01-02,2026-03-04)", notation.ISO80000, temporal.Limits{ParseBytes: 4}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseDate(bytes) error = %v", err)
	}
	if _, err := notation.ParseDate("[2026-01-02,2026-03-04)", notation.ISO80000, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("ParseDate(limits) error = %v", err)
	}
	if _, err := notation.FormatDate(civilPeriod(t, temporal.ClosedOpen), notation.Format(255), temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("FormatDate(format) error = %v", err)
	}
	if _, err := notation.FormatDate(civilPeriod(t, temporal.ClosedOpen), notation.ISO80000, temporal.Limits{FormatBytes: 4}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatDate(bytes) error = %v", err)
	}
	if _, err := notation.FormatDate(civilPeriod(t, temporal.ClosedOpen), notation.ISO80000, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("FormatDate(limits) error = %v", err)
	}
}
