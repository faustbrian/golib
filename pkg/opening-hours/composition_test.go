package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func scheduleWithMonday(t testing.TB, ranges ...openinghours.Range) openinghours.Schedule {
	t.Helper()

	rule, err := openinghours.OpenRanges(ranges, openinghours.MergeAdjacent)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Monday: rule},
	})
	if err != nil {
		t.Fatal(err)
	}

	return schedule
}

func TestScheduleAlgebraCombinesAvailabilityWithoutMutatingOperands(t *testing.T) {
	left := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	right := scheduleWithMonday(t, mustRange(t, 11, 0, 14, 0))
	date := openinghours.MustDate(2026, time.January, 5)

	tests := []struct {
		name    string
		compose func() (openinghours.Schedule, error)
		at10    bool
		at11    bool
		at13    bool
	}{
		{"union", func() (openinghours.Schedule, error) { return left.Union(right) }, true, true, true},
		{"intersection", func() (openinghours.Schedule, error) { return left.Intersection(right) }, false, true, false},
		{"subtraction", func() (openinghours.Schedule, error) { return left.Subtract(right) }, true, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			combined, err := test.compose()
			if err != nil {
				t.Fatal(err)
			}
			for _, point := range []struct {
				hour int
				want bool
			}{{10, test.at10}, {11, test.at11}, {13, test.at13}} {
				result, queryErr := combined.IsOpenLocal(date, mustTime(t, point.hour, 0), openinghours.RejectDST)
				if queryErr != nil {
					t.Fatal(queryErr)
				}
				if result.Open != point.want || result.Explanation.Rule != openinghours.RuleComposition {
					t.Errorf("%s at %d = %#v, want open=%t", test.name, point.hour, result, point.want)
				}
			}
		})
	}

	original, err := left.IsOpenLocal(date, mustTime(t, 11, 30), openinghours.RejectDST)
	if err != nil || !original.Open {
		t.Fatalf("left operand changed: result=%#v error=%v", original, err)
	}
}

func TestCompositionRequiresMatchingTimezones(t *testing.T) {
	left := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	rule, _ := openinghours.OpenRanges([]openinghours.Range{mustRange(t, 9, 0, 12, 0)}, openinghours.RejectOverlap)
	right, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "Europe/Helsinki",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Monday: rule},
	})

	_, err := left.Union(right)
	if !openinghours.IsCode(err, openinghours.CodeTimezoneMismatch) {
		t.Fatalf("Union() error = %v, want timezone mismatch", err)
	}
}

func TestCompositionDepthIsBounded(t *testing.T) {
	base := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	combined := base
	var err error
	for range openinghours.MaxCompositionDepth {
		combined, err = combined.Union(base)
		if err != nil {
			break
		}
	}
	if !openinghours.IsCode(err, openinghours.CodeLimitExceeded) {
		t.Fatalf("deep Union() error = %v, want limit exceeded", err)
	}
}

func TestOverlayUsesOnlyExplicitRightHandRules(t *testing.T) {
	leftRule, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 17, 0),
	}, openinghours.RejectOverlap)
	left, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{
			time.Monday: leftRule, time.Tuesday: leftRule,
		},
	})
	right, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Monday: openinghours.Closed(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := left.Overlay(right)
	if err != nil {
		t.Fatal(err)
	}

	monday := openinghours.MustDate(2026, time.January, 5)
	result, err := overlay.IsOpenLocal(monday, mustTime(t, 10, 0), openinghours.RejectDST)
	if err != nil || result.Open {
		t.Fatalf("explicit closure did not override: result=%#v error=%v", result, err)
	}
	tuesday := openinghours.MustDate(2026, time.January, 6)
	result, err = overlay.IsOpenLocal(tuesday, mustTime(t, 10, 0), openinghours.RejectDST)
	if err != nil || !result.Open {
		t.Fatalf("inherited Tuesday did not preserve left: result=%#v error=%v", result, err)
	}
}

func TestOverlayIncomingSpillOverridesOnlySpillInterval(t *testing.T) {
	tuesdayRule, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 0, 0, 8, 0),
	}, openinghours.RejectOverlap)
	left, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Tuesday: tuesdayRule},
	})
	mondayOvernight, _ := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 22, 0, 2, 0),
	}, openinghours.RejectOverlap)
	right, _ := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC", Weekly: map[time.Weekday]openinghours.DayRule{time.Monday: mondayOvernight},
	})
	overlay, err := left.Overlay(right)
	if err != nil {
		t.Fatal(err)
	}
	tuesday := openinghours.MustDate(2026, time.January, 6)

	for _, test := range []struct {
		hour int
		open bool
	}{{1, true}, {3, true}, {7, true}, {8, false}} {
		result, queryErr := overlay.IsOpenLocal(tuesday, mustTime(t, test.hour, 0), openinghours.RejectDST)
		if queryErr != nil || result.Open != test.open {
			t.Fatalf("overlay at %d = %#v error=%v, want %t", test.hour, result, queryErr, test.open)
		}
	}
}
