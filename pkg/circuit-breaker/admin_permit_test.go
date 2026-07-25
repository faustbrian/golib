package breaker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestPermitCancellationReleasesHalfOpenCapacity(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := openBreaker(t, clock, &breaker.HalfOpenPolicy{
		MaxProbes:         1,
		RequiredSuccesses: 1,
	})
	clock.Advance(time.Minute)
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Cancel(); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if got := b.Snapshot().ActiveHalfOpen; got != 0 {
		t.Fatalf("Snapshot().ActiveHalfOpen = %d, want 0", got)
	}
	if _, err := b.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire() after cancellation error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeSuccess, false); !errors.Is(err, breaker.ErrPermitCanceled) {
		t.Fatalf("Complete() after cancellation error = %v", err)
	}
}

func TestAbandonedPermitExpiresAndReleasesHalfOpenCapacity(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		PermitTTL:         time.Second,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
	})
	opener, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	_ = opener.Complete(breaker.OutcomeFailure, false)
	clock.Advance(time.Second)
	abandoned, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("half-open Acquire() error = %v", err)
	}
	clock.Advance(time.Second)

	if _, err := b.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire() after expiry error = %v", err)
	}
	if err := abandoned.Complete(breaker.OutcomeSuccess, false); !errors.Is(err, breaker.ErrPermitExpired) {
		t.Fatalf("expired Complete() error = %v, want ErrPermitExpired", err)
	}
}

func TestAdministrativeModesAreExplicitAndReversible(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{Name: "inventory"})

	if err := b.ForceOpen(); err != nil {
		t.Fatalf("ForceOpen() error = %v", err)
	}
	if _, err := b.Acquire(context.Background()); !errors.Is(err, breaker.ErrForceOpen) {
		t.Fatalf("Acquire() force-open error = %v", err)
	}
	if got := b.Snapshot(); got.Mode != breaker.ModeForceOpen || got.State != breaker.StateClosed {
		t.Fatalf("Snapshot() force-open = %+v", got)
	}

	if err := b.Isolate(); err != nil {
		t.Fatalf("Isolate() error = %v", err)
	}
	if _, err := b.Acquire(context.Background()); !errors.Is(err, breaker.ErrIsolated) {
		t.Fatalf("Acquire() isolated error = %v", err)
	}

	if err := b.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() disabled error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, true); err != nil {
		t.Fatalf("Complete() disabled error = %v", err)
	}
	if got := b.Snapshot(); got.WindowClassified != 0 || got.Ignored != 0 ||
		got.Completed != 1 || got.TotalFailures != 1 {
		t.Fatalf("Snapshot() disabled recorded outcome = %+v", got)
	}

	if err := b.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := b.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
}

func TestResetReturnsToFreshClosedGeneration(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
	})
	permit, _ := b.Acquire(context.Background())
	_ = permit.Complete(breaker.OutcomeFailure, false)
	before := b.Snapshot().Generation

	if err := b.Reset(); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	got := b.Snapshot()
	if got.State != breaker.StateClosed || got.Mode != breaker.ModeNormal {
		t.Fatalf("Snapshot() after reset = %+v", got)
	}
	if got.Generation <= before || got.WindowClassified != 0 {
		t.Fatalf("Snapshot() after reset generation/window = %+v", got)
	}
}

func TestAdministrativeGenerationStartsFreshHalfOpenSample(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         3,
			RequiredSuccesses: 2,
			FailureAction:     breaker.ReopenAfterSample,
		},
	})
	completeOutcome(t, b, breaker.OutcomeFailure)
	clock.Advance(time.Second)
	completeOutcome(t, b, breaker.OutcomeSuccess)
	if got := b.Snapshot(); got.HalfOpenCompleted != 1 || got.HalfOpenSuccesses != 1 {
		t.Fatalf("half-open progress before administrative change = %+v", got)
	}

	if err := b.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if err := b.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if got := b.Snapshot(); got.HalfOpenCompleted != 0 || got.HalfOpenSuccesses != 0 {
		t.Fatalf("half-open progress after administrative generation = %+v", got)
	}

	completeOutcome(t, b, breaker.OutcomeSuccess)
	if got := b.Snapshot().State; got != breaker.StateHalfOpen {
		t.Fatalf("state after one new-generation success = %s, want half-open", got)
	}
}

func TestNewRejectsNegativePermitTTL(t *testing.T) {
	t.Parallel()

	_, err := breaker.New(breaker.Config{Name: "inventory", PermitTTL: -time.Second})
	if !errors.Is(err, breaker.ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}
