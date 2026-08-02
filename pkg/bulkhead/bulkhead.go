// Package bulkhead provides process-local fixed-capacity resource isolation.
package bulkhead

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
)

// Bulkhead owns admission policy and lifecycle for one caller-named resource.
// Its configuration is immutable after New returns.
type Bulkhead struct {
	config    normalizedConfig
	semaphore *semaphore.Weighted

	mu              sync.Mutex
	active          int64
	queued          int
	admissions      uint64
	rejections      uint64
	cancellations   uint64
	executions      uint64
	totalWait       time.Duration
	totalExecution  time.Duration
	rejectionCounts map[RejectionReason]uint64
	draining        bool
	drained         chan struct{}
	waiters         list.List
}

// New validates config and constructs one independent resource partition.
func New(config Config) (*Bulkhead, error) {
	normalized, err := normalize(config)
	if err != nil {
		return nil, err
	}
	return &Bulkhead{
		config:          normalized,
		semaphore:       semaphore.NewWeighted(normalized.capacity),
		rejectionCounts: make(map[RejectionReason]uint64),
		drained:         make(chan struct{}),
	}, nil
}

// Acquire admits weight or returns a typed terminal admission error. A
// successful permit remains valid after ctx is canceled and must be released.
func (bulkhead *Bulkhead) Acquire(ctx context.Context, weight int64) (*Permit, error) {
	if ctx == nil {
		return nil, bulkhead.reject(weight, RejectionCaller, ErrCallerCanceled)
	}
	if weight <= 0 || weight > bulkhead.config.capacity {
		return nil, bulkhead.reject(weight, RejectionWeight, ErrInvalidWeight)
	}
	if err := ctx.Err(); err != nil {
		return nil, bulkhead.reject(weight, RejectionCaller, errors.Join(ErrCallerCanceled, err))
	}
	if scope, _ := ctx.Value(executionContextKey{}).(*executionScope); scope != nil && scope.contains(bulkhead) {
		return nil, bulkhead.reject(weight, RejectionReentry, ErrReentrant)
	}

	start := bulkhead.config.clock.Now()
	bulkhead.mu.Lock()
	if bulkhead.draining {
		bulkhead.mu.Unlock()
		return nil, bulkhead.reject(weight, RejectionShutdown, ErrClosed)
	}
	if bulkhead.queued == 0 && bulkhead.semaphore.TryAcquire(weight) {
		bulkhead.active += weight
		bulkhead.admissions++
		event := bulkhead.eventLocked(EventAdmitted, "", weight, 0, 0)
		bulkhead.mu.Unlock()
		bulkhead.observe(event)
		return &Permit{bulkhead: bulkhead, weight: weight}, nil
	}
	if !bulkhead.config.wait {
		bulkhead.mu.Unlock()
		return nil, bulkhead.reject(weight, RejectionCapacity, ErrRejected)
	}
	if bulkhead.queued >= bulkhead.config.maxQueued {
		bulkhead.mu.Unlock()
		return nil, bulkhead.reject(weight, RejectionQueue, ErrQueueFull)
	}
	bulkhead.queued++
	waiter := &waiter{
		weight:   weight,
		deadline: start.Add(bulkhead.config.maxWait),
		ready:    make(chan struct{}),
	}
	waiter.element = bulkhead.waiters.PushBack(waiter)
	queuedEvent := bulkhead.eventLocked(EventQueued, "", weight, 0, 0)
	bulkhead.mu.Unlock()
	bulkhead.observe(queuedEvent)

	return bulkhead.wait(ctx, waiter, start)
}

func (bulkhead *Bulkhead) reject(weight int64, reason RejectionReason, err error) error {
	bulkhead.mu.Lock()
	bulkhead.rejections++
	bulkhead.rejectionCounts[reason]++
	if reason == RejectionCaller {
		bulkhead.cancellations++
	}
	event := bulkhead.eventLocked(terminalEventKind(reason), reason, weight, 0, 0)
	bulkhead.mu.Unlock()
	bulkhead.observe(event)
	return err
}

