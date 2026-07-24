package openinghours_test

import (
	"strings"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestHumanSummaryIsSeparateFromCanonicalEncoding(t *testing.T) {
	ranged, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 12, 0), mustRange(t, 13, 0, 17, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	date := openinghours.MustDate(2026, time.January, 5)
	closure, err := openinghours.NewException(openinghours.ExceptionConfig{
		Date: date, Operation: openinghours.ExceptionClose,
		Priority: 10, Source: "private-source", Revision: "private-revision",
	})
	if err != nil {
		t.Fatal(err)
	}
	end := openinghours.MustDate(2026, time.December, 31)
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "Europe/Helsinki",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Sunday: openinghours.Closed(), time.Monday: ranged,
			time.Tuesday: openinghours.OpenAllDay(),
		},
		Exceptions: []openinghours.Exception{closure}, EffectiveStart: &date,
		EffectiveEnd: &end, OutsideEffective: openinghours.OutsideError,
	})
	if err != nil {
		t.Fatal(err)
	}

	summary, err := schedule.HumanSummary()
	if err != nil {
		t.Fatal(err)
	}
	wantParts := []string{
		"timezone Europe/Helsinki", "sunday closed",
		"monday 09:00:00-12:00:00,13:00:00-17:00:00",
		"tuesday all day", "wednesday inherited", "exceptions 1",
		"effective 2026-01-05 through 2026-12-31", "outside error",
	}
	for _, want := range wantParts {
		if !strings.Contains(summary, want) {
			t.Errorf("HumanSummary() = %q, missing %q", summary, want)
		}
	}
	if strings.Contains(summary, "private-source") ||
		strings.Contains(summary, "private-revision") || strings.HasPrefix(summary, "{") {
		t.Fatalf("HumanSummary() exposed provenance or wire text: %q", summary)
	}
}

func TestHumanSummaryHandlesZeroAndComposition(t *testing.T) {
	zero, err := (openinghours.Schedule{}).HumanSummary()
	if err != nil || zero != "closed schedule (timezone unset)" {
		t.Fatalf("zero HumanSummary() = %q, %v", zero, err)
	}

	left := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	right := scheduleWithMonday(t, mustRange(t, 13, 0, 17, 0))
	plain, err := left.HumanSummary()
	if err != nil || !strings.Contains(plain, "exceptions 0; outside closed") {
		t.Fatalf("plain HumanSummary() = %q, %v", plain, err)
	}
	combined, err := left.Union(right)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := combined.HumanSummary()
	if err != nil || summary != "timezone UTC; composition union; depth 2" {
		t.Fatalf("composition HumanSummary() = %q, %v", summary, err)
	}
}

func TestHumanSummaryShowsOneSidedEffectiveBounds(t *testing.T) {
	date := openinghours.MustDate(2026, time.January, 5)
	tests := []struct {
		name  string
		start *openinghours.Date
		end   *openinghours.Date
		want  string
	}{
		{name: "start", start: &date, want: "effective 2026-01-05 through unbounded"},
		{name: "end", end: &date, want: "effective unbounded through 2026-01-05"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedule, err := openinghours.NewSchedule(openinghours.Config{
				Timezone: "UTC", EffectiveStart: test.start, EffectiveEnd: test.end,
			})
			if err != nil {
				t.Fatal(err)
			}
			summary, err := schedule.HumanSummary()
			if err != nil || !strings.Contains(summary, test.want) {
				t.Fatalf("HumanSummary() = %q, %v; want %q", summary, err, test.want)
			}
		})
	}
}
