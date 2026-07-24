package openinghours_test

import (
	"testing"
	"time"

	openinghours "github.com/faustbrian/golib/pkg/opening-hours"
)

type fixedElapsedClock struct{ duration time.Duration }

func (clock fixedElapsedClock) Since(time.Time) time.Duration { return clock.duration }
func (clock fixedElapsedClock) Measure() func() time.Duration {
	return func() time.Duration { return clock.duration }
}

func TestObservationIsBoundedAndCannotChangeQueryResult(t *testing.T) {
	schedule := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	instant := time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC)
	var got openinghours.Observation
	result, err := schedule.ObserveIsOpen(instant, fixedElapsedClock{duration: 12 * time.Millisecond}, func(observation openinghours.Observation) {
		got = observation
		panic("observer failure")
	})
	if err != nil || !result.Open {
		t.Fatalf("ObserveIsOpen() = %#v, error=%v", result, err)
	}
	if got.Operation != openinghours.OperationIsOpen || got.Outcome != openinghours.OutcomeOpen ||
		got.RangeCount != 1 || got.Duration != 12*time.Millisecond {
		t.Fatalf("observation = %#v", got)
	}
}

func TestNilObserverIsAllowed(t *testing.T) {
	schedule := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	result, err := schedule.ObserveIsOpen(
		time.Date(2026, time.January, 5, 10, 0, 0, 0, time.UTC), nil, nil,
	)
	if err != nil || !result.Open {
		t.Fatalf("ObserveIsOpen(nil) = %#v, error=%v", result, err)
	}
}

func TestObservationWithoutElapsedClockIsExplicitlyUnmeasured(t *testing.T) {
	schedule := scheduleWithMonday(t, mustRange(t, 9, 0, 12, 0))
	var got openinghours.Observation
	_, err := schedule.ObserveNextTransition(
		time.Date(2026, time.January, 5, 8, 0, 0, 0, time.UTC),
		2*time.Hour, nil, func(value openinghours.Observation) { got = value },
	)
	if err != nil || got.Duration != 0 {
		t.Fatalf("unmeasured observation = %#v, %v", got, err)
	}
}
