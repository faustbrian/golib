package breaker_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

type fixedRandom float64

func (r fixedRandom) Float64() float64 { return float64(r) }

func TestOpenDurationAppliesBoundedDeterministicJitter(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:               "inventory",
		Clock:              clock,
		MinimumThroughput:  1,
		Opening:            &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:       breaker.FixedOpenDuration(10 * time.Second),
		OpenDurationJitter: 0.2,
		Random:             fixedRandom(0.5),
	})
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	got := b.Snapshot()
	if got.CurrentOpenDuration != 9*time.Second {
		t.Fatalf("Snapshot().CurrentOpenDuration = %v, want 9s", got.CurrentOpenDuration)
	}
	if !got.NextProbeAt.Equal(clock.Now().Add(9 * time.Second)) {
		t.Fatalf("Snapshot().NextProbeAt = %v", got.NextProbeAt)
	}
}

func TestExponentialOpenDurationCapsAndResetsAfterRecovery(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration: breaker.ExponentialOpenDuration{
			Initial:    time.Second,
			Multiplier: 3,
			Maximum:    5 * time.Second,
		},
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
	})
	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot().CurrentOpenDuration; got != time.Second {
		t.Fatalf("first open duration = %v", got)
	}
	clock.Advance(time.Second)
	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot().CurrentOpenDuration; got != 3*time.Second {
		t.Fatalf("second open duration = %v", got)
	}
	clock.Advance(3 * time.Second)
	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot().CurrentOpenDuration; got != 5*time.Second {
		t.Fatalf("capped open duration = %v", got)
	}
	clock.Advance(5 * time.Second)
	completeOutcome(t, b, breaker.OutcomeSuccess)
	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot().CurrentOpenDuration; got != time.Second {
		t.Fatalf("duration after recovery = %v, want reset initial", got)
	}
}

func TestNewValidatesOpenDurationJitter(t *testing.T) {
	t.Parallel()

	for _, jitter := range []float64{-0.1, 1, math.NaN(), math.Inf(1)} {
		_, err := breaker.New(breaker.Config{
			Name:               "inventory",
			OpenDurationJitter: jitter,
		})
		if !errors.Is(err, breaker.ErrInvalidConfig) {
			t.Fatalf("New() jitter %v error = %v, want ErrInvalidConfig", jitter, err)
		}
	}
}

func TestExponentialMultiplierOneRemainsAtInitialDuration(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration: breaker.ExponentialOpenDuration{
			Initial:    time.Second,
			Multiplier: 1,
			Maximum:    time.Minute,
		},
		HalfOpen: &breaker.HalfOpenPolicy{MaxProbes: 1, RequiredSuccesses: 1},
	})
	completeOutcome(t, b, breaker.OutcomeFailure)
	clock.Advance(time.Second)
	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot().CurrentOpenDuration; got != time.Second {
		t.Fatalf("second open duration = %v, want initial duration", got)
	}
}

func completeOutcome(t *testing.T, b *breaker.Breaker, outcome breaker.Outcome) {
	t.Helper()
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(outcome, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
}
