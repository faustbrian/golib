package openinghours_test

import (
	"bytes"
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

func TestExceptionPrecedenceIsStableAcrossEveryInsertionOrder(t *testing.T) {
	date := openinghours.MustDate(2026, time.January, 5)
	baseRule, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 8, 0, 16, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	replacementRule, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 9, 0, 15, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	additionRule, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 16, 0, 17, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	subtractionRule, err := openinghours.OpenRanges([]openinghours.Range{
		mustRange(t, 10, 0, 11, 0),
	}, openinghours.RejectOverlap)
	if err != nil {
		t.Fatal(err)
	}
	configs := []openinghours.ExceptionConfig{
		{Date: date, Operation: openinghours.ExceptionReplace, Rule: replacementRule, Priority: 10, Source: "replace", Revision: "1"},
		{Date: date, Operation: openinghours.ExceptionAdd, Rule: additionRule, Priority: 20, Source: "add", Revision: "1"},
		{Date: date, Operation: openinghours.ExceptionSubtract, Rule: subtractionRule, Priority: 30, Source: "subtract", Revision: "1"},
	}
	exceptions := make([]openinghours.Exception, len(configs))
	for index, config := range configs {
		exception, err := openinghours.NewException(config)
		if err != nil {
			t.Fatal(err)
		}
		exceptions[index] = exception
	}

	orders := [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}}
	var canonical []byte
	for _, order := range orders {
		input := []openinghours.Exception{
			exceptions[order[0]], exceptions[order[1]], exceptions[order[2]],
		}
		schedule, err := openinghours.NewSchedule(openinghours.Config{
			Timezone:       "UTC",
			Weekly:         map[time.Weekday]openinghours.DayRule{time.Monday: baseRule},
			Exceptions:     input,
			ConflictPolicy: openinghours.ResolveCanonical,
		})
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := schedule.CanonicalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if canonical == nil {
			canonical = encoded
		} else if !bytes.Equal(canonical, encoded) {
			t.Fatalf("canonical precedence depends on insertion order %v", order)
		}
		for _, point := range []struct {
			hour int
			open bool
		}{{8, false}, {9, true}, {10, false}, {11, true}, {14, true}, {15, false}, {16, true}, {17, false}} {
			result, err := schedule.IsOpenLocal(date, mustTime(t, point.hour, 0), openinghours.RejectDST)
			if err != nil || result.Open != point.open {
				t.Fatalf("order %v at %02d:00 = %#v, error=%v", order, point.hour, result, err)
			}
		}
	}
}
