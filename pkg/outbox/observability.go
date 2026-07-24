package outbox

import (
	"context"
	"time"
)

// Operation identifies a bounded outbox lifecycle action.
type Operation string

const (
	OperationClaim       Operation = "claim"
	OperationPublish     Operation = "publish"
	OperationDeliver     Operation = "deliver"
	OperationRetry       Operation = "retry"
	OperationDeadLetter  Operation = "dead_letter"
	OperationRelease     Operation = "release"
	OperationExtendLease Operation = "extend_lease"
	OperationReplay      Operation = "replay"
	OperationPrune       Operation = "prune"
	OperationArchive     Operation = "archive"
)

// Outcome identifies whether an operation completed successfully.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
)

// Event contains payload-safe diagnostics for one outbox operation. It never
// includes payloads, metadata, or error text.
type Event struct {
	Operation Operation
	Outcome   Outcome
	Count     int
	MessageID string
	Topic     string
	Attempts  int
	Duration  time.Duration
}

// BacklogStats summarizes operationally relevant message states.
type BacklogStats struct {
	Pending         int64
	Leased          int64
	Dead            int64
	OldestPendingAt *time.Time
}

// Observer receives structured lifecycle events.
type Observer interface {
	Observe(context.Context, Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Event)

// Observe forwards the event to the wrapped function.
func (observe ObserverFunc) Observe(ctx context.Context, event Event) {
	observe(ctx, event)
}
