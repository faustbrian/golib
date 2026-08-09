package workflow

import (
	"cmp"
	"slices"
	"time"
)

// CompensationProgressStatus identifies durable compensating activity state.
type CompensationProgressStatus uint8

const (
	// CompensationReady is scheduled and eligible for attempt admission.
	CompensationReady CompensationProgressStatus = 1
	// CompensationRunning has one externally observable in-flight attempt.
	CompensationRunning CompensationProgressStatus = 2
	// CompensationSucceeded is a known successful compensating side effect.
	CompensationSucceeded CompensationProgressStatus = 3
	// CompensationFailed is a known failed attempt awaiting policy action.
	CompensationFailed CompensationProgressStatus = 4
	// CompensationUnknown requires reconciliation or manual resolution.
	CompensationUnknown CompensationProgressStatus = 5
	// CompensationRetryWaiting has a persisted next-attempt admission time.
	CompensationRetryWaiting CompensationProgressStatus = 6
	// CompensationManuallyResolved is an explicit operator disposition and is
	// never equivalent to successful rollback.
	CompensationManuallyResolved CompensationProgressStatus = 7
)

// CompensationProgress is immutable state reconstructed from persisted
// decisions only.
type CompensationProgress struct {
	stepName          string
	status            CompensationProgressStatus
	scheduledSequence uint64
	attempt           uint32
	idempotencyKey    string
	dueAt             time.Time
	input             []byte
	result            []byte
	code              string
	retryable         bool
}

// StepName returns the compensated activity step.
func (progress CompensationProgress) StepName() string { return progress.stepName }

// Status returns the durable compensation state.
func (progress CompensationProgress) Status() CompensationProgressStatus { return progress.status }

// ScheduledSequence returns persisted compensation ordering.
func (progress CompensationProgress) ScheduledSequence() uint64 { return progress.scheduledSequence }

// Attempt returns the latest one-based attempt.
func (progress CompensationProgress) Attempt() uint32 { return progress.attempt }

// IdempotencyKey returns the latest externally observable attempt identity.
func (progress CompensationProgress) IdempotencyKey() string { return progress.idempotencyKey }

// DueAt returns the attempt deadline or retry admission time.
func (progress CompensationProgress) DueAt() time.Time { return progress.dueAt }

// Input returns an owned copy of scheduled compensation input.
func (progress CompensationProgress) Input() []byte { return cloneBytes(progress.input) }

// Result returns an owned copy of result, failure detail, or manual evidence.
func (progress CompensationProgress) Result() []byte { return cloneBytes(progress.result) }

// Code returns the latest failure, unknown-outcome, or manual resolution code.
func (progress CompensationProgress) Code() string { return progress.code }

// Retryable reports the persisted known-failure retry classification.
func (progress CompensationProgress) Retryable() bool { return progress.retryable }

func validCompensationEventFields(spec HistoryEventSpec) bool {
	if !stableName.MatchString(spec.StepName) || spec.Definition != (DefinitionReference{}) || spec.SuccessorID != "" {
		return false
	}
	switch spec.Kind {
	case EventCompensationScheduled:
		return spec.Attempt == 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			spec.Code == "" && !spec.Retryable
	case EventCompensationAttemptStarted:
		return spec.Attempt > 0 && instanceIDPattern.MatchString(spec.IdempotencyKey) &&
			spec.DueAt.After(spec.OccurredAt) && spec.Code == "" && !spec.Retryable && len(spec.Data) == 0
	case EventCompensationAttemptSucceeded:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			spec.Code == "" && !spec.Retryable
	case EventCompensationAttemptFailed:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			stableName.MatchString(spec.Code)
	case EventCompensationAttemptUnknown:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			stableName.MatchString(spec.Code) && !spec.Retryable
	case EventCompensationRetryScheduled:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.After(spec.OccurredAt) &&
			spec.Code == "" && !spec.Retryable && len(spec.Data) == 0
	default:
		return spec.Attempt == 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			stableName.MatchString(spec.Code) && !spec.Retryable
	}
}

