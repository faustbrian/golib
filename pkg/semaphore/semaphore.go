// Package semaphore provides a process-local FIFO weighted semaphore with
// explicit permit ownership and deterministic shutdown.
package semaphore

import (
	"context"
	"sync"
)

// MaxWaiters is the largest supported bounded FIFO queue.
const MaxWaiters = 1_000_000

// Config defines immutable semaphore capacity and queue bounds.
type Config struct {
	Capacity   int64
	MaxWaiters int
	Observer   Observer
}

// Snapshot is an immutable copy of observable semaphore state.
type Snapshot struct {
	Capacity      int64
	Acquired      int64
	Available     int64
	Waiters       int
	Admissions    uint64
	Rejections    uint64
	Cancellations uint64
	Closed        bool
}

// Semaphore is a process-local weighted counting semaphore.
type Semaphore struct {
	mu            sync.Mutex
	capacity      int64
	acquired      int64
	maxWaiters    int
	admissions    uint64
	rejections    uint64
	cancellations uint64
	nextID        uint64
	closed        bool
	drained       chan struct{}
	waiterCount   int
	waiterHead    *waiter
	waiterTail    *waiter
	observer      Observer
}

type waiter struct {
	weight   int64
	ready    chan struct{}
	permit   *Permit
	err      error
	previous *waiter
	next     *waiter
}

// New constructs a semaphore after validating all configuration.
func New(config Config) (*Semaphore, error) {
	if config.Capacity <= 0 {
		return nil, &ConfigError{field: FieldCapacity, problem: ProblemMustBePositive}
	}
	if config.MaxWaiters < 0 {
		return nil, &ConfigError{field: FieldMaxWaiters, problem: ProblemMustNotBeNegative}
	}
	if config.MaxWaiters > MaxWaiters {
		return nil, &ConfigError{field: FieldMaxWaiters, problem: ProblemExceedsBound}
	}
	return &Semaphore{
		capacity:   config.Capacity,
		maxWaiters: config.MaxWaiters,
		observer:   config.Observer,
	}, nil
}

// Snapshot returns a consistent immutable copy of current state.
func (semaphore *Semaphore) Snapshot() Snapshot {
	semaphore.mu.Lock()
	defer semaphore.mu.Unlock()
	return semaphore.snapshotLocked()
}

func (semaphore *Semaphore) snapshotLocked() Snapshot {
	return Snapshot{
		Capacity:      semaphore.capacity,
		Acquired:      semaphore.acquired,
		Available:     semaphore.capacity - semaphore.acquired,
		Waiters:       semaphore.waiterCount,
		Admissions:    semaphore.admissions,
		Rejections:    semaphore.rejections,
		Cancellations: semaphore.cancellations,
		Closed:        semaphore.closed,
	}
}

// Close idempotently stops new admission and rejects every queued waiter.
// Existing permits remain valid and releasable.
func (semaphore *Semaphore) Close() error {
	semaphore.mu.Lock()
	if semaphore.closed {
		semaphore.mu.Unlock()
		return nil
	}

	semaphore.closed = true
	var events []Event
	for semaphore.waiterHead != nil {
		waiter := semaphore.waiterHead
		semaphore.removeWaiterLocked(waiter)
		waiter.err = &ClosedError{}
		semaphore.rejections++
		events = semaphore.appendEventLocked(events, EventRejected, ReasonClosed, PermitID(0), waiter.weight)
		close(waiter.ready)
	}
	events = semaphore.appendEventLocked(events, EventClosed, ReasonShutdown, PermitID(0), 0)
	semaphore.mu.Unlock()
	semaphore.observe(events...)
	return nil
}

func (semaphore *Semaphore) enqueueWaiterLocked(waiter *waiter) {
	waiter.previous = semaphore.waiterTail
	if semaphore.waiterTail == nil {
		semaphore.waiterHead = waiter
	} else {
		semaphore.waiterTail.next = waiter
	}
	semaphore.waiterTail = waiter
	semaphore.waiterCount++
}

func (semaphore *Semaphore) removeWaiterLocked(waiter *waiter) {
	if waiter.previous == nil {
		semaphore.waiterHead = waiter.next
	} else {
		waiter.previous.next = waiter.next
	}
	if waiter.next == nil {
		semaphore.waiterTail = waiter.previous
	} else {
		waiter.next.previous = waiter.previous
	}
	waiter.previous = nil
	waiter.next = nil
	semaphore.waiterCount--
}

// Wait blocks until all acquired weight is returned or ctx is done. It does
// not close the semaphore or wait for queued callers unless they are admitted.
func (semaphore *Semaphore) Wait(ctx context.Context) error {
	semaphore.mu.Lock()
	if semaphore.acquired == 0 {
		semaphore.mu.Unlock()
		return nil
	}
	if semaphore.drained == nil {
		semaphore.drained = make(chan struct{})
	}
	drained := semaphore.drained
	semaphore.mu.Unlock()

	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return canceledError(ctx.Err())
	}
}

func (semaphore *Semaphore) eventLocked(kind EventKind, reason Reason, id PermitID, weight int64) Event {
	if semaphore.observer == nil {
		return Event{}
	}
	return Event{Kind: kind, Reason: reason, PermitID: id, Weight: weight, Snapshot: semaphore.snapshotLocked()}
}

func (semaphore *Semaphore) appendEventLocked(events []Event, kind EventKind, reason Reason, id PermitID, weight int64) []Event {
	if semaphore.observer == nil {
		return events
	}
	return append(events, semaphore.eventLocked(kind, reason, id, weight))
}

func (semaphore *Semaphore) observe(events ...Event) {
	if semaphore.observer == nil {
		return
	}
	for _, event := range events {
		func() {
			defer func() { _ = recover() }()
			semaphore.observer.Observe(event)
		}()
	}
}
