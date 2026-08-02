package concurrencylimit

import (
	"sync/atomic"
	"time"
)

// Permit owns exactly one admitted execution and must receive one terminal
// outcome. Complete is safe for concurrent duplicate calls.
type Permit struct {
	limiter    *Limiter
	id         uint64
	generation uint64
	metadata   Metadata
	started    time.Time
	completed  atomic.Bool
}

// Metadata returns the bounded value copied at admission.
func (permit *Permit) Metadata() Metadata { return permit.metadata }

// Complete records the first valid terminal outcome and releases capacity.
func (permit *Permit) Complete(outcome Outcome) error {
	if !outcome.valid() {
		return ErrInvalidOutcome
	}
	if !permit.completed.CompareAndSwap(false, true) {
		return ErrPermitCompleted
	}
	return permit.limiter.complete(permit, outcome)
}

func (limiter *Limiter) complete(permit *Permit, outcome Outcome) error {
	now, ok := safeNow(limiter.config.clock)
	if !ok {
		now = time.Time{}
	}
	limiter.mu.Lock()
	state, exists := limiter.permits[permit.id]
	if !exists || state.generation != permit.generation || state.generation != limiter.generation {
		limiter.mu.Unlock()
		return ErrStalePermit
	}
	before := limiter.snapshotLocked()
	delete(limiter.permits, permit.id)
	limiter.inFlight--
	duration := now.Sub(state.started)
	var queueEvents []Event
	var window *Window
	if !ok {
		duration = 0
		saturatingIncrement(&limiter.clockErrors)
		queueEvents = limiter.rejectQueuedLocked(now, ErrClock)
	} else if duration < 0 {
		duration = 0
		saturatingIncrement(&limiter.clockErrors)
	}
	incrementOutcome(&limiter.outcomes, outcome)
	if ok {
		window = limiter.addSampleLocked(now, duration, outcome)
		queueEvents = limiter.grantQueuedLocked(now)
	}
	after := limiter.snapshotLocked()
	limiter.mu.Unlock()

	events := []Event{{Type: EventCompleted, Outcome: outcome, Duration: duration, Metadata: state.metadata, Before: before, After: after, Timestamp: now}}
	events = append(events, queueEvents...)
	if window != nil {
		events = append(events, limiter.applyWindow(*window, now)...)
	}
	limiter.dispatch(events)
	if !ok {
		return ErrClock
	}
	return nil
}
