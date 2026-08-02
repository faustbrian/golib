package bulkhead_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestRandomizedHistoriesConserveCapacity(t *testing.T) {
	for seed := uint64(1); seed <= 100; seed++ {
		random := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		policy := mustPolicy(t, bulkhead.Config{
			Resource: "database",
			Capacity: 5,
		})
		var permits []*bulkhead.Permit
		var active int64
		for step := 0; step < 1_000; step++ {
			if len(permits) != 0 && random.IntN(3) == 0 {
				index := random.IntN(len(permits))
				permit := permits[index]
				if err := permit.Release(); err != nil {
					t.Fatalf("seed %d step %d Release() error = %v", seed, step, err)
				}
				active -= permit.Weight()
				if err := permit.Release(); !errors.Is(err, bulkhead.ErrPermitReleased) {
					t.Fatalf("seed %d step %d duplicate Release() error = %v", seed, step, err)
				}
				permits[index] = permits[len(permits)-1]
				permits = permits[:len(permits)-1]
			} else {
				weight := int64(random.IntN(5) + 1)
				permit, err := policy.Acquire(context.Background(), weight)
				if active+weight <= 5 {
					if err != nil {
						t.Fatalf("seed %d step %d Acquire(%d) error = %v", seed, step, weight, err)
					}
					permits = append(permits, permit)
					active += weight
				} else if !errors.Is(err, bulkhead.ErrRejected) {
					t.Fatalf("seed %d step %d Acquire(%d) error = %v, want ErrRejected", seed, step, weight, err)
				}
			}
			snapshot := policy.Snapshot()
			if snapshot.ActiveWeight != active || snapshot.ActiveWeight+snapshot.AvailableWeight != snapshot.Capacity {
				t.Fatalf("seed %d step %d model = %d snapshot = %+v", seed, step, active, snapshot)
			}
		}
		for _, permit := range permits {
			_ = permit.Release()
		}
	}
}

func TestConcurrentAdmissionCancellationReleaseAndClose(t *testing.T) {
	const workers = 32
	const attempts = 200

	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "database",
		Capacity:  8,
		Admission: bulkhead.Wait{MaxQueued: workers, MaxWait: 20 * time.Millisecond},
	})
	var admitted atomic.Uint64
	var unexpected atomic.Pointer[concurrentError]
	start := make(chan struct{})
	var group sync.WaitGroup
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			random := rand.New(rand.NewPCG(uint64(worker+1), uint64(worker+101)))
			<-start
			for range attempts {
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(random.IntN(100)+1)*time.Microsecond)
				permit, err := policy.Acquire(ctx, int64(random.IntN(3)+1))
				cancel()
				if err != nil {
					if !errors.Is(err, bulkhead.ErrCallerCanceled) &&
						!errors.Is(err, bulkhead.ErrWaitTimeout) &&
						!errors.Is(err, bulkhead.ErrQueueFull) &&
						!errors.Is(err, bulkhead.ErrClosed) {
						unexpected.CompareAndSwap(nil, &concurrentError{err: err})
						return
					}
					continue
				}
				admitted.Add(1)
				runtime.Gosched()
				if err := permit.Release(); err != nil {
					unexpected.CompareAndSwap(nil, &concurrentError{err: err})
					return
				}
			}
		}()
	}
	close(start)
	time.AfterFunc(5*time.Millisecond, func() { _ = policy.Close() })
	group.Wait()
	if failure := unexpected.Load(); failure != nil {
		t.Fatalf("concurrent history error = %v", failure.err)
	}
	if err := drainWithin(policy); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	snapshot := policy.Snapshot()
	if snapshot.ActiveWeight != 0 || snapshot.QueueDepth != 0 ||
		snapshot.AvailableWeight != snapshot.Capacity || snapshot.Admissions != admitted.Load() {
		t.Fatalf("final Snapshot() = %+v, admitted = %d", snapshot, admitted.Load())
	}
}

type concurrentError struct {
	err error
}

func TestConcurrentDuplicateReleaseChangesCapacityOnce(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
	permit, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	var successes atomic.Int64
	var duplicates atomic.Int64
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			switch err := permit.Release(); {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, bulkhead.ErrPermitReleased):
				duplicates.Add(1)
			default:
				t.Errorf("Release() error = %v", err)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 || duplicates.Load() != 99 {
		t.Fatalf("release results = %d successes, %d duplicates", successes.Load(), duplicates.Load())
	}
	if got := policy.Snapshot(); got.ActiveWeight != 0 || got.AvailableWeight != 1 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}
