package concurrencylimit

import "errors"

var (
	// ErrInvalidConfig identifies configuration that cannot preserve bounds.
	ErrInvalidConfig = errors.New("concurrency-limit: invalid configuration")
	// ErrInvalidMetadata identifies unconfigured or out-of-range admission metadata.
	ErrInvalidMetadata = errors.New("concurrency-limit: invalid metadata")
	// ErrLimitExceeded identifies immediate rejection at the current limit.
	ErrLimitExceeded = errors.New("concurrency-limit: limit exceeded")
	// ErrQueueFull identifies rejection at the configured absolute queue bound.
	ErrQueueFull = errors.New("concurrency-limit: queue full")
	// ErrQueueTimeout identifies expiry of the configured local queue wait.
	ErrQueueTimeout = errors.New("concurrency-limit: queue wait exceeded")
	// ErrPermitCompleted identifies a duplicate terminal outcome.
	ErrPermitCompleted = errors.New("concurrency-limit: permit already completed")
	// ErrStalePermit identifies a permit invalidated by lifecycle reset.
	ErrStalePermit = errors.New("concurrency-limit: stale permit")
	// ErrInvalidOutcome identifies a terminal value outside the defined set.
	ErrInvalidOutcome = errors.New("concurrency-limit: invalid outcome")
	// ErrDraining identifies admission rejected during graceful drain.
	ErrDraining = errors.New("concurrency-limit: draining")
	// ErrReset identifies queued admission invalidated by reset.
	ErrReset = errors.New("concurrency-limit: reset")
	// ErrClock identifies a clock or timer implementation failure.
	ErrClock = errors.New("concurrency-limit: clock failure")
	// ErrClassifierPanic identifies a contained classifier panic.
	ErrClassifierPanic = errors.New("concurrency-limit: classifier panic")
	// ErrIdentifierExhausted identifies exhaustion of the process-local permit sequence.
	ErrIdentifierExhausted = errors.New("concurrency-limit: permit identifier exhausted")
)
