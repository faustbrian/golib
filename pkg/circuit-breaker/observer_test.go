package breaker_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestSynchronousObserverReceivesImmutableTransitionsOutsideLock(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	var b *breaker.Breaker
	var mu sync.Mutex
	var events []breaker.TransitionEvent
	observer := func(event breaker.TransitionEvent) error {
		_ = b.Snapshot()
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
		return nil
	}
	b = mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
		Observer:      observer,
		EventDelivery: breaker.SynchronousEvents{},
	})

	complete := make(chan error, 1)
	go func() {
		permit, err := b.Acquire(context.Background())
		if err != nil {
			complete <- err
			return
		}
		complete <- permit.Complete(breaker.OutcomeFailure, false)
	}()
	select {
	case err := <-complete:
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("observer deadlocked while reading Snapshot")
	}

	clock.Advance(time.Second)
	probe, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("probe Acquire() error = %v", err)
	}
	if err := probe.Complete(breaker.OutcomeSuccess, false); err != nil {
		t.Fatalf("probe Complete() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("observer event count = %d, want 3", len(events))
	}
	wantReasons := []breaker.TransitionReason{
		breaker.ReasonPolicyOpened,
		breaker.ReasonOpenIntervalElapsed,
		breaker.ReasonHalfOpenRecovered,
	}
	for index, event := range events {
		if event.Reason != wantReasons[index] {
			t.Fatalf("event[%d].Reason = %v, want %v", index, event.Reason, wantReasons[index])
		}
		if event.After.Generation != event.Before.Generation+1 {
			t.Fatalf("event[%d] generations = %d -> %d", index, event.Before.Generation, event.After.Generation)
		}
		if event.Generation != event.After.Generation || !event.Timestamp.Equal(clock.Now()) && index == 2 {
			t.Fatalf("event[%d] metadata = %+v", index, event)
		}
	}
	if events[0].Before.State != breaker.StateClosed || events[0].After.State != breaker.StateOpen {
		t.Fatalf("opening event = %+v", events[0])
	}
	if events[0].After.Failures != 1 {
		t.Fatalf("opening event failures = %d, want 1", events[0].After.Failures)
	}
}

func TestObserverPanicAndFailureDoNotCorruptBreaker(t *testing.T) {
	t.Parallel()

	for name, observer := range map[string]breaker.Observer{
		"panic": func(breaker.TransitionEvent) error { panic("observer panic") },
		"error": func(breaker.TransitionEvent) error { return errors.New("observer error") },
	} {
		observer := observer
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := mustBreaker(t, breaker.Config{
				Name:              "inventory",
				MinimumThroughput: 1,
				Opening:           &breaker.OpeningRules{FailureCount: 1},
				Observer:          observer,
				EventDelivery:     breaker.SynchronousEvents{},
			})
			permit, err := b.Acquire(context.Background())
			if err != nil {
				t.Fatalf("Acquire() error = %v", err)
			}
			if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
				t.Fatalf("Complete() error = %v", err)
			}
			got := b.Snapshot()
			if got.State != breaker.StateOpen || got.ObserverFailures != 1 {
				t.Fatalf("Snapshot() = %+v", got)
			}
		})
	}
}

func TestRepeatedOpenRejectionDoesNotAmplifyTransitionEvents(t *testing.T) {
	var events atomic.Uint64
	circuit := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		Observer: func(breaker.TransitionEvent) error {
			events.Add(1)
			return nil
		},
		EventDelivery: breaker.SynchronousEvents{},
	})
	permit, err := circuit.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	for range 1_000 {
		if _, err := circuit.Acquire(context.Background()); !errors.Is(err, breaker.ErrOpen) {
			t.Fatalf("Acquire() error = %v, want ErrOpen", err)
		}
	}
	if got := circuit.Snapshot(); events.Load() != 1 || got.Rejected != 1_000 {
		t.Fatalf("events/rejections = %d/%d, want 1/1000", events.Load(), got.Rejected)
	}
}

func TestAsynchronousObserverIsBoundedAndDoesNotBlockAdmission(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		Observer: func(breaker.TransitionEvent) error {
			once.Do(func() { close(started) })
			<-release
			return nil
		},
		EventDelivery: breaker.AsynchronousEvents{
			Buffer:   1,
			Overflow: breaker.DropNewestEvent,
		},
	})

	permit, err := b.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("asynchronous observer did not start")
	}

	if err := b.ForceOpen(); err != nil {
		t.Fatalf("ForceOpen() error = %v", err)
	}
	if err := b.Isolate(); err != nil {
		t.Fatalf("Isolate() error = %v", err)
	}
	if _, err := b.Acquire(context.Background()); !errors.Is(err, breaker.ErrIsolated) {
		t.Fatalf("Acquire() error = %v, want ErrIsolated", err)
	}
	if got := b.Snapshot().DroppedEvents; got == 0 {
		t.Fatal("Snapshot().DroppedEvents = 0, want bounded overflow")
	}

	close(release)
	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestAsynchronousObserverCanRequestCloseReentrantly(t *testing.T) {
	closeResult := make(chan error, 1)
	var circuit *breaker.Breaker
	observer := func(breaker.TransitionEvent) error {
		closeResult <- circuit.Close()
		return nil
	}
	var err error
	circuit, err = breaker.New(breaker.Config{
		Name:              "inventory",
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		Observer:          observer,
		EventDelivery: breaker.AsynchronousEvents{
			Buffer:   1,
			Overflow: breaker.DropNewestEvent,
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	permit, err := circuit.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("observer Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("asynchronous observer deadlocked while requesting Close")
	}
	if err := circuit.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestShutdownHonorsContextWhileObserverIsBlocked(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	circuit, err := breaker.New(breaker.Config{
		Name:              "inventory",
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		Observer: func(breaker.TransitionEvent) error {
			close(started)
			<-release
			return nil
		},
		EventDelivery: breaker.AsynchronousEvents{Buffer: 1},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	permit, err := circuit.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := permit.Complete(breaker.OutcomeFailure, false); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := circuit.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Shutdown() error = %v, want context.Canceled", err)
	}
	close(release)
	if err := circuit.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestShutdownWithoutAsynchronousObserverReturnsImmediately(t *testing.T) {
	t.Parallel()

	circuit, err := breaker.New(breaker.Config{Name: "inventory"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := circuit.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestNewValidatesEventDelivery(t *testing.T) {
	t.Parallel()

	tests := []breaker.Config{
		{
			Name:     "inventory",
			Observer: func(breaker.TransitionEvent) error { return nil },
			EventDelivery: breaker.AsynchronousEvents{
				Buffer: 0,
			},
		},
		{
			Name:     "inventory",
			Observer: func(breaker.TransitionEvent) error { return nil },
			EventDelivery: breaker.AsynchronousEvents{
				Buffer:   1,
				Overflow: breaker.EventOverflowPolicy(99),
			},
		},
	}

	for _, config := range tests {
		if _, err := breaker.New(config); !errors.Is(err, breaker.ErrInvalidConfig) {
			t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
		}
	}
}
