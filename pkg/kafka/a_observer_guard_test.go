package kafka

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestObserverCriticalGuardsTerminateDeterministically(t *testing.T) {
	startedAt := time.Unix(1, 0)
	for name, observation := range map[string]Observation{
		"first kind": {
			Kind:        ObservationProduceRecord,
			StartedAt:   startedAt,
			RecordCount: 1,
			Succeeded:   true,
		},
		"last kind": {
			Kind:           ObservationConsumeRetryScheduled,
			StartedAt:      startedAt,
			Topic:          "events",
			Partition:      1,
			PartitionKnown: true,
			Offset:         4,
			OffsetKnown:    true,
			RecordCount:    1,
			PartitionCount: 1,
			RecordBytes:    64,
			Category:       ErrorRetryable,
		},
	} {
		if err := observation.Validate(); err != nil {
			t.Fatalf("%s observation error = %v", name, err)
		}
	}
	for name, kind := range map[string]ObservationKind{
		"below first kind": ObservationProduceRecord - 1,
		"above last kind":  ObservationConsumeRetryScheduled + 1,
	} {
		observation := Observation{
			Kind: kind, StartedAt: startedAt, Succeeded: true,
		}
		if err := observation.Validate(); !errors.Is(err, ErrInvalidObservation) {
			t.Fatalf("%s observation error = %v", name, err)
		}
	}

	observers := make([]ObserverFunc, 16)
	for index := range observers {
		observers[index] = func(context.Context, Observation) error {
			return nil
		}
	}
	base := ObserverPolicy{
		Observers: observers,
		FailureHandler: func(context.Context, ObservationFailure) {
		},
	}
	for name, timeout := range map[string]time.Duration{
		"minimum timeout": time.Millisecond,
		"maximum timeout": 5 * time.Second,
	} {
		policy := base
		policy.Timeout = timeout
		if _, err := normalizeObserverPolicy(policy); err != nil {
			t.Fatalf("%s policy error = %v", name, err)
		}
	}
	for name, mutate := range map[string]func(*ObserverPolicy){
		"too many observers": func(policy *ObserverPolicy) {
			policy.Observers = append(policy.Observers, policy.Observers[0])
		},
		"timeout below minimum": func(policy *ObserverPolicy) {
			policy.Timeout = time.Millisecond - time.Nanosecond
		},
		"timeout above maximum": func(policy *ObserverPolicy) {
			policy.Timeout = 5*time.Second + time.Nanosecond
		},
	} {
		policy := base
		mutate(&policy)
		if _, err := normalizeObserverPolicy(policy); !errors.Is(
			err,
			ErrInvalidObserverPolicy,
		) {
			t.Fatalf("%s policy error = %v", name, err)
		}
	}
}
