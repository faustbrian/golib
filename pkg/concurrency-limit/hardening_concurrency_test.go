package concurrencylimit_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestFIFOAdmissionPreventsStarvationAcrossMetadataAndDurations(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(0, 0)}
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm:   concurrencylimit.NewFixedAlgorithm(),
		Clock:       clock,
		MaxPriority: 10,
		Partitions:  []string{"interactive", "batch"},
		Queue:       concurrencylimit.QueueConfig{MaxQueued: 12, MaxWait: time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	type admission struct {
		index  int
		permit *concurrencylimit.Permit
		err    error
	}
	const waiters = 12
	results := make([]chan admission, waiters)
	for index := range waiters {
		results[index] = make(chan admission, 1)
		metadata := concurrencylimit.Metadata{Priority: (index * 7) % 11, Partition: "interactive"}
		if index%2 == 1 {
			metadata.Partition = "batch"
		}
		go func() {
			permit, acquireErr := limiter.Acquire(context.Background(), metadata)
			results[index] <- admission{index: index, permit: permit, err: acquireErr}
		}()
		waitForQueued(t, limiter, index+1)
	}
	if err = active.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatal(err)
	}

	for index := range waiters {
		var admitted admission
		select {
		case admitted = <-results[index]:
		case <-time.After(time.Second):
			t.Fatalf("waiter %d starved", index)
		}
		if admitted.err != nil || admitted.permit == nil || admitted.index != index {
			t.Fatalf("waiter %d admission = %+v", index, admitted)
		}
		wantPartition := "interactive"
		if index%2 == 1 {
			wantPartition = "batch"
		}
		if admitted.permit.Metadata().Partition != wantPartition ||
			admitted.permit.Metadata().Priority != (index*7)%11 {
			t.Fatalf("waiter %d metadata = %+v", index, admitted.permit.Metadata())
		}
		clock.Advance(time.Duration(1+index%4) * time.Millisecond)
		if err = admitted.permit.Complete(concurrencylimit.OutcomeSuccess); err != nil {
			t.Fatalf("waiter %d completion = %v", index, err)
		}
	}
	if snapshot := limiter.Snapshot(); snapshot.InFlight != 0 || snapshot.Queued != 0 || snapshot.Outcomes.Success != waiters+1 {
		t.Fatalf("final fairness snapshot = %+v", snapshot)
	}
}

func TestCompletionCancellationTimeoutResetSnapshotDrainRaceMatrix(t *testing.T) {
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 8, InitialLimit: 4,
		Algorithm: concurrencylimit.NewDefaultAlgorithm(),
		Queue:     concurrencylimit.QueueConfig{MaxQueued: 32, MaxWait: 2 * time.Millisecond},
		PermitTTL: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	const workers = 16
	const iterations = 150
	var group sync.WaitGroup
	group.Add(workers)
	for worker := range workers {
		go func() {
			defer group.Done()
			for iteration := range iterations {
				switch (worker + iteration) % 7 {
				case 0:
					ctx, cancel := context.WithTimeout(context.Background(), 4*time.Millisecond)
					permit, acquireErr := limiter.Acquire(ctx)
					cancel()
					if acquireErr != nil {
						if !expectedConcurrentAcquireError(acquireErr) {
							t.Errorf("Acquire() error = %v", acquireErr)
							return
						}
						continue
					}
					completionErr := permit.Complete(concurrencylimit.Outcome(iteration % 5))
					if completionErr != nil && !errors.Is(completionErr, concurrencylimit.ErrStalePermit) {
						t.Errorf("Complete() error = %v", completionErr)
						return
					}
					if duplicateErr := permit.Complete(concurrencylimit.OutcomeSuccess); !errors.Is(duplicateErr, concurrencylimit.ErrPermitCompleted) {
						t.Errorf("duplicate Complete() error = %v", duplicateErr)
						return
					}
				case 1:
					_ = limiter.Snapshot()
				case 2:
					limiter.Reset()
				case 3:
					limiter.BeginDrain()
				case 4:
					_ = limiter.ReapExpired()
				case 5:
					ctx, cancel := context.WithCancel(context.Background())
					cancel()
					_, acquireErr := limiter.Acquire(ctx)
					if !errors.Is(acquireErr, context.Canceled) {
						t.Errorf("canceled Acquire() error = %v", acquireErr)
						return
					}
				case 6:
					limiter.Reset()
					_ = limiter.Snapshot()
				}
			}
		}()
	}
	group.Wait()
	limiter.Reset()
	snapshot := limiter.Snapshot()
	if snapshot.Limit != 4 || snapshot.InFlight != 0 || snapshot.Queued != 0 || snapshot.Draining {
		t.Fatalf("final race snapshot = %+v", snapshot)
	}
}

func expectedConcurrentAcquireError(err error) bool {
	return errors.Is(err, concurrencylimit.ErrDraining) ||
		errors.Is(err, concurrencylimit.ErrReset) ||
		errors.Is(err, concurrencylimit.ErrLimitExceeded) ||
		errors.Is(err, concurrencylimit.ErrQueueFull) ||
		errors.Is(err, concurrencylimit.ErrQueueTimeout) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}
