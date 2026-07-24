package timeofday_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/timeofday"
)

func TestApplyRequiresExplicitCalendarContextAndResolvesDST(t *testing.T) {
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	date := calendar.MustDate(2026, time.March, 29)
	ordinary, err := timeofday.New(1, 30, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ordinary.Apply(date, location, calendartz.Reject)
	if err != nil || got.Hour() != 1 || got.Location() != location {
		t.Fatalf("Apply() = %v, %v", got, err)
	}

	nonexistent, err := timeofday.New(3, 30, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nonexistent.Apply(date, location, calendartz.Reject); !errors.Is(err, calendartz.ErrNonexistent) {
		t.Fatalf("Apply(nonexistent) error = %v", err)
	}
	if _, err := ordinary.Apply(date, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("Apply(nil location) error = %v", err)
	}
	if _, err := ordinary.Apply(calendar.Date{}, location, calendartz.Reject); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("Apply(invalid date) error = %v", err)
	}
}

func TestApplyTreatsEndOfDayAsNextCivilMidnight(t *testing.T) {
	location := time.FixedZone("test", 2*60*60)
	date := calendar.MustDate(2026, time.July, 16)
	got, err := timeofday.EndOfDay().Apply(date, location, calendartz.Reject)
	if err != nil {
		t.Fatal(err)
	}
	if got.Day() != 17 || got.Hour() != 0 {
		t.Fatalf("EndOfDay.Apply() = %v", got)
	}
	maximum := calendar.MustDate(9999, time.December, 31)
	if _, err := timeofday.EndOfDay().Apply(maximum, location, calendartz.Reject); err == nil {
		t.Fatal("EndOfDay.Apply accepted maximum-date overflow")
	}
}

func TestFromInstantPreservesLocalComponentsAndPrecision(t *testing.T) {
	location := time.FixedZone("test", -5*60*60)
	value := time.Date(2026, time.July, 16, 12, 34, 56, 123_000_000, time.UTC)
	got, date, err := timeofday.FromInstant(value, location, 3)
	if err != nil {
		t.Fatal(err)
	}
	if date.String() != "2026-07-16" || got.String() != "07:34:56.123" {
		t.Fatalf("FromInstant() = %s %s", date, got)
	}
	if _, _, err := timeofday.FromInstant(value, nil, 3); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("FromInstant(nil) error = %v", err)
	}
	if _, _, err := timeofday.FromInstant(value, location, 10); err == nil {
		t.Fatal("FromInstant accepted excessive precision")
	}
	if _, _, err := timeofday.FromInstant(value, location, 2); !errors.Is(err, temporal.ErrPrecision) {
		t.Fatalf("FromInstant(inexact precision) error = %v", err)
	}
}

func TestTimeRoundUsesExplicitDirectionAndDailyBoundary(t *testing.T) {
	value, err := timeofday.New(10, 29, 30, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		mode timeofday.RoundingMode
		want string
	}{
		{timeofday.RoundFloor, "10:00:00"},
		{timeofday.RoundNearest, "10:00:00"},
		{timeofday.RoundCeil, "11:00:00"},
	}
	for _, test := range tests {
		got, err := value.Round(time.Hour, test.mode)
		if err != nil || got.String() != test.want {
			t.Fatalf("Round(%d) = %q, %v; want %q", test.mode, got, err, test.want)
		}
	}
	tie, _ := timeofday.New(23, 30, 0, 0, 0)
	got, err := tie.Round(time.Hour, timeofday.RoundNearest)
	if err != nil || !got.IsEndBoundary() {
		t.Fatalf("tie Round() = %v, %v", got, err)
	}
	if _, err := value.Round(0, timeofday.RoundFloor); err == nil {
		t.Fatal("Round accepted zero unit")
	}
	if _, err := value.Round(7*time.Hour, timeofday.RoundFloor); err == nil {
		t.Fatal("Round accepted non-divisor of day")
	}
	if _, err := value.Round(time.Hour, 99); err == nil {
		t.Fatal("Round accepted unknown mode")
	}
	exact, err := timeofday.Noon().Round(time.Hour, timeofday.RoundCeil)
	if err != nil || !exact.Equal(timeofday.Noon()) {
		t.Fatalf("Round(exact) = %v, %v", exact, err)
	}
}
