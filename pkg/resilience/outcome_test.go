package resilience_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/resilience"
)

type collectingObserver struct {
	mu     sync.Mutex
	events []resilience.Event
	panic  bool
}

func (observer *collectingObserver) Observe(event resilience.Event) {
	observer.mu.Lock()
	observer.events = append(observer.events, event)
	shouldPanic := observer.panic
	observer.mu.Unlock()
	if shouldPanic {
		panic("observer failure")
	}
}

func (observer *collectingObserver) Events() []resilience.Event {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]resilience.Event(nil), observer.events...)
}

type nilStagePolicy struct{}

func (nilStagePolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "nil-stage", Scope: resilience.ScopeLogical}
}

func (nilStagePolicy) Wrap(resilience.Stage[int]) resilience.Stage[int] { return nil }

type panickingDescriptorPolicy struct{}

func (panickingDescriptorPolicy) Descriptor() resilience.PolicyDescriptor { panic("descriptor") }
func (panickingDescriptorPolicy) Wrap(next resilience.Stage[int]) resilience.Stage[int] {
	return next
}

type detachedContextPolicy struct{}

func (detachedContextPolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "detached", Scope: resilience.ScopeAttempt}
}

func (detachedContextPolicy) Wrap(next resilience.Stage[string]) resilience.Stage[string] {
	return func(_ context.Context, execution resilience.Execution, operation resilience.Operation[string]) resilience.Result[string] {
		return next(context.Background(), execution, operation)
	}
}

type retryOncePolicy struct{ clock resilience.Clock }

func (retryOncePolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "retry-once", Scope: resilience.ScopeLogical}
}

type emittingPolicy struct {
	kind   resilience.EventKind
	policy resilience.PolicyID
	reason string
}

func (emittingPolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "emitter", Scope: resilience.ScopeLogical}
}

func (policy emittingPolicy) Wrap(next resilience.Stage[string]) resilience.Stage[string] {
	return func(ctx context.Context, execution resilience.Execution, operation resilience.Operation[string]) resilience.Result[string] {
		execution.Emit(policy.kind, policy.policy, policy.reason)
		return next(ctx, execution, operation)
	}
}

type nilContextPolicy struct{}

func (nilContextPolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "nil-context", Scope: resilience.ScopeAttempt}
}

func (nilContextPolicy) Wrap(next resilience.Stage[string]) resilience.Stage[string] {
	return func(_ context.Context, execution resilience.Execution, operation resilience.Operation[string]) resilience.Result[string] {
		return next(nil, execution, operation)
	}
}

type cancelingDetachedPolicy struct{ cancel context.CancelFunc }

func (cancelingDetachedPolicy) Descriptor() resilience.PolicyDescriptor {
	return resilience.PolicyDescriptor{ID: "cancel-detached", Scope: resilience.ScopeAttempt}
}

func (policy cancelingDetachedPolicy) Wrap(next resilience.Stage[string]) resilience.Stage[string] {
	return func(_ context.Context, execution resilience.Execution, operation resilience.Operation[string]) resilience.Result[string] {
		policy.cancel()
		return next(context.Background(), execution, operation)
	}
}

func (policy retryOncePolicy) Wrap(next resilience.Stage[string]) resilience.Stage[string] {
	return func(ctx context.Context, execution resilience.Execution, operation resilience.Operation[string]) resilience.Result[string] {
		first := next(ctx, execution, operation)
		if first.Err == nil {
			return first
		}
		attempt, err := resilience.NewAttempt(2, resilience.OriginRetry, 1, policy.clock.Now())
		if err != nil {
			return resilience.PolicyFailure[string](execution.Attempt, "retry-once", "attempt", err)
		}
		retryExecution, err := execution.WithAttempt(attempt)
		if err != nil {
			return resilience.PolicyFailure[string](execution.Attempt, "retry-once", "attempt", err)
		}
		return next(ctx, retryExecution, operation)
	}
}

func TestResultConstructorsKeepFailureClassesDistinct(t *testing.T) {
	t.Parallel()

	attempt, err := resilience.NewAttempt(2, resilience.OriginRetry, 1, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("new attempt: %v", err)
	}
	cause := errors.New("downstream")
	tests := []struct {
		name string
		got  resilience.Result[int]
		kind resilience.OutcomeKind
		is   error
	}{
		{name: "success", got: resilience.Success(7, attempt), kind: resilience.OutcomeSuccess},
		{name: "operation", got: resilience.Failure(7, cause, attempt), kind: resilience.OutcomeOperationFailure, is: cause},
		{name: "rejection", got: resilience.LocalRejection[int](attempt, "bulkhead", "full", cause), kind: resilience.OutcomeLocalRejection, is: resilience.ErrLocalRejection},
		{name: "ignored", got: resilience.Ignored[int](attempt, "not eligible"), kind: resilience.OutcomeIgnored, is: resilience.ErrIgnored},
		{name: "policy", got: resilience.PolicyFailure[int](attempt, "retry", "classifier", cause), kind: resilience.OutcomePolicyFailure, is: resilience.ErrPolicyFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got.Outcome.Kind != test.kind {
				t.Fatalf("kind = %q, want %q", test.got.Outcome.Kind, test.kind)
			}
			if test.is != nil && !errors.Is(test.got.Err, test.is) {
				t.Fatalf("error = %v, want errors.Is %v", test.got.Err, test.is)
			}
		})
	}
	var rejection *resilience.LocalRejectionError
	if !errors.As(tests[2].got.Err, &rejection) || rejection.Policy != "bulkhead" || rejection.Reason != "full" || !errors.Is(rejection, cause) {
		t.Fatalf("local rejection = %#v", tests[2].got.Err)
	}
	var policy *resilience.PolicyExecutionError
	if !errors.As(tests[4].got.Err, &policy) || policy.Policy != "retry" || policy.Stage != "classifier" || !errors.Is(policy, cause) {
		t.Fatalf("policy failure = %#v", tests[4].got.Err)
	}
}

