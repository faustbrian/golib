package dateperiod_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func date(year int, month time.Month, day int) calendar.Date {
	return calendar.MustDate(year, month, day)
}

func mustDatePeriod(t *testing.T, start, end calendar.Date, bounds temporal.Bounds) dateperiod.Period {
	t.Helper()

	period, err := dateperiod.New(start, end, bounds)
	if err != nil {
		t.Fatalf("New(%s,%s,%v): %v", start, end, bounds, err)
	}
	return period
}

func TestDatePeriodValidatesDatesOrderAndBounds(t *testing.T) {
	t.Parallel()

	if _, err := dateperiod.New(calendar.Date{}, date(2026, time.January, 1), temporal.Closed); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("New(invalid date) error = %v", err)
	}
	if _, err := dateperiod.New(date(2026, time.January, 2), date(2026, time.January, 1), temporal.Closed); !errors.Is(err, temporal.ErrReversed) {
		t.Fatalf("New(reversed) error = %v", err)
	}
	if _, err := dateperiod.New(date(2026, time.January, 1), date(2026, time.January, 2), temporal.Bounds(255)); !errors.Is(err, temporal.ErrBounds) {
		t.Fatalf("New(bounds) error = %v", err)
	}
}

func TestDiscreteBoundsDefineEmptyAndSingletonPeriods(t *testing.T) {
	t.Parallel()

	start := date(2026, time.January, 1)
	end := date(2026, time.January, 2)
	tests := []struct {
		bounds    temporal.Bounds
		empty     bool
		singleton bool
		included  calendar.Date
	}{
		{temporal.Closed, false, false, start},
		{temporal.ClosedOpen, false, true, start},
		{temporal.OpenClosed, false, true, end},
		{temporal.Open, true, false, calendar.Date{}},
	}
	for _, test := range tests {
		period := mustDatePeriod(t, start, end, test.bounds)
		if period.IsEmpty() != test.empty || period.IsSingleton() != test.singleton {
			t.Fatalf("%v empty/singleton = %v/%v", test.bounds, period.IsEmpty(), period.IsSingleton())
		}
		if test.included.IsValid() && !period.Includes(test.included) {
			t.Fatalf("%v excluded %s", test.bounds, test.included)
		}
	}

	for _, bounds := range temporal.AllBounds() {
		period := mustDatePeriod(t, start, start, bounds)
		wantSingleton := bounds == temporal.Closed
		if period.IsSingleton() != wantSingleton || period.IsEmpty() == wantSingleton {
			t.Fatalf("equal %v empty/singleton mismatch", bounds)
		}
	}
}

func TestDateFactoriesUseCalendarUnits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func() (dateperiod.Period, error)
		start calendar.Date
		end   calendar.Date
	}{
		{"day", func() (dateperiod.Period, error) { return dateperiod.Day(date(2024, time.February, 29)) }, date(2024, time.February, 29), date(2024, time.February, 29)},
		{"month", func() (dateperiod.Period, error) { return dateperiod.Month(2024, time.February) }, date(2024, time.February, 1), date(2024, time.February, 29)},
		{"quarter", func() (dateperiod.Period, error) { return dateperiod.Quarter(2024, 2) }, date(2024, time.April, 1), date(2024, time.June, 30)},
		{"semester", func() (dateperiod.Period, error) { return dateperiod.Semester(2024, 2) }, date(2024, time.July, 1), date(2024, time.December, 31)},
		{"year", func() (dateperiod.Period, error) { return dateperiod.Year(2024) }, date(2024, time.January, 1), date(2024, time.December, 31)},
		{"iso week", func() (dateperiod.Period, error) { return dateperiod.ISOWeek(2020, 53) }, date(2020, time.December, 28), date(2021, time.January, 3)},
		{"iso year", func() (dateperiod.Period, error) { return dateperiod.ISOYear(2020) }, date(2019, time.December, 30), date(2021, time.January, 3)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			period, err := test.build()
			if err != nil {
				t.Fatalf("factory: %v", err)
			}
			if !period.Start().Equal(test.start) || !period.End().Equal(test.end) || period.Bounds() != temporal.Closed {
				t.Fatalf("factory = %s..%s %v", period.Start(), period.End(), period.Bounds())
			}
		})
	}
}

func TestDateFactoriesPropagateInvalidUnitErrors(t *testing.T) {
	t.Parallel()

	for name, build := range map[string]func() error{
		"month":    func() error { _, err := dateperiod.Month(0, time.January); return err },
		"quarter":  func() error { _, err := dateperiod.Quarter(2026, 5); return err },
		"semester": func() error { _, err := dateperiod.Semester(2026, 3); return err },
		"year":     func() error { _, err := dateperiod.Year(10_000); return err },
		"iso week": func() error { _, err := dateperiod.ISOWeek(2026, 54); return err },
		"iso year": func() error { _, err := dateperiod.ISOYear(0); return err },
	} {
		if err := build(); err == nil {
			t.Fatalf("%s invalid factory error = nil", name)
		}
	}
}

