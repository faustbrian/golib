package resilience_test

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/resilience"
)

func TestBudgetConcurrentAdmissionNeverExceedsConfiguredCapacity(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(1_000, 0)}
	config := validBudgetConfig(clock)
	config.MaxAdditionalPerExecution = 64
	config.MaxConcurrentAdditional = 8
	config.MaxAdditionalPerWindow = 64
	budget, err := resilience.NewBudget(config)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical", "resource"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	admitOriginal(ctx, t, scope, clock.Now())

	var admitted atomic.Uint64
	var active atomic.Int64
	var maximum atomic.Int64
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := uint64(2); index <= 65; index++ {
		wait.Add(1)
		go func(ordinal uint64) {
			defer wait.Done()
			<-start
			permit, acquireErr := scope.Acquire(ctx, attemptFor(t, ordinal, resilience.OriginHedge, 1, clock.Now()))
			if acquireErr != nil {
				if resilience.RejectionReasonOf(acquireErr) != resilience.ReasonConcurrentLimit {
					t.Errorf("acquire %d: %v", ordinal, acquireErr)
				}
				return
			}
			admitted.Add(1)
			current := active.Add(1)
			for {
				observed := maximum.Load()
				if current <= observed || maximum.CompareAndSwap(observed, current) {
					break
				}
			}
			runtime.Gosched()
			active.Add(-1)
			if completeErr := permit.Complete(); completeErr != nil {
				t.Errorf("complete %d: %v", ordinal, completeErr)
			}
		}(index)
	}
	close(start)
	wait.Wait()
	if got := maximum.Load(); got > int64(config.MaxConcurrentAdditional) {
		t.Fatalf("maximum active = %d, limit = %d", got, config.MaxConcurrentAdditional)
	}
	snapshot := scope.Snapshot()
	if snapshot.AdditionalAdmitted != admitted.Load() || snapshot.AdditionalActive != 0 {
		t.Fatalf("snapshot = %+v, admitted = %d", snapshot, admitted.Load())
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestObserverCanReenterExecutorWithoutDeadlock(t *testing.T) {
	t.Parallel()

	metadata := metadataFor(t, "logical", "resource")
	var executor resilience.Executor[int]
	var calls atomic.Uint64
	observer := resilience.ObserverFunc(func(event resilience.Event) {
		if event.Kind != resilience.EventExecutionStarted || calls.Add(1) != 1 {
			return
		}
		result := executor.Execute(context.Background(), metadata, func(context.Context, resilience.Attempt) (int, error) {
			return 2, nil
		})
		if result.Value != 2 || result.Err != nil {
			panic("reentrant execution failed")
		}
	})
	base, err := resilience.NewExecutor[int]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = base.WithObserver(observer, 8)
	if err != nil {
		t.Fatalf("with observer: %v", err)
	}
	result := executor.Execute(context.Background(), metadata, func(context.Context, resilience.Attempt) (int, error) {
		return 1, nil
	})
	if result.Value != 1 || result.Err != nil || calls.Load() == 0 {
		t.Fatalf("result = %+v, observer calls = %d", result, calls.Load())
	}
}

func TestBlockingObserverCannotConsumeOperationDeadline(t *testing.T) {
	t.Parallel()

	base, err := resilience.NewExecutor[string]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err := base.WithObserver(resilience.ObserverFunc(func(resilience.Event) {
		time.Sleep(20 * time.Millisecond)
	}), 8)
	if err != nil {
		t.Fatalf("with observer: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	result := executor.Execute(ctx, metadataFor(t, "observer", "resource"), func(ctx context.Context, _ resilience.Attempt) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "ok", nil
	})
	if result.Value != "ok" || result.Err != nil || result.Outcome.Kind != resilience.OutcomeSuccess {
		t.Fatalf("result = %+v", result)
	}
}

func TestUncooperativeOperationRemainsCallerOwnedAndSynchronous(t *testing.T) {
	t.Parallel()

	executor, err := resilience.NewExecutor[string]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	started := time.Now()
	result := executor.Execute(ctx, metadataFor(t, "logical", "resource"), func(context.Context, resilience.Attempt) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "finished", nil
	})
	if result.Value != "finished" || result.Err != nil || result.Outcome.Kind != resilience.OutcomeSuccess {
		t.Fatalf("result = %+v", result)
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("executor returned before synchronous operation: %s", elapsed)
	}
}

func TestPlainExecutionRetainsZeroAllocationFastPath(t *testing.T) {
	executor, err := resilience.NewExecutor[int]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	operation := func(context.Context, resilience.Attempt) (int, error) { return 1, nil }
	allocations := testing.AllocsPerRun(100, func() {
		result := executor.Execute(context.Background(), metadata, operation)
		if result.Value != 1 || result.Err != nil {
			panic("execution failed")
		}
	})
	if allocations != 0 {
		t.Fatalf("allocations = %.0f, want 0", allocations)
	}
}

func TestDetachedPolicyReceivesCancellationAfterAttemptStarts(t *testing.T) {
	t.Parallel()

	executor, err := resilience.NewExecutor[string](detachedContextPolicy{})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	total, cancel := context.WithCancel(context.Background())
	result := executor.Execute(total, metadataFor(t, "logical", "resource"), func(ctx context.Context, _ resilience.Attempt) (string, error) {
		cancel()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return "not canceled", errors.New("detached context ignored total cancellation")
		}
	})
	if !errors.Is(result.Err, context.Canceled) || result.Outcome.Kind != resilience.OutcomeCancellation {
		t.Fatalf("result = %+v", result)
	}
}

