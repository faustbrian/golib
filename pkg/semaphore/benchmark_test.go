package semaphore_test

import (
	"context"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/semaphore"
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
			sem.acquire()
			sem.release()
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
	sem, err := semaphore.New(semaphore.Config{Capacity: 8, MaxWaiters: semaphore.MaxWaiters})
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
}

func BenchmarkMixedWeights(b *testing.B) {
	sem, err := semaphore.New(semaphore.Config{Capacity: 16})
	if err != nil {
		b.Fatal(err)
	}
	weights := [...]int64{1, 2, 4, 8}
	b.ReportAllocs()
	for index := 0; b.Loop(); index++ {
		permit, acquired, acquireErr := sem.TryAcquire(weights[index%len(weights)])
		if acquireErr != nil || !acquired || permit == nil {
			b.Fatalf("TryAcquire() = %v, %t, %v", permit, acquired, acquireErr)
		}
		if err := permit.Release(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanceledAcquire(b *testing.B) {
	sem, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		b.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := sem.Acquire(ctx, 1); err == nil {
			b.Fatal("Acquire() admitted canceled context")
		}
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

func (sem *benchmarkCondSemaphore) acquire() {
	sem.mutex.Lock()
	for sem.available == 0 {
		sem.condition.Wait()
	}
	sem.available--
	sem.mutex.Unlock()
}

func (sem *benchmarkCondSemaphore) release() {
	sem.mutex.Lock()
	sem.available++
	sem.condition.Signal()
	sem.mutex.Unlock()
}
