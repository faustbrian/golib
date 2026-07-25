package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func mustTime(t testing.TB, hour, minute int) openinghours.LocalTime {
	t.Helper()

	value, err := openinghours.NewLocalTime(hour, minute, 0, 0)
	if err != nil {
		t.Fatalf("NewLocalTime() error = %v", err)
	}

	return value
}

func TestZeroScheduleIsClosed(t *testing.T) {
	var schedule openinghours.Schedule

	date := openinghours.MustDate(2026, time.January, 5)
	result, err := schedule.Ranges(date)
	if err != nil {
		t.Fatalf("Ranges() error = %v", err)
	}
	if result.State != openinghours.DayClosed || len(result.Ranges) != 0 {
		t.Fatalf("Ranges() = %#v, want an explicitly closed result", result)
	}
}

func TestRangeRejectsEqualEndpoints(t *testing.T) {
	start := mustTime(t, 9, 0)

	_, err := openinghours.NewRange(start, start)
	if !openinghours.IsCode(err, openinghours.CodeInvalidRange) {
		t.Fatalf("NewRange() error = %v, want %q", err, openinghours.CodeInvalidRange)
	}
}

func TestScheduleNormalizesAndCopiesWeeklyRanges(t *testing.T) {
	first, err := openinghours.NewRange(mustTime(t, 13, 0), mustTime(t, 17, 0))
	if err != nil {
		t.Fatal(err)
	}
	second, err := openinghours.NewRange(mustTime(t, 9, 0), mustTime(t, 12, 0))
	if err != nil {
		t.Fatal(err)
	}

	ranges := []openinghours.Range{first, second}
	monday, err := openinghours.OpenRanges(ranges, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "Europe/Helsinki",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Monday: monday,
		},
	})
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}

	ranges[0] = second
	result, err := schedule.Ranges(openinghours.MustDate(2026, time.January, 5))
	if err != nil {
		t.Fatalf("Ranges() error = %v", err)
	}
	if len(result.Ranges) != 2 || result.Ranges[0] != second || result.Ranges[1] != first {
		t.Fatalf("Ranges() = %#v, want sorted copied ranges", result.Ranges)
	}
}

func TestOpenRangesRejectsOverlapWithoutExplicitMergePolicy(t *testing.T) {
	first, _ := openinghours.NewRange(mustTime(t, 9, 0), mustTime(t, 12, 0))
	second, _ := openinghours.NewRange(mustTime(t, 11, 0), mustTime(t, 13, 0))

	_, err := openinghours.OpenRanges([]openinghours.Range{first, second}, openinghours.RejectOverlap)
	if !openinghours.IsCode(err, openinghours.CodeOverlap) {
		t.Fatalf("OpenRanges() error = %v, want %q", err, openinghours.CodeOverlap)
	}
}
