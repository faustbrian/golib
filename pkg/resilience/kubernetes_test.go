package resilience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/resilience"
)

func TestReplicaLocalBudgetsMultiplyClusterCapacity(t *testing.T) {
	t.Parallel()

	const replicas = 3
	const perReplica uint64 = 2
	const clusterCapacity = int(replicas * perReplica)
	clock := &manualClock{now: time.Unix(3_000, 0)}
	permits := make([]resilience.Permit, 0, clusterCapacity)
	for replica := range replicas {
		config := validBudgetConfig(clock)
		config.MaxAdditionalPerExecution = perReplica + 1
		config.MaxConcurrentAdditional = perReplica
		config.MaxAdditionalPerWindow = perReplica + 1
		budget, err := resilience.NewBudget(config)
		if err != nil {
			t.Fatalf("new replica %d budget: %v", replica, err)
		}
		scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical", "resource"))
		if err != nil {
			t.Fatalf("start replica %d scope: %v", replica, err)
		}
		admitOriginal(ctx, t, scope, clock.Now())
		for offset := range perReplica {
			permit, acquireErr := scope.Acquire(ctx, attemptFor(t, offset+2, resilience.OriginHedge, 1, clock.Now()))
			if acquireErr != nil {
				t.Fatalf("replica %d admission %d: %v", replica, offset, acquireErr)
			}
			permits = append(permits, permit)
		}
		if _, acquireErr := scope.Acquire(ctx, attemptFor(t, perReplica+2, resilience.OriginHedge, 1, clock.Now())); resilience.RejectionReasonOf(acquireErr) != resilience.ReasonConcurrentLimit {
			t.Fatalf("replica %d overflow = %v", replica, acquireErr)
		}
	}
	if len(permits) != clusterCapacity {
		t.Fatalf("cluster permits = %d, want %d", len(permits), clusterCapacity)
	}
	for _, permit := range permits {
		if err := permit.Complete(); err != nil {
			t.Fatalf("complete permit: %v", err)
		}
	}
}

func TestMixedReplicaRevisionsKeepIndependentLimits(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(4_000, 0)}
	limits := []uint64{1, 3}
	for revision, limit := range limits {
		config := validBudgetConfig(clock)
		config.MaxAdditionalPerExecution = limit
		config.MaxConcurrentAdditional = limit
		config.MaxAdditionalPerWindow = limit
		budget, err := resilience.NewBudget(config)
		if err != nil {
			t.Fatalf("new revision %d budget: %v", revision, err)
		}
		scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical", "resource"))
		if err != nil {
			t.Fatalf("start revision %d: %v", revision, err)
		}
		admitOriginal(ctx, t, scope, clock.Now())
		for ordinal := uint64(2); ordinal <= limit+1; ordinal++ {
			permit, acquireErr := scope.Acquire(ctx, attemptFor(t, ordinal, resilience.OriginRetry, 1, clock.Now()))
			if acquireErr != nil {
				t.Fatalf("revision %d ordinal %d: %v", revision, ordinal, acquireErr)
			}
			if completeErr := permit.Complete(); completeErr != nil {
				t.Fatalf("complete revision %d ordinal %d: %v", revision, ordinal, completeErr)
			}
		}
		if _, acquireErr := scope.Acquire(ctx, attemptFor(t, limit+2, resilience.OriginRetry, 1, clock.Now())); resilience.RejectionReasonOf(acquireErr) != resilience.ReasonExecutionLimit {
			t.Fatalf("revision %d overflow = %v", revision, acquireErr)
		}
	}
}

func TestPodTerminationCancellationRemainsCallerOwned(t *testing.T) {
	t.Parallel()

	executor, err := resilience.NewExecutor[string]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	podContext, terminate := context.WithCancel(context.Background())
	result := executor.Execute(podContext, metadataFor(t, "termination", "resource"), func(ctx context.Context, _ resilience.Attempt) (string, error) {
		terminate()
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !errors.Is(result.Err, context.Canceled) || result.Outcome.Kind != resilience.OutcomeCancellation {
		t.Fatalf("termination result = %+v", result)
	}
}

func TestAbruptLossRecoversOnlyExpiredConcurrentCapacity(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(5_000, 0)}
	config := validBudgetConfig(clock)
	config.PermitTTL = time.Second
	config.MaxAdditionalPerExecution = 2
	config.MaxAdditionalPerWindow = 2
	budget, err := resilience.NewBudget(config)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "abrupt", "resource"))
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	admitOriginal(ctx, t, scope, clock.Now())
	abandoned, err := scope.Acquire(ctx, attemptFor(t, 2, resilience.OriginHedge, 1, clock.Now()))
	if err != nil {
		t.Fatalf("acquire abandoned permit: %v", err)
	}
	if _, err := scope.Acquire(ctx, attemptFor(t, 3, resilience.OriginRetry, 1, clock.Now())); resilience.RejectionReasonOf(err) != resilience.ReasonConcurrentLimit {
		t.Fatalf("pre-expiry admission = %v", err)
	}
	clock.Advance(time.Second + time.Nanosecond)
	recovered, err := scope.Acquire(ctx, attemptFor(t, 3, resilience.OriginRetry, 1, clock.Now()))
	if err != nil {
		t.Fatalf("post-expiry admission: %v", err)
	}
	if err := abandoned.Complete(); !errors.Is(err, resilience.ErrPermitExpired) {
		t.Fatalf("abandoned completion = %v", err)
	}
	if err := recovered.Complete(); err != nil {
		t.Fatalf("recovered completion: %v", err)
	}
}