func TestObserverPanicCannotChangeResultAndTimelineIsBounded(t *testing.T) {
	t.Parallel()

	observer := &collectingObserver{panic: true}
	executor, err := resilience.NewExecutor[string]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithObserver(observer, 3)
	if err != nil {
		t.Fatalf("with observer: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	result := executor.Execute(context.Background(), metadata, func(context.Context, resilience.Attempt) (string, error) {
		return "ok", nil
	})
	if result.Value != "ok" || result.Err != nil || len(result.Events) != 3 {
		t.Fatalf("result = %+v", result)
	}
	if got := len(observer.Events()); got != 3 {
		t.Fatalf("observer events = %d, want bounded 3", got)
	}

	execution := resilience.Execution{}
	execution.Emit(resilience.EventPolicyEntered, "ignored", strings.Repeat("x", resilience.MaxIdentityLength+1))
}

func TestOperationPanicReleasesBudgetAndPreservesPanic(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(600, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	observer := &collectingObserver{}
	executor, err := resilience.NewExecutor[string]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithClock(clock)
	if err != nil {
		t.Fatalf("with clock: %v", err)
	}
	executor, err = executor.WithObserver(observer, 8)
	if err != nil {
		t.Fatalf("with observer: %v", err)
	}

	func() {
		defer func() {
			if recovered := recover(); recovered != "operation panic" {
				t.Fatalf("recovered = %v", recovered)
			}
		}()
		executor.Execute(ctx, metadata, func(context.Context, resilience.Attempt) (string, error) {
			panic("operation panic")
		})
	}()
	if got := scope.Snapshot().AdditionalActive; got != 0 {
		t.Fatalf("active work after panic = %d", got)
	}
	events := observer.Events()
	if events[len(events)-2].Kind != resilience.EventAttemptCompleted || events[len(events)-2].Reason != "panic" || events[len(events)-1].Kind != resilience.EventExecutionCompleted {
		t.Fatalf("panic events = %+v", events)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestExecutionAttemptCopiesPreserveMetadataAndValidateLineage(t *testing.T) {
	t.Parallel()

	metadata := metadataFor(t, "logical", "resource")
	original, err := resilience.NewAttempt(1, resilience.OriginOriginal, 0, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("new original: %v", err)
	}
	execution := resilience.Execution{Metadata: metadata, Attempt: original}
	retry, err := resilience.NewAttempt(2, resilience.OriginRetry, 1, time.Unix(2, 0))
	if err != nil {
		t.Fatalf("new retry: %v", err)
	}
	next, err := execution.WithAttempt(retry)
	if err != nil || next.Metadata != metadata || next.Attempt != retry || execution.Attempt != original {
		t.Fatalf("next = %+v, original = %+v, err = %v", next, execution, err)
	}
	if _, err := execution.WithAttempt(resilience.Attempt{}); !errors.Is(err, resilience.ErrInvalidAttempt) {
		t.Fatalf("invalid attempt error = %v", err)
	}
	if metadata.LogicalID() != "logical" || metadata.Operation() != "lookup" || metadata.Resource() != "resource" {
		t.Fatalf("metadata getters returned unexpected values")
	}
}

func TestExecutorRejectsPolicyConstructionFailures(t *testing.T) {
	t.Parallel()

	for _, policy := range []resilience.Policy[int]{nilStagePolicy{}, panickingDescriptorPolicy{}} {
		if _, err := resilience.NewExecutor[int](policy); !errors.Is(err, resilience.ErrInvalidComposition) {
			t.Fatalf("construction error = %v", err)
		}
	}
	executor, err := resilience.NewExecutor[int]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	if _, err := executor.WithObserver(nil, 1); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("nil observer error = %v", err)
	}
	if _, err := executor.WithObserver(&collectingObserver{}, 0); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("event bound error = %v", err)
	}
	if _, err := executor.WithObserver(&collectingObserver{}, 1025); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("large observer bound error = %v", err)
	}
	if _, err := executor.WithObserver(&collectingObserver{}, 1024); err != nil {
		t.Fatalf("maximum observer bound error = %v", err)
	}
	if _, err := executor.WithObserver(&collectingObserver{}, 1); err != nil {
		t.Fatalf("minimum observer bound error = %v", err)
	}
	if _, err := executor.WithTimeline(0); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("timeline bound error = %v", err)
	}
	if _, err := executor.WithTimeline(1025); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("large timeline bound error = %v", err)
	}
	if _, err := executor.WithTimeline(1024); err != nil {
		t.Fatalf("maximum timeline bound error = %v", err)
	}
	if _, err := executor.WithTimeline(1); err != nil {
		t.Fatalf("minimum timeline bound error = %v", err)
	}
	if _, err := executor.WithClock(nil); !errors.Is(err, resilience.ErrInvalidComposition) {
		t.Fatalf("nil clock error = %v", err)
	}
}

