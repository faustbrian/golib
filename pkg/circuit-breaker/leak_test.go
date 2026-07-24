package breaker_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

func TestCanceledHalfOpenWaitReleasesClockTimer(t *testing.T) {
	clock := breakertest.NewClock(time.Unix(100, 0))
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
		HalfOpenAdmission: breaker.WaitForProbe{MaxWait: time.Hour},
	})
	completeOutcome(t, b, breaker.OutcomeFailure)
	clock.Advance(time.Second)
	active, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	defer func() { _ = active.Cancel() }()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, waitErr := b.Acquire(ctx)
		result <- waitErr
	}()
	eventually(t, func() bool { return clock.ActiveTimers() == 1 })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
	eventually(t, func() bool { return clock.ActiveTimers() == 0 })
}

func TestCloseDrainsObserverQueueAndDropsLaterEvents(t *testing.T) {
	recorder, err := breakertest.NewRecorder(16)
	if err != nil {
		t.Fatalf("NewRecorder() error = %v", err)
	}
	b := mustBreaker(t, breaker.Config{
		Name:     "inventory",
		Observer: recorder.Observe,
		EventDelivery: breaker.AsynchronousEvents{
			Buffer:   16,
			Overflow: breaker.DropNewestEvent,
		},
	})
	for range 5 {
		_ = b.ForceOpen()
		_ = b.Release()
	}
	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	events := recorder.Events()
	if len(events) != 10 || recorder.Dropped() != 0 {
		t.Fatalf("events/dropped after Shutdown() = %d/%d, want 10/0", len(events), recorder.Dropped())
	}
	for index, event := range events {
		wantReason := breaker.ReasonForceOpen
		if index%2 == 1 {
			wantReason = breaker.ReasonReleased
		}
		if event.Reason != wantReason || event.Generation != uint64(index+2) {
			t.Fatalf("event[%d] = %+v, want reason %v generation %d", index, event, wantReason, index+2)
		}
	}
	if err := b.ForceOpen(); err != nil {
		t.Fatalf("ForceOpen() after Close() error = %v", err)
	}
	if got := b.Snapshot().DroppedEvents; got != 1 {
		t.Fatalf("Snapshot().DroppedEvents = %d, want 1", got)
	}
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not satisfied before deadline")
		}
		runtime.Gosched()
	}
}
