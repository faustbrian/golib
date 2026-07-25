package instant

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestNextCivilBoundaryCoversAllUnits(t *testing.T) {
	value := time.Date(2025, time.December, 29, 0, 0, 0, 0, time.UTC)
	units := []CivilUnit{Second, Minute, Hour, Day, ISOWeek, Month, Quarter, Semester, Year, ISOYear}
	for _, unit := range units {
		if _, err := nextCivilBoundary(value, unit, time.UTC, calendartz.Reject); err != nil {
			t.Fatalf("nextCivilBoundary(%d) error = %v", unit, err)
		}
	}
	if _, err := nextCivilBoundary(value, 0, time.UTC, calendartz.Reject); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("nextCivilBoundary(unknown) error = %v", err)
	}
	if _, err := nextCivilBoundary(value, Day, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("nextCivilBoundary(nil location) error = %v", err)
	}
}

func TestNextCivilBoundaryReportsCalendarOverflow(t *testing.T) {
	lastSecond := time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)
	for _, unit := range []CivilUnit{Second, Minute, Hour, Day, ISOWeek, Month, Quarter, Semester, Year, ISOYear} {
		if _, err := nextCivilBoundary(lastSecond, unit, time.UTC, calendartz.Reject); err == nil {
			t.Fatalf("nextCivilBoundary(%d) accepted calendar overflow", unit)
		}
	}
}

func TestResolveCivilRejectsInvalidValuesAndContext(t *testing.T) {
	if _, err := resolveCivil(calendar.Date{}, 0, 0, 0, 0, time.UTC, calendartz.Reject); err == nil {
		t.Fatal("resolveCivil accepted invalid date")
	}
	date := calendar.MustDate(2026, time.January, 1)
	if _, err := resolveCivil(date, 0, 0, 0, 0, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("resolveCivil(nil location) error = %v", err)
	}
}

func TestSnapOutwardPropagatesBoundaryResolutionFailures(t *testing.T) {
	period, _ := New(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC), time.Date(2026, time.January, 1, 2, 0, 0, 0, time.UTC), temporal.ClosedOpen)
	if _, err := period.SnapOutward(Hour, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("SnapOutward(start error) = %v", err)
	}

	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	period, _ = New(
		time.Date(2026, time.March, 29, 1, 30, 0, 0, location),
		time.Date(2026, time.March, 29, 2, 30, 0, 0, location),
		temporal.ClosedOpen,
	)
	if _, err := period.SnapOutward(Hour, location, calendartz.Reject); !errors.Is(err, calendartz.ErrNonexistent) {
		t.Fatalf("SnapOutward(end gap) = %v", err)
	}

	period, _ = New(
		time.Date(9999, time.December, 30, 0, 0, 0, 0, time.UTC),
		time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC),
		temporal.Closed,
	)
	if _, err := period.SnapOutward(Day, time.UTC, calendartz.Reject); err == nil {
		t.Fatal("SnapOutward accepted inclusive maximum boundary")
	}
}

func TestMergeHullEndpointChoices(t *testing.T) {
	t0 := time.Unix(0, 0).UTC()
	t1 := t0.Add(time.Hour)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	tests := []struct {
		left  Period
		right Period
		want  temporal.Bounds
	}{
		{Period{start: t0, end: t2, bounds: temporal.Open}, Period{start: t1, end: t3, bounds: temporal.Closed}, temporal.OpenClosed},
		{Period{start: t1, end: t3, bounds: temporal.Closed}, Period{start: t0, end: t2, bounds: temporal.Open}, temporal.OpenClosed},
		{Period{start: t0, end: t3, bounds: temporal.Open}, Period{start: t0, end: t2, bounds: temporal.ClosedOpen}, temporal.ClosedOpen},
		{Period{start: t0, end: t3, bounds: temporal.Open}, Period{start: t1, end: t3, bounds: temporal.OpenClosed}, temporal.OpenClosed},
	}
	for _, test := range tests {
		if got := test.left.Merge(test.right).Bounds(); got != test.want {
			t.Fatalf("Merge bounds = %v; want %v", got, test.want)
		}
	}
}
