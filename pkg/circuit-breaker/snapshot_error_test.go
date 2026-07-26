package breaker_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestSnapshotExposesConfiguredBoundsAndRatioDefinedness(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Window:            breaker.CountWindow{Size: 20},
		MinimumThroughput: 5,
	})
	initial := b.Snapshot()
	if initial.WindowCapacity != 20 || initial.MinimumThroughput != 5 || initial.WindowSize != 0 {
		t.Fatalf("initial Snapshot() bounds = %+v", initial)
	}
	if initial.FailureRatioDefined || initial.SlowRatioDefined {
		t.Fatalf("initial Snapshot() ratios unexpectedly defined = %+v", initial)
	}

	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, true); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	got := b.Snapshot()
	if !got.FailureRatioDefined || got.FailureRatio != 1 {
		t.Fatalf("Snapshot() failure ratio = %+v", got)
	}
	if !got.SlowRatioDefined || got.SlowRatio != 1 || got.WindowSize != 1 {
		t.Fatalf("Snapshot() slow ratio/window size = %+v", got)
	}
}

func TestSnapshotExposesHalfOpenRecoveryProgress(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := openBreaker(t, clock, &breaker.HalfOpenPolicy{
		MaxProbes:         2,
		RequiredSuccesses: 2,
	})
	clock.Advance(time.Minute)
	first, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	second, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second Acquire() error = %v", err)
	}
	if err := first.Complete(breaker.OutcomeSuccess, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	got := b.Snapshot()
	if got.HalfOpenCompleted != 1 || got.HalfOpenSuccesses != 1 || got.ActiveHalfOpen != 1 {
		t.Fatalf("Snapshot() half-open progress = %+v", got)
	}
	_ = second.Cancel()
}

func TestInvalidOutcomeIsTypedAndDoesNotConsumePermit(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{Name: "inventory"})
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	err = permit.Complete(breaker.Outcome(99), false)
	if !errors.Is(err, breaker.ErrInvalidOutcome) {
		t.Fatalf("Complete() error = %v, want ErrInvalidOutcome", err)
	}
	var invalid *breaker.InvalidOutcomeError
	if !errors.As(err, &invalid) || invalid.Outcome != breaker.Outcome(99) {
		t.Fatalf("Complete() error = %#v", err)
	}
	if strings.Contains(err.Error(), "result") {
		t.Fatalf("Complete() error contains operation data: %q", err)
	}
	if err := permit.Complete(breaker.OutcomeSuccess, false); err != nil {
		t.Fatalf("valid Complete() after invalid outcome error = %v", err)
	}
}