func (bulkhead *Bulkhead) failedWaitLocked(
	weight int64,
	reason RejectionReason,
	err error,
	waited time.Duration,
) (Event, bool, error) {
	bulkhead.rejections++
	bulkhead.rejectionCounts[reason]++
	bulkhead.totalWait += waited
	if reason == RejectionCaller {
		bulkhead.cancellations++
	}
	event := bulkhead.eventLocked(terminalEventKind(reason), reason, weight, waited, 0)
	drained := bulkhead.maybeDrainedLocked()
	return event, drained, err
}

func terminalEventKind(reason RejectionReason) EventKind {
	switch reason {
	case RejectionCaller:
		return EventCanceled
	default:
		return EventRejected
	}
}

// Snapshot returns a defensive immutable copy of current state and counters.
func (bulkhead *Bulkhead) Snapshot() Snapshot {
	bulkhead.mu.Lock()
	defer bulkhead.mu.Unlock()
	counts := make(map[RejectionReason]uint64, len(bulkhead.rejectionCounts))
	for reason, count := range bulkhead.rejectionCounts {
		counts[reason] = count
	}
	return Snapshot{
		Resource:               bulkhead.config.resource,
		PolicyRevision:         bulkhead.config.policyRevision,
		Capacity:               bulkhead.config.capacity,
		ActiveWeight:           bulkhead.active,
		AvailableWeight:        bulkhead.config.capacity - bulkhead.active,
		QueueDepth:             bulkhead.queued,
		Admissions:             bulkhead.admissions,
		Rejections:             bulkhead.rejections,
		Cancellations:          bulkhead.cancellations,
		Executions:             bulkhead.executions,
		TotalWaitDuration:      bulkhead.totalWait,
		TotalExecutionDuration: bulkhead.totalExecution,
		RejectionCounts:        counts,
		Draining:               bulkhead.draining,
		Drained:                bulkhead.draining && bulkhead.active == 0 && bulkhead.queued == 0,
	}
}

func (bulkhead *Bulkhead) eventLocked(kind EventKind, reason RejectionReason, weight int64, wait, execution time.Duration) Event {
	return Event{Resource: bulkhead.config.resource, PolicyRevision: bulkhead.config.policyRevision,
		Kind: kind, Reason: reason, Weight: weight, QueueDepth: bulkhead.queued,
		ActiveWeight: bulkhead.active, WaitDuration: wait, ExecutionTime: execution}
}

func (bulkhead *Bulkhead) observe(event Event) {
	if bulkhead.config.observer == nil {
		return
	}
	event.At = bulkhead.now()
	defer func() { _ = recover() }()
	_ = bulkhead.config.observer.Observe(event)
}

func (bulkhead *Bulkhead) now() (now time.Time) {
	defer func() { _ = recover() }()
	return bulkhead.config.clock.Now()
}

// Permit owns admitted capacity until its first successful Release.
type Permit struct {
	bulkhead     *Bulkhead
	weight       int64
	waitDuration time.Duration
	released     atomic.Bool
}

// Weight reports the acquired weight.
func (permit *Permit) Weight() int64 { return permit.weight }

// Resource reports the stable resource identity associated with the permit.
func (permit *Permit) Resource() string { return permit.bulkhead.config.resource }

// Release returns capacity exactly once and reports duplicate calls.
func (permit *Permit) Release() error {
	if !permit.released.CompareAndSwap(false, true) {
		return ErrPermitReleased
	}
	bulkhead := permit.bulkhead
	now := bulkhead.now()
	bulkhead.mu.Lock()
	bulkhead.semaphore.Release(permit.weight)
	bulkhead.active -= permit.weight
	bulkhead.grantWaitersLocked(now)
	event := bulkhead.eventLocked(EventReleased, "", permit.weight, 0, 0)
	drained := bulkhead.maybeDrainedLocked()
	bulkhead.mu.Unlock()
	bulkhead.observe(event)
	bulkhead.observeDrained(drained)
	return nil
}

func (bulkhead *Bulkhead) maybeDrainedLocked() bool {
	if !bulkhead.draining || bulkhead.active != 0 || bulkhead.queued != 0 {
		return false
	}
	select {
	case <-bulkhead.drained:
		return false
	default:
		close(bulkhead.drained)
		return true
	}
}

func (bulkhead *Bulkhead) observeDrained(drained bool) {
	if drained {
		bulkhead.observe(Event{Resource: bulkhead.config.resource,
			PolicyRevision: bulkhead.config.policyRevision, Kind: EventDrained})
	}
}
