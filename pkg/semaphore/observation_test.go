package semaphore_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/semaphore"
)

func TestObserverMayReenterSnapshotAndClose(t *testing.T) {
	t.Parallel()

	var sem *semaphore.Semaphore
	var closeOnce sync.Once
	observer := semaphore.ObserverFunc(func(event semaphore.Event) {
		if sem == nil {
			t.Fatal("observer called before construction completed")
		}
		_ = sem.Snapshot()
		if event.Kind == semaphore.EventAdmitted {
			closeOnce.Do(func() {
				if err := sem.Close(); err != nil {
					t.Errorf("reentrant Close() error = %v", err)
				}
			})
		}
	})
	var err error
	sem, err = semaphore.New(semaphore.Config{Capacity: 1, Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot := sem.Snapshot(); !snapshot.Closed || snapshot.Acquired != 1 {
		t.Fatalf("reentrant snapshot = %+v", snapshot)
	}
	if err := permit.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestObserverReportsQueuedCancellationAndClosedRejection(t *testing.T) {
	t.Parallel()

	var mutex sync.Mutex
	var events []semaphore.Event
	sem, err := semaphore.New(semaphore.Config{
		Capacity:   1,
		MaxWaiters: 1,
		Observer: semaphore.ObserverFunc(func(event semaphore.Event) {
			mutex.Lock()
			events = append(events, event)
			mutex.Unlock()
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	held, err := sem.Acquire(testContext(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, acquireErr := sem.Acquire(ctx, 1)
		result <- acquireErr
	}()
	waitForSnapshot(t, sem, func(snapshot semaphore.Snapshot) bool { return snapshot.Waiters == 1 })
	cancel()
	if err := receive(t, result); !errors.Is(err, semaphore.ErrCanceled) {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := sem.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := sem.Acquire(testContext(t), 1); !errors.Is(err, semaphore.ErrClosed) {
		t.Fatalf("closed Acquire() error = %v", err)
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}

	mutex.Lock()
	defer mutex.Unlock()
	want := map[[2]string]bool{
		{string(semaphore.EventQueued), string(semaphore.ReasonFIFO)}:              false,
		{string(semaphore.EventCanceled), string(semaphore.ReasonContextCanceled)}: false,
		{string(semaphore.EventClosed), string(semaphore.ReasonShutdown)}:          false,
		{string(semaphore.EventRejected), string(semaphore.ReasonClosed)}:          false,
	}
	for _, event := range events {
		key := [2]string{string(event.Kind), string(event.Reason)}
		if _, exists := want[key]; exists {
			want[key] = true
		}
	}
	for transition, observed := range want {
		if !observed {
			t.Errorf("transition %v missing from %+v", transition, events)
		}
	}
}
