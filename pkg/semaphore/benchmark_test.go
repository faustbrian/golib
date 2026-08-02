package semaphore_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/semaphore"
	kitsemaphore "github.com/v8fg/kit4go/semaphore"
	xsemaphore "golang.org/x/sync/semaphore"
)

func BenchmarkUncontendedWeightedAcquire(b *testing.B) {
	b.Run("semaphore", func(b *testing.B) {
		sem, err := semaphore.New(semaphore.Config{Capacity: 1})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			permit, acquired, acquireErr := sem.TryAcquire(1)
			if acquireErr != nil || !acquired {
				b.Fatalf("TryAcquire() = %t, %v", acquired, acquireErr)
			}
			if err := permit.Release(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("x-sync-v0.22.0", func(b *testing.B) {
		sem := xsemaphore.NewWeighted(1)
		b.ReportAllocs()
		for b.Loop() {
			if !sem.TryAcquire(1) {
				b.Fatal("TryAcquire() rejected")
			}
			sem.Release(1)
		}
	})

	b.Run("buffered-channel", func(b *testing.B) {
		permits := make(chan struct{}, 1)
		b.ReportAllocs()
		for b.Loop() {
			permits <- struct{}{}
			<-permits
		}
	})

	b.Run("sync-cond", func(b *testing.B) {
		sem := newBenchmarkCondSemaphore(1)
		b.ReportAllocs()
		for b.Loop() {
			sem.acquire(1)
			sem.release(1)
		}
	})

	b.Run("kit4go-v0.9.0", func(b *testing.B) {
		sem := kitsemaphore.New(1)
		b.ReportAllocs()
		for b.Loop() {
			if !sem.TryAcquire(1) {
				b.Fatal("TryAcquire() rejected")
			}
			sem.Release(1)
		}
	})

	b.Run("semaphore-observer", func(b *testing.B) {
		sem, err := semaphore.New(semaphore.Config{
			Capacity: 1,
			Observer: semaphore.ObserverFunc(func(semaphore.Event) {}),
		})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			permit, acquired, acquireErr := sem.TryAcquire(1)
			if acquireErr != nil || !acquired {
				b.Fatalf("TryAcquire() = %t, %v", acquired, acquireErr)
			}
			if err := permit.Release(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkContendedWeightedAcquire(b *testing.B) {
	const capacity = 2

	b.Run("semaphore-fifo-bounded", func(b *testing.B) {
		sem, err := semaphore.New(semaphore.Config{Capacity: capacity, MaxWaiters: semaphore.MaxWaiters})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.RunParallel(func(parallel *testing.PB) {
			for parallel.Next() {
				permit, acquireErr := sem.Acquire(context.Background(), 1)
				if acquireErr != nil {
					b.Fatal(acquireErr)
				}
				if err := permit.Release(); err != nil {
					b.Fatal(err)
				}
			}
		})
	})

	b.Run("x-sync-v0.22.0-fifo-unbounded", func(b *testing.B) {
		sem := xsemaphore.NewWeighted(capacity)
		b.ReportAllocs()
		b.RunParallel(func(parallel *testing.PB) {
			for parallel.Next() {
				if err := sem.Acquire(context.Background(), 1); err != nil {
					b.Fatal(err)
				}
				sem.Release(1)
			}
		})
	})

	b.Run("buffered-channel-unweighted", func(b *testing.B) {
		permits := make(chan struct{}, capacity)
		b.ReportAllocs()
		b.RunParallel(func(parallel *testing.PB) {
			for parallel.Next() {
				permits <- struct{}{}
				<-permits
			}
		})
	})

	b.Run("sync-cond-unweighted-unfair", func(b *testing.B) {
		sem := newBenchmarkCondSemaphore(capacity)
		b.ReportAllocs()
		b.RunParallel(func(parallel *testing.PB) {
			for parallel.Next() {
				sem.acquire(1)
				sem.release(1)
			}
		})
	})

	b.Run("kit4go-v0.9.0-unfair", func(b *testing.B) {
		sem := kitsemaphore.New(capacity)
		b.ReportAllocs()
		b.RunParallel(func(parallel *testing.PB) {
			for parallel.Next() {
				if err := sem.Acquire(context.Background(), 1); err != nil {
					b.Fatal(err)
				}
				sem.Release(1)
			}
		})
	})
}

func BenchmarkMixedWeights(b *testing.B) {
	weights := [...]int64{1, 2, 4, 8}

	b.Run("semaphore", func(b *testing.B) {
		sem, err := semaphore.New(semaphore.Config{Capacity: 16})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for index := 0; b.Loop(); index++ {
			weight := weights[index%len(weights)]
			permit, acquired, acquireErr := sem.TryAcquire(weight)
			if acquireErr != nil || !acquired || permit == nil {
				b.Fatalf("TryAcquire() = %v, %t, %v", permit, acquired, acquireErr)
			}
			if err := permit.Release(); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("x-sync-v0.22.0", func(b *testing.B) {
		sem := xsemaphore.NewWeighted(16)
		b.ReportAllocs()
		for index := 0; b.Loop(); index++ {
			weight := weights[index%len(weights)]
			if !sem.TryAcquire(weight) {
				b.Fatal("TryAcquire() rejected")
			}
			sem.Release(weight)
		}
	})

	b.Run("kit4go-v0.9.0", func(b *testing.B) {
		sem := kitsemaphore.New(16)
		b.ReportAllocs()
		for index := 0; b.Loop(); index++ {
			weight := int(weights[index%len(weights)])
			if !sem.TryAcquire(weight) {
				b.Fatal("TryAcquire() rejected")
			}
			sem.Release(weight)
		}
	})
}

func BenchmarkCanceledAcquire(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b.Run("semaphore", func(b *testing.B) {
		sem, err := semaphore.New(semaphore.Config{Capacity: 1})
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := sem.Acquire(ctx, 1); err == nil {
				b.Fatal("Acquire() admitted canceled context")
			}
		}
	})

	b.Run("x-sync-v0.22.0", func(b *testing.B) {
		sem := xsemaphore.NewWeighted(1)
		if err := sem.Acquire(context.Background(), 1); err != nil {
			b.Fatal(err)
		}
		defer sem.Release(1)
		b.ReportAllocs()
		for b.Loop() {
			if err := sem.Acquire(ctx, 1); err == nil {
				b.Fatal("Acquire() admitted canceled context")
			}
		}
	})

	b.Run("kit4go-v0.9.0", func(b *testing.B) {
		sem := kitsemaphore.New(1)
		if err := sem.Acquire(context.Background(), 1); err != nil {
			b.Fatal(err)
		}
		defer sem.Release(1)
		b.ReportAllocs()
		for b.Loop() {
			if err := sem.Acquire(ctx, 1); err == nil {
				b.Fatal("Acquire() admitted canceled context")
			}
		}
	})
}

func BenchmarkCancellationQueueDepth(b *testing.B) {
	for _, depth := range []int{1, 32, 256} {
		b.Run(fmt.Sprintf("depth-%d", depth), func(b *testing.B) {
			for b.Loop() {
				sem, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: depth})
				if err != nil {
					b.Fatal(err)
				}
				held, err := sem.Acquire(context.Background(), 1)
				if err != nil {
					b.Fatal(err)
				}
				contexts := make([]context.CancelFunc, depth)
				results := make(chan error, depth)
				for index := range depth {
					ctx, cancel := context.WithCancel(context.Background())
					contexts[index] = cancel
					go func() {
						_, acquireErr := sem.Acquire(ctx, 1)
						results <- acquireErr
					}()
				}
				waitForBenchmarkQueue(b, sem, depth)
				for _, cancel := range contexts {
					cancel()
				}
				for range depth {
					if err := <-results; err == nil {
						b.Fatal("queued acquisition unexpectedly succeeded")
					}
				}
				if err := held.Release(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkStrictFIFOHeadOfLine(b *testing.B) {
	b.Run("semaphore-bounded-owned", func(b *testing.B) {
		for b.Loop() {
			sem, err := semaphore.New(semaphore.Config{Capacity: 8, MaxWaiters: 8})
			if err != nil {
				b.Fatal(err)
			}
			held, err := sem.Acquire(context.Background(), 8)
			if err != nil {
				b.Fatal(err)
			}
			head := make(chan *semaphore.Permit, 1)
			go func() {
				permit, acquireErr := sem.Acquire(context.Background(), 8)
				if acquireErr != nil {
					b.Error(acquireErr)
				}
				head <- permit
			}()
			waitForBenchmarkQueue(b, sem, 1)
			followers := make(chan *semaphore.Permit, 7)
			for index := 1; index <= 7; index++ {
				go func() {
					permit, acquireErr := sem.Acquire(context.Background(), 1)
					if acquireErr != nil {
						b.Error(acquireErr)
					}
					followers <- permit
				}()
				waitForBenchmarkQueue(b, sem, index+1)
			}
			if err := held.Release(); err != nil {
				b.Fatal(err)
			}
			headPermit := <-head
			if err := headPermit.Release(); err != nil {
				b.Fatal(err)
			}
			for range 7 {
				if err := (<-followers).Release(); err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("x-sync-v0.22.0-unbounded-caller-release", func(b *testing.B) {
		for b.Loop() {
			sem := xsemaphore.NewWeighted(8)
			if err := sem.Acquire(context.Background(), 8); err != nil {
				b.Fatal(err)
			}
			type result struct {
				weight int64
				err    error
			}
			results := make(chan result, 8)
			go func() { results <- result{weight: 8, err: sem.Acquire(context.Background(), 8)} }()
			for range 16 {
				runtime.Gosched()
			}
			for range 7 {
				go func() { results <- result{weight: 1, err: sem.Acquire(context.Background(), 1)} }()
				runtime.Gosched()
			}
			sem.Release(8)
			for range 8 {
				result := <-results
				if result.err != nil {
					b.Fatal(result.err)
				}
				sem.Release(result.weight)
			}
		}
	})
}

func waitForBenchmarkQueue(b *testing.B, sem *semaphore.Semaphore, depth int) {
	b.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for sem.Snapshot().Waiters != depth {
		if time.Now().After(deadline) {
			b.Fatalf("timed out waiting for queue depth %d: %+v", depth, sem.Snapshot())
		}
		runtime.Gosched()
	}
}

type benchmarkCondSemaphore struct {
	mutex     sync.Mutex
	condition *sync.Cond
	available int
}

func newBenchmarkCondSemaphore(capacity int) *benchmarkCondSemaphore {
	sem := &benchmarkCondSemaphore{available: capacity}
	sem.condition = sync.NewCond(&sem.mutex)
	return sem
}

func (sem *benchmarkCondSemaphore) acquire(weight int) {
	sem.mutex.Lock()
	for sem.available < weight {
		sem.condition.Wait()
	}
	sem.available -= weight
	sem.mutex.Unlock()
}

func (sem *benchmarkCondSemaphore) release(weight int) {
	sem.mutex.Lock()
	sem.available += weight
	sem.condition.Signal()
	sem.mutex.Unlock()
}
