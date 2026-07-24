package postgres_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
	"github.com/faustbrian/golib/pkg/temporal/instant"
	temporalpostgres "github.com/faustbrian/golib/pkg/temporal/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

func pgInstant(t *testing.T, start, end int, bounds temporal.Bounds) instant.Period {
	t.Helper()
	base := time.Date(2026, time.January, 1, 0, 0, 0, 123_456_000, time.UTC)
	period, err := instant.New(base.Add(time.Duration(start)*time.Hour), base.Add(time.Duration(end)*time.Hour), bounds)
	if err != nil {
		t.Fatalf("instant.New(): %v", err)
	}
	return period
}

func TestInstantPGXRangeRoundTripsEveryBoundsMode(t *testing.T) {
	t.Parallel()

	for _, bounds := range temporal.AllBounds() {
		period := pgInstant(t, 0, 2, bounds)
		value, err := temporalpostgres.InstantRangeValue(period)
		if err != nil {
			t.Fatalf("InstantRangeValue(%v): %v", bounds, err)
		}
		if !value.Valid || (value.LowerType == pgtype.Inclusive) != bounds.IncludesStart() || (value.UpperType == pgtype.Inclusive) != bounds.IncludesEnd() {
			t.Fatalf("pgx bounds for %v = %v/%v", bounds, value.LowerType, value.UpperType)
		}
		decoded, err := temporalpostgres.InstantPeriod(value)
		if err != nil || !decoded.SetEqual(period) || decoded.Bounds() != bounds {
			t.Fatalf("InstantPeriod() = %+v, %v", decoded, err)
		}
	}
}

func TestInstantPGXRangeRejectsLossyOrUnsupportedStates(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.January, 1, 0, 0, 0, 1, time.UTC)
	period, _ := instant.Range(start, start.Add(time.Hour))
	if _, err := temporalpostgres.InstantRangeValue(period); !errors.Is(err, temporal.ErrPrecision) {
		t.Fatalf("nanosecond range error = %v", err)
	}
	empty, _ := instant.New(start.Truncate(time.Microsecond), start.Truncate(time.Microsecond), temporal.ClosedOpen)
	if _, err := temporalpostgres.InstantRangeValue(empty); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("empty range error = %v", err)
	}

	for name, value := range map[string]pgtype.Range[time.Time]{
		"null":      {},
		"unbounded": {Upper: start, LowerType: pgtype.Unbounded, UpperType: pgtype.Exclusive, Valid: true},
		"empty":     {LowerType: pgtype.Empty, UpperType: pgtype.Empty, Valid: true},
		"unknown":   {Lower: start, Upper: start.Add(time.Hour), LowerType: pgtype.BoundType('x'), UpperType: pgtype.Exclusive, Valid: true},
	} {
		if _, err := temporalpostgres.InstantPeriod(value); !errors.Is(err, temporal.ErrUnsupported) {
			t.Fatalf("InstantPeriod(%s) error = %v", name, err)
		}
	}
	if _, err := temporalpostgres.InstantPeriod(pgtype.Range[time.Time]{
		Lower:     start,
		Upper:     start.Add(time.Hour),
		LowerType: pgtype.Inclusive,
		UpperType: pgtype.Exclusive,
		Valid:     true,
	}); !errors.Is(err, temporal.ErrPrecision) {
		t.Fatalf("lossy InstantPeriod() error = %v", err)
	}
	microsecond := start.Truncate(time.Microsecond)
	for name, value := range map[string]pgtype.Range[time.Time]{
		"reversed":     {Lower: microsecond.Add(time.Hour), Upper: microsecond, LowerType: pgtype.Inclusive, UpperType: pgtype.Exclusive, Valid: true},
		"finite empty": {Lower: microsecond, Upper: microsecond, LowerType: pgtype.Inclusive, UpperType: pgtype.Exclusive, Valid: true},
	} {
		if _, err := temporalpostgres.InstantPeriod(value); !errors.Is(err, temporal.ErrUnsupported) {
			t.Fatalf("InstantPeriod(%s) error = %v", name, err)
		}
	}
}

func TestInstantMultirangeRoundTripAndLimits(t *testing.T) {
	t.Parallel()

	set, err := instant.NewSet(temporal.Limits{},
		pgInstant(t, 0, 1, temporal.ClosedOpen),
		pgInstant(t, 2, 3, temporal.OpenClosed),
	)
	if err != nil {
		t.Fatalf("instant.NewSet(): %v", err)
	}
	value, err := temporalpostgres.InstantMultirangeValue(set)
	if err != nil || len(value) != 2 {
		t.Fatalf("InstantMultirangeValue() = %+v, %v", value, err)
	}
	decoded, err := temporalpostgres.InstantSet(value, temporal.Limits{})
	if err != nil || !decoded.Equal(set) {
		t.Fatalf("InstantSet() = %+v, %v", decoded.Periods(), err)
	}
	if _, err := temporalpostgres.InstantSet(value, temporal.Limits{InputPeriods: 1}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("InstantSet(limit) error = %v", err)
	}
	if _, err := temporalpostgres.InstantSet(value, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("InstantSet(invalid limits) error = %v", err)
	}
	invalid := append(pgtype.Multirange[pgtype.Range[time.Time]]{}, value...)
	invalid[0].Valid = false
	if _, err := temporalpostgres.InstantSet(invalid, temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("InstantSet(invalid item) error = %v", err)
	}

	nanosecondStart := time.Date(2026, time.January, 1, 0, 0, 0, 1, time.UTC)
	nanosecondPeriod, _ := instant.Range(nanosecondStart, nanosecondStart.Add(time.Hour))
	nanosecondSet, _ := instant.NewSet(temporal.Limits{}, nanosecondPeriod)
	if _, err := temporalpostgres.InstantMultirangeValue(nanosecondSet); !errors.Is(err, temporal.ErrPrecision) {
		t.Fatalf("InstantMultirangeValue(lossy) error = %v", err)
	}
}

