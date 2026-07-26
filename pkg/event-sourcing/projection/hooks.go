package projection

import (
	"context"
	"errors"
)

var (
	// ErrReplayHookPanic reports a contained replay lifecycle-hook panic.
	ErrReplayHookPanic = errors.New("projection replay hook panicked")
)

// ReplayHookPhase identifies one replay lifecycle boundary.
type ReplayHookPhase uint8

const (
	// ReplayHookBefore runs before reading history when no checkpoint exists.
	ReplayHookBefore ReplayHookPhase = iota + 1
	// ReplayHookAfter runs after a terminal batch returns no messages.
	ReplayHookAfter
)

// String returns a stable diagnostic phase.
func (phase ReplayHookPhase) String() string {
	switch phase {
	case ReplayHookBefore:
		return "before"
	case ReplayHookAfter:
		return "after"
	default:
		return "unknown"
	}
}

// ReplayHook performs explicit application work at a replay lifecycle
// boundary. Hooks must be idempotent because a failed batch or repeated
// terminal probe can invoke the same boundary again.
type ReplayHook func(context.Context) error

// ReplayHookError preserves a lifecycle-hook failure without exposing
// application diagnostics.
type ReplayHookError struct {
	Phase ReplayHookPhase
	Cause error
}

// Error implements error without exposing application data.
func (*ReplayHookError) Error() string {
	return "projection replay hook failed"
}

// Unwrap preserves the hook cause for errors.Is and errors.As.
func (err *ReplayHookError) Unwrap() error {
	return err.Cause
}

func callReplayHook(
	ctx context.Context,
	phase ReplayHookPhase,
	hook ReplayHook,
) (err error) {
	defer func() {
		if recover() != nil {
			err = &ReplayHookError{
				Phase: phase,
				Cause: ErrReplayHookPanic,
			}
		}
	}()

	if err := hook(ctx); err != nil {
		return &ReplayHookError{Phase: phase, Cause: err}
	}

	return nil
}

var _ error = (*ReplayHookError)(nil)
