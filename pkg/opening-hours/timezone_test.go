package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestResolveLocalRejectsGapAndCanShiftForward(t *testing.T) {
	schedule, err := openinghours.NewSchedule(openinghours.Config{Timezone: "Europe/Helsinki"})
	if err != nil {
		t.Fatal(err)
	}
	date := openinghours.MustDate(2026, time.March, 29)
	local := mustTime(t, 3, 30)

	_, err = schedule.ResolveLocal(date, local, openinghours.RejectDST)
	if !openinghours.IsCode(err, openinghours.CodeNonexistentLocalTime) {
		t.Fatalf("ResolveLocal() error = %v, want nonexistent local time", err)
	}

	resolved, err := schedule.ResolveLocal(date, local, openinghours.ShiftForward)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Kind != openinghours.LocalGap ||
		!resolved.Instant.Equal(time.Date(2026, time.March, 29, 1, 30, 0, 0, time.UTC)) {
		t.Fatalf("ResolveLocal() = %#v, want 04:30 EEST", resolved)
	}
}

func TestResolveLocalRequiresFoldPolicy(t *testing.T) {
	schedule, err := openinghours.NewSchedule(openinghours.Config{Timezone: "America/New_York"})
	if err != nil {
		t.Fatal(err)
	}
	date := openinghours.MustDate(2026, time.November, 1)
	local := mustTime(t, 1, 30)

	_, err = schedule.ResolveLocal(date, local, openinghours.RejectDST)
	if !openinghours.IsCode(err, openinghours.CodeAmbiguousLocalTime) {
		t.Fatalf("ResolveLocal() error = %v, want ambiguous local time", err)
	}

	earlier, err := schedule.ResolveLocal(date, local, openinghours.PreferEarlier)
	if err != nil {
		t.Fatal(err)
	}
	later, err := schedule.ResolveLocal(date, local, openinghours.PreferLater)
	if err != nil {
		t.Fatal(err)
	}
	if earlier.Kind != openinghours.LocalFold || later.Kind != openinghours.LocalFold ||
		later.Instant.Sub(earlier.Instant) != time.Hour {
		t.Fatalf("fold resolutions = %#v and %#v", earlier, later)
	}
}

func TestIsOpenLocalRequiresExplicitGapAndFoldResolution(t *testing.T) {
	helsinki, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "Europe/Helsinki",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Sunday: openinghours.OpenAllDay(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	gapDate := openinghours.MustDate(2026, time.March, 29)
	gapTime := mustTime(t, 3, 30)
	if _, err := helsinki.IsOpenLocal(gapDate, gapTime, openinghours.RejectDST); !openinghours.IsCode(err, openinghours.CodeNonexistentLocalTime) {
		t.Fatalf("gap IsOpenLocal error = %v", err)
	}
	shifted, err := helsinki.IsOpenLocal(gapDate, gapTime, openinghours.ShiftForward)
	if err != nil || !shifted.Open {
		t.Fatalf("shifted IsOpenLocal = %#v, %v", shifted, err)
	}

	newYork, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "America/New_York",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Sunday: openinghours.OpenAllDay(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	foldDate := openinghours.MustDate(2026, time.November, 1)
	foldTime := mustTime(t, 1, 30)
	if _, err := newYork.IsOpenLocal(foldDate, foldTime, openinghours.RejectDST); !openinghours.IsCode(err, openinghours.CodeAmbiguousLocalTime) {
		t.Fatalf("fold IsOpenLocal error = %v", err)
	}
	for _, policy := range []openinghours.LocalResolutionPolicy{
		openinghours.PreferEarlier, openinghours.PreferLater,
	} {
		result, err := newYork.IsOpenLocal(foldDate, foldTime, policy)
		if err != nil || !result.Open {
			t.Fatalf("fold IsOpenLocal(%v) = %#v, %v", policy, result, err)
		}
	}
}

func TestHistoricalDatelineChangeIsAnExplicitFullDayGap(t *testing.T) {
	schedule, err := openinghours.NewSchedule(openinghours.Config{Timezone: "Pacific/Apia"})
	if err != nil {
		t.Fatal(err)
	}
	date := openinghours.MustDate(2011, time.December, 30)
	local := mustTime(t, 12, 0)
	if _, err := schedule.ResolveLocal(date, local, openinghours.RejectDST); !openinghours.IsCode(err, openinghours.CodeNonexistentLocalTime) {
		t.Fatalf("Apia skipped date error = %v", err)
	}
	shifted, err := schedule.ResolveLocal(date, local, openinghours.ShiftForward)
	if err != nil || shifted.Kind != openinghours.LocalGap ||
		!shifted.Instant.Equal(time.Date(2011, time.December, 30, 22, 0, 0, 0, time.UTC)) {
		t.Fatalf("Apia shifted local = %#v, %v", shifted, err)
	}
}

func TestHalfHourHistoricalFoldPreservesThirtyMinuteDifference(t *testing.T) {
	schedule, err := openinghours.NewSchedule(openinghours.Config{Timezone: "Australia/Lord_Howe"})
	if err != nil {
		t.Fatal(err)
	}
	date := openinghours.MustDate(2024, time.April, 7)
	local := mustTime(t, 1, 45)
	earlier, err := schedule.ResolveLocal(date, local, openinghours.PreferEarlier)
	if err != nil {
		t.Fatal(err)
	}
	later, err := schedule.ResolveLocal(date, local, openinghours.PreferLater)
	if err != nil || later.Instant.Sub(earlier.Instant) != 30*time.Minute {
		t.Fatalf("Lord Howe fold = %#v and %#v, %v", earlier, later, err)
	}
}

func TestIsOpenAtInstantCoversBothFoldOccurrences(t *testing.T) {
	sunday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 1, 30, 2, 30),
	}, openinghours.RejectOverlap)
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "America/New_York",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Sunday: sunday},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, instant := range []time.Time{
		time.Date(2026, time.November, 1, 5, 45, 0, 0, time.UTC),
		time.Date(2026, time.November, 1, 6, 45, 0, 0, time.UTC),
	} {
		result, queryErr := schedule.IsOpen(instant)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if !result.Open {
			t.Errorf("IsOpen(%s) = false, want true", instant)
		}
	}
}

func TestNextTransitionIsBoundedAndEndExclusive(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 10, 0),
	}, openinghours.RejectOverlap)
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Monday: monday},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC)

	opening, err := schedule.NextTransition(start, 3*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if opening.Kind != openinghours.TransitionOpen ||
		!opening.Instant.Equal(time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("NextTransition() = %#v", opening)
	}

	closing, err := schedule.NextTransition(opening.Instant, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if closing.Kind != openinghours.TransitionClose ||
		!closing.Instant.Equal(time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("NextTransition() = %#v", closing)
	}

	_, err = schedule.NextTransition(start, 30*time.Minute)
	if !openinghours.IsCode(err, openinghours.CodeSearchExhausted) {
		t.Fatalf("NextTransition() error = %v, want search exhausted", err)
	}
}
