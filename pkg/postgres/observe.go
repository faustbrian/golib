package postgres

import (
	"context"
	"time"
)

// Operation is a fixed low-cardinality operation name.
type Operation string

const (
	// OperationAcquire identifies pool connection acquisition.
	OperationAcquire Operation = "pool.acquire"
	// OperationPing identifies pool health checks.
	OperationPing Operation = "pool.ping"
	// OperationClose identifies bounded pool shutdown waits.
	OperationClose Operation = "pool.close"
	// OperationTransaction identifies top-level transactions.
	OperationTransaction Operation = "transaction"
	// OperationSavepoint identifies explicit nested savepoints.
	OperationSavepoint Operation = "savepoint"
)

// Outcome is a fixed low-cardinality operation result.
type Outcome string

const (
	// OutcomeSuccess indicates a nil operation error.
	OutcomeSuccess Outcome = "success"
	// OutcomeError indicates a classified operation error.
	OutcomeError Outcome = "error"
	// OutcomePanic indicates that a transaction callback panicked.
	OutcomePanic Outcome = "panic"
	// OutcomeAborted indicates that a callback terminated its goroutine.
	OutcomeAborted Outcome = "aborted"
)

// Observation contains bounded metadata only. It intentionally excludes SQL,
// query arguments, DSNs, database error text, and arbitrary caller labels.
type Observation struct {
	Operation Operation
	Outcome   Outcome
	Duration  time.Duration
	ErrorKind ErrorKind
	SQLState  string
	Pool      Stats
	// HasPoolStats distinguishes an actual zero-valued snapshot from an
	// operation, such as a transaction, that did not sample pool state.
	HasPoolStats bool
}

// Observer consumes bounded lifecycle and transaction observations.
type Observer interface {
	Observe(context.Context, Observation)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Observation)

// Observe implements Observer.
func (f ObserverFunc) Observe(ctx context.Context, observation Observation) {
	f(ctx, observation)
}

func safeObserve(ctx context.Context, observer Observer, observation Observation) {
	if observer == nil {
		return
	}

	defer func() {
		_ = recover()
	}()
	observer.Observe(ctx, observation)
}

func observationFor(operation Operation, started time.Time, err error) Observation {
	info := Classify(err)
	outcome := OutcomeSuccess
	if err != nil {
		outcome = OutcomeError
	}

	return Observation{
		Operation: operation,
		Outcome:   outcome,
		Duration:  time.Since(started),
		ErrorKind: info.Kind,
		SQLState:  info.SQLState,
	}
}
