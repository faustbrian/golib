package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const compensationProcessorTransitionPrefix = "compensation-processor-"

var (
	// ErrInvalidCompensationProcessor classifies malformed compensation
	// processor configuration or poison durable compensation work.
	ErrInvalidCompensationProcessor = errors.New("invalid workflow compensation processor")
)

// CompensationWorkProcessorConfig supplies explicit bounded compensation
// execution dependencies. Compensations is an explicit activity registry
// resolved from each immutable CompensationSpec target.
type CompensationWorkProcessorConfig struct {
	Store            ActivityExecutionStore
	Definitions      *Registry
	Compensations    *ActivityRegistry
	Clock            Clock
	PageSize         uint32
	MaxHistoryEvents uint32
}

// CompensationWorkProcessor executes leased compensating side effects. It
// never translates a failed or unknown compensation into successful rollback.
type CompensationWorkProcessor struct {
	config CompensationWorkProcessorConfig
}

// NewCompensationWorkProcessor validates one bounded explicit processor.
func NewCompensationWorkProcessor(config CompensationWorkProcessorConfig) (*CompensationWorkProcessor, error) {
	if config.Store == nil || config.Definitions == nil || config.Compensations == nil || config.Clock == nil ||
		!validHistoryTraversal("processor-instance", config.PageSize, config.MaxHistoryEvents) {
		return nil, ErrInvalidCompensationProcessor
	}
	return &CompensationWorkProcessor{config: config}, nil
}

// Process persists compensation attempt start before handler invocation and
// persists an explicit result before returning WorkComplete.
func (processor *CompensationWorkProcessor) Process(ctx context.Context, lease WorkLease) (WorkDecision, error) {
	if processor == nil || ctx == nil || !lease.Valid() || lease.Work().Kind() != WorkCompensation {
		return WorkDecision{}, ErrInvalidCompensationProcessor
	}
	work := lease.Work()
	dispatch, err := DecodeCompensationDispatch(work.Payload())
	if err != nil {
		return compensationDeadLetter("invalid-compensation-dispatch")
	}
	instance, definition, step, compensation, err := processor.load(ctx, work, dispatch)
	if err != nil {
		if errors.Is(err, ErrActivityNotFound) || errors.Is(err, ErrInvalidCompensationProcessor) {
			return compensationDeadLetter("invalid-compensation-definition")
		}
		return WorkDecision{}, err
	}
	progress, exists := instance.Compensation(dispatch.StepName())
	if !exists {
		return compensationDeadLetter("invalid-compensation-state")
	}
	return processor.processProgress(ctx, lease, instance, definition, step, compensation, dispatch, progress)
}

func (processor *CompensationWorkProcessor) processProgress(
	ctx context.Context,
	lease WorkLease,
	instance Instance,
	definition Definition,
	step StepSpec,
	compensation Activity,
	dispatch CompensationDispatch,
	progress CompensationProgress,
) (WorkDecision, error) {
	switch progress.Status() {
	case CompensationReady, CompensationRetryWaiting:
		if dispatch.Attempt() != progress.Attempt()+1 {
			return compensationDeadLetter("invalid-compensation-state")
		}
		return processor.execute(ctx, lease, instance, definition, step, compensation, dispatch)
	case CompensationRunning:
		if progress.Attempt() != dispatch.Attempt() || progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return compensationDeadLetter("invalid-compensation-state")
		}
		unknown, _ := NewActivityOutcome(ActivityOutcomeSpec{
			Kind: ActivityUnknown, Code: "compensation-outcome-unknown",
		})
		return processor.persistOutcome(ctx, lease.Work(), instance, definition, dispatch, unknown)
	case CompensationFailed:
		if progress.Attempt() != dispatch.Attempt() || progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return compensationDeadLetter("invalid-compensation-state")
		}
		return processor.scheduleRetry(ctx, lease.Work(), instance, definition, step, dispatch)
	case CompensationSucceeded, CompensationUnknown, CompensationManuallyResolved:
		if progress.Attempt() != dispatch.Attempt() || progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return compensationDeadLetter("invalid-compensation-state")
		}
		return activityComplete()
	default:
		return compensationDeadLetter("invalid-compensation-state")
	}
}

func (processor *CompensationWorkProcessor) load(
	ctx context.Context,
	work PendingWork,
	dispatch CompensationDispatch,
) (Instance, Definition, StepSpec, Activity, error) {
	instance, err := processor.inspect(ctx, work.InstanceID())
	if err != nil {
		return Instance{}, Definition{}, StepSpec{}, Activity{}, err
	}
	definition, _ := processor.config.Definitions.Resolve(instance.Definition().Name(), instance.Definition().Version())
	step, ok := activityProcessorStep(definition, dispatch.StepName())
	if !ok || step.Compensation == nil || dispatch.Attempt() > step.Compensation.Retry.MaxAttempts ||
		instance.Sequence() < work.Sequence() {
		return Instance{}, Definition{}, StepSpec{}, Activity{}, ErrInvalidCompensationProcessor
	}
	compensation, err := processor.config.Compensations.Resolve(step.Compensation.Target)
	if err != nil {
		return Instance{}, Definition{}, StepSpec{}, Activity{}, err
	}
	return instance, definition, step, compensation, nil
}

