package queue

import (
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

// EventKind identifies an observable queue lifecycle transition.
type EventKind string

const (
	EventEnqueued          EventKind = "enqueued"
	EventHandlerStarted    EventKind = "handler_started"
	EventRetryScheduled    EventKind = "retry_scheduled"
	EventHandlerSucceeded  EventKind = "handler_succeeded"
	EventHandlerFailed     EventKind = "handler_failed"
	EventAcknowledged      EventKind = "acknowledged"
	EventAckFailed         EventKind = "ack_failed"
	EventRejected          EventKind = "rejected"
	EventRejectFailed      EventKind = "reject_failed"
	EventShutdownStarted   EventKind = "shutdown_started"
	EventShutdownCompleted EventKind = "shutdown_completed"
)

// Event describes a queue or backend lifecycle transition.
type Event struct {
	Kind           EventKind
	Backend        string
	Queue          string
	OccurredAt     time.Time
	Duration       time.Duration
	RetryRemaining int64
	RetryDelay     time.Duration
	Depth          int64
	JobAge         time.Duration
	Err            error
	Classification management.Classification
	FailureCode    string
}

// Observer receives synchronous queue lifecycle events.
type Observer interface {
	Observe(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

// Observe calls the wrapped observer function.
func (f ObserverFunc) Observe(event Event) {
	f(event)
}

type emptyObserver struct{}

func (emptyObserver) Observe(Event) {}
