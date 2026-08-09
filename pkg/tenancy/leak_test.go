package tenancy_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/faustbrian/golib/pkg/tenancy"
)

func TestGroupStressCloseAndShutdownDoNotLeak(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	const (
		iterations = 100
		tasks      = 16
	)
	var completed atomic.Int64
	for iteration := range iterations {
		group, err := tenancy.NewGroup(context.Background(), tenancy.GroupOptions{MaxConcurrent: 4})
		if err != nil {
			t.Fatalf("iteration %d: NewGroup() error = %v", iteration, err)
		}
		for task := range tasks {
			tenant := "tenant-a"
			if task%2 == 1 {
				tenant = "tenant-b"
			}
			scope := mustTenantScope(t, tenant)
			if err := group.Submit(context.Background(), scope, func(ctx context.Context) error {
				if _, err := tenancy.RequireTenant(ctx); err != nil {
					return err
				}
				completed.Add(1)
				return nil
			}); err != nil {
				t.Fatalf("iteration %d task %d: Submit() error = %v", iteration, task, err)
			}
		}
		closeContext, cancel := context.WithTimeout(context.Background(), time.Second)
		var closeErr error
		if iteration%2 == 0 {
			closeErr = group.Close(closeContext)
		} else {
			closeErr = group.Shutdown(closeContext)
		}
		if closeErr != nil {
			cancel()
			t.Fatalf("iteration %d: group close error = %v", iteration, closeErr)
		}
		cancel()
	}
	if got, want := completed.Load(), int64(iterations*tasks); got != want {
		t.Fatalf("completed tasks = %d, want %d", got, want)
	}
}