func (instance *Instance) applyCompensation(registry *Registry, event HistoryEvent) error {
	if event.kind < EventCompensationScheduled {
		return ErrInvalidTransition
	}
	if event.kind > EventCompensationManuallyResolved {
		return ErrInvalidTransition
	}
	definition, _ := registry.Resolve(instance.definition.Name(), instance.definition.Version())
	step, ok := definitionActivityStep(definition, event.stepName)
	if !ok {
		return ErrInvalidTransition
	}
	if step.Compensation == nil {
		return ErrInvalidTransition
	}
	policy := *step.Compensation
	progress, exists := instance.compensations[event.stepName]
	switch event.kind {
	case EventCompensationScheduled:
		activity, activityExists := instance.activities[event.stepName]
		if instance.status != StatusRunning && instance.status != StatusCancelling {
			return ErrInvalidTransition
		}
		if exists {
			return ErrInvalidTransition
		}
		if !activityExists {
			return ErrInvalidTransition
		}
		if activity.status != ActivityProgressSucceeded {
			return ErrInvalidTransition
		}
		instance.compensations[event.stepName] = CompensationProgress{
			stepName: event.stepName, status: CompensationReady,
			scheduledSequence: event.sequence, input: cloneBytes(event.data),
		}
	case EventCompensationAttemptStarted:
		if !exists {
			return ErrInvalidTransition
		}
		if progress.status != CompensationReady && progress.status != CompensationRetryWaiting {
			return ErrInvalidTransition
		}
		if event.attempt != progress.attempt+1 {
			return ErrInvalidTransition
		}
		if event.attempt > policy.Retry.MaxAttempts {
			return ErrInvalidTransition
		}
		if event.dueAt != canonicalTime(event.occurredAt.Add(policy.Timeout)) {
			return ErrInvalidTransition
		}
		if progress.status == CompensationRetryWaiting && event.occurredAt.Before(progress.dueAt) {
			return ErrInvalidTransition
		}
		progress.status = CompensationRunning
		progress.attempt = event.attempt
		progress.idempotencyKey = event.idempotencyKey
		progress.dueAt = event.dueAt
		progress.result = nil
		progress.code = ""
		progress.retryable = false
		instance.compensations[event.stepName] = progress
	case EventCompensationAttemptSucceeded, EventCompensationAttemptFailed, EventCompensationAttemptUnknown:
		if !exists {
			return ErrInvalidTransition
		}
		if progress.status != CompensationRunning {
			return ErrInvalidTransition
		}
		if event.attempt != progress.attempt {
			return ErrInvalidTransition
		}
		if len(event.data) > int(policy.ResultLimit) {
			return ErrInvalidTransition
		}
		progress.dueAt = time.Time{}
		progress.result = cloneBytes(event.data)
		progress.code = event.code
		progress.retryable = event.retryable
		switch event.kind {
		case EventCompensationAttemptSucceeded:
			progress.status = CompensationSucceeded
		case EventCompensationAttemptFailed:
			progress.status = CompensationFailed
		default:
			progress.status = CompensationUnknown
		}
		instance.compensations[event.stepName] = progress
	case EventCompensationRetryScheduled:
		if !exists {
			return ErrInvalidTransition
		}
		if progress.status != CompensationFailed {
			return ErrInvalidTransition
		}
		if !progress.retryable {
			return ErrInvalidTransition
		}
		if progress.attempt >= policy.Retry.MaxAttempts {
			return ErrInvalidTransition
		}
		if event.attempt != progress.attempt {
			return ErrInvalidTransition
		}
		if event.dueAt != canonicalTime(event.occurredAt.Add(retryDelay(policy.Retry, event.attempt))) {
			return ErrInvalidTransition
		}
		progress.status = CompensationRetryWaiting
		progress.dueAt = event.dueAt
		instance.compensations[event.stepName] = progress
	case EventCompensationManuallyResolved:
		if !exists {
			return ErrInvalidTransition
		}
		if progress.status != CompensationFailed && progress.status != CompensationUnknown {
			return ErrInvalidTransition
		}
		if len(event.data) > int(policy.ResultLimit) {
			return ErrInvalidTransition
		}
		progress.status = CompensationManuallyResolved
		progress.result = cloneBytes(event.data)
		progress.code = event.code
		progress.retryable = false
		instance.compensations[event.stepName] = progress
	}
	return nil
}

func cloneCompensationProgress(progress CompensationProgress) CompensationProgress {
	progress.input = cloneBytes(progress.input)
	progress.result = cloneBytes(progress.result)
	return progress
}

func sortedCompensationProgress(progress map[string]CompensationProgress) []CompensationProgress {
	if len(progress) == 0 {
		return nil
	}
	result := make([]CompensationProgress, 0, len(progress))
	for _, item := range progress {
		result = append(result, cloneCompensationProgress(item))
	}
	slices.SortFunc(result, func(left, right CompensationProgress) int {
		return cmp.Compare(left.scheduledSequence, right.scheduledSequence)
	})
	return result
}

type compensationProgressSnapshot struct {
	StepName          string
	Status            CompensationProgressStatus
	ScheduledSequence uint64
	Attempt           uint32
	IdempotencyKey    string
	DueAt             time.Time
	Input             []byte
	Result            []byte
	Code              string
	Retryable         bool
}

func compensationProgressSnapshots(progress map[string]CompensationProgress) []compensationProgressSnapshot {
	items := sortedCompensationProgress(progress)
	if items == nil {
		return nil
	}
	result := make([]compensationProgressSnapshot, 0, len(items))
	for _, item := range items {
		result = append(result, compensationProgressSnapshot{
			StepName: item.stepName, Status: item.status, ScheduledSequence: item.scheduledSequence,
			Attempt: item.attempt, IdempotencyKey: item.idempotencyKey, DueAt: item.dueAt,
			Input: item.input, Result: item.result, Code: item.code, Retryable: item.retryable,
		})
	}
	return result
}
