package resilience

import (
	"sync"
	"time"
)

// EventKind identifies a bounded execution lifecycle event.
type EventKind string

const (
	// EventExecutionStarted identifies logical execution admission.
	EventExecutionStarted EventKind = "execution_started"
	// EventPolicyEntered identifies entry into one composed policy.
	EventPolicyEntered EventKind = "policy_entered"
	// EventWorkAdmitted identifies budget admission of physical work.
	EventWorkAdmitted EventKind = "work_admitted"
	// EventWorkRejected identifies local budget rejection.
	EventWorkRejected EventKind = "work_rejected"
	// EventAttemptStarted identifies invocation of physical work.
	EventAttemptStarted EventKind = "attempt_started"
	// EventAttemptCompleted identifies return from physical work.
	EventAttemptCompleted EventKind = "attempt_completed"
	// EventExecutionCanceled identifies caller cancellation of an execution.
	EventExecutionCanceled EventKind = "execution_canceled"
	// EventExecutionCompleted identifies the terminal logical outcome.
	EventExecutionCompleted EventKind = "execution_completed"
)

// Event contains only bounded metadata and never retains operation values.
type Event struct {
	Kind      EventKind
	Policy    PolicyID
	Reason    string
	LogicalID string
	Attempt   Attempt
	At        time.Time
}

// Observer receives events after internal state changes and outside locks.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe invokes the adapted function.
func (function ObserverFunc) Observe(event Event) { function(event) }

type recorder struct {
	mu       sync.Mutex
	max      int
	clock    Clock
	observer Observer
	events   []Event
}

func (recorder *recorder) emit(event Event) {
	event.At = recorder.clock.Now()
	recorder.mu.Lock()
	if len(recorder.events) < recorder.max {
		recorder.events = append(recorder.events, event)
	}
	recorder.mu.Unlock()
}

func (recorder *recorder) snapshot() []Event {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]Event(nil), recorder.events...)
}

func (recorder *recorder) notify(events []Event) {
	if recorder.observer == nil {
		return
	}
	for _, event := range events {
		func() {
			defer func() { _ = recover() }()
			recorder.observer.Observe(event)
		}()
	}
}
