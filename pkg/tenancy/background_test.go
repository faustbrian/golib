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
		if err := group.Submit(context.Background(), scope, func(ctx context.Context) error {
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
		}); err != nil {
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
	if err := group.Submit(context.Background(), scope, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	<-started
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
	if err := group.Submit(context.Background(), scope, func(context.Context) error { return want }); err != nil {
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

func TestGroupValidatesConstructionAndSubmission(t *testing.T) {
	t.Parallel()

	if _, err := tenancy.NewGroup(nil, tenancy.GroupOptions{MaxConcurrent: 1}); !errors.Is(err, tenancy.ErrInvalidGroup) {
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
	if err := group.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	var nilGroup *tenancy.Group
	if err := nilGroup.Close(context.Background()); !errors.Is(err, tenancy.ErrInvalidGroup) {
		t.Fatalf("nil Close() error = %v", err)
	}
}
