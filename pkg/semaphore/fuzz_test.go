package semaphore_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/semaphore"
)

func FuzzConfigAndTryAcquire(fuzz *testing.F) {
	fuzz.Add(int64(1), int64(1), 0)
	fuzz.Add(int64(8), int64(9), 16)
	fuzz.Add(int64(0), int64(1), -1)

	fuzz.Fuzz(func(t *testing.T, capacity, weight int64, maxWaiters int) {
		sem, err := semaphore.New(semaphore.Config{Capacity: capacity, MaxWaiters: maxWaiters})
		if capacity <= 0 || maxWaiters < 0 || maxWaiters > semaphore.MaxWaiters {
			if sem != nil || !errors.Is(err, semaphore.ErrInvalidConfig) {
				t.Fatalf("New(%d, %d) = %v, %v", capacity, maxWaiters, sem, err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}

		permit, acquired, err := sem.TryAcquire(weight)
		switch {
		case weight <= 0:
			if permit != nil || acquired || !errors.Is(err, semaphore.ErrInvalidWeight) {
				t.Fatalf("TryAcquire(%d) = %v, %t, %v", weight, permit, acquired, err)
			}
		case weight > capacity:
			if permit != nil || acquired || !errors.Is(err, semaphore.ErrOversize) {
				t.Fatalf("TryAcquire(%d) = %v, %t, %v", weight, permit, acquired, err)
			}
		default:
			if err != nil || !acquired || permit == nil {
				t.Fatalf("TryAcquire(%d) = %v, %t, %v", weight, permit, acquired, err)
			}
			if snapshot := sem.Snapshot(); snapshot.Acquired != weight || snapshot.Available != capacity-weight {
				t.Fatalf("acquired snapshot = %+v", snapshot)
			}
			if err := permit.Release(); err != nil {
				t.Fatal(err)
			}
			if snapshot := sem.Snapshot(); snapshot.Acquired != 0 || snapshot.Available != capacity {
				t.Fatalf("released snapshot = %+v", snapshot)
			}
		}
	})
}
