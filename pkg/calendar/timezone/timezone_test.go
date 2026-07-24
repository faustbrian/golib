package timezone_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	calendartz "github.com/faustbrian/golib/pkg/calendar/timezone"
)

func TestResolveRejectsGapAndSelectsFoldOccurrence(t *testing.T) {
	t.Parallel()

	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	gap := calendartz.MustLocalDateTime(calendar.MustDate(2024, time.March, 10), 2, 30, 0, 0)
	if _, err := calendartz.Resolve(gap, newYork, calendartz.Reject); !errors.Is(err, calendartz.ErrNonexistent) {
		t.Fatalf("gap error = %v", err)
	}

	fold := calendartz.MustLocalDateTime(calendar.MustDate(2024, time.November, 3), 1, 30, 0, 0)
	if _, err := calendartz.Resolve(fold, newYork, calendartz.Reject); !errors.Is(err, calendartz.ErrAmbiguous) {
		t.Fatalf("fold error = %v", err)
	}
	earlier, err := calendartz.Resolve(fold, newYork, calendartz.Earlier)
	if err != nil {
		t.Fatal(err)
	}
	later, err := calendartz.Resolve(fold, newYork, calendartz.Later)
	if err != nil {
		t.Fatal(err)
	}
	if !earlier.Before(later) || later.Sub(earlier) != time.Hour {
		t.Fatalf("fold occurrences = %s and %s", earlier, later)
	}
	_, earlierOffset := earlier.Zone()
	_, laterOffset := later.Zone()
	if earlierOffset != -4*60*60 || laterOffset != -5*60*60 {
		t.Fatalf("fold offsets = %d, %d", earlierOffset, laterOffset)
	}
	matched, err := calendartz.Resolve(fold, newYork, calendartz.MatchOffset(-5*60*60))
	if err != nil || !matched.Equal(later) {
		t.Fatalf("offset match = %s, %v", matched, err)
	}
}

func TestResolveRejectsSkippedCivilDate(t *testing.T) {
	t.Parallel()

	apia, err := time.LoadLocation("Pacific/Apia")
	if err != nil {
		t.Fatal(err)
	}
	local := calendartz.MustLocalDateTime(calendar.MustDate(2011, time.December, 30), 12, 0, 0, 0)
	if _, err := calendartz.Resolve(local, apia, calendartz.Reject); !errors.Is(err, calendartz.ErrNonexistent) {
		t.Fatalf("Apia skipped-date error = %v", err)
	}
}

func TestInstantRoundTripAndExclusiveDayRange(t *testing.T) {
	t.Parallel()

	helsinki, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Fatal(err)
	}
	date := calendar.MustDate(2024, time.March, 31)
	start, end, err := calendartz.DayRange(date, helsinki, calendartz.Reject)
	if err != nil {
		t.Fatal(err)
	}
	if end.Sub(start) != 23*time.Hour {
		t.Fatalf("DST day length = %s", end.Sub(start))
	}
	local, err := calendartz.FromInstant(start.Add(12*time.Hour), helsinki)
	if err != nil || local.Date() != date || local.Hour() != 13 {
		t.Fatalf("FromInstant() = %v, %v", local, err)
	}
}

func TestTimezoneInputsAreExplicitAndValidated(t *testing.T) {
	t.Parallel()

	date := calendar.MustDate(2024, time.January, 1)
	if _, err := calendartz.NewLocalDateTime(date, 24, 0, 0, 0); !errors.Is(err, calendartz.ErrInvalidLocalTime) {
		t.Fatalf("invalid time error = %v", err)
	}
	local := calendartz.MustLocalDateTime(date, 0, 0, 0, 0)
	if _, err := calendartz.Resolve(local, nil, calendartz.Reject); !errors.Is(err, calendartz.ErrInvalidLocation) {
		t.Fatalf("nil location error = %v", err)
	}
	if _, err := calendartz.Resolve(local, time.UTC, calendartz.MatchOffset(3600)); !errors.Is(err, calendartz.ErrOffsetMismatch) {
		t.Fatalf("offset mismatch error = %v", err)
	}
}

func TestLoadLocationBoundsIANAIdentifiers(t *testing.T) {
	t.Parallel()

	location, err := calendartz.LoadLocation("America/New_York")
	if err != nil || location.String() != "America/New_York" {
		t.Fatalf("LoadLocation() = %v, %v", location, err)
	}
	for _, name := range []string{"", string([]byte{0xff}), string(make([]byte, calendartz.MaxZoneNameBytes+1)), "../UTC", "/UTC", `A\B`, "A//B", "A/./B", "Not/A_Real_Zone"} {
		if _, err := calendartz.LoadLocation(name); !errors.Is(err, calendartz.ErrInvalidZone) {
			t.Fatalf("LoadLocation(%q) error = %v", name, err)
		}
	}
}
