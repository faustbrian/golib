package projection

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	// ErrReplayGuardPanic reports a contained replay-guard panic.
	ErrReplayGuardPanic = errors.New("projection replay guard panicked")
)

// ReplayAttempt describes one bounded projection replay operation before any
// replay hook, history read, handler, or checkpoint mutation occurs.
//
// Its zero value is invalid.
type ReplayAttempt struct {
	projectionName string
	checkpoint     eventsourcing.GlobalPosition
	hasCheckpoint  bool
	batchSize      uint32
	valid          bool
}

// ProjectionName returns the stable projection identity.
func (attempt ReplayAttempt) ProjectionName() string {
	return attempt.projectionName
}

// Checkpoint returns the durable position from which the batch will resume.
func (attempt ReplayAttempt) Checkpoint() (
	eventsourcing.GlobalPosition,
	bool,
) {
	return attempt.checkpoint, attempt.hasCheckpoint
}

// BatchSize returns the maximum number of stored messages the batch may scan.
func (attempt ReplayAttempt) BatchSize() uint32 {
	return attempt.batchSize
}

// Valid reports whether the attempt was created by a configured runner.
func (attempt ReplayAttempt) Valid() bool {
	return attempt.valid
}

// ReplayGuard authorizes and may audit one replay attempt.
//
// A guard runs before every batch, including resumed and terminal probes. It
// must be idempotent because a failed or repeated batch invokes it again.
type ReplayGuard func(context.Context, ReplayAttempt) error

// PermitReplay is an explicit opt-in for applications that enforce replay
// authorization and auditing outside the runner.
func PermitReplay(context.Context, ReplayAttempt) error {
	return nil
}

// ReplayGuardError preserves a guard failure without exposing application
// diagnostics.
type ReplayGuardError struct {
	Cause error
}

// Error implements error without exposing application data.
func (*ReplayGuardError) Error() string {
	return "projection replay guard failed"
}

// Unwrap preserves the guard cause for errors.Is and errors.As.
func (err *ReplayGuardError) Unwrap() error {
	return err.Cause
}

func callReplayGuard(
	ctx context.Context,
	guard ReplayGuard,
	attempt ReplayAttempt,
) (err error) {
	defer func() {
		if recover() != nil {
			err = &ReplayGuardError{Cause: ErrReplayGuardPanic}
		}
	}()

	if err := guard(ctx, attempt); err != nil {
		return &ReplayGuardError{Cause: err}
	}

	return nil
}

var _ error = (*ReplayGuardError)(nil)
