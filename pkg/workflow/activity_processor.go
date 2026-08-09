package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const activityProcessorTransitionPrefix = "activity-processor-"

var (
	// ErrInvalidActivityProcessor classifies malformed activity processor
	// configuration or durable work that cannot represent a valid attempt.
	ErrInvalidActivityProcessor = errors.New("invalid workflow activity processor")
)

// ActivityExecutionStore is the narrow durable contract needed to execute an
// activity. Commit and reconciliation must address the same durable store.
type ActivityExecutionStore interface {
	TransitionStore
	ReconcileTransition(context.Context, TransitionReconciliation) (TransitionReconciliationOutcome, error)
}

// ActivityWorkProcessorConfig supplies explicit bounded activity execution
// dependencies. Definitions and activities are immutable explicit registries.
type ActivityWorkProcessorConfig struct {
	Store            ActivityExecutionStore
	Definitions      *Registry
	Activities       *ActivityRegistry
	Clock            Clock
	PageSize         uint32
	MaxHistoryEvents uint32
}

// ActivityWorkProcessor executes leased activity work without claiming
// exactly-once external effects. It persists attempt start before invocation
// and converts an already-running redelivery to an unknown durable outcome.
type ActivityWorkProcessor struct{ config ActivityWorkProcessorConfig }

// NewActivityWorkProcessor validates one bounded explicit processor.
func NewActivityWorkProcessor(config ActivityWorkProcessorConfig) (*ActivityWorkProcessor, error) {
	if config.Store == nil || config.Definitions == nil || config.Activities == nil || config.Clock == nil ||
		!validHistoryTraversal("processor-instance", config.PageSize, config.MaxHistoryEvents) {
		return nil, ErrInvalidActivityProcessor
	}
	return &ActivityWorkProcessor{config: config}, nil
}

// Process persists each externally observable activity boundary. Poison work
// is dead-lettered; store failures retain the lease for fenced recovery.
func (processor *ActivityWorkProcessor) Process(ctx context.Context, lease WorkLease) (WorkDecision, error) {
	if processor == nil || ctx == nil || !lease.Valid() || lease.Work().Kind() != WorkActivity {
		return WorkDecision{}, ErrInvalidActivityProcessor
	}
	work := lease.Work()
	dispatch, err := DecodeActivityDispatch(work.Payload())
	if err != nil {
		return activityDeadLetter("invalid-activity-dispatch")
	}
	instance, definition, step, activity, err := processor.load(ctx, work, dispatch)
	if err != nil {
		if errors.Is(err, ErrActivityNotFound) || errors.Is(err, ErrInvalidActivityProcessor) {
			return activityDeadLetter("invalid-activity-definition")
		}
		return WorkDecision{}, err
	}
	progress, exists := instance.Activity(dispatch.StepName())
	if !exists {
		return activityDeadLetter("invalid-activity-state")
	}
	return processor.processProgress(ctx, lease, instance, definition, step, activity, dispatch, progress)
}

func (processor *ActivityWorkProcessor) processProgress(
	ctx context.Context,
	lease WorkLease,
	instance Instance,
	definition Definition,
	step StepSpec,
	activity Activity,
	dispatch ActivityDispatch,
	progress ActivityProgress,
) (WorkDecision, error) {
	switch progress.Status() {
	case ActivityProgressReady, ActivityProgressRetryWaiting:
		if dispatch.Attempt() != progress.Attempt()+1 {
			return activityDeadLetter("invalid-activity-state")
		}
		return processor.execute(ctx, lease, instance, definition, step, activity, dispatch)
	case ActivityProgressRunning:
		if progress.Attempt() != dispatch.Attempt() || progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return activityDeadLetter("invalid-activity-state")
		}
		unknown, _ := NewActivityOutcome(ActivityOutcomeSpec{
			Kind: ActivityUnknown, Code: "activity-outcome-unknown",
		})
		return processor.persistOutcome(ctx, lease.Work(), instance, definition, dispatch, unknown)
	case ActivityProgressFailed:
		if progress.Attempt() != dispatch.Attempt() || progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return activityDeadLetter("invalid-activity-state")
		}
		return processor.scheduleRetry(ctx, lease.Work(), instance, definition, step, dispatch)
	case ActivityProgressSucceeded, ActivityProgressUnknown:
		if progress.Attempt() != dispatch.Attempt() || progress.IdempotencyKey() != dispatch.IdempotencyKey() {
			return activityDeadLetter("invalid-activity-state")
		}
		return activityComplete()
	default:
		return activityDeadLetter("invalid-activity-state")
	}
}

