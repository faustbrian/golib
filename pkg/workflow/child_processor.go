package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const childProcessorTransitionPrefix = "child-processor-"

var (
	// ErrInvalidChildProcessor classifies malformed processor configuration or
	// poison durable child work.
	ErrInvalidChildProcessor = errors.New("invalid workflow child processor")
)

// ChildWorkProcessorConfig supplies explicit bounded child-start dependencies.
type ChildWorkProcessorConfig struct {
	Store            ActivityExecutionStore
	Definitions      *Registry
	Starter          ChildStarter
	Clock            Clock
	PageSize         uint32
	MaxHistoryEvents uint32
}

// ChildWorkProcessor starts pinned child instances without claiming exactly
// once creation. It persists the start boundary before invoking the adapter.
type ChildWorkProcessor struct{ config ChildWorkProcessorConfig }

// NewChildWorkProcessor validates one explicit bounded processor.
func NewChildWorkProcessor(config ChildWorkProcessorConfig) (*ChildWorkProcessor, error) {
	if config.Store == nil || config.Definitions == nil || config.Starter == nil || config.Clock == nil ||
		!validHistoryTraversal("processor-instance", config.PageSize, config.MaxHistoryEvents) {
		return nil, ErrInvalidChildProcessor
	}
	return &ChildWorkProcessor{config: config}, nil
}

// Process persists every child-creation boundary. Poison work is dead-lettered
// and an in-flight redelivery becomes an explicit unknown outcome.
func (processor *ChildWorkProcessor) Process(ctx context.Context, lease WorkLease) (WorkDecision, error) {
	if processor == nil || ctx == nil || !lease.Valid() || lease.Work().Kind() != WorkChild {
		return WorkDecision{}, ErrInvalidChildProcessor
	}
	work := lease.Work()
	dispatch, err := DecodeChildDispatch(work.Payload())
	if err != nil {
		return activityDeadLetter("invalid-child-dispatch")
	}
	instance, definition, step, err := processor.load(ctx, work, dispatch)
	if err != nil {
		if errors.Is(err, ErrInvalidChildProcessor) {
			return activityDeadLetter("invalid-child-definition")
		}
		return WorkDecision{}, err
	}
	progress, exists := instance.Child(dispatch.StepName())
	if !exists {
		return activityDeadLetter("invalid-child-state")
	}
	return processor.processProgress(ctx, lease, instance, definition, step, dispatch, progress)
}

func (processor *ChildWorkProcessor) processProgress(
	ctx context.Context,
	lease WorkLease,
	instance Instance,
	definition Definition,
	step StepSpec,
	dispatch ChildDispatch,
	progress ChildProgress,
) (WorkDecision, error) {
	switch progress.Status() {
	case ChildScheduled, ChildStartRetryWaiting:
		if dispatch.Attempt() != progress.Attempt()+1 {
			return activityDeadLetter("invalid-child-state")
		}
		return processor.execute(ctx, lease, instance, definition, step, dispatch)
	case ChildStartRunning:
		if progress.Attempt() != dispatch.Attempt() ||
			progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return activityDeadLetter("invalid-child-state")
		}
		unknown, _ := NewChildStartOutcome(ChildStartOutcomeSpec{
			Kind: ChildStartUnknown, Code: "child-start-outcome-unknown",
		})
		return processor.persistOutcome(ctx, lease.Work(), instance, definition, dispatch, unknown)
	case ChildStartFailedStatus:
		if progress.Attempt() != dispatch.Attempt() ||
			progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return activityDeadLetter("invalid-child-state")
		}
		return processor.scheduleRetry(ctx, lease.Work(), instance, definition, step, dispatch)
	case ChildActive, ChildStartUnknownStatus, ChildSucceeded, ChildFailed:
		if progress.Attempt() != dispatch.Attempt() ||
			progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return activityDeadLetter("invalid-child-state")
		}
		return activityComplete()
	default:
		return activityDeadLetter("invalid-child-state")
	}
}

func (processor *ChildWorkProcessor) load(
	ctx context.Context,
	work PendingWork,
	dispatch ChildDispatch,
) (Instance, Definition, StepSpec, error) {
	instance, err := processor.inspect(ctx, work.InstanceID())
	if err != nil {
		return Instance{}, Definition{}, StepSpec{}, err
	}
	definition, err := processor.config.Definitions.Resolve(
		instance.Definition().Name(), instance.Definition().Version(),
	)
	if err != nil {
		return Instance{}, Definition{}, StepSpec{}, ErrInvalidChildProcessor
	}
	step, ok := definitionStep(definition, dispatch.StepName(), StepChild)
	if !ok || step.ChildDefinition != dispatch.Definition() ||
		dispatch.Attempt() > step.Retry.MaxAttempts || instance.Sequence() < work.Sequence() {
		return Instance{}, Definition{}, StepSpec{}, ErrInvalidChildProcessor
	}
	return instance, definition, step, nil
}

