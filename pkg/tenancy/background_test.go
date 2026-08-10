package tenancy_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestGroupRunsBoundedTenantScopedWorkAndClosesGracefully(t *testing.T) {
	t.Parallel()

	var mutex sync.Mutex
	active := 0
	maximum := 0
	group, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 4})
	if err != nil {
		t.Fatalf("NewGroup() error = %v", err)
	}
	for index := range 64 {
		id := tenancy.MustTenantID("tenant-a")
		if index%2 == 1 {
			id = tenancy.MustTenantID("tenant-b")
		}
		scope, _ := tenancy.NewTenantScope(id, tenancy.Metadata{})
		submitContext, cancelSubmit := context.WithTimeout(context.Background(), time.Second)
		err := group.Submit(submitContext, scope, func(ctx context.Context) error {
			mutex.Lock()
			active++
			maximum = max(maximum, active)
			mutex.Unlock()
			if err := tenancy.AssertTenant(ctx, id); err != nil {
				return err
			}
			mutex.Lock()
			active--
			mutex.Unlock()
			return nil
		})
		cancelSubmit()
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
	}
	closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := group.Close(closeContext); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if maximum > 4 {
		t.Fatalf("maximum concurrency = %d", maximum)
	}
	if err := group.Submit(context.Background(), tenancy.Scope{}, func(context.Context) error { return nil }); !errors.Is(err, tenancy.ErrGroupClosed) {
		t.Fatalf("Submit(after close) error = %v", err)
	}
}

func TestGroupSubmissionAndShutdownAreCancellable(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	group, _ := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1})
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	submitContext, cancelSubmit := context.WithTimeout(context.Background(), time.Second)
	defer cancelSubmit()
	if err := group.Submit(submitContext, scope, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	waitForSignal(t, started)
	waitingContext, cancelWaiting := context.WithCancel(context.Background())
	waitingReached := make(chan struct{})
	waitingResult := make(chan error, 1)
	go func() {
		waitingResult <- group.Submit(
			&doneSignalingContext{Context: waitingContext, reached: waitingReached},
			scope,
			func(context.Context) error { return nil },
		)
	}()
	waitForSignal(t, waitingReached)
	cancelWaiting()
	if err := waitForError(t, waitingResult); !errors.Is(err, context.Canceled) {
		t.Fatalf("Submit(cancelled while waiting) error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := group.Submit(cancelled, scope, func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Submit(cancelled) error = %v", err)
	}
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if err := group.Shutdown(shutdownContext); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestGroupRejectsAlreadyCancelledTaskLifetimeBeforeStartingWork(t *testing.T) {
	t.Parallel()

	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	for iteration := range 128 {
		groupParent, cancelGroup := context.WithCancel(context.Background())
		group, err := tenancy.NewGroup(groupParent, tenancy.GroupOptions{MaxConcurrent: 1})
		if err != nil {
			t.Fatalf("iteration %d: NewGroup() error = %v", iteration, err)
		}
		submitContext, cancelSubmit := context.WithCancel(context.Background())
		cancelSubmit()
		started := make(chan struct{}, 1)
		err = group.Submit(submitContext, scope, func(context.Context) error {
			started <- struct{}{}
			return nil
		})
		if shutdownErr := shutdownWithin(t, group); shutdownErr != nil {
			t.Fatalf("iteration %d: Shutdown() error = %v", iteration, shutdownErr)
		}
		cancelGroup()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("iteration %d: Submit(cancelled context) error = %v", iteration, err)
		}
		select {
		case <-started:
			t.Fatalf("iteration %d: cancelled submission started work", iteration)
		default:
		}
	}

	t.Run("cancelled group parent", func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		group, _ := tenancy.NewGroup(parent, tenancy.GroupOptions{MaxConcurrent: 1})
		cancelParent()
		started := make(chan struct{}, 1)
		err := group.Submit(context.Background(), scope, func(context.Context) error {
			started <- struct{}{}
			return nil
		})
		if shutdownErr := shutdownWithin(t, group); shutdownErr != nil {
			t.Fatalf("Shutdown() error = %v", shutdownErr)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Submit(cancelled group) error = %v", err)
		}
		select {
		case <-started:
			t.Fatal("cancelled group started work")
		default:
		}
	})

	t.Run("submit cancelled immediately after capacity acquisition", func(t *testing.T) {
		group, _ := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1})
		base, cancelSubmit := context.WithCancel(context.Background())
		submitContext := &secondErrorHookContext{Context: base, hook: cancelSubmit}
		err := group.Submit(submitContext, scope, func(context.Context) error {
			t.Error("cancelled submission started work")
			return nil
		})
		if shutdownErr := shutdownWithin(t, group); shutdownErr != nil {
			t.Fatalf("Shutdown() error = %v", shutdownErr)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Submit(cancelled after acquisition) error = %v", err)
		}
	})

	t.Run("group cancelled immediately after capacity acquisition", func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		group, _ := tenancy.NewGroup(parent, tenancy.GroupOptions{MaxConcurrent: 1})
		submitContext := &secondErrorHookContext{
			Context: context.Background(),
			hook:    cancelParent,
		}
		err := group.Submit(submitContext, scope, func(context.Context) error {
			t.Error("cancelled group started work")
			return nil
		})
		if shutdownErr := shutdownWithin(t, group); shutdownErr != nil {
			t.Fatalf("Shutdown() error = %v", shutdownErr)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Submit(group cancelled after acquisition) error = %v", err)
		}
	})
}

