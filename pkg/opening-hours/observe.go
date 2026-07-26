package openinghours

import "time"

// Operation identifies an observable package operation.
type Operation uint8

const (
	// OperationIsOpen identifies an observed instant availability query.
	OperationIsOpen Operation = iota
	// OperationNextTransition identifies an observed transition search.
	OperationNextTransition
)

// Outcome is a bounded, non-sensitive operation result.
type Outcome uint8

const (
	// OutcomeClosed reports a successful query whose resource is closed.
	OutcomeClosed Outcome = iota
	// OutcomeOpen reports a successful query whose resource is open.
	OutcomeOpen
	// OutcomeFound reports a transition found within the search horizon.
	OutcomeFound
	// OutcomeError reports a typed query or search failure.
	OutcomeError
)

// Observation contains only bounded operational data. It intentionally has no
// schedule label, source, revision, date, timezone, or customer fields.
type Observation struct {
	Operation   Operation
	Outcome     Outcome
	RangeCount  int
	SearchSteps int
	Duration    time.Duration
}

// Observer receives one completed observation outside any lock. Panics are
// contained and cannot alter the query result.
type Observer func(Observation)

// ObserveIsOpen runs IsOpen and reports bounded operational data. A nil clock
// disables elapsed measurement and reports a zero duration.
func (s Schedule) ObserveIsOpen(instant time.Time, elapsedClock ElapsedClock, observer Observer) (Availability, error) {
	elapsed := measureElapsed(elapsedClock)
	result, err := s.IsOpen(instant)
	observation := Observation{Operation: OperationIsOpen, Duration: elapsed()}
	if err != nil {
		observation.Outcome = OutcomeError
	} else if result.Open {
		observation.Outcome = OutcomeOpen
	} else {
		observation.Outcome = OutcomeClosed
	}
	if s.data != nil {
		localized := instant.In(s.data.location)
		date, dateErr := NewDate(localized.Year(), localized.Month(), localized.Day())
		if dateErr == nil {
			ranges, rangeErr := s.EffectiveRanges(date)
			if rangeErr == nil {
				observation.RangeCount = len(ranges)
			}
		}
	}
	callObserver(observer, observation)

	return result, err
}

// ObserveNextTransition runs a bounded transition search and reports its
// outcome. A nil clock disables elapsed measurement and reports a zero duration.
func (s Schedule) ObserveNextTransition(instant time.Time, horizon time.Duration, elapsedClock ElapsedClock, observer Observer) (Transition, error) {
	elapsed := measureElapsed(elapsedClock)
	result, err := s.NextTransition(instant, horizon)
	outcome := OutcomeFound
	if err != nil {
		outcome = OutcomeError
	}
	observation := Observation{
		Operation: OperationNextTransition, Outcome: outcome,
		SearchSteps: boundedSearchSteps(horizon), Duration: elapsed(),
	}
	callObserver(observer, observation)

	return result, err
}

func measureElapsed(clock ElapsedClock) func() time.Duration {
	if clock == nil {
		return func() time.Duration { return 0 }
	}

	return clock.Measure()
}

func boundedSearchSteps(horizon time.Duration) int {
	if horizon <= 0 {
		return 0
	}
	steps := int(horizon/(24*time.Hour)) + 1

	return min(steps, 367)
}

func callObserver(observer Observer, observation Observation) {
	if observer == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	observer(observation)
}