func TestExecutorPreventsPoliciesFromExtendingCallerDeadline(t *testing.T) {
	t.Parallel()

	executor, err := resilience.NewExecutor[string](detachedContextPolicy{})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := executor.Execute(ctx, metadataFor(t, "logical", "resource"), func(ctx context.Context, _ resilience.Attempt) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
			return "wrong", nil
		}
	})
	if !errors.Is(result.Err, context.DeadlineExceeded) || result.Outcome.Kind != resilience.OutcomeDeadline {
		t.Fatalf("result = %+v", result)
	}
}

func TestRetryPolicyUsesTheSameCentralBudgetScope(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(700, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	executor, err := resilience.NewExecutor[string](retryOncePolicy{clock: clock})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithClock(clock)
	if err != nil {
		t.Fatalf("with clock: %v", err)
	}
	calls := 0
	result := executor.Execute(ctx, metadata, func(context.Context, resilience.Attempt) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("retryable")
		}
		return "ok", nil
	})
	if result.Value != "ok" || result.Err != nil || calls != 2 {
		t.Fatalf("result = %+v, calls = %d", result, calls)
	}
	snapshot := scope.Snapshot()
	if snapshot.AdditionalAdmitted != 1 || snapshot.AdditionalActive != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestExecutionBoundsEventsAndDetachedCancellation(t *testing.T) {
	t.Parallel()

	longReason := strings.Repeat("x", resilience.MaxIdentityLength+1)
	observer := &collectingObserver{}
	executor, err := resilience.NewExecutor[string](emittingPolicy{kind: resilience.EventKind(longReason), policy: resilience.PolicyID(longReason), reason: longReason}, nilContextPolicy{})
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithObserver(observer, 8)
	if err != nil {
		t.Fatalf("with observer: %v", err)
	}
	result := executor.Execute(context.Background(), metadataFor(t, "logical", "resource"), func(context.Context, resilience.Attempt) (string, error) {
		return "ok", nil
	})
	if result.Err != nil {
		t.Fatalf("nil proposed context did not inherit total context: %v", result.Err)
	}
	events := observer.Events()
	if len(events[1].Kind) != resilience.MaxIdentityLength || len(events[1].Policy) != resilience.MaxIdentityLength || len(events[1].Reason) != resilience.MaxIdentityLength {
		t.Fatalf("bounded event identity = (%d, %d, %d)", len(events[1].Kind), len(events[1].Policy), len(events[1].Reason))
	}

	total, cancel := context.WithCancel(context.Background())
	canceledExecutor, err := resilience.NewExecutor[string](cancelingDetachedPolicy{cancel: cancel})
	if err != nil {
		t.Fatalf("new canceled executor: %v", err)
	}
	result = canceledExecutor.Execute(total, metadataFor(t, "logical-2", "resource"), func(ctx context.Context, _ resilience.Attempt) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !errors.Is(result.Err, context.Canceled) || result.Outcome.Kind != resilience.OutcomeCancellation {
		t.Fatalf("detached cancellation result = %+v", result)
	}
}

func TestExecutorTimelineRecordsCallerCancellation(t *testing.T) {
	t.Parallel()

	executor, err := resilience.NewExecutor[int]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithTimeline(8)
	if err != nil {
		t.Fatalf("with timeline: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := executor.Execute(ctx, metadataFor(t, "canceled", "resource"), func(ctx context.Context, _ resilience.Attempt) (int, error) {
		cancel()
		return 0, ctx.Err()
	})
	if !errors.Is(result.Err, context.Canceled) || result.Outcome.Kind != resilience.OutcomeCancellation {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Events) != 5 || result.Events[3].Kind != resilience.EventExecutionCanceled || result.Events[3].Reason != string(resilience.OutcomeCancellation) {
		t.Fatalf("events = %+v", result.Events)
	}
}

func TestExecutorRejectsMismatchedBudgetMetadataBeforeOperation(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(900, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical-a", "resource"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	executor, err := resilience.NewExecutor[string]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithClock(clock)
	if err != nil {
		t.Fatalf("with clock: %v", err)
	}
	result := executor.Execute(ctx, metadataFor(t, "logical-b", "resource"), func(context.Context, resilience.Attempt) (string, error) {
		t.Fatal("operation ran with mismatched budget metadata")
		return "", nil
	})
	if !errors.Is(result.Err, resilience.ErrBudgetScopeMismatch) || result.Outcome.Kind != resilience.OutcomeLocalRejection {
		t.Fatalf("result = %+v", result)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}