func FuzzMetadataAndAttempts(fuzz *testing.F) {
	fuzz.Add("logical", "lookup", "resource", uint64(2), "retry", uint64(1), int64(1))
	fuzz.Add("", "", "", uint64(0), "", uint64(0), int64(0))
	fuzz.Fuzz(func(t *testing.T, logicalID, operation, resource string, ordinal uint64, origin string, parent uint64, unix int64) {
		metadata, metadataErr := resilience.NewMetadata(logicalID, operation, resource)
		if metadataErr == nil {
			if metadata.LogicalID() != logicalID || metadata.Operation() != operation || metadata.Resource() != resource {
				t.Fatalf("metadata round trip failed")
			}
		} else if !errors.Is(metadataErr, resilience.ErrInvalidMetadata) {
			t.Fatalf("metadata error = %v", metadataErr)
		}
		attempt, attemptErr := resilience.NewAttempt(ordinal, resilience.AttemptOrigin(origin), parent, time.Unix(unix, 0))
		if attemptErr == nil {
			if attempt.Ordinal != ordinal || attempt.Origin != resilience.AttemptOrigin(origin) || attempt.ParentOrdinal != parent {
				t.Fatalf("attempt round trip failed")
			}
		} else if !errors.Is(attemptErr, resilience.ErrInvalidAttempt) {
			t.Fatalf("attempt error = %v", attemptErr)
		}
	})
}

func FuzzBudgetStateMachine(fuzz *testing.F) {
	fuzz.Add([]byte{0, 1, 2, 3, 4, 5})
	fuzz.Add([]byte{1, 1, 1, 2, 2, 3})
	fuzz.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 128 {
			operations = operations[:128]
		}
		clock := &manualClock{now: time.Unix(2_000, 0)}
		config := validBudgetConfig(clock)
		config.MaxAdditionalPerExecution = 8
		config.MaxConcurrentAdditional = 4
		config.MaxAdditionalPerWindow = 8
		budget, err := resilience.NewBudget(config)
		if err != nil {
			t.Fatalf("new budget: %v", err)
		}
		scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical", "resource"))
		if err != nil {
			t.Fatalf("start scope: %v", err)
		}
		admitOriginal(ctx, t, scope, clock.Now())
		permits := make([]resilience.Permit, 0, 4)
		nextOrdinal := uint64(2)
		for _, operation := range operations {
			switch operation % 4 {
			case 0:
				permit, acquireErr := scope.Acquire(ctx, attemptFor(t, nextOrdinal, resilience.OriginHedge, 1, clock.Now()))
				nextOrdinal++
				if acquireErr == nil {
					permits = append(permits, permit)
				}
			case 1:
				if len(permits) > 0 {
					_ = permits[0].Complete()
					permits = permits[1:]
				}
			case 2:
				clock.Advance(time.Minute + time.Nanosecond)
			case 3:
				snapshot := scope.Snapshot()
				if snapshot.AdditionalActive > config.MaxConcurrentAdditional || snapshot.AdditionalAdmitted > config.MaxAdditionalPerExecution {
					t.Fatalf("snapshot violates bounds: %+v", snapshot)
				}
			}
		}
		for _, permit := range permits {
			_ = permit.Complete()
		}
		if snapshot := scope.Snapshot(); snapshot.AdditionalActive != 0 || snapshot.AdditionalAdmitted > config.MaxAdditionalPerExecution {
			t.Fatalf("terminal snapshot = %+v", snapshot)
		}
		if err := scope.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
}

func FuzzTypedErrorsAndClockedEvents(fuzz *testing.F) {
	fuzz.Add("policy", "reason", "stage", int64(1))
	fuzz.Add("", "", "", int64(-1))
	fuzz.Fuzz(func(t *testing.T, policy, reason, stage string, unix int64) {
		now := time.Unix(unix, 0)
		if now.IsZero() {
			now = time.Unix(1, 0)
		}
		clock := &manualClock{now: now}
		attempt := attemptFor(t, 1, resilience.OriginOriginal, 0, clock.Now())
		cause := errors.New("cause")
		rejection := resilience.LocalRejection[int](attempt, resilience.PolicyID(policy), reason, cause)
		var rejectionError *resilience.LocalRejectionError
		if !errors.As(rejection.Err, &rejectionError) || !errors.Is(rejection.Err, cause) || len(rejectionError.Policy) > resilience.MaxIdentityLength || len(rejectionError.Reason) > resilience.MaxIdentityLength {
			t.Fatalf("rejection = %#v", rejection.Err)
		}
		failure := resilience.PolicyFailure[int](attempt, resilience.PolicyID(policy), stage, cause)
		var policyError *resilience.PolicyExecutionError
		if !errors.As(failure.Err, &policyError) || !errors.Is(failure.Err, cause) || len(policyError.Policy) > resilience.MaxIdentityLength || len(policyError.Stage) > resilience.MaxIdentityLength {
			t.Fatalf("policy failure = %#v", failure.Err)
		}

		executor, err := resilience.NewExecutor[int]()
		if err != nil {
			t.Fatalf("new executor: %v", err)
		}
		executor, err = executor.WithClock(clock)
		if err != nil {
			t.Fatalf("with clock: %v", err)
		}
		executor, err = executor.WithTimeline(4)
		if err != nil {
			t.Fatalf("with timeline: %v", err)
		}
		result := executor.Execute(context.Background(), metadataFor(t, "fuzz", "resource"), func(context.Context, resilience.Attempt) (int, error) {
			return 1, nil
		})
		if result.Err != nil || len(result.Events) != 4 {
			t.Fatalf("clocked result = %+v", result)
		}
		for _, event := range result.Events {
			if !event.At.Equal(clock.Now()) {
				t.Fatalf("event time = %v, want %v", event.At, clock.Now())
			}
		}
	})
}
