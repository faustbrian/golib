package breaker

import (
	"context"
	"sync/atomic"
	"time"
)

// TransitionReason identifies why a policy or administrative transition ran.
type TransitionReason uint8

const (
	ReasonPolicyOpened TransitionReason = iota
	ReasonOpenIntervalElapsed
	ReasonHalfOpenRecovered
	ReasonHalfOpenFailed
	ReasonForceOpen
	ReasonDisabled
	ReasonIsolated
	ReasonReleased
	ReasonReset
)

func (r TransitionReason) String() string {
	switch r {
	case ReasonPolicyOpened:
		return "policy-opened"
	case ReasonOpenIntervalElapsed:
		return "open-interval-elapsed"
	case ReasonHalfOpenRecovered:
		return "half-open-recovered"
	case ReasonHalfOpenFailed:
		return "half-open-failed"
	case ReasonForceOpen:
		return "force-open"
	case ReasonDisabled:
		return "disabled"
	case ReasonIsolated:
		return "isolated"
	case ReasonReleased:
		return "released"
	case ReasonReset:
		return "reset"
	default:
		return "unknown"
	}
}

// TransitionEvent contains immutable state from both sides of a transition.
type TransitionEvent struct {
	Before     Snapshot
	After      Snapshot
	Reason     TransitionReason
	Generation uint64
	Timestamp  time.Time
}

// Observer receives state and administrative transition events.
type Observer func(TransitionEvent) error

// EventDeliveryPolicy controls how observer callbacks receive events.
type EventDeliveryPolicy interface{ eventDeliveryPolicy() }

// SynchronousEvents invokes the observer in the transitioning caller after the
// internal lock is released.
type SynchronousEvents struct{}

func (SynchronousEvents) eventDeliveryPolicy() {}

// EventOverflowPolicy controls a full asynchronous event queue.
type EventOverflowPolicy uint8

const (
	DropNewestEvent EventOverflowPolicy = iota
	DropOldestEvent
)

// AsynchronousEvents delivers events through one bounded worker queue.
type AsynchronousEvents struct {
	Buffer   int
	Overflow EventOverflowPolicy
}

func (AsynchronousEvents) eventDeliveryPolicy() {}

type observerRuntime struct {
	observer Observer
	sync     bool
	buffer   int
	overflow EventOverflowPolicy
}

type observerCounters struct {
	failures atomic.Uint64
	dropped  atomic.Uint64
}

func (b *Breaker) startObserver() {
	policy := b.config.observer
	if policy.observer == nil || policy.sync {
		return
	}
	b.eventChannel = make(chan TransitionEvent, policy.buffer)
	b.eventStop = make(chan struct{})
	b.eventDone = make(chan struct{})
	go b.runObserver()
}

func (b *Breaker) runObserver() {
	defer close(b.eventDone)
	for {
		select {
		case event := <-b.eventChannel:
			b.observe(event)
		case <-b.eventStop:
			for {
				select {
				case event := <-b.eventChannel:
					b.observe(event)
				default:
					return
				}
			}
		}
	}
}

func (b *Breaker) observe(event TransitionEvent) {
	failed := false
	func() {
		defer func() {
			if recover() != nil {
				failed = true
			}
		}()
		if err := b.config.observer.observer(event); err != nil {
			failed = true
		}
	}()
	if failed {
		b.observerCounters.failures.Add(1)
	}
}

func (b *Breaker) dispatch(events []TransitionEvent) {
	for _, event := range events {
		if b.config.observer.sync {
			b.observe(event)
			continue
		}
		b.enqueue(event)
	}
}

func (b *Breaker) enqueue(event TransitionEvent) {
	b.eventMu.Lock()
	defer b.eventMu.Unlock()
	if b.eventClosed.Load() {
		b.observerCounters.dropped.Add(1)
		return
	}
	select {
	case b.eventChannel <- event:
		return
	default:
	}
	if b.config.observer.overflow == DropNewestEvent {
		b.observerCounters.dropped.Add(1)
		return
	}
	select {
	case <-b.eventChannel:
		b.observerCounters.dropped.Add(1)
	default:
	}
	b.eventChannel <- event
}

func (b *Breaker) takeEventsLocked() []TransitionEvent {
	events := b.pendingEvents
	b.pendingEvents = nil
	return events
}

func (b *Breaker) unlockAndDispatch() {
	events := b.takeEventsLocked()
	b.mu.Unlock()
	b.dispatch(events)
}

// Close requests asynchronous observer shutdown without waiting for callbacks.
// It is idempotent, callback-safe, and does not change breaker admission or state.
func (b *Breaker) Close() error {
	if b.eventStop == nil {
		return nil
	}
	b.eventCloseOnce.Do(func() {
		b.eventMu.Lock()
		b.eventClosed.Store(true)
		close(b.eventStop)
		b.eventMu.Unlock()
	})
	return nil
}

// Shutdown requests asynchronous observer shutdown and waits for queued
// callbacks to finish. It must not be called from the asynchronous observer.
func (b *Breaker) Shutdown(ctx context.Context) error {
	_ = b.Close()
	if b.eventDone == nil {
		return nil
	}
	select {
	case <-b.eventDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
