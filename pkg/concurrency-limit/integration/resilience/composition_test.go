package resilience_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
	"github.com/faustbrian/golib/pkg/hedge"
	"github.com/faustbrian/golib/pkg/retry"
)

func TestLocalAdmissionRejectionIsNotRetried(t *testing.T) {
	limiter := mustFixedLimiter(t)
	holder, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Complete(concurrencylimit.OutcomeIgnored) }()

	policy, err := retry.NewPolicy(retry.Config{
		Backoff: retry.Constant(0), MaxAttempts: 3,
		Clock: retry.SystemClock{}, Sleeper: retry.SystemSleeper{},
		Classifier: retry.RetryableClassifier(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var logicalAttempts atomic.Uint64
	var downstreamCalls atomic.Uint64
	_, result, err := retry.Do(context.Background(), policy, func(ctx context.Context) (struct{}, error) {
		logicalAttempts.Add(1)
		return concurrencylimit.Execute(ctx, limiter, func(context.Context) (struct{}, error) {
			downstreamCalls.Add(1)
			return struct{}{}, errors.New("downstream failure")
		})
	})
	if !errors.Is(err, concurrencylimit.ErrLimitExceeded) {
		t.Fatalf("retry.Do() error = %v, want ErrLimitExceeded", err)
	}
	if result.Attempts != 1 || result.Reason != retry.ReasonPermanent || logicalAttempts.Load() != 1 {
		t.Fatalf("retry result = %+v, logical attempts = %d", result, logicalAttempts.Load())
	}
	if downstreamCalls.Load() != 0 {
		t.Fatalf("downstream calls = %d, want 0", downstreamCalls.Load())
	}
	if snapshot := limiter.Snapshot(); snapshot.Rejections != 1 || snapshot.Samples != 0 {
		t.Fatalf("limiter snapshot = %+v", snapshot)
	}
}

func TestEachHedgeAttemptRequiresAdmissionWithoutPollutingLearning(t *testing.T) {
	limiter := mustFixedLimiter(t)
	budget, err := hedge.NewOutstandingBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := hedge.NewPolicy(hedge.Config[struct{}]{
		MaxHedges: 1, ReplaySafe: true, Delay: time.Millisecond,
		TotalTimeout: time.Second, CleanupTimeout: time.Second,
		Clock: hedge.RealClock{}, Budget: budget, Resource: "dependency",
		Classifier: hedge.ClassifyFunc[struct{}](func(_ context.Context, result hedge.AttemptResult[struct{}]) (hedge.Classification, error) {
			switch {
			case result.Err == nil:
				return hedge.ClassificationSuccess, nil
			case errors.Is(result.Err, concurrencylimit.ErrLimitExceeded):
				return hedge.ClassificationCanceled, nil
			default:
				return hedge.ClassificationFailure, nil
			}
		}),
		Disposer:           hedge.DisposeFunc[struct{}](func(context.Context, struct{}) error { return nil }),
		FactoryFailureMode: hedge.FactoryFailureStop,
	})
	if err != nil {
		t.Fatal(err)
	}

	originalStarted := make(chan struct{})
	releaseOriginal := make(chan struct{})
	var downstreamCalls atomic.Uint64
	factory := hedge.AttemptFactoryFunc[struct{}](func(info hedge.AttemptInfo) (hedge.Attempt[struct{}], string, error) {
		return func(ctx context.Context) (struct{}, error) {
			return concurrencylimit.Execute(ctx, limiter, func(context.Context) (struct{}, error) {
				downstreamCalls.Add(1)
				if !info.Hedge {
					close(originalStarted)
					<-releaseOriginal
				}
				return struct{}{}, nil
			})
		}, "dependency", nil
	})

	type execution struct {
		report hedge.Report
		err    error
	}
	completed := make(chan execution, 1)
	go func() {
		_, report, executeErr := hedge.Do(context.Background(), policy, factory)
		completed <- execution{report: report, err: executeErr}
	}()
	select {
	case <-originalStarted:
	case <-time.After(time.Second):
		t.Fatal("original attempt did not start")
	}
	waitForRejection(t, limiter)
	close(releaseOriginal)

	var result execution
	select {
	case result = <-completed:
	case <-time.After(time.Second):
		t.Fatal("hedged execution did not finish")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.report.AttemptsStarted != 2 || result.report.HedgesStarted != 1 || downstreamCalls.Load() != 1 {
		t.Fatalf("hedge report = %+v, downstream calls = %d", result.report, downstreamCalls.Load())
	}
	if err = result.report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := limiter.Snapshot(); snapshot.Rejections != 1 || snapshot.Outcomes.Success != 1 || snapshot.Samples != 1 {
		t.Fatalf("limiter snapshot = %+v", snapshot)
	}
	if budget.Outstanding() != 0 {
		t.Fatalf("hedge budget outstanding = %d", budget.Outstanding())
	}
}

func mustFixedLimiter(t *testing.T) *concurrencylimit.Limiter {
	t.Helper()
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1,
		Algorithm: concurrencylimit.NewFixedAlgorithm(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

func waitForRejection(t *testing.T, limiter *concurrencylimit.Limiter) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if limiter.Snapshot().Rejections == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("hedge attempt did not reach local admission")
}
