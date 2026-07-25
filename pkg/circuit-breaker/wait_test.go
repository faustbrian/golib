package breaker_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestHalfOpenWaiterAcquiresReleasedCapacity(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0), timerCreated: make(chan struct{}, 1)}
	b := waitingOpenBreaker(t, clock, time.Second)
	clock.Advance(time.Second)

	active, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}

	result := make(chan struct {
		permit *breaker.Permit
		err    error
	}, 1)
	go func() {
		permit, acquireErr := b.Acquire(context.Background())
		result <- struct {
			permit *breaker.Permit
			err    error
		}{permit: permit, err: acquireErr}
	}()

	<-clock.timerCreated
	select {
	case got := <-result:
		t.Fatalf("waiting Acquire() returned early: %+v", got)
	default:
	}
	if err := active.Cancel(); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	select {
	case got := <-result:
		if got.err != nil {
			t.Fatalf("waiting Acquire() error = %v", got.err)
		}
		if got.permit == nil {
			t.Fatal("waiting Acquire() permit = nil")
		}
		_ = got.permit.Cancel()
	case <-time.After(time.Second):
		t.Fatal("waiting Acquire() did not receive released capacity")
	}
}

func TestHalfOpenWaitHonorsCallerCancellation(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	b := waitingOpenBreaker(t, clock, time.Second)
	clock.Advance(time.Second)
	active, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer func() { _ = active.Cancel() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
	if got := b.Snapshot().ActiveHalfOpen; got != 1 {
		t.Fatalf("Snapshot().ActiveHalfOpen = %d, want 1", got)
	}
}

func TestHalfOpenWaitHasFiniteMaximum(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0), timerCreated: make(chan struct{}, 1)}
	b := waitingOpenBreaker(t, clock, time.Minute)
	clock.Advance(time.Second)
	active, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer func() { _ = active.Cancel() }()

	result := make(chan error, 1)
	go func() {
		_, waitErr := b.Acquire(context.Background())
		result <- waitErr
	}()
	<-clock.timerCreated
	select {
	case err := <-result:
		t.Fatalf("waiting Acquire() returned before fake-clock advance: %v", err)
	default:
	}
	clock.Advance(time.Minute)
	select {
	case err := <-result:
		if !errors.Is(err, breaker.ErrHalfOpenWaitTimeout) {
			t.Fatalf("waiting Acquire() error = %v, want ErrHalfOpenWaitTimeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting Acquire() ignored fake-clock timer")
	}
}

type cancelOnNowClock struct {
	now    time.Time
	cancel context.CancelFunc
	once   sync.Once
}

func (c *cancelOnNowClock) Now() time.Time {
	c.once.Do(c.cancel)
	return c.now
}

func (*cancelOnNowClock) NewTimer(time.Duration) breaker.Timer {
	panic("unexpected timer")
}

func TestAcquireDoesNotAdmitCancellationObservedBeforeLinearization(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &cancelOnNowClock{now: time.Unix(100, 0), cancel: cancel}
	b := mustBreaker(t, breaker.Config{Name: "inventory", Clock: clock})

	permit, err := b.Acquire(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
	if permit != nil {
		t.Fatal("Acquire() returned permit after cancellation")
	}
	if got := b.Snapshot().Admitted; got != 0 {
		t.Fatalf("Snapshot().Admitted = %d, want 0", got)
	}
}

func TestHalfOpenWaitDeadlineWinsDelayedTimerDelivery(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0), timerCreated: make(chan struct{}, 1)}
	b := waitingOpenBreaker(t, clock, time.Minute)
	clock.Advance(time.Second)
	active, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}

	result := make(chan struct {
		permit *breaker.Permit
		err    error
	}, 1)
	go func() {
		permit, waitErr := b.Acquire(context.Background())
		result <- struct {
			permit *breaker.Permit
			err    error
		}{permit: permit, err: waitErr}
	}()
	<-clock.timerCreated
	clock.advanceWithoutFiring(time.Minute)
	if err := active.Cancel(); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	got := <-result
	if !errors.Is(got.err, breaker.ErrHalfOpenWaitTimeout) {
		if got.permit != nil {
			_ = got.permit.Cancel()
		}
		t.Fatalf("waiting Acquire() error = %v, want ErrHalfOpenWaitTimeout", got.err)
	}
	if got.permit != nil {
		t.Fatal("waiting Acquire() returned permit after maximum wait")
	}
}

func TestNewRejectsInvalidHalfOpenWait(t *testing.T) {
	t.Parallel()

	_, err := breaker.New(breaker.Config{
		Name:              "inventory",
		HalfOpenAdmission: breaker.WaitForProbe{MaxWait: 0},
	})
	if !errors.Is(err, breaker.ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func waitingOpenBreaker(t *testing.T, clock *fakeClock, maximumWait time.Duration) *breaker.Breaker {
	t.Helper()
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
		HalfOpenAdmission: breaker.WaitForProbe{MaxWait: maximumWait},
	})
	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("initial Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("initial Complete() error = %v", err)
	}
	return b
}
