package bulkhead_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

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

func BenchmarkRejectedFastPath(b *testing.B) {
	b.Run("Bulkhead", func(b *testing.B) {
		policy := mustBenchmarkPolicy(b)
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

}

func BenchmarkBulkheadParallelThroughput(b *testing.B) {
	policy, err := bulkhead.New(bulkhead.Config{Resource: "benchmark", Capacity: int64(runtime.GOMAXPROCS(0))})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	var admitted atomic.Uint64
	var rejected atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			permit, acquireErr := policy.Acquire(ctx, 1)
			if acquireErr != nil {
				rejected.Add(1)
				continue
			}
			admitted.Add(1)
			_ = permit.Release()
		}
	})
	b.ReportMetric(float64(admitted.Load())/b.Elapsed().Seconds(), "admitted/s")
	b.ReportMetric(float64(rejected.Load())/float64(b.N), "rejected/op")
}

func BenchmarkBulkheadWaitWakeupLatency(b *testing.B) {
	policy, err := bulkhead.New(bulkhead.Config{
		Resource:  "benchmark",
		Capacity:  1,
		Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: time.Second},
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		b.Fatalf("holder Acquire() error = %v", err)
	}
	samples := make([]time.Duration, 0, b.N)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result := make(chan acquireResult, 1)
		go func() {
			permit, acquireErr := policy.Acquire(ctx, 1)
			result <- acquireResult{permit: permit, err: acquireErr}
		}()
		benchmarkWaitForQueueDepth(b, policy, 1)
		started := time.Now()
		if err := holder.Release(); err != nil {
			b.Fatalf("holder Release() error = %v", err)
		}
		acquired := <-result
		if acquired.err != nil {
			b.Fatalf("waiter Acquire() error = %v", acquired.err)
		}
		samples = append(samples, time.Since(started))
		holder = acquired.permit
	}
	b.StopTimer()
	_ = holder.Release()
	reportWaitPercentiles(b, samples)
}

func BenchmarkBulkheadCancellationChurn(b *testing.B) {
	policy, err := bulkhead.New(bulkhead.Config{
		Resource:  "benchmark",
		Capacity:  1,
		Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: time.Second},
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		b.Fatalf("holder Acquire() error = %v", err)
	}
	defer func() { _ = holder.Release() }()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, acquireErr := policy.Acquire(ctx, 1)
			result <- acquireErr
		}()
		benchmarkWaitForQueueDepth(b, policy, 1)
		cancel()
		if err := <-result; !errors.Is(err, bulkhead.ErrCallerCanceled) ||
			!errors.Is(err, context.Canceled) {
			b.Fatalf("canceled waiter error = %v", err)
		}
	}
}

func BenchmarkBulkheadFIFOFairness(b *testing.B) {
	const waiters = 8
	var violations uint64
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		policy, err := bulkhead.New(bulkhead.Config{
			Resource:  "benchmark",
			Capacity:  1,
			Admission: bulkhead.Wait{MaxQueued: waiters, MaxWait: time.Second},
		})
		if err != nil {
			b.Fatalf("New() error = %v", err)
		}
		holder, err := policy.Acquire(context.Background(), 1)
		if err != nil {
			b.Fatalf("holder Acquire() error = %v", err)
		}
		order := make(chan int, waiters)
		results := make([]<-chan error, 0, waiters)
		for index := range waiters {
			index := index
			result := make(chan error, 1)
			results = append(results, result)
			go func() {
				_, _, executeErr := bulkhead.Execute(context.Background(), policy, 1, func(context.Context) (struct{}, error) {
					order <- index
					return struct{}{}, nil
				})
				result <- executeErr
			}()
			benchmarkWaitForQueueDepth(b, policy, index+1)
		}
		if err := holder.Release(); err != nil {
			b.Fatalf("holder Release() error = %v", err)
		}
		for expected := range waiters {
			if actual := <-order; actual != expected {
				violations++
			}
		}
		for _, result := range results {
			if err := <-result; err != nil {
				b.Fatalf("waiter Execute() error = %v", err)
			}
		}
	}
	b.ReportMetric(float64(violations)/float64(b.N), "fifo-violations/op")
}

func BenchmarkBulkheadObserver(b *testing.B) {
	var observations atomic.Uint64
	policy, err := bulkhead.New(bulkhead.Config{
		Resource: "benchmark",
		Capacity: 1,
		Observer: bulkhead.ObserveFunc(func(bulkhead.Event) error {
			observations.Add(1)
			return nil
		}),
	})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		permit, acquireErr := policy.Acquire(ctx, 1)
		if acquireErr != nil {
			b.Fatalf("Acquire() error = %v", acquireErr)
		}
		_ = permit.Release()
	}
	b.StopTimer()
	if got := observations.Load(); got != uint64(2*b.N) {
		b.Fatalf("observation count = %d, want %d", got, 2*b.N)
	}
}

func BenchmarkBulkheadPartitions(b *testing.B) {
	const partitions = 128
	registry, err := bulkhead.NewRegistry(bulkhead.FixedPartitions{Maximum: partitions})
	if err != nil {
		b.Fatalf("NewRegistry() error = %v", err)
	}
	resources := make([]string, 0, partitions)
	for index := range partitions {
		resource := fmt.Sprintf("partition-%d", index)
		if _, err := registry.Create(bulkhead.Config{Resource: resource, Capacity: 1}); err != nil {
			b.Fatalf("Create(%q) error = %v", resource, err)
		}
		resources = append(resources, resource)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := range b.N {
		if _, err := registry.Lookup(resources[index%len(resources)]); err != nil {
			b.Fatalf("Lookup() error = %v", err)
		}
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

func benchmarkWaitForQueueDepth(b *testing.B, policy *bulkhead.Bulkhead, want int) {
	b.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if policy.Snapshot().QueueDepth == want {
			return
		}
		runtime.Gosched()
	}
	b.Fatalf("QueueDepth did not reach %d; snapshot = %+v", want, policy.Snapshot())
}

func reportWaitPercentiles(b *testing.B, samples []time.Duration) {
	b.Helper()
	if len(samples) == 0 {
		return
	}
	slices.Sort(samples)
	percentile := func(numerator int) time.Duration {
		index := (len(samples)*numerator + 99) / 100
		return samples[max(index-1, 0)]
	}
	b.ReportMetric(float64(percentile(50).Nanoseconds()), "p50-wait-ns")
	b.ReportMetric(float64(percentile(95).Nanoseconds()), "p95-wait-ns")
	b.ReportMetric(float64(percentile(99).Nanoseconds()), "p99-wait-ns")
}