func TestDateMembershipRejectsInvalidOutsideAndExcludedBoundaries(t *testing.T) {
	t.Parallel()

	period := mustDatePeriod(t, date(2026, time.January, 2), date(2026, time.January, 4), temporal.Open)
	for _, excluded := range []calendar.Date{
		{},
		date(2026, time.January, 1),
		date(2026, time.January, 2),
		date(2026, time.January, 4),
		date(2026, time.January, 5),
	} {
		if period.Includes(excluded) {
			t.Fatalf("Includes(%s) = true", excluded)
		}
	}
	if !period.Includes(date(2026, time.January, 3)) {
		t.Fatal("period excluded its interior date")
	}
	if got := (dateperiod.Period{}).Days(); got != 0 {
		t.Fatalf("zero Period.Days() = %d", got)
	}
}

func TestDatePeriodCoveredDaysAndCalendarMovement(t *testing.T) {
	t.Parallel()

	period := mustDatePeriod(t, date(2026, time.January, 1), date(2026, time.January, 10), temporal.OpenClosed)
	if got := period.Days(); got != 9 {
		t.Fatalf("Days() = %d, want 9", got)
	}
	moved, err := period.MoveDays(5)
	if err != nil {
		t.Fatalf("MoveDays(): %v", err)
	}
	if !moved.Start().Equal(date(2026, time.January, 6)) || !moved.End().Equal(date(2026, time.January, 15)) {
		t.Fatalf("MoveDays() = %s..%s", moved.Start(), moved.End())
	}
	movedMonth, err := period.MoveMonths(1, calendar.Clamp)
	if err != nil {
		t.Fatalf("MoveMonths(): %v", err)
	}
	if movedMonth.Start().Month() != time.February || movedMonth.End().Month() != time.February {
		t.Fatalf("MoveMonths() = %s..%s", movedMonth.Start(), movedMonth.End())
	}
}

func TestCalendarMovementPropagatesEitherEndpointFailure(t *testing.T) {
	t.Parallel()

	minimum := mustDatePeriod(t, date(1, time.January, 1), date(1, time.January, 2), temporal.Closed)
	if _, err := minimum.MoveDays(-1); err == nil {
		t.Fatal("MoveDays(first endpoint) error = nil")
	}
	maximum := mustDatePeriod(t, date(9999, time.December, 30), date(9999, time.December, 31), temporal.Closed)
	if _, err := maximum.MoveDays(1); err == nil {
		t.Fatal("MoveDays(second endpoint) error = nil")
	}
	minimumMonth := mustDatePeriod(t, date(1, time.January, 1), date(1, time.January, 2), temporal.Closed)
	if _, err := minimumMonth.MoveMonths(-1, calendar.Clamp); err == nil {
		t.Fatal("MoveMonths(first endpoint) error = nil")
	}
	maximumMonth := mustDatePeriod(t, date(9999, time.November, 30), date(9999, time.December, 1), temporal.Closed)
	if _, err := maximumMonth.MoveMonths(1, calendar.Clamp); err == nil {
		t.Fatal("MoveMonths(second endpoint) error = nil")
	}
}

func TestDatePeriodConvertsIncludedDatesToNextBoundaryExclusiveInstants(t *testing.T) {
	t.Parallel()

	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatalf("LoadLocation(): %v", err)
	}
	period, err := dateperiod.Day(date(2026, time.March, 29))
	if err != nil {
		t.Fatalf("Day(): %v", err)
	}
	instantPeriod, err := period.ToInstant(location, calendartz.Reject)
	if err != nil {
		t.Fatalf("ToInstant(): %v", err)
	}
	if instantPeriod.Bounds() != temporal.ClosedOpen {
		t.Fatalf("instant bounds = %v", instantPeriod.Bounds())
	}
	duration, err := instantPeriod.Duration()
	if err != nil || duration != 23*time.Hour {
		t.Fatalf("DST day duration = %v, %v", duration, err)
	}
	if !instantPeriod.Start().Equal(time.Date(2026, time.March, 29, 0, 0, 0, 0, location)) ||
		!instantPeriod.End().Equal(time.Date(2026, time.March, 30, 0, 0, 0, 0, location)) {
		t.Fatalf("instant boundaries = %v..%v", instantPeriod.Start(), instantPeriod.End())
	}

	empty := mustDatePeriod(t, date(2026, time.January, 1), date(2026, time.January, 2), temporal.Open)
	if _, err := empty.ToInstant(location, calendartz.Reject); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("empty ToInstant() error = %v", err)
	}
	if _, err := period.ToInstant(nil, calendartz.Reject); err == nil {
		t.Fatal("ToInstant(nil location) error = nil")
	}

	for _, bounds := range []temporal.Bounds{temporal.ClosedOpen, temporal.OpenClosed} {
		bounded := mustDatePeriod(t, date(2026, time.January, 1), date(2026, time.January, 3), bounds)
		converted, err := bounded.ToInstant(time.UTC, calendartz.Reject)
		if err != nil {
			t.Fatalf("ToInstant(%v): %v", bounds, err)
		}
		if duration, _ := converted.Duration(); duration != 48*time.Hour {
			t.Fatalf("ToInstant(%v) duration = %v", bounds, duration)
		}
	}
}
