package semaphore_test

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/semaphore"
	xsemaphore "golang.org/x/sync/semaphore"
)

func TestGeneratedHistoriesMatchCapacityReferenceModel(t *testing.T) {
	t.Parallel()

	const capacity int64 = 9
	sem, err := semaphore.New(semaphore.Config{Capacity: capacity})
	if err != nil {
		t.Fatal(err)
	}
	var acquired int64
	var permits []*semaphore.Permit
	state := uint64(0x6a09e667f3bcc909)
	for range 2_000 {
		state = state*6364136223846793005 + 1442695040888963407
		if len(permits) != 0 && state&3 == 0 {
			index := int(state % uint64(len(permits)))
			permit := permits[index]
			if err := permit.Release(); err != nil {
				t.Fatal(err)
			}
			acquired -= permit.Weight()
			permits[index] = permits[len(permits)-1]
			permits = permits[:len(permits)-1]
		} else {
			weight := int64(state%5 + 1)
			permit, admitted, acquireErr := sem.TryAcquire(weight)
			wantAdmitted := capacity-acquired >= weight
			if acquireErr != nil || admitted != wantAdmitted || (permit != nil) != wantAdmitted {
				t.Fatalf("TryAcquire(%d) = %v, %t, %v; model acquired %d", weight, permit, admitted, acquireErr, acquired)
			}
			if admitted {
				acquired += weight
				permits = append(permits, permit)
			}
		}
		snapshot := sem.Snapshot()
		if snapshot.Acquired != acquired || snapshot.Available != capacity-acquired ||
			snapshot.Acquired+snapshot.Available != snapshot.Capacity {
			t.Fatalf("snapshot = %+v, model acquired = %d", snapshot, acquired)
		}
	}
	for _, permit := range permits {
		if err := permit.Release(); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot := sem.Snapshot(); snapshot.Acquired != 0 || snapshot.Available != capacity {
		t.Fatalf("drained snapshot = %+v", snapshot)
	}
}

func TestCloseReleaseRacesNeverStrandOwnership(t *testing.T) {
	t.Parallel()

	for range 100 {
		sem, err := semaphore.New(semaphore.Config{Capacity: 1, MaxWaiters: 1})
		if err != nil {
			t.Fatal(err)
		}
		held, err := sem.Acquire(testContext(t), 1)
		if err != nil {
			t.Fatal(err)
		}
		queued := make(chan struct {
			permit *semaphore.Permit
			err    error
		}, 1)
		acquireCtx := testContext(t)
		go func() {
			permit, acquireErr := sem.Acquire(acquireCtx, 1)
			queued <- struct {
				permit *semaphore.Permit
				err    error
			}{permit, acquireErr}
		}()
		waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })

		var group sync.WaitGroup
		group.Go(func() {
			if err := held.Release(); err != nil {
				t.Errorf("Release() error = %v", err)
			}
		})
		group.Go(func() {
			if err := sem.Close(); err != nil {
				t.Errorf("Close() error = %v", err)
			}
		})
		group.Wait()
		result := receive(t, queued)
		if result.err == nil {
			if result.permit == nil {
				t.Fatal("successful queued acquisition returned no permit")
			}
			if err := result.permit.Release(); err != nil {
				t.Fatal(err)
			}
		} else if result.permit != nil || !errors.Is(result.err, semaphore.ErrClosed) {
			t.Fatalf("queued result = %v, %v", result.permit, result.err)
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := sem.Wait(waitCtx); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
		if snapshot := sem.Snapshot(); !snapshot.Closed || snapshot.Acquired != 0 || snapshot.Waiters != 0 {
			t.Fatalf("final snapshot = %+v", snapshot)
		}
	}
}

func TestIntegerBoundsPreserveCapacity(t *testing.T) {
	t.Parallel()

	sem, err := semaphore.New(semaphore.Config{Capacity: math.MaxInt64, MaxWaiters: semaphore.MaxWaiters})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := sem.Acquire(testContext(t), math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := sem.Snapshot(); snapshot.Acquired != math.MaxInt64 || snapshot.Available != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestReferenceBehaviorDifferences(t *testing.T) {
	t.Parallel()

	ours, err := semaphore.New(semaphore.Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if permit, acquired, err := ours.TryAcquire(0); permit != nil || acquired || !errors.Is(err, semaphore.ErrInvalidWeight) {
		t.Fatalf("semaphore TryAcquire(0) = %v, %t, %v", permit, acquired, err)
	}

	reference := xsemaphore.NewWeighted(1)
	if !reference.TryAcquire(0) {
		t.Fatal("x/sync v0.22.0 rejected its documented zero-weight operation")
	}
	reference.Release(0)

	if permit, err := ours.Acquire(testContext(t), 2); permit != nil || !errors.Is(err, semaphore.ErrOversize) {
		t.Fatalf("semaphore Acquire(2) = %v, %v", permit, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reference.Acquire(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("x/sync oversized Acquire() error = %v", err)
	}
}
