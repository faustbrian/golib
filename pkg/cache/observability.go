package cache

import (
	"context"
	"time"
)

// Outcome is a low-cardinality semantic operation result.
type Outcome string

const (
	// OutcomeSuccess identifies a successful mutation or load.
	OutcomeSuccess Outcome = "success"
	// OutcomeHit identifies a fresh read.
	OutcomeHit Outcome = "hit"
	// OutcomeMiss identifies an absent read.
	OutcomeMiss Outcome = "miss"
	// OutcomeStale identifies a stale read.
	OutcomeStale Outcome = "stale"
	// OutcomeNegative identifies a negative-cache result.
	OutcomeNegative Outcome = "negative"
	// OutcomeRejected identifies a failed conditional mutation.
	OutcomeRejected Outcome = "rejected"
	// OutcomeError identifies an operation failure.
	OutcomeError Outcome = "error"
	// OutcomeEvicted identifies capacity eviction.
	OutcomeEvicted Outcome = "evicted"
	// OutcomeExpired identifies deadline expiration.
	OutcomeExpired Outcome = "expired"
)

// Event is a redacted semantic observation with no key or value fields.
type Event struct {
	Operation Operation
	Outcome   Outcome
	Duration  time.Duration
	Size      int
}

// Observer receives best-effort semantic events from cache operations.
type Observer interface {
	Observe(context.Context, Event) error
}

func notify(ctx context.Context, observer Observer, event Event) {
	if observer == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	_ = observer.Observe(ctx, event)
}

func elapsed(start, end time.Time) time.Duration {
	duration := end.Sub(start)
	if duration < 0 {
		return 0
	}
	return duration
}

func resultOutcome[V any](result Result[V], err error) Outcome {
	if err != nil {
		return OutcomeError
	}
	if result.Negative {
		return OutcomeNegative
	}
	switch result.State {
	case Hit:
		return OutcomeHit
	case Miss:
		return OutcomeMiss
	case Stale:
		return OutcomeStale
	default:
		return OutcomeSuccess
	}
}