func (processor *ActivityWorkProcessor) load(
	ctx context.Context,
	work PendingWork,
	dispatch ActivityDispatch,
) (Instance, Definition, StepSpec, Activity, error) {
	instance, err := InspectInstance(ctx, processor.config.Store, processor.config.Definitions, InstanceInspectionSpec{
		InstanceID: work.InstanceID(), PageSize: processor.config.PageSize,
		MaxEvents: processor.config.MaxHistoryEvents,
	})
	if err != nil {
		return Instance{}, Definition{}, StepSpec{}, Activity{}, err
	}
	definition, _ := processor.config.Definitions.Resolve(instance.Definition().Name(), instance.Definition().Version())
	step, ok := activityProcessorStep(definition, dispatch.StepName())
	if !ok || dispatch.Attempt() > step.Retry.MaxAttempts || instance.Sequence() < work.Sequence() {
		return Instance{}, Definition{}, StepSpec{}, Activity{}, ErrInvalidActivityProcessor
	}
	activity, err := processor.config.Activities.Resolve(step.Target)
	if err != nil {
		return Instance{}, Definition{}, StepSpec{}, Activity{}, err
	}
	return instance, definition, step, activity, nil
}

func (processor *ActivityWorkProcessor) execute(
	ctx context.Context,
	lease WorkLease,
	instance Instance,
	definition Definition,
	step StepSpec,
	activity Activity,
	dispatch ActivityDispatch,
) (WorkDecision, error) {
	startedAt := processor.config.Clock.Now()
	start, err := NewActivityAttemptStart(ActivityAttemptStartSpec{
		TransitionID: processorTransitionID(lease.Work().ID(), "start"), Lease: lease,
		Instance: instance, Definition: definition, StartedAt: startedAt,
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidActivityProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, start); err != nil {
		return WorkDecision{}, err
	}
	running, err := processor.inspect(ctx, lease.Work().InstanceID())
	if err != nil {
		return WorkDecision{}, err
	}
	progress, _ := running.Activity(dispatch.StepName())
	request, err := NewActivityRequest(ActivityRequestSpec{
		InstanceID: running.ID(), Definition: running.Definition(), StepName: dispatch.StepName(),
		Attempt: dispatch.Attempt(), MaxAttempts: step.Retry.MaxAttempts,
		IdempotencyKey: dispatch.IdempotencyKey(), StartedAt: progress.DueAt().Add(-step.Timeout),
		Deadline: progress.DueAt(), Input: progress.Input(), InputLimit: step.InputLimit,
		ResultLimit: step.ResultLimit, TenantID: lease.Work().TenantID(),
		CorrelationID: lease.Work().CorrelationID(),
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidActivityProcessor
	}
	outcome := executeActivitySafely(ctx, activity, request)
	return processor.persistOutcome(ctx, lease.Work(), running, definition, dispatch, outcome)
}

func (processor *ActivityWorkProcessor) persistOutcome(
	ctx context.Context,
	work PendingWork,
	instance Instance,
	definition Definition,
	dispatch ActivityDispatch,
	outcome ActivityOutcome,
) (WorkDecision, error) {
	occurredAt := processor.config.Clock.Now()
	if occurredAt.Before(instance.UpdatedAt()) {
		occurredAt = instance.UpdatedAt()
	}
	transition, err := NewActivityAttemptOutcome(ActivityAttemptOutcomeSpec{
		TransitionID: processorTransitionID(work.ID(), "outcome"), Instance: instance,
		Definition: definition, StepName: dispatch.StepName(), Attempt: dispatch.Attempt(),
		OccurredAt: occurredAt, Outcome: outcome,
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidActivityProcessor
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

func (processor *ActivityWorkProcessor) scheduleRetry(
	ctx context.Context,
	work PendingWork,
	instance Instance,
	definition Definition,
	step StepSpec,
	dispatch ActivityDispatch,
) (WorkDecision, error) {
	progress, _ := instance.Activity(dispatch.StepName())
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
	retry, err := NewActivityRetry(ActivityRetrySpec{
		TransitionID: processorTransitionID(work.ID(), "retry"),
		WorkID:       processorTransitionID(work.ID(), "retry-work"), Instance: instance,
		Definition: definition, StepName: dispatch.StepName(),
		IdempotencyKey: processorTransitionID(work.ID(), "retry-key"),
		ScheduledAt:    scheduledAt, Deadline: work.Deadline(), TenantID: work.TenantID(),
		CorrelationID: work.CorrelationID(),
	})
	if err != nil {
		return WorkDecision{}, ErrInvalidActivityProcessor
	}
	if err := commitActivityTransition(ctx, processor.config.Store, retry); err != nil {
		return WorkDecision{}, err
	}
	return activityComplete()
}

func executeActivitySafely(ctx context.Context, activity Activity, request ActivityRequest) (outcome ActivityOutcome) {
	defer func() {
		if recover() != nil {
			outcome, _ = NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivityUnknown, Code: "activity-panic"})
		}
	}()
	executed, err := activity.Execute(ctx, request)
	if err == nil {
		return executed
	}
	unknown, _ := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivityUnknown, Code: "activity-execution-unknown"})
	return unknown
}

func commitActivityTransition(ctx context.Context, store ActivityExecutionStore, transition Transition) error {
	err := store.Commit(ctx, transition)
	if err == nil || StoreCommitOutcomeOf(err) == StoreCommitCommitted {
		return nil
	}
	if StoreCommitOutcomeOf(err) == StoreCommitNotCommitted {
		return err
	}
	reconciliation, _ := NewTransitionReconciliation(TransitionReconciliationSpec{
		TransitionID: transition.ID(), Fingerprint: transition.Fingerprint(),
	})
	outcome, reconcileErr := store.ReconcileTransition(ctx, reconciliation)
	if reconcileErr != nil {
		return reconcileErr
	}
	switch outcome {
	case TransitionCommitted:
		return nil
	case TransitionConflicting:
		return ErrDuplicateTransition
	case TransitionMissing:
		return store.Commit(ctx, transition)
	default:
		return ErrInvalidStoreRequest
	}
}

func (processor *ActivityWorkProcessor) inspect(ctx context.Context, instanceID string) (Instance, error) {
	return InspectInstance(ctx, processor.config.Store, processor.config.Definitions, InstanceInspectionSpec{
		InstanceID: instanceID, PageSize: processor.config.PageSize,
		MaxEvents: processor.config.MaxHistoryEvents,
	})
}

func activityProcessorStep(definition Definition, name string) (StepSpec, bool) {
	for _, step := range definition.Steps() {
		if step.Name == name && step.Kind == StepActivity {
			return step, true
		}
	}
	return StepSpec{}, false
}

func processorTransitionID(workID, phase string) string {
	digest := sha256.Sum256([]byte(workID + "\x00" + phase))
	return activityProcessorTransitionPrefix + phase + "-" + hex.EncodeToString(digest[:])
}

func activityComplete() (WorkDecision, error) {
	return NewWorkDecision(WorkDecisionSpec{Kind: WorkComplete})
}

func activityDeadLetter(code string) (WorkDecision, error) {
	return NewWorkDecision(WorkDecisionSpec{Kind: WorkDeadLetterDecision, Code: code})
}
