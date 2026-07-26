package projection

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	// ErrRebuildPartial reports that read-model reset began but checkpoint reset
	// did not complete.
	ErrRebuildPartial = errors.New("projection rebuild is partially applied")
	// ErrReadModelResetPanic reports a contained read-model reset panic.
	ErrReadModelResetPanic = errors.New("projection read-model reset panicked")
)

// RebuildPhase identifies the operation that prevented a rebuild from
// completing.
type RebuildPhase uint8

const (
	// RebuildReadModel identifies the application-owned reset callback.
	RebuildReadModel RebuildPhase = iota + 1
	// RebuildCheckpoint identifies cancellation or checkpoint reset after the
	// application callback returned successfully.
	RebuildCheckpoint
)

// String returns a stable diagnostic phase.
func (phase RebuildPhase) String() string {
	switch phase {
	case RebuildReadModel:
		return "read_model"
	case RebuildCheckpoint:
		return "checkpoint"
	default:
		return "unknown"
	}
}

// ReadModelReset explicitly resets or replaces application-owned projection
// state. It must be idempotent because failure does not prove whether its
// application-side changes committed.
type ReadModelReset func(context.Context) error

// RebuildError reports that a read-model reset or its following checkpoint
// reset did not complete. The projection remains caller-controlled and is
// never resumed by the library.
type RebuildError struct {
	Phase RebuildPhase
	Cause error
}

// Error implements error without exposing application or storage data.
func (*RebuildError) Error() string {
	return "projection rebuild did not complete"
}

// Unwrap preserves the partial-rebuild category and specific cause.
func (err *RebuildError) Unwrap() []error {
	return []error{ErrRebuildPartial, err.Cause}
}

// Rebuild resets application-owned read-model state and then compare-and-resets
// its checkpoint.
//
// The projection must already be paused and all application-owned in-flight
// batches must be drained. Rebuild never resumes the projection, owns no
// transaction, and does not make the callback and checkpoint reset atomic.
// Callers must serialize it with Resume and other control operations. On
// failure it returns the last status confirmed before invoking reset.
func (controller *Controller) Rebuild(
	ctx context.Context,
	expected eventsourcing.GlobalPosition,
	reset ReadModelReset,
) (Status, error) {
	if controller == nil || ctx == nil || reset == nil {
		return Status{}, eventsourcing.ErrInvalidArgument
	}
	status, err := controller.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if status.State() != StatePaused {
		return status, ErrProjectionRunning
	}
	if err := callReadModelReset(ctx, reset); err != nil {
		return status, &RebuildError{
			Phase: RebuildReadModel,
			Cause: err,
		}
	}
	if err := ctx.Err(); err != nil {
		return status, &RebuildError{
			Phase: RebuildCheckpoint,
			Cause: err,
		}
	}
	rebuilt, err := controller.ResetCheckpoint(ctx, expected)
	if err != nil {
		return status, &RebuildError{
			Phase: RebuildCheckpoint,
			Cause: err,
		}
	}

	return rebuilt, nil
}

func callReadModelReset(
	ctx context.Context,
	reset ReadModelReset,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrReadModelResetPanic
		}
	}()

	return reset(ctx)
}

var _ error = (*RebuildError)(nil)
