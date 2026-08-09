package workflow

import (
	"encoding/json"
	"time"
)

// OperatorActivityRetrySpec supplies one caller-authorized audited activity
// retry command. The command records audit history and retry due work in one
// optimistic transition.
type OperatorActivityRetrySpec struct {
	CommandID      string
	WorkID         string
	Instance       Instance
	Definition     Definition
	StepName       string
	IdempotencyKey string
	Actor          string
	Reason         string
	OccurredAt     time.Time
	Deadline       time.Time
	TenantID       string
	CorrelationID  string
}

// NewOperatorActivityRetry atomically records caller-supplied authorization
// audit data before admitting a definition-policy activity retry.
func NewOperatorActivityRetry(spec OperatorActivityRetrySpec) (Transition, error) {
	audit, commandInstance, err := newOperatorWorkAudit(
		spec.CommandID, spec.Instance, OperatorRetryActivity, spec.Actor, spec.Reason, spec.OccurredAt,
	)
	if err != nil {
		return Transition{}, ErrInvalidOperatorCommand
	}
	retry, err := NewActivityRetry(ActivityRetrySpec{
		TransitionID: spec.CommandID, WorkID: spec.WorkID, Instance: commandInstance,
		Definition: spec.Definition, StepName: spec.StepName, IdempotencyKey: spec.IdempotencyKey,
		ScheduledAt: spec.OccurredAt, Deadline: spec.Deadline,
		TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
	})
	if err != nil {
		return Transition{}, ErrInvalidOperatorCommand
	}
	return newOperatorWorkTransition(spec.CommandID, spec.Instance, audit, retry)
}

// OperatorCompensationSpec supplies one caller-authorized audited explicit
// compensation command.
type OperatorCompensationSpec struct {
	CommandID      string
	WorkID         string
	Instance       Instance
	Definition     Definition
	StepName       string
	Attempt        uint32
	IdempotencyKey string
	Actor          string
	Reason         string
	OccurredAt     time.Time
	Deadline       time.Time
	Input          []byte
	TenantID       string
	CorrelationID  string
}

// NewOperatorCompensation atomically records audit data before scheduling the
// requested compensation and its first durable work item.
func NewOperatorCompensation(spec OperatorCompensationSpec) (Transition, error) {
	audit, commandInstance, err := newOperatorWorkAudit(
		spec.CommandID, spec.Instance, OperatorCompensate, spec.Actor, spec.Reason, spec.OccurredAt,
	)
	if err != nil {
		return Transition{}, ErrInvalidOperatorCommand
	}
	compensation, err := NewCompensationSchedule(CompensationScheduleSpec{
		TransitionID: spec.CommandID, WorkID: spec.WorkID, Instance: commandInstance,
		Definition: spec.Definition, StepName: spec.StepName, Attempt: spec.Attempt,
		IdempotencyKey: spec.IdempotencyKey, ScheduledAt: spec.OccurredAt,
		Deadline: spec.Deadline, Input: spec.Input, TenantID: spec.TenantID,
		CorrelationID: spec.CorrelationID,
	})
	if err != nil {
		return Transition{}, ErrInvalidOperatorCommand
	}
	return newOperatorWorkTransition(spec.CommandID, spec.Instance, audit, compensation)
}

// OperatorCompensationResolutionSpec supplies caller-authorized audit data and
// bounded manual reconciliation evidence for one failed or unknown
// compensation.
type OperatorCompensationResolutionSpec struct {
	CommandID  string
	Instance   Instance
	Definition Definition
	StepName   string
	Actor      string
	Reason     string
	Code       string
	Evidence   []byte
	OccurredAt time.Time
}

// NewOperatorCompensationResolution records manual reconciliation as its own
// durable outcome. It never emits CompensationSucceeded.
func NewOperatorCompensationResolution(spec OperatorCompensationResolutionSpec) (Transition, error) {
	audit, commandInstance, err := newOperatorWorkAudit(
		spec.CommandID, spec.Instance, OperatorResolveCompensation,
		spec.Actor, spec.Reason, spec.OccurredAt,
	)
	if err != nil || spec.Definition.Reference() != spec.Instance.definition {
		return Transition{}, ErrInvalidOperatorCommand
	}
	step, ok := definitionActivityStep(spec.Definition, spec.StepName)
	progress, exists := spec.Instance.Compensation(spec.StepName)
	if !ok || step.Compensation == nil || !exists ||
		(progress.Status() != CompensationFailed && progress.Status() != CompensationUnknown) ||
		!stableName.MatchString(spec.Code) || len(spec.Evidence) > int(step.Compensation.ResultLimit) {
		return Transition{}, ErrInvalidOperatorCommand
	}
	resolved, _ := NewHistoryEvent(HistoryEventSpec{
		Sequence: commandInstance.sequence + 1, InstanceID: commandInstance.id,
		Kind: EventCompensationManuallyResolved, OccurredAt: spec.OccurredAt,
		StepName: spec.StepName, Code: spec.Code, Data: spec.Evidence,
	})
	action, _ := NewTransition(TransitionSpec{
		ID: spec.CommandID, InstanceID: commandInstance.id,
		ExpectedSequence: commandInstance.sequence, Definition: commandInstance.definition,
		Events: []HistoryEvent{resolved},
	})
	return newOperatorWorkTransition(spec.CommandID, spec.Instance, audit, action)
}

func newOperatorWorkAudit(
	commandID string,
	instance Instance,
	action OperatorAction,
	actor string,
	reason string,
	occurredAt time.Time,
) (HistoryEvent, Instance, error) {
	if !instanceIDPattern.MatchString(commandID) || occurredAt.IsZero() ||
		occurredAt.Before(instance.updatedAt) || instance.sequence > ^uint64(0)-2 ||
		!instanceIDPattern.MatchString(instance.id) || !instance.definition.valid() {
		return HistoryEvent{}, Instance{}, ErrInvalidOperatorCommand
	}
	document := operatorAuditDocument{Action: action, Actor: actor, Reason: reason}
	auditData, _ := json.Marshal(document)
	if _, err := decodeOperatorAudit(auditData); err != nil {
		return HistoryEvent{}, Instance{}, ErrInvalidOperatorCommand
	}
	occurredAt = canonicalTime(occurredAt)
	audit, _ := NewHistoryEvent(HistoryEventSpec{
		Sequence: instance.sequence + 1, InstanceID: instance.id,
		Kind: EventOperatorCommandRecorded, OccurredAt: occurredAt,
		IdempotencyKey: commandID, Data: auditData,
	})
	commandInstance := instance
	commandInstance.sequence++
	commandInstance.updatedAt = occurredAt
	return audit, commandInstance, nil
}

func newOperatorWorkTransition(
	commandID string,
	instance Instance,
	audit HistoryEvent,
	action Transition,
) (Transition, error) {
	events := append([]HistoryEvent{audit}, action.Events()...)
	transition, err := NewTransition(TransitionSpec{
		ID: commandID, InstanceID: instance.id, ExpectedSequence: instance.sequence,
		Definition: instance.definition, Events: events, Work: action.Work(),
	})
	if err != nil {
		return Transition{}, ErrInvalidOperatorCommand
	}
	return transition, nil
}
