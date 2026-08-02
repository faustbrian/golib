package bulkhead_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestExecuteReleasesOnErrorPanicAndReentrantRejection(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "inventory-db",
		Capacity:  1,
		Admission: bulkhead.RejectImmediately{},
	})
	operationError := errors.New("query failed")
	_, _, err := bulkhead.Execute(context.Background(), policy, 1, func(ctx context.Context) (int, error) {
		if _, err := policy.Acquire(ctx, 1); !errors.Is(err, bulkhead.ErrReentrant) {
			t.Fatalf("nested Acquire() error = %v, want ErrReentrant", err)
		}
		return 0, operationError
	})
	if !errors.Is(err, operationError) {
		t.Fatalf("Execute() error = %v, want operation error", err)
	}
	if got := policy.Snapshot().ActiveWeight; got != 0 {
		t.Fatalf("ActiveWeight after error = %d", got)
	}

	func() {
		defer func() {
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("recovered panic = %v", recovered)
			}
		}()
		_, _, _ = bulkhead.Execute(context.Background(), policy, 1, func(context.Context) (int, error) {
			panic("boom")
		})
	}()
	if got := policy.Snapshot(); got.ActiveWeight != 0 || got.Executions != 2 {
		t.Fatalf("Snapshot() after panic = %+v", got)
	}
}

func TestCloseRejectsWaitersAndDrainHonorsCallerBound(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "inventory-db",
		Capacity:  1,
		Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: 100 * time.Millisecond},
	})
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	waiter := acquireAsync(policy, context.Background())
	waitForQueueDepth(t, policy, 1)

	if err := policy.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := policy.Close(); err != nil {
		t.Fatalf("duplicate Close() error = %v", err)
	}
	if result := receiveAcquire(t, waiter); !errors.Is(result.err, bulkhead.ErrClosed) {
		t.Fatalf("queued Acquire() error = %v, want ErrClosed", result.err)
	}
	if _, err := policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrClosed) {
		t.Fatalf("post-close Acquire() error = %v, want ErrClosed", err)
	}

	drainContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := policy.Drain(drainContext); !errors.Is(err, bulkhead.ErrDrainIncomplete) || !errors.Is(err, context.Canceled) {
		t.Fatalf("bounded Drain() error = %v", err)
	}
	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release() error = %v", err)
	}
	if err := drainWithin(policy); err != nil {
		t.Fatalf("final Drain() error = %v", err)
	}
	if got := policy.Snapshot(); !got.Draining || !got.Drained || got.QueueDepth != 0 || got.ActiveWeight != 0 {
		t.Fatalf("drained Snapshot() = %+v", got)
	}
}

func TestExecuteRejectsInvalidOperationAndFailedAdmission(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
	if _, _, err := bulkhead.Execute[int](context.Background(), policy, 1, nil); !errors.Is(err, bulkhead.ErrInvalidOperation) {
		t.Fatalf("Execute(nil) error = %v", err)
	}
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	defer func() { _ = holder.Release() }()
	if _, _, err := bulkhead.Execute(context.Background(), policy, 1, func(context.Context) (int, error) {
		return 0, nil
	}); !errors.Is(err, bulkhead.ErrRejected) {
		t.Fatalf("Execute(saturated) error = %v", err)
	}
}

func TestReentrancyDetectionTraversesNestedDifferentBulkheads(t *testing.T) {
	outer := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
	inner := mustPolicy(t, bulkhead.Config{Resource: "cache", Capacity: 1})
	_, _, err := bulkhead.Execute(context.Background(), outer, 1, func(ctx context.Context) (struct{}, error) {
		_, _, innerErr := bulkhead.Execute(ctx, inner, 1, func(nested context.Context) (struct{}, error) {
			if _, acquireErr := outer.Acquire(nested, 1); !errors.Is(acquireErr, bulkhead.ErrReentrant) {
				t.Fatalf("outer Acquire() error = %v, want ErrReentrant", acquireErr)
			}
			return struct{}{}, nil
		})
		return struct{}{}, innerErr
	})
	if err != nil {
		t.Fatalf("nested Execute() error = %v", err)
	}
}

func TestDrainRejectsNilContext(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
	//lint:ignore SA1012 Public boundary must reject a nil context safely.
	if err := policy.Drain(nil); !errors.Is(err, bulkhead.ErrDrainIncomplete) || //nolint:staticcheck // Explicit nil rejection.
		!errors.Is(err, context.Canceled) {
		t.Fatalf("Drain(nil) error = %v", err)
	}
}