func (processor *CompensationWorkProcessor) execute(
	ctx context.Context,
	lease WorkLease,
	instance Instance,
	definition Definition,
	step StepSpec,
	compensation Activity,
	dispatch CompensationDispatch,
) (WorkDecision, error) {
	startedAt := processor.config.Clock.Now()
	start, err := NewCompensationAttemptStart(CompensationAttemptStartSpec{
		TransitionID: compensationProcessorTransitionID(lease.Work().ID(), "start"), Lease: lease,
		Instance: instance, Definition: definition, StartedAt: startedAt,
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidCompensationProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, start); err != nil {
		return WorkDecision{}, err
	}
	running, err := processor.inspect(ctx, lease.Work().InstanceID())
	if err != nil {
		return WorkDecision{}, err
	}
	progress, _ := running.Compensation(dispatch.StepName())
	request, err := NewActivityRequest(ActivityRequestSpec{
		InstanceID: running.ID(), Definition: running.Definition(), StepName: dispatch.StepName(),
		Attempt: dispatch.Attempt(), MaxAttempts: step.Compensation.Retry.MaxAttempts,
		IdempotencyKey: dispatch.IdempotencyKey(), StartedAt: progress.DueAt().Add(-step.Compensation.Timeout),
		Deadline: progress.DueAt(), Input: progress.Input(), InputLimit: step.InputLimit,
		ResultLimit: step.Compensation.ResultLimit, TenantID: lease.Work().TenantID(),
		CorrelationID: lease.Work().CorrelationID(),
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidCompensationProcessor
	}
	outcome := executeCompensationSafely(ctx, compensation, request)
	return processor.persistOutcome(ctx, lease.Work(), running, definition, dispatch, outcome)
}

func (processor *CompensationWorkProcessor) persistOutcome(
	ctx context.Context,
	work PendingWork,
	instance Instance,
	definition Definition,
	dispatch CompensationDispatch,
	outcome ActivityOutcome,
) (WorkDecision, error) {
	occurredAt := processor.config.Clock.Now()
	if occurredAt.Before(instance.UpdatedAt()) {
		occurredAt = instance.UpdatedAt()
	}
	transition, err := NewCompensationAttemptOutcome(CompensationAttemptOutcomeSpec{
		TransitionID: compensationProcessorTransitionID(work.ID(), "outcome"), Instance: instance,
		Definition: definition, StepName: dispatch.StepName(), Attempt: dispatch.Attempt(),
		OccurredAt: occurredAt, Outcome: outcome,
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidCompensationProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, transition); err != nil {
		return WorkDecision{}, err
	}
	updated, err := processor.inspect(ctx, work.InstanceID())
	if err != nil {
		return WorkDecision{}, err
	}
	if outcome.Kind() == ActivityFailed {
		step, _ := activityProcessorStep(definition, dispatch.StepName())
		return processor.scheduleRetry(ctx, work, updated, definition, step, dispatch)
	}
	return activityComplete()
}

func (processor *CompensationWorkProcessor) scheduleRetry(
	ctx context.Context,
	work PendingWork,
	instance Instance,
	definition Definition,
	step StepSpec,
	dispatch CompensationDispatch,
) (WorkDecision, error) {
	progress, _ := instance.Compensation(dispatch.StepName())
	if step.Compensation == nil || !progress.Retryable() || progress.Attempt() >= step.Compensation.Retry.MaxAttempts {
		return activityComplete()
	}
	scheduledAt := processor.config.Clock.Now()
	if scheduledAt.Before(instance.UpdatedAt()) {
		scheduledAt = instance.UpdatedAt()
	}
	if !scheduledAt.Add(retryDelay(step.Compensation.Retry, progress.Attempt())).Before(work.Deadline()) {
		return activityComplete()
	}
	retry, err := NewCompensationRetry(CompensationRetrySpec{
		TransitionID: compensationProcessorTransitionID(work.ID(), "retry"),
		WorkID:       compensationProcessorTransitionID(work.ID(), "retry-work"), Instance: instance,
		Definition: definition, StepName: dispatch.StepName(),
		IdempotencyKey: compensationProcessorTransitionID(work.ID(), "retry-key"),
		ScheduledAt:    scheduledAt, Deadline: work.Deadline(), TenantID: work.TenantID(),
		CorrelationID: work.CorrelationID(),
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidCompensationProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, retry); err != nil {
		return WorkDecision{}, err
	}
	return activityComplete()
}

func executeCompensationSafely(ctx context.Context, compensation Activity, request ActivityRequest) (outcome ActivityOutcome) {
	defer func() {
		if recover() != nil {
			outcome, _ = NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivityUnknown, Code: "compensation-panic"})
		}
	}()
	executed, err := compensation.Execute(ctx, request)
	if err == nil {
		return executed
	}
	unknown, _ := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivityUnknown, Code: "compensation-execution-unknown"})
	return unknown
}

func (processor *CompensationWorkProcessor) inspect(ctx context.Context, instanceID string) (Instance, error) {
	return InspectInstance(ctx, processor.config.Store, processor.config.Definitions, InstanceInspectionSpec{
		InstanceID: instanceID, PageSize: processor.config.PageSize,
		MaxEvents: processor.config.MaxHistoryEvents,
	})
}

func compensationProcessorTransitionID(workID, phase string) string {
	digest := sha256.Sum256([]byte(workID + "\x00" + phase))
	return compensationProcessorTransitionPrefix + phase + "-" + hex.EncodeToString(digest[:])
}

func compensationDeadLetter(code string) (WorkDecision, error) {
	return NewWorkDecision(WorkDecisionSpec{Kind: WorkDeadLetterDecision, Code: code})
}