func TestGroupTaskPreservesSubmitContext(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	reported := make(chan error, 1)
	observed := make(chan struct {
		value       string
		hasDeadline bool
	}, 1)
	group, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{
		MaxConcurrent: 1,
		HandleError: func(_ tenancy.Scope, err error) {
			reported <- err
		},
	})
	if err != nil {
		t.Fatalf("NewGroup() error = %v", err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	submitContext, cancelSubmit := context.WithTimeout(
		context.WithValue(context.Background(), contextKey{}, "request-value"),
		time.Second,
	)
	if err := group.Submit(submitContext, scope, func(ctx context.Context) error {
		_, hasDeadline := ctx.Deadline()
		value, _ := ctx.Value(contextKey{}).(string)
		observed <- struct {
			value       string
			hasDeadline bool
		}{value: value, hasDeadline: hasDeadline}
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		cancelSubmit()
		t.Fatalf("Submit() error = %v", err)
	}
	var got struct {
		value       string
		hasDeadline bool
	}
	select {
	case got = <-observed:
	case <-time.After(time.Second):
		cancelSubmit()
		t.Fatal("task did not receive the submission context")
	}
	if got.value != "request-value" || !got.hasDeadline {
		cancelSubmit()
		t.Fatalf("task context = %#v", got)
	}
	cancelSubmit()
	select {
	case err := <-reported:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reported error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("submission cancellation was not reported")
	}
	if err := closeWithin(t, group); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestGroupRejectsConflictingSubmitScopeSynchronously(t *testing.T) {
	t.Parallel()

	tenantA, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	tenantB, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-b"), tenancy.Metadata{})
	submitContext, _ := tenancy.WithScope(context.Background(), tenantA)
	group, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1})
	if err != nil {
		t.Fatalf("NewGroup() error = %v", err)
	}
	started := make(chan struct{}, 1)
	if err := group.Submit(submitContext, tenantB, func(context.Context) error {
		started <- struct{}{}
		return nil
	}); !errors.Is(err, tenancy.ErrConflictingScope) {
		t.Fatalf("Submit(conflicting scope) error = %v", err)
	}
	accepted := make(chan struct{}, 1)
	if err := group.Submit(submitContext, tenantA, func(ctx context.Context) error {
		if err := tenancy.AssertScope(ctx, tenantA); err != nil {
			return err
		}
		accepted <- struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("Submit(equal scope) error = %v", err)
	}
	if err := closeWithin(t, group); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-started:
		t.Fatal("conflicting submission started work")
	default:
	}
	select {
	case <-accepted:
	default:
		t.Fatal("equal scoped submission did not start work")
	}
}

func TestGroupReportsTaskErrorsOutsideSynchronization(t *testing.T) {
	t.Parallel()

	reported := make(chan error, 1)
	group, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{
		MaxConcurrent: 1,
		HandleError: func(_ tenancy.Scope, err error) {
			reported <- err
		},
	})
	if err != nil {
		t.Fatalf("NewGroup() error = %v", err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	want := errors.New("task failed")
	submitContext, cancelSubmit := context.WithTimeout(context.Background(), time.Second)
	defer cancelSubmit()
	if err := group.Submit(submitContext, scope, func(context.Context) error { return want }); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := group.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case got := <-reported:
		if !errors.Is(got, want) {
			t.Fatalf("reported error = %v", got)
		}
	default:
		t.Fatal("task error was not reported")
	}
}

func TestGroupCloseReleasesOwnedTaskContext(t *testing.T) {
	t.Parallel()

	group, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1})
	if err != nil {
		t.Fatalf("NewGroup() error = %v", err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	captured := make(chan context.Context, 1)
	if err := group.Submit(context.Background(), scope, func(ctx context.Context) error {
		captured <- ctx
		return nil
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	var taskContext context.Context
	select {
	case taskContext = <-captured:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task context")
	}
	if err := closeWithin(t, group); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-taskContext.Done():
		if !errors.Is(taskContext.Err(), context.Canceled) {
			t.Fatalf("task context error = %v", taskContext.Err())
		}
	default:
		t.Fatal("Close() retained the group-owned task context")
	}
}

func TestGroupValidatesConstructionAndSubmission(t *testing.T) {
	t.Parallel()

	//lint:ignore SA1012 Nil context rejection is the contract under test.
	if _, err := tenancy.NewGroup(nil, tenancy.GroupOptions{MaxConcurrent: 1}); !errors.Is(err, tenancy.ErrInvalidGroup) { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatalf("NewGroup(nil) error = %v", err)
	}
	if _, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{}); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("NewGroup(zero) error = %v", err)
	}
	tenantScope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-parent"), tenancy.Metadata{})
	scopedParent, _ := tenancy.WithScope(context.Background(), tenantScope)
	if _, err := tenancy.NewGroup(scopedParent, tenancy.GroupOptions{MaxConcurrent: 1}); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("NewGroup(scoped parent) error = %v", err)
	}
	group, _ := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1})
	if err := group.Submit(context.Background(), tenancy.Scope{}, func(context.Context) error { return nil }); !errors.Is(err, tenancy.ErrInvalidOperation) {
		t.Fatalf("Submit(invalid scope) error = %v", err)
	}
	scope, _ := tenancy.NewTenantScope(tenancy.MustTenantID("tenant-a"), tenancy.Metadata{})
	if err := group.Submit(context.Background(), scope, nil); !errors.Is(err, tenancy.ErrInvalidOperation) {
		t.Fatalf("Submit(nil operation) error = %v", err)
	}
	if err := shutdownWithin(t, group); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	var nilGroup *tenancy.Group
	if err := nilGroup.Close(context.Background()); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("nil Close() error = %v", err)
	}
	if err := nilGroup.Shutdown(context.Background()); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("nil Shutdown() error = %v", err)
	}
	validGroup, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 1024})
	if err != nil {
		t.Fatalf("NewGroup(maximum) error = %v", err)
	}
	//lint:ignore SA1012 Nil context rejection is the contract under test.
	if err := validGroup.Shutdown(nil); !errors.Is(err, tenancy.ErrInvalidGroup) { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatalf("Shutdown(nil context) error = %v", err)
	}
	if err := shutdownWithin(t, validGroup); err != nil {
		t.Fatalf("Shutdown(valid) error = %v", err)
	}
}

type secondErrorHookContext struct {
	context.Context
	calls int
	hook  func()
}

func (ctx *secondErrorHookContext) Err() error {
	ctx.calls++
	if ctx.calls == 2 {
		ctx.hook()
	}
	return ctx.Context.Err()
}
