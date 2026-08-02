package bulkhead_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestWaitingIsFIFOQueueBoundedAndCancelable(t *testing.T) {
	observer := &recordingObserver{}
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "inventory-db",
		Capacity:  1,
		Admission: bulkhead.Wait{MaxQueued: 2, MaxWait: 100 * time.Millisecond},
		Observer:  observer,
	})
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}

	first := acquireAsync(policy, context.Background())
	waitForQueueDepth(t, policy, 1)
	secondContext, cancelSecond := context.WithCancel(context.Background())
	second := acquireAsync(policy, secondContext)
	waitForQueueDepth(t, policy, 2)
	if _, err := policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrQueueFull) {
		t.Fatalf("saturated Acquire() error = %v, want ErrQueueFull", err)
	}

	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release() error = %v", err)
	}
	firstPermit := receivePermit(t, first)
	select {
	case result := <-second:
		if result.err == nil {
			_ = result.permit.Release()
		}
		t.Fatalf("second waiter completed before first release: %v", result.err)
	default:
	}
	cancelSecond()
	secondResult := receiveAcquire(t, second)
	if !errors.Is(secondResult.err, bulkhead.ErrCallerCanceled) || !errors.Is(secondResult.err, context.Canceled) {
		t.Fatalf("canceled waiter error = %v", secondResult.err)
	}
	if err := firstPermit.Release(); err != nil {
		t.Fatalf("first waiter Release() error = %v", err)
	}

	snapshot := policy.Snapshot()
	if snapshot.QueueDepth != 0 || snapshot.ActiveWeight != 0 || snapshot.Cancellations != 1 ||
		snapshot.Admissions != 2 || snapshot.Rejections != 2 ||
		snapshot.RejectionCounts[bulkhead.RejectionQueue] != 1 ||
		snapshot.RejectionCounts[bulkhead.RejectionCaller] != 1 {
		t.Fatalf("final Snapshot() = %+v", snapshot)
	}
	var cancellationEvents int
	for _, event := range observer.Events() {
		if event.Kind == bulkhead.EventCanceled && event.Reason == bulkhead.RejectionCaller {
			cancellationEvents++
		}
	}
	if cancellationEvents != 1 {
		t.Fatalf("cancellation event count = %d, want 1", cancellationEvents)
	}
}

func TestMaximumWaitIsDistinctFromCallerDeadline(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "inventory-db",
		Capacity:  1,
		Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: 5 * time.Millisecond},
	})
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	defer func() { _ = holder.Release() }()

	if _, err := policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrWaitTimeout) {
		t.Fatalf("waiting Acquire() error = %v, want ErrWaitTimeout", err)
	}
}

type acquireResult struct {
	permit *bulkhead.Permit
	err    error
}

func acquireAsync(policy *bulkhead.Bulkhead, ctx context.Context) <-chan acquireResult {
	result := make(chan acquireResult, 1)
	go func() {
		permit, err := policy.Acquire(ctx, 1)
		result <- acquireResult{permit: permit, err: err}
	}()
	return result
}

func receivePermit(t *testing.T, result <-chan acquireResult) *bulkhead.Permit {
	t.Helper()
	acquired := receiveAcquire(t, result)
	if acquired.err != nil {
		t.Fatalf("Acquire() error = %v", acquired.err)
	}
	return acquired.permit
}

func receiveAcquire(t *testing.T, result <-chan acquireResult) acquireResult {
	t.Helper()
	select {
	case acquired := <-result:
		return acquired
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Acquire() did not complete")
		return acquireResult{}
	}
}

func waitForQueueDepth(t *testing.T, policy *bulkhead.Bulkhead, want int) {
	t.Helper()
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if policy.Snapshot().QueueDepth == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("QueueDepth did not reach %d; snapshot = %+v", want, policy.Snapshot())
}

func mustPolicy(t *testing.T, config bulkhead.Config) *bulkhead.Bulkhead {
	t.Helper()
	policy, err := bulkhead.New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return policy
}

func drainWithin(policy *bulkhead.Bulkhead) error {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	return policy.Drain(ctx)
}