func TestDatePGXRangeCanonicalizesRepresentedDates(t *testing.T) {
	t.Parallel()

	period, err := dateperiod.New(
		calendar.MustDate(2026, time.January, 1),
		calendar.MustDate(2026, time.January, 3),
		temporal.OpenClosed,
	)
	if err != nil {
		t.Fatalf("dateperiod.New(): %v", err)
	}
	value, err := temporalpostgres.DateRangeValue(period)
	if err != nil {
		t.Fatalf("DateRangeValue(): %v", err)
	}
	if value.Lower.String() != "2026-01-02" || value.Upper.String() != "2026-01-04" || value.LowerType != pgtype.Inclusive || value.UpperType != pgtype.Exclusive {
		t.Fatalf("DateRangeValue() = %+v", value)
	}
	decoded, err := temporalpostgres.DatePeriod(value)
	if err != nil || decoded.Bounds() != temporal.Closed || !decoded.Includes(calendar.MustDate(2026, time.January, 2)) || !decoded.Includes(calendar.MustDate(2026, time.January, 3)) {
		t.Fatalf("DatePeriod() = %+v, %v", decoded, err)
	}
}

func TestDatePGXRangeRejectsEmptyUnboundedAndMaximumCanonicalOverflow(t *testing.T) {
	t.Parallel()

	empty, _ := dateperiod.New(calendar.MustDate(2026, time.January, 1), calendar.MustDate(2026, time.January, 2), temporal.Open)
	if _, err := temporalpostgres.DateRangeValue(empty); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("empty daterange error = %v", err)
	}
	maximum, _ := dateperiod.Day(calendar.MustDate(9999, time.December, 31))
	if _, err := temporalpostgres.DateRangeValue(maximum); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("maximum daterange error = %v", err)
	}
	if _, err := temporalpostgres.DatePeriod(pgtype.Range[calendar.Date]{Valid: false}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("null DatePeriod() error = %v", err)
	}
	if _, err := temporalpostgres.DatePeriod(pgtype.Range[calendar.Date]{LowerType: pgtype.Unbounded, UpperType: pgtype.Unbounded, Valid: true}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("unbounded DatePeriod() error = %v", err)
	}
	for name, value := range map[string]pgtype.Range[calendar.Date]{
		"invalid date": {Lower: calendar.Date{}, Upper: calendar.MustDate(2026, time.January, 2), LowerType: pgtype.Inclusive, UpperType: pgtype.Exclusive, Valid: true},
		"empty":        {Lower: calendar.MustDate(2026, time.January, 1), Upper: calendar.MustDate(2026, time.January, 1), LowerType: pgtype.Inclusive, UpperType: pgtype.Exclusive, Valid: true},
	} {
		if _, err := temporalpostgres.DatePeriod(value); !errors.Is(err, temporal.ErrUnsupported) {
			t.Fatalf("DatePeriod(%s) error = %v", name, err)
		}
	}
}

func TestDateMultirangeRoundTrip(t *testing.T) {
	t.Parallel()

	first, _ := dateperiod.Day(calendar.MustDate(2026, time.January, 1))
	second, _ := dateperiod.Day(calendar.MustDate(2026, time.January, 3))
	set, _ := dateperiod.NewSet(temporal.Limits{}, first, second)
	value, err := temporalpostgres.DateMultirangeValue(set)
	if err != nil || len(value) != 2 {
		t.Fatalf("DateMultirangeValue() = %+v, %v", value, err)
	}
	decoded, err := temporalpostgres.DateSet(value, temporal.Limits{})
	if err != nil || !decoded.Equal(set) {
		t.Fatalf("DateSet() = %+v, %v", decoded.Periods(), err)
	}
	if _, err := temporalpostgres.DateSet(value, temporal.Limits{Precision: 10}); !errors.Is(err, temporal.ErrLimit) {
		t.Fatalf("DateSet(invalid limits) error = %v", err)
	}
	invalid := append(pgtype.Multirange[pgtype.Range[calendar.Date]]{}, value...)
	invalid[0].Valid = false
	if _, err := temporalpostgres.DateSet(invalid, temporal.Limits{}); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("DateSet(invalid item) error = %v", err)
	}

	maximum, _ := dateperiod.Day(calendar.MustDate(9999, time.December, 31))
	maximumSet, _ := dateperiod.NewSet(temporal.Limits{}, maximum)
	if _, err := temporalpostgres.DateMultirangeValue(maximumSet); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("DateMultirangeValue(maximum) error = %v", err)
	}
}
