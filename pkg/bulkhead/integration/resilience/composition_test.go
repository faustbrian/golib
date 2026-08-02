package resilience_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/retry"
)

func TestBulkheadRejectionIsNeitherRetriedNorRecordedAsDownstreamFailure(t *testing.T) {
	policy, err := bulkhead.New(bulkhead.Config{Resource: "database", Capacity: 1})
	if err != nil {
		t.Fatalf("bulkhead.New() error = %v", err)
	}
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	defer func() { _ = holder.Release() }()

	circuit, err := breaker.New(breaker.Config{
		Name:              "database",
		Window:            breaker.CountWindow{Size: 2},
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Minute),
		HalfOpen:          &breaker.HalfOpenPolicy{MaxProbes: 1, RequiredSuccesses: 1},
	})
	if err != nil {
		t.Fatalf("breaker.New() error = %v", err)
	}
	retryPolicy, err := retry.NewPolicy(retry.Config{
		Backoff:     retry.Constant(0),
		MaxAttempts: 3,
		Clock:       retry.SystemClock{},
		Sleeper:     retry.SystemSleeper{},
		Classifier:  retry.RetryableClassifier(),
	})
	if err != nil {
		t.Fatalf("retry.NewPolicy() error = %v", err)
	}

	var logicalAttempts atomic.Uint64
	var downstreamCalls atomic.Uint64
	_, result, err := retry.Do(context.Background(), retryPolicy, func(ctx context.Context) (struct{}, error) {
		logicalAttempts.Add(1)
		value, _, executeErr := bulkhead.Execute(ctx, policy, 1, func(ctx context.Context) (struct{}, error) {
			return breaker.Execute(ctx, circuit, func(context.Context) (struct{}, error) {
				downstreamCalls.Add(1)
				return struct{}{}, errors.New("downstream failure")
			})
		})
		return value, executeErr
	})
	if !errors.Is(err, bulkhead.ErrRejected) {
		t.Fatalf("retry.Do() error = %v, want ErrRejected", err)
	}
	if result.Attempts != 1 || result.Reason != retry.ReasonPermanent || logicalAttempts.Load() != 1 {
		t.Fatalf("retry result = %+v, logical attempts = %d", result, logicalAttempts.Load())
	}
	if downstreamCalls.Load() != 0 {
		t.Fatalf("downstream call count = %d, want 0", downstreamCalls.Load())
	}
	if snapshot := circuit.Snapshot(); snapshot.Admitted != 0 || snapshot.Completed != 0 ||
		snapshot.TotalFailures != 0 || snapshot.Failures != 0 {
		t.Fatalf("circuit Snapshot() = %+v", snapshot)
	}
}
