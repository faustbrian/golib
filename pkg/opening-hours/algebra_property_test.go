package openinghours_test

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

type minuteInterval struct {
	start int
	end   int
}

func scheduleFromMinuteIntervals(t testing.TB, intervals []minuteInterval) openinghours.Schedule {
	t.Helper()

	ranges := make([]openinghours.Range, 0, len(intervals))
	for _, interval := range intervals {
		start, err := openinghours.NewLocalTime(interval.start/60, interval.start%60, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		end, err := openinghours.NewLocalTime(interval.end/60, interval.end%60, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		value, err := openinghours.NewRange(start, end)
		if err != nil {
			t.Fatal(err)
		}
		ranges = append(ranges, value)
	}
	rule, err := openinghours.OpenRanges(ranges, openinghours.MergeAdjacent)
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC",
		Weekly: map[time.Weekday]openinghours.DayRule{
			time.Monday: rule,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	return schedule
}

func TestAlgebraPropertiesAgreeWithPointSet(t *testing.T) {
	fixtures := [][]minuteInterval{
		{{0, 60}},
		{{60, 120}},
		{{0, 120}},
		{{30, 90}},
		{{0, 30}, {90, 120}},
		{{15, 45}, {60, 105}},
		{{120, 180}},
	}
	date := openinghours.MustDate(2026, time.January, 5)

	for leftIndex, leftFixture := range fixtures {
		left := scheduleFromMinuteIntervals(t, leftFixture)
		for rightIndex, rightFixture := range fixtures {
			right := scheduleFromMinuteIntervals(t, rightFixture)
			union, err := left.Union(right)
			if err != nil {
				t.Fatal(err)
			}
			intersection, err := left.Intersection(right)
			if err != nil {
				t.Fatal(err)
			}
			subtraction, err := left.Subtract(right)
			if err != nil {
				t.Fatal(err)
			}

			counts := map[string]int{}
			for minute := 0; minute < 4*60; minute += 15 {
				at, err := openinghours.NewLocalTime(minute/60, minute%60, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				leftOpen := queryLocal(t, left, date, at)
				rightOpen := queryLocal(t, right, date, at)
				unionOpen := queryLocal(t, union, date, at)
				intersectionOpen := queryLocal(t, intersection, date, at)
				subtractionOpen := queryLocal(t, subtraction, date, at)

				if unionOpen != (leftOpen || rightOpen) {
					t.Fatalf("union[%d,%d] at minute %d disagrees with operands", leftIndex, rightIndex, minute)
				}
				if intersectionOpen != (leftOpen && rightOpen) {
					t.Fatalf("intersection[%d,%d] at minute %d disagrees with operands", leftIndex, rightIndex, minute)
				}
				if subtractionOpen != (leftOpen && !rightOpen) {
					t.Fatalf("subtraction[%d,%d] at minute %d disagrees with operands", leftIndex, rightIndex, minute)
				}
				for name, open := range map[string]bool{
					"left": leftOpen, "right": rightOpen, "union": unionOpen,
					"intersection": intersectionOpen,
				} {
					if open {
						counts[name]++
					}
				}
			}
			if counts["union"]+counts["intersection"] != counts["left"]+counts["right"] {
				t.Fatalf("conservation[%d,%d] failed: %#v", leftIndex, rightIndex, counts)
			}
		}
	}
}

func TestNormalizationIsIdempotentAndCanonical(t *testing.T) {
	first := mustRange(t, 11, 0, 12, 0)
	second := mustRange(t, 9, 0, 10, 0)
	third := mustRange(t, 10, 0, 11, 0)

	rule, err := openinghours.OpenRanges(
		[]openinghours.Range{first, second, third},
		openinghours.MergeAdjacent,
	)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := openinghours.OpenRanges(rule.Ranges(), openinghours.MergeAdjacent)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.State() != rule.State() || !reflect.DeepEqual(repeated.Ranges(), rule.Ranges()) {
		t.Fatalf("normalization is not idempotent: first=%#v repeated=%#v", rule, repeated)
	}

	forward := scheduleFromMinuteIntervals(t, []minuteInterval{{0, 30}, {60, 90}})
	reverseRule, err := openinghours.OpenRanges(
		[]openinghours.Range{mustRange(t, 1, 0, 1, 30), mustRange(t, 0, 0, 0, 30)},
		openinghours.MergeAdjacent,
	)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Monday: reverseRule},
	})
	if err != nil {
		t.Fatal(err)
	}
	forwardJSON, err := forward.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	reverseJSON, err := reverse.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(forwardJSON, reverseJSON) {
		t.Fatalf("canonical order depends on input order:\n%s\n%s", forwardJSON, reverseJSON)
	}
}

func TestQueriesAgreeWithRepresentedInstantSet(t *testing.T) {
	monday, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 22, 0, 2, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	tuesday := openinghours.MustDate(2026, time.January, 6)
	additionRule, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 3, 0, 4, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	addition, err := openinghours.NewException(openinghours.ExceptionConfig{
		Date: tuesday, Operation: openinghours.ExceptionAdd, Rule: additionRule,
		Priority: 10, Source: "audit", Revision: "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := openinghours.NewSchedule(openinghours.Config{
		Timezone: "UTC",
		Weekly:   map[time.Weekday]openinghours.DayRule{time.Monday: monday},
		Exceptions: []openinghours.Exception{
			addition,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, time.January, 5, 20, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.January, 6, 6, 0, 0, 0, time.UTC)
	ranges, err := schedule.EffectiveInstantRanges(start, end)
	if err != nil {
		t.Fatal(err)
	}
	for instant := start; instant.Before(end); instant = instant.Add(15 * time.Minute) {
		availability, err := schedule.IsOpen(instant)
		if err != nil {
			t.Fatal(err)
		}
		represented := false
		for _, interval := range ranges {
			if !instant.Before(interval.Start) && instant.Before(interval.End) {
				represented = true
				break
			}
		}
		if availability.Open != represented {
			t.Fatalf("IsOpen(%s)=%t, represented=%t", instant, availability.Open, represented)
		}
	}

	duration, err := schedule.OpenDuration(start, end)
	if err != nil {
		t.Fatal(err)
	}
	var representedDuration time.Duration
	for _, interval := range ranges {
		representedDuration += interval.End.Sub(interval.Start)
	}
	if duration != representedDuration {
		t.Fatalf("OpenDuration()=%s, represented=%s", duration, representedDuration)
	}

	cursor := start
	for index, interval := range ranges {
		opening, err := schedule.NextTransition(cursor, end.Sub(cursor))
		if err != nil || opening.Kind != openinghours.TransitionOpen || !opening.Instant.Equal(interval.Start) {
			t.Fatalf("opening transition %d = %#v, error=%v", index, opening, err)
		}
		closing, err := schedule.NextTransition(opening.Instant, end.Sub(opening.Instant))
		if err != nil || closing.Kind != openinghours.TransitionClose || !closing.Instant.Equal(interval.End) {
			t.Fatalf("closing transition %d = %#v, error=%v", index, closing, err)
		}
		if !closing.Instant.After(cursor) {
			t.Fatalf("transition search did not advance: cursor=%s transition=%s", cursor, closing.Instant)
		}
		cursor = closing.Instant
	}
}

func queryLocal(t testing.TB, schedule openinghours.Schedule, date openinghours.Date, at openinghours.LocalTime) bool {
	t.Helper()

	result, err := schedule.IsOpenLocal(date, at, openinghours.RejectDST)
	if err != nil {
		t.Fatal(err)
	}

	return result.Open
}
