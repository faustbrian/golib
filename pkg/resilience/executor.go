package resilience

import (
	"context"
	"reflect"
	"slices"
	"time"
)

// PolicyID is a bounded stable policy identity used by errors and diagnostics.
type PolicyID string

// PolicyDescriptor makes ordering and execution scope inspectable.
type PolicyDescriptor struct {
	ID         PolicyID
	Scope      Scope
	Repeatable bool
}

// Operation is caller-owned synchronous work for one physical attempt.
type Operation[T any] func(context.Context, Attempt) (T, error)

// Stage is the explicit policy composition boundary.
type Stage[T any] func(context.Context, Execution, Operation[T]) Result[T]

// Policy wraps the next stage without hidden registration or discovery.
type Policy[T any] interface {
	Descriptor() PolicyDescriptor
	Wrap(Stage[T]) Stage[T]
}

// Clock permits deterministic attempt and event timestamps.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Execution is immutable call metadata passed through policy stages.
type Execution struct {
	Metadata Metadata
	Attempt  Attempt
	recorder *recorder
	total    context.Context
}

// WithAttempt returns an execution copy for validated additional physical work.
func (execution Execution) WithAttempt(attempt Attempt) (Execution, error) {
	if _, err := NewAttempt(attempt.Ordinal, attempt.Origin, attempt.ParentOrdinal, attempt.StartedAt); err != nil {
		return Execution{}, err
	}
	execution.Attempt = attempt
	return execution, nil
}

// Emit records bounded diagnostic metadata without retaining results or errors.
func (execution Execution) Emit(kind EventKind, policy PolicyID, reason string) {
	if execution.recorder == nil {
		return
	}
	kind = EventKind(bounded(string(kind)))
	reason = bounded(reason)
	policy = PolicyID(bounded(string(policy)))
	execution.recorder.emit(Event{Kind: kind, Policy: policy, Reason: reason, LogicalID: execution.Metadata.logicalID, Attempt: execution.Attempt})
}

// Executor is an immutable, reusable policy composition.
type Executor[T any] struct {
	policies  []PolicyDescriptor
	stage     Stage[T]
	clock     Clock
	observer  Observer
	maxEvents int
}

// NewExecutor validates policies and composes them in outer-to-inner order.
func NewExecutor[T any](policies ...Policy[T]) (Executor[T], error) {
	descriptors := make([]PolicyDescriptor, 0, len(policies))
	seen := make(map[PolicyID]PolicyDescriptor, len(policies))
	attemptScopeSeen := false
	for _, policy := range policies {
		if nilInterface(policy) {
			return Executor[T]{}, invalid(ErrInvalidComposition, "policy", "must not be nil")
		}
		descriptor, err := describePolicy(policy)
		if err != nil {
			return Executor[T]{}, err
		}
		if descriptor.ID == "" {
			return Executor[T]{}, invalid(ErrInvalidComposition, "policy_id", "must be bounded and non-empty")
		}
		if PolicyID(bounded(string(descriptor.ID))) != descriptor.ID {
			return Executor[T]{}, invalid(ErrInvalidComposition, "policy_id", "must be bounded and non-empty")
		}
		if descriptor.Scope != ScopeLogical && descriptor.Scope != ScopeAttempt {
			return Executor[T]{}, invalid(ErrInvalidComposition, "policy_scope", "is unknown")
		}
		if descriptor.Scope == ScopeAttempt {
			attemptScopeSeen = true
		} else if attemptScopeSeen {
			return Executor[T]{}, invalid(ErrInvalidComposition, "policy_scope", "logical policies must wrap attempt policies")
		}
		if previous, exists := seen[descriptor.ID]; exists && (!previous.Repeatable || !descriptor.Repeatable || previous.Scope != descriptor.Scope) {
			return Executor[T]{}, invalid(ErrInvalidComposition, "policy", "duplicate policies are incompatible")
		}
		seen[descriptor.ID] = descriptor
		descriptors = append(descriptors, descriptor)
	}

	stage := Stage[T](terminalStage[T])
	for _, policy := range slices.Backward(policies) {
		wrapped, err := wrapPolicy(policy, stage)
		if err != nil {
			return Executor[T]{}, err
		}
		stage = wrapped
	}
	return Executor[T]{policies: descriptors, stage: stage, clock: systemClock{}}, nil
}

