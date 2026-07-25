package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestEffectiveRangesExposeSubtractionFragments(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 15, 0),
	}, openinghours.RejectOverlap)
	date := openinghours.MustDate(2026, time.January, 5)
	removalRule, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 10, 0, 11, 0),
	}, openinghours.RejectOverlap)
	removal, _ := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionSubtract, Rule: removalRule,
		Priority: 10, Source: "maintenance", Revision: "1",
	})
	schedule, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: monday},
		Exceptions: []openinghours.Exception{removal},
	})

	ranges, err := schedule.EffectiveRanges(date)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 2 || ranges[0].Start != mustTime(t, 9, 0) ||
		ranges[0].End != mustTime(t, 10, 0) || ranges[1].Start != mustTime(t, 11, 0) ||
		ranges[1].End != mustTime(t, 15, 0) {
		t.Fatalf("EffectiveRanges() = %#v", ranges)
	}
}

func TestOpenDurationUsesElapsedTimeAcrossDSTFold(t *testing.T) {
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "America/New_York",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Sunday: openinghours.OpenAllDay()},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.November, 1, 4, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.November, 2, 5, 0, 0, 0, time.UTC)

	duration, err := schedule.OpenDuration(start, end)
	if err != nil {
		t.Fatal(err)
	}
	if duration != 25*time.Hour {
		t.Fatalf("OpenDuration() = %s, want 25h", duration)
	}
}

func TestPreviousTransitionAndTypedOpeningClosingSearch(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 10, 0),
	}, openinghours.RejectOverlap)
	schedule, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: monday},
	})
	at := time.Date(2026, time.January, 5, 9, 30, 0, 0, time.UTC)

	previous, err := schedule.PreviousTransition(at, 2*time.Hour)
	if err != nil || previous.Kind != openinghours.TransitionOpen || previous.Instant.Hour() != 9 {
		t.Fatalf("PreviousTransition() = %#v, error=%v", previous, err)
	}
	closing, err := schedule.NextClosing(at, 2*time.Hour)
	if err != nil || closing.Kind != openinghours.TransitionClose || closing.Instant.Hour() != 10 {
		t.Fatalf("NextClosing() = %#v, error=%v", closing, err)
	}
	opening, err := schedule.NextOpening(
		time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC), 2*time.Hour,
	)
	if err != nil || opening.Kind != openinghours.TransitionOpen || opening.Instant.Hour() != 9 {
		t.Fatalf("NextOpening() = %#v, error=%v", opening, err)
	}
}

func TestEffectiveInstantRangesClipsToCallerInterval(t *testing.T) {
	monday, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 12, 0),
	}, openinghours.RejectOverlap)
	schedule, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: monday},
	})
	start := time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.January, 5, 11, 0, 0, 0, time.UTC)

	ranges, err := schedule.EffectiveInstantRanges(start, end)
	if err != nil {
		t.Fatal(err)
	}
	if len(ranges) != 1 || !ranges[0].Start.Equal(start) || !ranges[0].End.Equal(end) {
		t.Fatalf("EffectiveInstantRanges() = %#v", ranges)
	}
}
