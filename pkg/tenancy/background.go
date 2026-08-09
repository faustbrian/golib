package tenancy

import (
	"context"
	"errors"
	"sync"
)

const maximumGroupConcurrency = 1024

var (
	// ErrInvalidGroup reports invalid ownership, limits, or receiver state.
	ErrInvalidGroup = errors.New("tenancy: invalid background group")
	// ErrGroupClosed reports submission after graceful close or shutdown begins.
	ErrGroupClosed = errors.New("tenancy: background group closed")
)

// GroupOptions define bounded concurrency and task error ownership.
type GroupOptions struct {
	MaxConcurrent int
	HandleError   func(Scope, error)
}

// Group owns every goroutine it starts. Submit is bounded and cancellable;
// callers must invoke Close for graceful completion or Shutdown for
// cancellation. No task scope is retained between submissions.
type Group struct {
	ctx         context.Context
	cancel      context.CancelFunc
	semaphore   chan struct{}
	handleError func(Scope, error)

	mutex      sync.Mutex
	active     int
	closed     bool
	doneClosed bool
	done       chan struct{}
}

// NewGroup derives task lifetime from parent and validates a hard concurrency
// bound. The group owns the derived cancellation function.
func NewGroup(parent context.Context, options GroupOptions) (*Group, error) {
	if parent == nil || options.MaxConcurrent <= 0 || options.MaxConcurrent > maximumGroupConcurrency {
		return nil, ErrInvalidGroup
	}
	if _, scoped := ScopeFromContext(parent); scoped {
		return nil, ErrInvalidGroup
	}
	ctx, cancel := context.WithCancel(parent)
	return &Group{
		ctx: ctx, cancel: cancel, semaphore: make(chan struct{}, options.MaxConcurrent),
		handleError: options.HandleError, done: make(chan struct{}),
	}, nil
}

// Submit starts one explicitly scoped task after acquiring bounded capacity.
// Waiting for capacity observes both submitCtx and the group lifetime.
func (group *Group) Submit(
	submitCtx context.Context,
	scope Scope,
	operation func(context.Context) error,
) error {
	if group == nil || group.ctx == nil || submitCtx == nil {
		return ErrInvalidGroup
	}
	if group.isClosed() {
		return ErrGroupClosed
	}
	if !scope.Valid() || operation == nil {
		return ErrInvalidOperation
	}
	select {
	case group.semaphore <- struct{}{}:
	case <-submitCtx.Done():
		return submitCtx.Err()
	case <-group.ctx.Done():
		return group.ctx.Err()
	}

	group.mutex.Lock()
	if group.closed {
		group.mutex.Unlock()
		<-group.semaphore
		return ErrGroupClosed
	}
	group.active++
	group.mutex.Unlock()

	go group.run(scope, operation)
	return nil
}

// Close stops new submissions and waits for active tasks without cancelling
// them. Waiting observes ctx.
func (group *Group) Close(ctx context.Context) error {
	if group == nil || group.ctx == nil || ctx == nil {
		return ErrInvalidGroup
	}
	group.beginClose()
	if err := group.wait(ctx); err != nil {
		return err
	}
	group.cancel()
	return nil
}

// Shutdown stops new submissions, cancels active tasks, and waits for their
// return. Waiting observes ctx, while task cancellation uses the group context.
func (group *Group) Shutdown(ctx context.Context) error {
	if group == nil || group.ctx == nil || ctx == nil {
		return ErrInvalidGroup
	}
	group.beginClose()
	group.cancel()
	return group.wait(ctx)
}

func (group *Group) run(scope Scope, operation func(context.Context) error) {
	defer group.complete()
	scoped, err := WithScope(group.ctx, scope)
	if err == nil {
		err = operation(scoped)
	}
	if err != nil && group.handleError != nil {
		group.handleError(scope, err)
	}
}

func (group *Group) complete() {
	<-group.semaphore
	group.mutex.Lock()
	group.active--
	group.closeDoneLocked()
	group.mutex.Unlock()
}

func (group *Group) beginClose() {
	group.mutex.Lock()
	group.closed = true
	group.closeDoneLocked()
	group.mutex.Unlock()
}

func (group *Group) closeDoneLocked() {
	if group.closed && group.active == 0 && !group.doneClosed {
		close(group.done)
		group.doneClosed = true
	}
}

func (group *Group) wait(ctx context.Context) error {
	select {
	case <-group.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (group *Group) isClosed() bool {
	group.mutex.Lock()
	defer group.mutex.Unlock()
	return group.closed
}
