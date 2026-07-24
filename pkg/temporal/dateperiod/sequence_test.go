package dateperiod_test

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/dateperiod"
)

func TestDateSetIterationSearchTransformAndReduction(t *testing.T) {
	t.Parallel()

	first, _ := dateperiod.New(calendar.MustDate(2026, time.January, 2), calendar.MustDate(2026, time.January, 3), temporal.Closed)
	second, _ := dateperiod.New(calendar.MustDate(2026, time.January, 5), calendar.MustDate(2026, time.January, 6), temporal.Closed)
	set, _ := dateperiod.NewSet(temporal.Limits{}, second, first)
	var iterated []dateperiod.Period
	for period := range set.All() {
		iterated = append(iterated, period)
	}
	if len(iterated) != 2 || !iterated[0].SetEqual(first) || !iterated[1].SetEqual(second) {
		t.Fatalf("All() = %+v", iterated)
	}
	stopped := 0
	for range set.All() {
		stopped++
		break
	}
	if stopped != 1 {
		t.Fatalf("early All() count = %d", stopped)
	}
	if index, ok := set.Search(calendar.Date{}); ok || index != 0 {
		t.Fatalf("Search(invalid) = %d, %v", index, ok)
	}
	if index, ok := set.Search(calendar.MustDate(2026, time.January, 3)); !ok || index != 0 {
		t.Fatalf("Search(first) = %d, %v", index, ok)
	}
	if index, ok := set.Search(calendar.MustDate(2026, time.January, 4)); ok || index != 1 {
		t.Fatalf("Search(gap) = %d, %v", index, ok)
	}
	if index, ok := set.Search(calendar.MustDate(2026, time.January, 7)); ok || index != 2 {
		t.Fatalf("Search(after) = %d, %v", index, ok)
	}

	moved, err := set.Transform(func(period dateperiod.Period) (dateperiod.Period, error) { return period.MoveDays(1) })
	if err != nil || !moved.Includes(calendar.MustDate(2026, time.January, 4)) || set.Includes(calendar.MustDate(2026, time.January, 4)) {
		t.Fatalf("Transform() = %+v, %v", moved.Periods(), err)
	}
	total, err := dateperiod.Reduce(set, 0, func(sum int, period dateperiod.Period) (int, error) { return sum + period.Days(), nil })
	if err != nil || total != 4 {
		t.Fatalf("Reduce() = %d, %v", total, err)
	}
}

func TestDateSetTransformAndReducePropagateErrors(t *testing.T) {
	t.Parallel()

	period, _ := dateperiod.Day(calendar.MustDate(2026, time.January, 2))
	set, _ := dateperiod.NewSet(temporal.Limits{}, period)
	want := errors.New("stop")
	if _, err := set.Transform(func(dateperiod.Period) (dateperiod.Period, error) { return dateperiod.Period{}, want }); !errors.Is(err, want) {
		t.Fatalf("Transform(error) = %v", err)
	}
	if _, err := set.Transform(nil); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Transform(nil) = %v", err)
	}
	if _, err := dateperiod.Reduce(set, 0, func(int, dateperiod.Period) (int, error) { return 0, want }); !errors.Is(err, want) {
		t.Fatalf("Reduce(error) = %v", err)
	}
	if _, err := dateperiod.Reduce[int](set, 0, nil); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Reduce(nil) = %v", err)
	}
}
