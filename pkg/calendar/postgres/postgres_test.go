package postgres_test

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendarpg "github.com/faustbrian/golib/pkg/calendar/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestDateSQLAndPGXRoundTrip(t *testing.T) {
	t.Parallel()

	want := calendar.MustDate(2024, time.February, 29)
	value := calendarpg.NewDate(want)
	sqlValue, err := value.Value()
	if err != nil || sqlValue != driver.Value("2024-02-29") {
		t.Fatalf("Value() = %#v, %v", sqlValue, err)
	}
	var fromSQL calendarpg.Date
	if err := fromSQL.Scan("2024-02-29"); err != nil || fromSQL.CalendarDate() != want {
		t.Fatalf("Scan() = %v, %v", fromSQL.CalendarDate(), err)
	}
	pgValue, err := value.DateValue()
	if err != nil || !pgValue.Valid || pgValue.InfinityModifier != pgtype.Finite {
		t.Fatalf("DateValue() = %#v, %v", pgValue, err)
	}
	var fromPG calendarpg.Date
	if err := fromPG.ScanDate(pgValue); err != nil || fromPG.CalendarDate() != want {
		t.Fatalf("ScanDate() = %v, %v", fromPG.CalendarDate(), err)
	}
}

func TestOrdinaryDateRejectsNullInfinityAndInvalidValues(t *testing.T) {
	t.Parallel()

	for _, input := range []any{nil, "infinity", "-infinity", "2024-02-30", 42} {
		var value calendarpg.Date
		if err := value.Scan(input); err == nil {
			t.Fatalf("Scan(%#v) unexpectedly succeeded", input)
		}
	}
	var zero calendarpg.Date
	if _, err := zero.Value(); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("zero Value error = %v", err)
	}
	if err := zero.ScanDate(pgtype.Date{Valid: true, InfinityModifier: pgtype.Infinity}); !errors.Is(err, calendarpg.ErrInfinity) {
		t.Fatalf("infinity error = %v", err)
	}
}

func TestInfinityDateIsDistinctAndRoundTrips(t *testing.T) {
	t.Parallel()

	for _, kind := range []calendarpg.InfinityKind{calendarpg.NegativeInfinity, calendarpg.PositiveInfinity} {
		value := calendarpg.NewInfinityDate(kind)
		sqlValue, err := value.Value()
		if err != nil {
			t.Fatal(err)
		}
		var decoded calendarpg.InfinityDate
		if err := decoded.Scan(sqlValue); err != nil || decoded.Kind() != kind {
			t.Fatalf("infinity round trip = %v, %v", decoded.Kind(), err)
		}
		pgValue, err := value.DateValue()
		if err != nil || !pgValue.Valid || pgValue.InfinityModifier == pgtype.Finite {
			t.Fatalf("infinity DateValue() = %#v, %v", pgValue, err)
		}
	}
	finite := calendarpg.NewFiniteDate(calendar.MustDate(2025, time.January, 2))
	if finite.Kind() != calendarpg.Finite || finite.Date().String() != "2025-01-02" {
		t.Fatalf("finite infinity-aware value = %#v", finite)
	}
}
