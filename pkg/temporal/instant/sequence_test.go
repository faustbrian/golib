package instant_test

import (
	"errors"
	"testing"
	"time"

	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func TestSetIterationSearchTransformAndReduction(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	first := mustSequencePeriod(t, base, base.Add(time.Hour), temporal.ClosedOpen)
	second := mustSequencePeriod(t, base.Add(2*time.Hour), base.Add(3*time.Hour), temporal.ClosedOpen)
	set, err := instant.NewSet(temporal.Limits{}, second, first)
	if err != nil {
		t.Fatalf("NewSet(): %v", err)
	}

	var iterated []instant.Period
	for period := range set.All() {
		iterated = append(iterated, period)
	}
	if len(iterated) != 2 || !iterated[0].Equal(first) || !iterated[1].Equal(second) {
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
	if index, ok := set.Search(base.Add(30 * time.Minute)); !ok || index != 0 {
		t.Fatalf("Search(first) = %d, %v", index, ok)
	}
	if index, ok := set.Search(base.Add(90 * time.Minute)); ok || index != 1 {
		t.Fatalf("Search(gap) = %d, %v", index, ok)
	}
	if index, ok := set.Search(base.Add(4 * time.Hour)); ok || index != 2 {
		t.Fatalf("Search(after) = %d, %v", index, ok)
	}

	moved, err := set.Transform(func(period instant.Period) (instant.Period, error) {
		return period.Move(time.Hour), nil
	})
	if err != nil || !moved.Includes(base.Add(90*time.Minute)) || set.Includes(base.Add(90*time.Minute)) {
		t.Fatalf("Transform() = %+v, %v", moved.Periods(), err)
	}
	total, err := instant.Reduce(set, time.Duration(0), func(sum time.Duration, period instant.Period) (time.Duration, error) {
		duration, err := period.Duration()
		return sum + duration, err
	})
	if err != nil || total != 2*time.Hour {
		t.Fatalf("Reduce() = %v, %v", total, err)
	}
}

func TestSetTransformAndReducePropagateErrorsAndLimits(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	period := mustSequencePeriod(t, base, base.Add(time.Hour), temporal.ClosedOpen)
	set, _ := instant.NewSet(temporal.Limits{}, period)
	want := errors.New("stop")
	if _, err := set.Transform(func(instant.Period) (instant.Period, error) { return instant.Period{}, want }); !errors.Is(err, want) {
		t.Fatalf("Transform(error) = %v", err)
	}
	if _, err := set.Transform(nil); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Transform(nil) = %v", err)
	}
	if _, err := instant.Reduce(set, 0, func(int, instant.Period) (int, error) { return 0, want }); !errors.Is(err, want) {
		t.Fatalf("Reduce(error) = %v", err)
	}
	if _, err := instant.Reduce[int](set, 0, nil); !errors.Is(err, temporal.ErrUnsupported) {
		t.Fatalf("Reduce(nil) = %v", err)
	}
}

func mustSequencePeriod(t *testing.T, start, end time.Time, bounds temporal.Bounds) instant.Period {
	t.Helper()
	period, err := instant.New(start, end, bounds)
	if err != nil {
		t.Fatalf("instant.New(): %v", err)
	}
	return period
}
