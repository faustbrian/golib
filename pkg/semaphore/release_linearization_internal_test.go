package semaphore

import (
	"errors"
	"testing"
	"time"
)

func TestConcurrentDuplicateReleaseWaitsForAccounting(t *testing.T) {
	t.Parallel()

	sem, err := New(Config{Capacity: 1})
	if err != nil {
		t.Fatal(err)
	}
	permit, acquired, err := sem.TryAcquire(1)
	if err != nil || !acquired || permit == nil {
		t.Fatalf("TryAcquire() = %v, %t, %v", permit, acquired, err)
	}

	sem.mu.Lock()
	started := make(chan struct{}, 2)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			started <- struct{}{}
			results <- permit.Release()
		}()
	}
	<-started
	<-started
	select {
	case result := <-results:
		sem.mu.Unlock()
		t.Fatalf("Release() completed before capacity accounting: %v", result)
	case <-time.After(20 * time.Millisecond):
	}
	sem.mu.Unlock()

	first := <-results
	second := <-results
	valid := first == nil && errors.Is(second, ErrDuplicateRelease)
	if errors.Is(first, ErrDuplicateRelease) && second == nil {
		valid = true
	}
	if !valid {
		t.Fatalf("release results = %v, %v", first, second)
	}
	if snapshot := sem.Snapshot(); snapshot.Acquired != 0 || snapshot.Available != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}
