package breaker_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

type fakeClock struct {
	mu           sync.Mutex
	now          time.Time
	timers       []*fakeTimer
	timerCreated chan struct{}
}

type fakeTimer struct {
	clock    *fakeClock
	deadline time.Time
	channel  chan time.Time
	stopped  bool
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	for _, timer := range c.timers {
		if !timer.stopped && !c.now.Before(timer.deadline) {
			timer.stopped = true
			timer.channel <- c.now
		}
	}
	c.mu.Unlock()
}

func (c *fakeClock) advanceWithoutFiring(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func (c *fakeClock) NewTimer(duration time.Duration) breaker.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &fakeTimer{
		clock:    c,
		deadline: c.now.Add(duration),
		channel:  make(chan time.Time, 1),
	}
	c.timers = append(c.timers, timer)
	if c.timerCreated != nil {
		select {
		case c.timerCreated <- struct{}{}:
		default:
		}
	}
	return timer
}

func (t *fakeTimer) C() <-chan time.Time { return t.channel }

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

func TestConsecutiveFailuresOpenAndReject(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 2,
		Opening: &breaker.OpeningRules{
			ConsecutiveFailures: 2,
		},
		OpenDuration: breaker.FixedOpenDuration(time.Minute),
	})

	for range 2 {
		permit, err := b.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
	}

	if got := b.Snapshot().State; got != breaker.StateOpen {
		t.Fatalf("Snapshot().State = %v, want open", got)
	}
	_, err := b.Acquire(context.Background())
	if !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("Acquire() error = %v, want ErrOpen", err)
	}
	var rejection *breaker.RejectionError
	if !errors.As(err, &rejection) {
		t.Fatalf("Acquire() error type = %T, want *RejectionError", err)
	}
	if rejection.Name != "inventory" || rejection.State != breaker.StateOpen {
		t.Fatalf("RejectionError = %+v", rejection)
	}
	if rejection.Generation != b.Snapshot().Generation {
		t.Fatalf("RejectionError.Generation = %d, want %d", rejection.Generation, b.Snapshot().Generation)
	}
	if !rejection.RetryAt.Equal(clock.Now().Add(time.Minute)) {
		t.Fatalf("RejectionError.RetryAt = %v", rejection.RetryAt)
	}
}

func TestOpenEligibilityTransitionsOnceAndBoundsHalfOpenProbes(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := openBreaker(t, clock, &breaker.HalfOpenPolicy{
		MaxProbes:         2,
		RequiredSuccesses: 2,
		FailureAction:     breaker.ReopenImmediately,
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
	if _, err := b.Acquire(context.Background()); !errors.Is(err, breaker.ErrHalfOpenExhausted) {
		t.Fatalf("third Acquire() error = %v, want ErrHalfOpenExhausted", err)
	}

	snapshot := b.Snapshot()
	if snapshot.State != breaker.StateHalfOpen || snapshot.ActiveHalfOpen != 2 {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
	if err := first.Complete(breaker.OutcomeSuccess, false); err != nil {
		t.Fatalf("first Complete() error = %v", err)
	}
	if _, err := b.Acquire(context.Background()); !errors.Is(err, breaker.ErrHalfOpenExhausted) {
		t.Fatalf("replacement Acquire() error = %v, want sample exhaustion", err)
	}
	if err := second.Complete(breaker.OutcomeSuccess, false); err != nil {
		t.Fatalf("second Complete() error = %v", err)
	}
	if got := b.Snapshot(); got.State != breaker.StateClosed || got.WindowClassified != 0 {
		t.Fatalf("Snapshot() after recovery = %+v", got)
	}
}

func TestIgnoredHalfOpenProbeCanBeReplaced(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := openBreaker(t, clock, &breaker.HalfOpenPolicy{
		MaxProbes:         1,
		RequiredSuccesses: 1,
	})
	clock.Advance(time.Minute)

	ignored, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := ignored.Complete(breaker.OutcomeIgnored, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if _, err := b.Acquire(context.Background()); err != nil {
		t.Fatalf("replacement Acquire() error = %v", err)
	}
}

func TestIgnoredOutcomeConsecutiveFailureBehavior(t *testing.T) {
	t.Parallel()

	for name, behavior := range map[string]breaker.IgnoredConsecutiveBehavior{
		"preserve": breaker.PreserveConsecutiveFailures,
		"reset":    breaker.ResetConsecutiveFailures,
	} {
		behavior := behavior
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := mustBreaker(t, breaker.Config{
				Name:              "inventory",
				MinimumThroughput: 2,
				Opening: &breaker.OpeningRules{
					ConsecutiveFailures: 2,
					IgnoredBehavior:     behavior,
				},
			})
			for _, outcome := range []breaker.Outcome{
				breaker.OutcomeFailure,
				breaker.OutcomeIgnored,
				breaker.OutcomeFailure,
			} {
				permit, err := b.Acquire(context.Background())
				if err != nil {
					t.Fatalf("Acquire() error = %v", err)
				}
				if err := permit.Complete(outcome, false); err != nil {
					t.Fatalf("Complete() error = %v", err)
				}
			}
			want := breaker.StateOpen
			if behavior == breaker.ResetConsecutiveFailures {
				want = breaker.StateClosed
			}
			if got := b.Snapshot().State; got != want {
				t.Fatalf("Snapshot().State = %v, want %v", got, want)
			}
		})
	}
}

func TestStaleCompletionCannotMutateNewGeneration(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Minute),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
	})

	stale, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("stale Acquire() error = %v", err)
	}
	opener, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("opener Acquire() error = %v", err)
	}
	if err := opener.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("opener Complete() error = %v", err)
	}
	openGeneration := b.Snapshot().Generation

	if err := stale.Complete(breaker.OutcomeSuccess, false); err != nil {
		t.Fatalf("stale Complete() error = %v", err)
	}
	if got := b.Snapshot(); got.State != breaker.StateOpen || got.Generation != openGeneration ||
		got.Completed != 2 || got.TotalFailures != 1 || got.TotalSuccesses != 1 {
		t.Fatalf("Snapshot() after stale completion = %+v", got)
	}
}

func TestPermitCompletionIsExactlyOnce(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{Name: "inventory"})
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeSuccess, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); !errors.Is(err, breaker.ErrPermitCompleted) {
		t.Fatalf("duplicate Complete() error = %v, want ErrPermitCompleted", err)
	}
	if got := b.Snapshot(); got.Successes != 1 || got.Failures != 0 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func openBreaker(t *testing.T, clock *fakeClock, halfOpen *breaker.HalfOpenPolicy) *breaker.Breaker {
	t.Helper()
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Minute),
		HalfOpen:          halfOpen,
	})
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	return b
}

func mustBreaker(t *testing.T, config breaker.Config) *breaker.Breaker {
	t.Helper()
	b, err := breaker.New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return b
}