func describePolicy[T any](policy Policy[T]) (descriptor PolicyDescriptor, err error) {
	defer func() {
		if recover() != nil {
			descriptor = PolicyDescriptor{}
			err = invalid(ErrInvalidComposition, "policy", "panicked while describing itself")
		}
	}()
	return policy.Descriptor(), nil
}

func wrapPolicy[T any](policy Policy[T], next Stage[T]) (wrapped Stage[T], err error) {
	defer func() {
		if recover() != nil {
			wrapped = nil
			err = invalid(ErrInvalidComposition, "policy", "panicked during composition")
		}
	}()
	wrapped = policy.Wrap(next)
	if nilInterface(wrapped) {
		return nil, invalid(ErrInvalidComposition, "policy", "returned a nil stage")
	}
	return wrapped, nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// WithObserver returns a copy that sends at most maxEvents retained events to observer.
func (executor Executor[T]) WithObserver(observer Observer, maxEvents int) (Executor[T], error) {
	if nilInterface(observer) {
		return Executor[T]{}, invalid(ErrInvalidComposition, "observer", "must not be nil")
	}
	if maxEvents < 1 {
		return Executor[T]{}, invalid(ErrInvalidComposition, "max_events", "must be between 1 and 1024")
	}
	if maxEvents > 1024 {
		return Executor[T]{}, invalid(ErrInvalidComposition, "max_events", "must be between 1 and 1024")
	}
	executor.observer = observer
	executor.maxEvents = maxEvents
	return executor, nil
}

// WithTimeline returns a copy that retains at most maxEvents events per result.
func (executor Executor[T]) WithTimeline(maxEvents int) (Executor[T], error) {
	if maxEvents < 1 {
		return Executor[T]{}, invalid(ErrInvalidComposition, "max_events", "must be between 1 and 1024")
	}
	if maxEvents > 1024 {
		return Executor[T]{}, invalid(ErrInvalidComposition, "max_events", "must be between 1 and 1024")
	}
	executor.maxEvents = maxEvents
	return executor, nil
}

// WithClock returns a copy using clock for attempts and event timestamps.
func (executor Executor[T]) WithClock(clock Clock) (Executor[T], error) {
	if nilInterface(clock) {
		return Executor[T]{}, invalid(ErrInvalidComposition, "clock", "must not be nil")
	}
	executor.clock = clock
	return executor, nil
}

// Policies returns a caller-owned copy in outer-to-inner order.
func (executor Executor[T]) Policies() []PolicyDescriptor {
	return append([]PolicyDescriptor(nil), executor.policies...)
}

// Execute invokes synchronous work without creating an operation goroutine.
func (executor Executor[T]) Execute(ctx context.Context, metadata Metadata, operation Operation[T]) (result Result[T]) {
	var zero T
	if ctx == nil {
		return configurationFailure[T](invalid(ErrInvalidMetadata, "context", "must not be nil"))
	}
	if err := metadata.validate(); err != nil {
		return configurationFailure[T](err)
	}
	if operation == nil {
		return configurationFailure[T](ErrNilOperation)
	}
	if err := ctx.Err(); err != nil {
		return resultFrom(zero, err, Attempt{})
	}
	attempt, err := NewAttempt(1, OriginOriginal, 0, executor.clock.Now())
	if err != nil {
		return configurationFailure[T](err)
	}
	var callRecorder *recorder
	if executor.maxEvents != 0 || executor.observer != nil {
		callRecorder = &recorder{max: executor.maxEvents, clock: executor.clock, observer: executor.observer}
	}
	execution := Execution{Metadata: metadata, Attempt: attempt, recorder: callRecorder, total: ctx}
	execution.Emit(EventExecutionStarted, "", "")
	defer func() {
		recovered := recover()
		if recovered != nil {
			execution.Emit(EventExecutionCompleted, "", "panic")
		} else {
			if result.Outcome.Kind == OutcomeCancellation || result.Outcome.Kind == OutcomeDeadline {
				execution.Emit(EventExecutionCanceled, "", string(result.Outcome.Kind))
			}
			execution.Emit(EventExecutionCompleted, "", string(result.Outcome.Kind))
		}
		if callRecorder != nil {
			result.Events = callRecorder.snapshot()
			callRecorder.notify(result.Events)
		}
		if recovered != nil {
			panic(recovered)
		}
	}()
	result = normalizeResult(executor.stage(ctx, execution, operation), execution.Attempt)
	return result
}

func configurationFailure[T any](err error) Result[T] {
	return Result[T]{Err: err, Outcome: Outcome{Kind: OutcomePolicyFailure}}
}

func normalizeResult[T any](result Result[T], fallback Attempt) Result[T] {
	validKind := false
	switch result.Outcome.Kind {
	case OutcomeSuccess:
		validKind = result.Err == nil
	case OutcomeOperationFailure, OutcomeLocalRejection, OutcomeCancellation, OutcomeDeadline, OutcomeIgnored, OutcomePolicyFailure:
		validKind = result.Err != nil
	}
	if _, err := NewAttempt(result.Outcome.Attempt.Ordinal, result.Outcome.Attempt.Origin, result.Outcome.Attempt.ParentOrdinal, result.Outcome.Attempt.StartedAt); err != nil {
		validKind = false
	}
	if validKind {
		return result
	}
	return PolicyFailure[T](fallback, "executor", "invalid_result", ErrPolicyFailure)
}

func terminalStage[T any](ctx context.Context, execution Execution, operation Operation[T]) (result Result[T]) {
	ctx, releaseContext := constrainContext(execution.total, ctx)
	defer releaseContext()
	execution.Emit(EventAttemptStarted, "", "")
	defer func() {
		if recovered := recover(); recovered != nil {
			execution.Emit(EventAttemptCompleted, "", "panic")
			panic(recovered)
		}
		execution.Emit(EventAttemptCompleted, "", string(result.Outcome.Kind))
	}()
	if scope, ok := BudgetScopeFromContext(ctx); ok {
		if !scope.Matches(execution.Metadata) {
			return Result[T]{Err: ErrBudgetScopeMismatch, Outcome: Outcome{Kind: OutcomeLocalRejection, Attempt: execution.Attempt}}
		}
		state, _ := ctx.Value(budgetContextKey{}).(*budgetExecutionState)
		attemptCtx, _, permit, err := admitKnownAttempt(ctx, state, execution.Attempt)
		if err != nil {
			var zero T
			execution.Emit(EventWorkRejected, "", string(RejectionReasonOf(err)))
			return Result[T]{Value: zero, Err: err, Outcome: Outcome{Kind: OutcomeLocalRejection, Attempt: execution.Attempt}}
		}
		ctx = attemptCtx
		execution.Emit(EventWorkAdmitted, "", "")
		defer func() { _ = permit.Complete() }()
	}
	value, err := operation(ctx, execution.Attempt)
	return resultFrom(value, err, execution.Attempt)
}

func constrainContext(total, proposed context.Context) (context.Context, func()) {
	if proposed == nil {
		proposed = total
	}
	if proposed == total {
		return total, func() {}
	}
	manualContext, cancelManual := context.WithCancel(proposed)
	var boundedContext context.Context
	boundedContext = manualContext
	cancelDeadline := func() {}
	if deadline, ok := total.Deadline(); ok {
		boundedContext, cancelDeadline = context.WithDeadline(manualContext, deadline)
	}
	stop := context.AfterFunc(total, func() {
		if total.Err() != context.DeadlineExceeded {
			cancelManual()
		}
	})
	if total.Err() == context.Canceled {
		cancelManual()
	}
	return boundedContext, func() {
		stop()
		cancelDeadline()
		cancelManual()
	}
}
