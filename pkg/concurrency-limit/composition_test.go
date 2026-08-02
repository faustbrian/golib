package concurrencylimit_test

import (
	"context"
	"errors"
	"testing"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestRetryAndHedgeAttemptsCannotBypassAdmissionOrAmplifyPastBudget(t *testing.T) {
	t.Parallel()

	limiter := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{})
	active, err := limiter.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	const sharedAttemptBudget = 3
	called := 0
	for range sharedAttemptBudget {
		_, executeErr := concurrencylimit.Execute(context.Background(), limiter, func(context.Context) (struct{}, error) {
			called++
			return struct{}{}, nil
		})
		if !errors.Is(executeErr, concurrencylimit.ErrLimitExceeded) {
			t.Fatalf("attempt error = %v, want local rejection", executeErr)
		}
	}
	if called != 0 {
		t.Fatalf("rejected attempts called dependency %d times", called)
	}
	snapshot := limiter.Snapshot()
	if snapshot.Rejections != sharedAttemptBudget || snapshot.Samples != 0 || snapshot.Outcomes.DependencyFailure != 0 {
		t.Fatalf("rejected attempts polluted learning: %+v", snapshot)
	}
	if err = active.Complete(concurrencylimit.OutcomeIgnored); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPolicyOutcomesAreExcludedFromCapacityLearning(t *testing.T) {
	t.Parallel()

	limiter := newFixedLimiter(t, 1, concurrencylimit.QueueConfig{})
	for _, outcome := range []concurrencylimit.Outcome{
		concurrencylimit.OutcomeLocalDrop,
		concurrencylimit.OutcomeIgnored,
	} {
		permit, err := limiter.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err = permit.Complete(outcome); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot := limiter.Snapshot(); snapshot.Samples != 0 || snapshot.RecentSamples != 0 {
		t.Fatalf("local outcomes became samples: %+v", snapshot)
	}
}
