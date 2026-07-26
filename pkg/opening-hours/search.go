package openinghours

import (
	"slices"
	"time"
)

const maxSearchHorizon = 366 * 24 * time.Hour

// TransitionKind identifies an opening or closing boundary.
type TransitionKind uint8

const (
	// TransitionOpen identifies a boundary whose following instant is open.
	TransitionOpen TransitionKind = iota
	// TransitionClose identifies a boundary whose following instant is closed.
	TransitionClose
)

// Transition is an explained availability boundary.
type Transition struct {
	Instant     time.Time
	Kind        TransitionKind
	Explanation Explanation
}

// NextTransition finds the first availability boundary strictly after instant.
// The horizon must be positive and no greater than 366 elapsed days.
func (s Schedule) NextTransition(instant time.Time, horizon time.Duration) (Transition, error) {
	if horizon <= 0 || horizon > maxSearchHorizon {
		return Transition{}, newError("next transition", CodeInvalidHorizon)
	}
	if s.data == nil {
		return Transition{}, newError("next transition", CodeSearchExhausted)
	}
	deadline := instant.Add(horizon)

	localized := instant.In(s.data.location)
	date, _ := NewDate(localized.Year(), localized.Month(), localized.Day())
	transitions := make([]Transition, 0, 16)
	for step := 0; step <= 367; step++ {
		day, err := addDate(date, step)
		if err != nil {
			break
		}
		midnight := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.data.location)
		if midnight.After(deadline.Add(48 * time.Hour)) {
			break
		}
		segments, _, _, err := s.effectiveSegments(day)
		if err != nil {
			return Transition{}, err
		}
		for _, item := range segments {
			opening, openingErr := s.resolveBoundary(day, item.start, true)
			if openingErr == nil {
				transitions = append(transitions, Transition{Instant: opening, Kind: TransitionOpen})
			}
			closing, closingErr := s.resolveBoundary(day, item.end, false)
			if closingErr == nil {
				transitions = append(transitions, Transition{Instant: closing, Kind: TransitionClose})
			}
		}
	}
	slices.SortFunc(transitions, func(left, right Transition) int {
		if comparison := left.Instant.Compare(right.Instant); comparison != 0 {
			return comparison
		}
		return int(left.Kind) - int(right.Kind)
	})
	for _, transition := range transitions {
		if !transition.Instant.After(instant) || transition.Instant.After(deadline) {
			continue
		}
		// Candidate generation already evaluated these civil dates, so point
		// queries at their boundaries cannot introduce a new schedule error.
		before, _ := s.IsOpen(transition.Instant.Add(-time.Nanosecond))
		after, _ := s.IsOpen(transition.Instant)
		if before.Open == after.Open {
			continue
		}
		if after.Open {
			transition.Kind = TransitionOpen
		} else {
			transition.Kind = TransitionClose
		}
		transition.Explanation = after.Explanation
		return transition, nil
	}

	return Transition{}, newError("next transition", CodeSearchExhausted)
}

// NextOpening finds the first opening boundary strictly after instant.
func (s Schedule) NextOpening(instant time.Time, horizon time.Duration) (Transition, error) {
	return s.nextTransitionOfKind(instant, horizon, TransitionOpen)
}

// NextClosing finds the first closing boundary strictly after instant.
func (s Schedule) NextClosing(instant time.Time, horizon time.Duration) (Transition, error) {
	return s.nextTransitionOfKind(instant, horizon, TransitionClose)
}

func (s Schedule) nextTransitionOfKind(instant time.Time, horizon time.Duration, kind TransitionKind) (Transition, error) {
	if horizon <= 0 || horizon > maxSearchHorizon {
		return Transition{}, newError("next typed transition", CodeInvalidHorizon)
	}
	deadline := instant.Add(horizon)
	cursor := instant
	for cursor.Before(deadline) {
		transition, err := s.NextTransition(cursor, deadline.Sub(cursor))
		if err != nil {
			return Transition{}, err
		}
		if transition.Kind == kind {
			return transition, nil
		}
		cursor = transition.Instant
	}

	return Transition{}, newError("next typed transition", CodeSearchExhausted)
}

// PreviousTransition finds the nearest availability boundary strictly before
// instant within a positive horizon of at most 366 elapsed days.
func (s Schedule) PreviousTransition(instant time.Time, horizon time.Duration) (Transition, error) {
	if horizon <= 0 || horizon > maxSearchHorizon {
		return Transition{}, newError("previous transition", CodeInvalidHorizon)
	}
	if s.data == nil {
		return Transition{}, newError("previous transition", CodeSearchExhausted)
	}
	oldest := instant.Add(-horizon)
	localized := instant.In(s.data.location)
	date, _ := NewDate(localized.Year(), localized.Month(), localized.Day())
	candidates := make([]Transition, 0, 16)
	for step := 0; step <= 367; step++ {
		day, err := addDate(date, -step)
		if err != nil {
			break
		}
		dayEnd := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, s.data.location).Add(48 * time.Hour)
		if dayEnd.Before(oldest) {
			break
		}
		segments, _, _, err := s.effectiveSegments(day)
		if err != nil {
			return Transition{}, err
		}
		for _, item := range segments {
			opening, openingErr := s.resolveBoundary(day, item.start, true)
			if openingErr == nil {
				candidates = append(candidates, Transition{Instant: opening, Kind: TransitionOpen})
			}
			closing, closingErr := s.resolveBoundary(day, item.end, false)
			if closingErr == nil {
				candidates = append(candidates, Transition{Instant: closing, Kind: TransitionClose})
			}
		}
	}
	slices.SortFunc(candidates, func(left, right Transition) int {
		return right.Instant.Compare(left.Instant)
	})
	for _, transition := range candidates {
		if !transition.Instant.Before(instant) || transition.Instant.Before(oldest) {
			continue
		}
		// Candidate generation already evaluated these civil dates, so point
		// queries at their boundaries cannot introduce a new schedule error.
		before, _ := s.IsOpen(transition.Instant.Add(-time.Nanosecond))
		after, _ := s.IsOpen(transition.Instant)
		if before.Open == after.Open {
			continue
		}
		if after.Open {
			transition.Kind = TransitionOpen
			transition.Explanation = after.Explanation
		} else {
			transition.Kind = TransitionClose
			transition.Explanation = before.Explanation
		}
		return transition, nil
	}

	return Transition{}, newError("previous transition", CodeSearchExhausted)
}

func (s Schedule) resolveBoundary(date Date, nanosecond int64, opening bool) (time.Time, error) {
	if nanosecond == nanosecondsPerDay {
		var err error
		date, err = addDate(date, 1)
		if err != nil {
			return time.Time{}, err
		}
		nanosecond = 0
	}
	localTime := LocalTime{nanosecond: nanosecond}
	policy := PreferLater
	if opening {
		policy = PreferEarlier
	}
	resolved, err := s.ResolveLocal(date, localTime, policy)
	if IsCode(err, CodeNonexistentLocalTime) {
		resolved, err = s.ResolveLocal(date, localTime, ShiftForward)
	}
	if err != nil {
		return time.Time{}, err
	}

	return resolved.Instant, nil
}
