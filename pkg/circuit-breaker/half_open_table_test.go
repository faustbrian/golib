package breaker_test

import (
	"context"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func TestHalfOpenRecoveryTruthTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		policy   breaker.HalfOpenPolicy
		outcomes []breaker.Outcome
		want     breaker.State
	}{
		{
			name: "required successes recover at sample boundary",
			policy: breaker.HalfOpenPolicy{
				MaxProbes:         3,
				RequiredSuccesses: 2,
				FailureAction:     breaker.ReopenAfterSample,
			},
			outcomes: []breaker.Outcome{
				breaker.OutcomeSuccess,
				breaker.OutcomeFailure,
				breaker.OutcomeSuccess,
			},
			want: breaker.StateClosed,
		},
		{
			name: "required successes miss reopens at sample boundary",
			policy: breaker.HalfOpenPolicy{
				MaxProbes:         3,
				RequiredSuccesses: 2,
				FailureAction:     breaker.ReopenAfterSample,
			},
			outcomes: []breaker.Outcome{
				breaker.OutcomeSuccess,
				breaker.OutcomeFailure,
				breaker.OutcomeFailure,
			},
			want: breaker.StateOpen,
		},
		{
			name: "success ratio exact boundary recovers",
			policy: breaker.HalfOpenPolicy{
				MaxProbes:     3,
				SuccessRatio:  2.0 / 3.0,
				FailureAction: breaker.ReopenAfterSample,
			},
			outcomes: []breaker.Outcome{
				breaker.OutcomeSuccess,
				breaker.OutcomeFailure,
				breaker.OutcomeSuccess,
			},
			want: breaker.StateClosed,
		},
		{
			name: "success ratio miss reopens",
			policy: breaker.HalfOpenPolicy{
				MaxProbes:     3,
				SuccessRatio:  2.0 / 3.0,
				FailureAction: breaker.ReopenAfterSample,
			},
			outcomes: []breaker.Outcome{
				breaker.OutcomeSuccess,
				breaker.OutcomeFailure,
				breaker.OutcomeFailure,
			},
			want: breaker.StateOpen,
		},
		{
			name: "immediate failure ignores remaining sample",
			policy: breaker.HalfOpenPolicy{
				MaxProbes:         3,
				RequiredSuccesses: 3,
				FailureAction:     breaker.ReopenImmediately,
			},
			outcomes: []breaker.Outcome{breaker.OutcomeFailure},
			want:     breaker.StateOpen,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := breakertest.NewClock(time.Unix(100, 0))
			b := mustBreaker(t, breaker.Config{
				Name:              "inventory",
				Clock:             clock,
				MinimumThroughput: 1,
				Opening:           &breaker.OpeningRules{FailureCount: 1},
				OpenDuration:      breaker.FixedOpenDuration(time.Second),
				HalfOpen:          &test.policy,
			})
			completeOutcome(t, b, breaker.OutcomeFailure)
			clock.Advance(time.Second)
			for _, outcome := range test.outcomes {
				permit, err := b.Acquire(context.Background())
				if err != nil {
					t.Fatalf("Acquire() error = %v", err)
				}
				if err := permit.Complete(outcome, false); err != nil {
					t.Fatalf("Complete() error = %v", err)
				}
			}
			if got := b.Snapshot().State; got != test.want {
				t.Fatalf("Snapshot().State = %v, want %v", got, test.want)
			}
		})
	}
}
