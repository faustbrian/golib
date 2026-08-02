package bulkhead_test

import (
	"context"
	"testing"

	failsafebulkhead "github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/faustbrian/golib/pkg/bulkhead"
	"golang.org/x/sync/semaphore"
)

func BenchmarkBulkheadAcquireRelease(b *testing.B) {
	policy := mustBenchmarkPolicy(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		permit, err := policy.Acquire(ctx, 1)
		if err != nil {
			b.Fatalf("Acquire() error = %v", err)
		}
		_ = permit.Release()
	}
}

func BenchmarkBulkheadRejected(b *testing.B) {
	policy := mustBenchmarkPolicy(b)
	holder, _ := policy.Acquire(context.Background(), 1)
	defer func() { _ = holder.Release() }()
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = policy.Acquire(ctx, 1)
	}
}

func BenchmarkXSyncSemaphoreAcquireRelease(b *testing.B) {
	policy := semaphore.NewWeighted(1)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = policy.Acquire(ctx, 1)
		policy.Release(1)
	}
}

func BenchmarkFailsafeGoAcquireRelease(b *testing.B) {
	policy := failsafebulkhead.New[any](1)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = policy.AcquirePermit(ctx)
		policy.ReleasePermit()
	}
}

func mustBenchmarkPolicy(b *testing.B) *bulkhead.Bulkhead {
	b.Helper()
	policy, err := bulkhead.New(bulkhead.Config{Resource: "benchmark", Capacity: 1})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return policy
}
