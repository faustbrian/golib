package postgres_test

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	temporalpostgres "github.com/faustbrian/golib/pkg/temporal/postgres"
)

func TestInstantSQLRangeRoundTripAndNull(t *testing.T) {
	t.Parallel()

	period := pgInstant(t, 0, 2, temporal.OpenClosed)
	value, err := temporalpostgres.NewInstantRange(period)
	if err != nil {
		t.Fatalf("NewInstantRange(): %v", err)
	}
	driverValue, err := value.Value()
	if err != nil {
		t.Fatalf("Value(): %v", err)
	}
	text, ok := driverValue.(string)
	if !ok || text[0] != '(' || text[len(text)-1] != ']' {
		t.Fatalf("Value() = %#v", driverValue)
	}

	var decoded temporalpostgres.InstantRange
	if err := decoded.Scan([]byte(text)); err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	got, valid := decoded.Period()
	if !valid || !got.SetEqual(period) || got.Bounds() != period.Bounds() {
		t.Fatalf("Period() = %+v, %v", got, valid)
	}
	if err := decoded.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if _, valid := decoded.Period(); valid {
		t.Fatal("Scan(nil) retained a valid period")
	}
	if got, err := decoded.Value(); err != nil || got != nil {
		t.Fatalf("null Value() = %#v, %v", got, err)
	}
}

func TestInstantSQLRangeRejectsInvalidSourcesAndLoss(t *testing.T) {
	t.Parallel()

	for _, source := range []any{
		42,
		"empty",
		"[,)",
		`x"2026-01-01T00:00:00Z","2026-01-01T01:00:00Z")`,
		`["2026-01-01T00:00:00Z","2026-01-01T01:00:00Z"x`,
		`["bad","2026-01-01T01:00:00Z")`,
		`["2026-01-01T00:00:00Z","bad")`,
		`["2026-01-01T00:00:00Z","2026-01-01T01:00:00Z","2026-01-01T02:00:00Z")`,
		`["2026-01-01T00:00:00.000000001Z","2026-01-01T01:00:00Z")`,
		`["2026-01-02T00:00:00Z","2026-01-01T01:00:00Z")`,
	} {
		var value temporalpostgres.InstantRange
		if err := value.Scan(source); err == nil {
			t.Fatalf("Scan(%#v) error = nil", source)
		}
	}
	var oversized temporalpostgres.InstantRange
	if err := oversized.Scan(string(make([]byte, temporal.DefaultLimits().ParseBytes+1))); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("Scan(oversized) error = %v", err)
	}

	nanosecond := time.Date(2026, time.January, 1, 0, 0, 0, 1, time.UTC)
	period, _ := instant.Range(nanosecond, nanosecond.Add(time.Hour))
	if _, err := temporalpostgres.NewInstantRange(period); !errors.Is(err, temporal.ErrPrecision) {
		t.Fatalf("NewInstantRange(lossy) error = %v", err)
	}
	var nilTarget *temporalpostgres.InstantRange
	if err := nilTarget.Scan(nil); err == nil {
		t.Fatal("nil InstantRange.Scan() error = nil")
	}
}

func TestDateSQLRangeRoundTripCanonicalizesBounds(t *testing.T) {
	t.Parallel()

	period, _ := dateperiod.New(
		calendar.MustDate(2026, time.January, 1),
		calendar.MustDate(2026, time.January, 3),
		temporal.OpenClosed,
	)
	value, err := temporalpostgres.NewDateRange(period)
	if err != nil {
		t.Fatalf("NewDateRange(): %v", err)
	}
	driverValue, err := value.Value()
	if err != nil || driverValue != "[2026-01-02,2026-01-04)" {
		t.Fatalf("Value() = %#v, %v", driverValue, err)
	}

	var decoded temporalpostgres.DateRange
	if err := decoded.Scan(driverValue); err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	got, valid := decoded.Period()
	if !valid || got.Bounds() != temporal.Closed || got.Days() != 2 {
		t.Fatalf("Period() = %+v, %v", got, valid)
	}
	if err := decoded.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if got, err := decoded.Value(); err != nil || got != nil {
		t.Fatalf("null Value() = %#v, %v", got, err)
	}
}

func TestDateSQLRangeRejectsInvalidSources(t *testing.T) {
	t.Parallel()

	for _, source := range []driver.Value{
		int64(42),
		"empty",
		"[2026-01-01,)",
		"[bad,2026-01-02)",
		"[2026-01-01,bad)",
		"[2026-01-01,2026-01-02,2026-01-03)",
		"[2026-01-03,2026-01-01)",
	} {
		var value temporalpostgres.DateRange
		if err := value.Scan(source); err == nil {
			t.Fatalf("Scan(%#v) error = nil", source)
		}
	}
	maximum, _ := dateperiod.Day(calendar.MustDate(9999, time.December, 31))
	if _, err := temporalpostgres.NewDateRange(maximum); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("NewDateRange(maximum) error = %v", err)
	}
	var nilTarget *temporalpostgres.DateRange
	if err := nilTarget.Scan(nil); err == nil {
		t.Fatal("nil DateRange.Scan() error = nil")
	}
}
