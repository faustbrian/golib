package scheduler

import (
	"context"
	"sync/atomic"
)

// PauseSource reports whether ordinary schedules should be paused.
type PauseSource interface {
	Paused(context.Context) (bool, error)
}

// PauseController idempotently changes an application's scheduler pause state.
type PauseController interface {
	Pause(context.Context) error
	Resume(context.Context) error
}

// PauseState is a process-local, concurrency-safe pause source and controller.
// Distributed runners should use an application-owned shared persistent
// implementation of PauseSource and PauseController instead.
type PauseState struct {
	paused atomic.Bool
}

// NewPauseState constructs an initially resumed process-local pause state.
func NewPauseState() *PauseState { return &PauseState{} }

// Paused reports the current process-local pause state.
func (state *PauseState) Paused(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return state.paused.Load(), nil
}

// Pause idempotently pauses ordinary schedules.
func (state *PauseState) Pause(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state.paused.Store(true)
	return nil
}

// Resume idempotently resumes ordinary schedules.
func (state *PauseState) Resume(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	state.paused.Store(false)
	return nil
}

var _ PauseSource = (*PauseState)(nil)
var _ PauseController = (*PauseState)(nil)
