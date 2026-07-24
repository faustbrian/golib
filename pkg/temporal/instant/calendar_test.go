package instant_test

import (
	"errors"
	"testing"
	"time"

	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestSnapUsesCivilBoundariesAndExplicitDSTPolicy(t *testing.T) {
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	value := time.Date(2026, time.March, 29, 12, 15, 0, 0, location)
	floor, err := instant.Snap(value, instant.Day, instant.Floor, location, calendartz.Reject)
	if err != nil {
		t.Fatal(err)
	}
	ceil, err := instant.Snap(value, instant.Day, instant.Ceil, location, calendartz.Reject)
	if err != nil {
		t.Fatal(err)
	}
	if floor.Hour() != 0 || ceil.Day() != 30 || ceil.Hour() != 0 {
		t.Fatalf("day bounds = %v .. %v", floor, ceil)
	}
	if got := ceil.Sub(floor); got != 23*time.Hour {
		t.Fatalf("DST civil day duration = %v", got)
	}
	if _, err := instant.Snap(value, instant.Day, instant.Floor, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("Snap(nil location) error = %v", err)
	}
	if _, err := instant.Snap(value, instant.Day, instant.Floor, location, nil); err == nil {
		t.Fatal("Snap accepted nil policy")
	}
}

func TestSnapSupportsEveryCivilUnit(t *testing.T) {
	location := time.UTC
	value := time.Date(2026, time.July, 16, 12, 34, 56, 789, location)
	tests := []struct {
		unit instant.CivilUnit
		want string
	}{
		{instant.Second, "2026-07-16T12:34:56Z"},
		{instant.Minute, "2026-07-16T12:34:00Z"},
		{instant.Hour, "2026-07-16T12:00:00Z"},
		{instant.Day, "2026-07-16T00:00:00Z"},
		{instant.ISOWeek, "2026-07-13T00:00:00Z"},
		{instant.Month, "2026-07-01T00:00:00Z"},
		{instant.Quarter, "2026-07-01T00:00:00Z"},
		{instant.Semester, "2026-07-01T00:00:00Z"},
		{instant.Year, "2026-01-01T00:00:00Z"},
		{instant.ISOYear, "2025-12-29T00:00:00Z"},
	}
	for _, test := range tests {
		got, err := instant.Snap(value, test.unit, instant.Floor, location, calendartz.Reject)
		if err != nil || got.Format(time.RFC3339Nano) != test.want {
			t.Fatalf("Snap(%d) = %v, %v; want %s", test.unit, got, err, test.want)
		}
	}
	if _, err := instant.Snap(value, 0, instant.Floor, location, calendartz.Reject); err == nil {
		t.Fatal("Snap accepted unknown unit")
	}
	if _, err := instant.Snap(value, instant.Day, 0, location, calendartz.Reject); err == nil {
		t.Fatal("Snap accepted unknown direction")
	}
}

func TestSnapCeilKeepsExactBoundary(t *testing.T) {
	value := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	got, err := instant.Snap(value, instant.Month, instant.Ceil, time.UTC, calendartz.Reject)
	if err != nil || !got.Equal(value) {
		t.Fatalf("Snap exact ceil = %v, %v", got, err)
	}
}

func TestPeriodSnapOutwardReturnsCanonicalContainingRange(t *testing.T) {
	start := time.Date(2026, time.July, 16, 10, 15, 0, 0, time.UTC)
	end := time.Date(2026, time.July, 16, 11, 0, 0, 0, time.UTC)
	period, err := instant.New(start, end, temporal.Closed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := period.SnapOutward(instant.Hour, time.UTC, calendartz.Reject)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != temporal.ClosedOpen || got.Start().Hour() != 10 || got.End().Hour() != 12 {
		t.Fatalf("SnapOutward() = %v .. %v %v", got.Start(), got.End(), got.Bounds())
	}
	empty, _ := instant.New(start, start, temporal.Open)
	if _, err := empty.SnapOutward(instant.Hour, time.UTC, calendartz.Reject); !errors.Is(err, temporal.ErrEmpty) {
		t.Fatalf("empty SnapOutward error = %v", err)
	}
}
