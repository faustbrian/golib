package breaker_test

import (
	"context"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func TestBreakerTimeWindowExpiresOutcomesBeforeOpeningDecision(t *testing.T) {
	t.Parallel()

	clock := breakertest.NewClock(time.Unix(100, 0))
	b := mustBreaker(t, breaker.Config{
		Name:  "inventory",
		Clock: clock,
		Window: breaker.TimeWindow{
			BucketDuration: time.Second,
			BucketCount:    2,
		},
		MinimumThroughput: 2,
		Opening:           &breaker.OpeningRules{FailureRatio: 1},
	})

	completeOutcome(t, b, breaker.OutcomeFailure)
	clock.Advance(2 * time.Second)
	completeOutcome(t, b, breaker.OutcomeFailure)

	got := b.Snapshot()
	if got.State != breaker.StateClosed || got.WindowSize != 1 || got.Failures != 1 {
		t.Fatalf("Snapshot() after expiry = %+v", got)
	}
}

func TestBreakerTimeWindowOpensAtExactBucketedThreshold(t *testing.T) {
	t.Parallel()

	clock := breakertest.NewClock(time.Unix(100, 0))
	b := mustBreaker(t, breaker.Config{
		Name:  "inventory",
		Clock: clock,
		Window: breaker.TimeWindow{
			BucketDuration: time.Second,
			BucketCount:    3,
		},
		MinimumThroughput: 3,
		Opening:           &breaker.OpeningRules{FailureRatio: 2.0 / 3.0},
	})

	for _, outcome := range []breaker.Outcome{
		breaker.OutcomeFailure,
		breaker.OutcomeSuccess,
		breaker.OutcomeFailure,
	} {
		permit, err := b.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		if err := permit.Complete(outcome, false); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		clock.Advance(time.Second)
	}
	if got := b.Snapshot().State; got != breaker.StateOpen {
		t.Fatalf("Snapshot().State = %v, want open", got)
	}
}
