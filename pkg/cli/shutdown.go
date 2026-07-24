package cli

import (
	"context"
	"errors"
	"sync"
)

// ErrSignal is the default cancellation cause when no signal cause is supplied.
var ErrSignal = errors.New("shutdown signal received")

// ShutdownAction describes a caller-owned signal policy transition.
type ShutdownAction uint8

const (
	// ShutdownGraceful means the first signal canceled the graceful context.
	ShutdownGraceful ShutdownAction = iota + 1
	// ShutdownForced means a repeated signal requested forced termination.
	ShutdownForced
	// ShutdownAlreadyForced means forced termination was already requested.
	ShutdownAlreadyForced
)

// ShutdownController translates caller-delivered signals without registering
// process signal handlers or starting goroutines. The application remains
// responsible for signal.Notify and signal.Stop ownership.
type ShutdownController struct {
	context context.Context
	cancel  context.CancelCauseFunc
	forced  chan struct{}
	mu      sync.Mutex
	count   uint64
	closed  bool
}

// NewShutdownController derives a cancelable context from the caller context.
func NewShutdownController(parent context.Context) (*ShutdownController, error) {
	if parent == nil {
		return nil, newInternalError("create shutdown controller with nil context", nil)
	}
	ctx, cancel := context.WithCancelCause(parent)

	return &ShutdownController{
		context: ctx,
		cancel:  cancel,
		forced:  make(chan struct{}),
	}, nil
}

// Context returns the graceful cancellation context.
func (controller *ShutdownController) Context() context.Context {
	if controller == nil {
		return nil
	}

	return controller.context
}

// Forced closes after the second delivered signal.
func (controller *ShutdownController) Forced() <-chan struct{} {
	if controller == nil {
		return nil
	}

	return controller.forced
}

// Signal applies graceful-then-forced policy to one caller-delivered signal.
func (controller *ShutdownController) Signal(cause error) ShutdownAction {
	if controller == nil {
		return ShutdownAlreadyForced
	}
	if cause == nil {
		cause = ErrSignal
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.closed {
		return ShutdownAlreadyForced
	}
	controller.count++
	switch controller.count {
	case 1:
		controller.cancel(cause)
		return ShutdownGraceful
	case 2:
		close(controller.forced)
		return ShutdownForced
	default:
		return ShutdownAlreadyForced
	}
}

// Close releases the derived context when signal handling is no longer needed.
func (controller *ShutdownController) Close() {
	if controller == nil {
		return
	}
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if controller.closed {
		return
	}
	controller.closed = true
	controller.cancel(context.Canceled)
}