func (processor *ChildWorkProcessor) execute(
	ctx context.Context,
	lease WorkLease,
	instance Instance,
	definition Definition,
	step StepSpec,
	dispatch ChildDispatch,
) (WorkDecision, error) {
	start, err := NewChildStartAttempt(ChildStartAttemptSpec{
		TransitionID: childProcessorTransitionID(lease.Work().ID(), "start"),
		Lease:        lease, Instance: instance, Definition: definition,
		StartedAt: processor.config.Clock.Now(),
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidChildProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, start); err != nil {
		return WorkDecision{}, err
	}
	running, err := processor.inspect(ctx, instance.ID())
	if err != nil {
		return WorkDecision{}, err
	}
	progress, _ := running.Child(dispatch.StepName())
	request, err := NewChildStartRequest(ChildStartRequestSpec{
		ParentInstanceID: running.ID(), ParentDefinition: running.Definition(),
		StepName: dispatch.StepName(), ChildID: dispatch.ChildID(),
		ChildDefinition: dispatch.Definition(), Attempt: dispatch.Attempt(),
		MaxAttempts: step.Retry.MaxAttempts, IdempotencyKey: dispatch.IdempotencyKey(),
		StartedAt: progress.DueAt().Add(-step.Timeout), Deadline: progress.DueAt(),
		Input: progress.Input(), InputLimit: step.InputLimit,
		TenantID: lease.Work().TenantID(), CorrelationID: lease.Work().CorrelationID(),
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidChildProcessor
	}
	outcome := executeChildStartSafely(ctx, processor.config.Starter, request)
	return processor.persistOutcome(ctx, lease.Work(), running, definition, dispatch, outcome)
}

func (processor *ChildWorkProcessor) persistOutcome(
	ctx context.Context,
	work PendingWork,
	instance Instance,
	definition Definition,
	dispatch ChildDispatch,
	outcome ChildStartOutcome,
) (WorkDecision, error) {
	occurredAt := processor.config.Clock.Now()
	if occurredAt.Before(instance.UpdatedAt()) {
		occurredAt = instance.UpdatedAt()
	}
	transition, err := NewChildStartAttemptOutcome(ChildStartAttemptOutcomeSpec{
		TransitionID: childProcessorTransitionID(work.ID(), "outcome"),
		Instance:     instance, Definition: definition, StepName: dispatch.StepName(),
		ChildID: dispatch.ChildID(), Attempt: dispatch.Attempt(),
		OccurredAt: occurredAt, Outcome: outcome,
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidChildProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, transition); err != nil {
		return WorkDecision{}, err
	}
	if outcome.Kind() != ChildStartFailed {
		return activityComplete()
	}
	updated, err := processor.inspect(ctx, work.InstanceID())
	if err != nil {
		return WorkDecision{}, err
	}
	step, _ := definitionStep(definition, dispatch.StepName(), StepChild)
	return processor.scheduleRetry(ctx, work, updated, definition, step, dispatch)
}

func (processor *ChildWorkProcessor) scheduleRetry(
	ctx context.Context,
	work PendingWork,
	instance Instance,
	definition Definition,
	step StepSpec,
	dispatch ChildDispatch,
) (WorkDecision, error) {
	progress, _ := instance.Child(dispatch.StepName())
	if !progress.Retryable() || progress.Attempt() >= step.Retry.MaxAttempts {
		return activityComplete()
	}
	scheduledAt := processor.config.Clock.Now()
	if scheduledAt.Before(instance.UpdatedAt()) {
		scheduledAt = instance.UpdatedAt()
	}
	if !scheduledAt.Add(retryDelay(step.Retry, progress.Attempt())).Before(work.Deadline()) {
		return activityComplete()
	}
	retry, err := NewChildStartRetry(ChildStartRetrySpec{
		TransitionID: childProcessorTransitionID(work.ID(), "retry"),
		WorkID:       childProcessorTransitionID(work.ID(), "retry-work"),
		Instance:     instance, Definition: definition, StepName: dispatch.StepName(),
		ScheduledAt: scheduledAt, Deadline: work.Deadline(),
		TenantID: work.TenantID(), CorrelationID: work.CorrelationID(),
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidChildProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, retry); err != nil {
		return WorkDecision{}, err
	}
	return activityComplete()
}

func executeChildStartSafely(
	ctx context.Context,
	starter ChildStarter,
	request ChildStartRequest,
) (outcome ChildStartOutcome) {
	defer func() {
		if recover() != nil {
			outcome, _ = NewChildStartOutcome(ChildStartOutcomeSpec{
				Kind: ChildStartUnknown, Code: "child-start-panic",
			})
		}
	}()
	outcome = starter.Start(ctx, request)
	if !outcome.valid() {
		outcome, _ = NewChildStartOutcome(ChildStartOutcomeSpec{
			Kind: ChildStartUnknown, Code: "child-start-invalid-outcome",
		})
	}
	return outcome
}

func (processor *ChildWorkProcessor) inspect(ctx context.Context, instanceID string) (Instance, error) {
	return InspectInstance(ctx, processor.config.Store, processor.config.Definitions, InstanceInspectionSpec{
		InstanceID: instanceID, PageSize: processor.config.PageSize,
		MaxEvents: processor.config.MaxHistoryEvents,
	})
}

func childProcessorTransitionID(workID, phase string) string {
	digest := sha256.Sum256([]byte(workID + "\x00" + phase))
	return childProcessorTransitionPrefix + phase + "-" + hex.EncodeToString(digest[:])
}
