package concurrencylimit_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestExecuteClassifiesResultsAndExcludesQueueWait(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(0, 0)}
	var mu sync.Mutex
	var classified time.Duration
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm: concurrencylimit.NewFixedAlgorithm(), Clock: clock,
		Queue: concurrencylimit.QueueConfig{MaxQueued: 1, MaxWait: time.Second},
		Classifier: func(completion concurrencylimit.Completion) concurrencylimit.Outcome {
			mu.Lock()
			classified = completion.Duration
			mu.Unlock()
			return concurrencylimit.OutcomeSuccess
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	result := make(chan error, 1)
	go func() {
		value, executeErr := concurrencylimit.Execute(context.Background(), limiter, func(context.Context) (int, error) {
			clock.Advance(10 * time.Millisecond)
			return 42, nil
		})
		if value != 42 && executeErr == nil {
			executeErr = errors.New("unexpected result")
		}
		result <- executeErr
	}()
	waitForQueued(t, limiter, 1)
	clock.Advance(100 * time.Millisecond)
	if err = first.Complete(concurrencylimit.OutcomeSuccess); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err = <-result; err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	mu.Lock()
	duration := classified
	mu.Unlock()
	if duration != 10*time.Millisecond {
		t.Fatalf("classifier duration = %s, want execution-only 10ms", duration)
	}
}

func TestExecuteContainsClassifierPanicAndOperationPanicReleasesPermit(t *testing.T) {
	t.Parallel()

	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm:  concurrencylimit.NewFixedAlgorithm(),
		Classifier: func(concurrencylimit.Completion) concurrencylimit.Outcome { panic("classifier") },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err = concurrencylimit.Execute(context.Background(), limiter, func(context.Context) (int, error) { return 0, nil }); !errors.Is(err, concurrencylimit.ErrClassifierPanic) {
		t.Fatalf("Execute() error = %v, want ErrClassifierPanic", err)
	}
	if snapshot := limiter.Snapshot(); snapshot.InFlight != 0 || snapshot.Outcomes.Ignored != 1 || snapshot.ClassifierPanics != 1 {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}

	defaultLimiter := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{})
	func() {
		defer func() {
			if recover() != "operation" {
				t.Fatal("Execute() did not re-panic operation value")
			}
		}()
		_, _ = concurrencylimit.Execute(context.Background(), defaultLimiter, func(context.Context) (int, error) {
			panic("operation")
		})
	}()
	if snapshot := defaultLimiter.Snapshot(); snapshot.InFlight != 0 || snapshot.Outcomes.DependencyFailure != 1 {
		t.Fatalf("panic Snapshot() = %+v", snapshot)
	}
}

func TestLifecycleDrainMetadataAndAbandonedPermitHandling(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(0, 0)}
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm: concurrencylimit.NewFixedAlgorithm(), Clock: clock,
		PermitTTL: time.Second, MaxPriority: 2, Partitions: []string{"live"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	metadata := concurrencylimit.Metadata{Priority: 2, Partition: "live"}
	permit, err := limiter.Acquire(context.Background(), metadata)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if permit.Metadata() != metadata {
		t.Fatalf("Metadata() = %+v, want %+v", permit.Metadata(), metadata)
	}
	clock.Advance(time.Second)
	if got := limiter.ReapExpired(); got != 1 {
		t.Fatalf("ReapExpired() = %d, want 1", got)
	}
	if err = permit.Complete(concurrencylimit.OutcomeSuccess); !errors.Is(err, concurrencylimit.ErrStalePermit) {
		t.Fatalf("expired Complete() error = %v, want ErrStalePermit", err)
	}
	limiter.BeginDrain()
	if _, err = limiter.Acquire(context.Background()); !errors.Is(err, concurrencylimit.ErrDraining) {
		t.Fatalf("draining Acquire() error = %v, want ErrDraining", err)
	}
	limiter.Reset()
	if _, err = limiter.Acquire(context.Background(), concurrencylimit.Metadata{Priority: 3}); !errors.Is(err, concurrencylimit.ErrInvalidMetadata) {
		t.Fatalf("invalid priority error = %v", err)
	}
	if _, err = limiter.Acquire(context.Background(), concurrencylimit.Metadata{Partition: "unknown"}); !errors.Is(err, concurrencylimit.ErrInvalidMetadata) {
		t.Fatalf("invalid partition error = %v", err)
	}
}
