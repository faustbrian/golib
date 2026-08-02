package comparison_test

import (
	"context"
	"testing"

	failsafebulkhead "github.com/failsafe-go/failsafe-go/bulkhead"
	"github.com/faustbrian/golib/pkg/bulkhead"
	fortifybulkhead "go.klarlabs.de/fortify/bulkhead"
	"golang.org/x/sync/semaphore"
)

func BenchmarkAdmittedFastPath(b *testing.B) {
	b.Run("Bulkhead", func(b *testing.B) {
		policy := mustPolicy(b)
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
	})

	b.Run("XSyncSemaphore", func(b *testing.B) {
		policy := semaphore.NewWeighted(1)
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := policy.Acquire(ctx, 1); err != nil {
				b.Fatalf("Acquire() error = %v", err)
			}
			policy.Release(1)
		}
	})

	b.Run("FailsafeGo", func(b *testing.B) {
		policy := failsafebulkhead.New[any](1)
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := policy.AcquirePermit(ctx); err != nil {
				b.Fatalf("AcquirePermit() error = %v", err)
			}
			policy.ReleasePermit()
		}
	})

	b.Run("Fortify", func(b *testing.B) {
		policy := fortifybulkhead.New[struct{}](fortifybulkhead.Config{MaxConcurrent: 1})
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, err := policy.Execute(ctx, func(context.Context) (struct{}, error) { return struct{}{}, nil })
			if err != nil {
				b.Fatalf("Execute() error = %v", err)
			}
		}
	})
}

func BenchmarkRejectedFastPath(b *testing.B) {
	b.Run("Bulkhead", func(b *testing.B) {
		policy := mustPolicy(b)
		holder, err := policy.Acquire(context.Background(), 1)
		if err != nil {
			b.Fatalf("holder Acquire() error = %v", err)
		}
		defer func() { _ = holder.Release() }()
		ctx := context.Background()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = policy.Acquire(ctx, 1)
		}
	})

	b.Run("XSyncTryAcquire", func(b *testing.B) {
		policy := semaphore.NewWeighted(1)
		if !policy.TryAcquire(1) {
			b.Fatal("holder TryAcquire() failed")
		}
		defer policy.Release(1)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if policy.TryAcquire(1) {
				policy.Release(1)
				b.Fatal("saturated TryAcquire() succeeded")
			}
		}
	})

	b.Run("FailsafeGo", func(b *testing.B) {
		policy := failsafebulkhead.New[any](1)
		if !policy.TryAcquirePermit() {
			b.Fatal("holder TryAcquirePermit() failed")
		}
		defer policy.ReleasePermit()
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if policy.TryAcquirePermit() {
				policy.ReleasePermit()
				b.Fatal("saturated TryAcquirePermit() succeeded")
			}
		}
	})

	b.Run("Fortify", func(b *testing.B) {
		policy := fortifybulkhead.New[struct{}](fortifybulkhead.Config{MaxConcurrent: 1})
		started := make(chan struct{})
		finish := make(chan struct{})
		completed := make(chan error, 1)
		go func() {
			_, err := policy.Execute(context.Background(), func(context.Context) (struct{}, error) {
				close(started)
				<-finish
				return struct{}{}, nil
			})
			completed <- err
		}()
		<-started
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = policy.Execute(context.Background(), func(context.Context) (struct{}, error) {
				b.Fatal("saturated Fortify bulkhead invoked protected work")
				return struct{}{}, nil
			})
		}
		b.StopTimer()
		close(finish)
		if err := <-completed; err != nil {
			b.Fatalf("holder Execute() error = %v", err)
		}
	})
}

func mustPolicy(b *testing.B) *bulkhead.Bulkhead {
	b.Helper()
	policy, err := bulkhead.New(bulkhead.Config{Resource: "benchmark", Capacity: 1})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	return policy
}
