package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const (
	// MaxCompensationDispatchBytes bounds internal durable dispatch metadata.
	MaxCompensationDispatchBytes = 1 << 10
)

var (
	// ErrInvalidCompensation classifies malformed compensation persistence or
	// dispatch requests.
	ErrInvalidCompensation = errors.New("invalid workflow compensation")
)

// CompensationDispatch is immutable durable metadata for one semantic
// compensation attempt. Lease redelivery retains the same idempotency key.
type CompensationDispatch struct {
	stepName       string
	attempt        uint32
	idempotencyKey string
}

// StepName returns the compensated activity step.
func (dispatch CompensationDispatch) StepName() string { return dispatch.stepName }

// Attempt returns the one-based semantic compensation attempt.
func (dispatch CompensationDispatch) Attempt() uint32 { return dispatch.attempt }

// IdempotencyKey returns the stable external side-effect identity.
func (dispatch CompensationDispatch) IdempotencyKey() string { return dispatch.idempotencyKey }

type compensationDispatchDocument struct {
	StepName       string `json:"step_name"`
	Attempt        uint32 `json:"attempt"`
	IdempotencyKey string `json:"idempotency_key"`
}

// DecodeCompensationDispatch validates bounded durable work metadata.
func DecodeCompensationDispatch(payload []byte) (CompensationDispatch, error) {
	if len(payload) == 0 {
		return CompensationDispatch{}, ErrInvalidCompensation
	}
	if len(payload) > MaxCompensationDispatchBytes {
		return CompensationDispatch{}, ErrInvalidCompensation
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var document compensationDispatchDocument
	if err := decoder.Decode(&document); err != nil {
		return CompensationDispatch{}, ErrInvalidCompensation
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return CompensationDispatch{}, ErrInvalidCompensation
	}
	dispatch := CompensationDispatch{
		stepName: document.StepName, attempt: document.Attempt, idempotencyKey: document.IdempotencyKey,
	}
	if !stableName.MatchString(dispatch.stepName) || dispatch.attempt == 0 ||
		!instanceIDPattern.MatchString(dispatch.idempotencyKey) {
		return CompensationDispatch{}, ErrInvalidCompensation
	}
	return dispatch, nil
}

// CompensationScheduleSpec supplies atomic history and durable dispatch for
// the first attempt of one explicit compensation.
type CompensationScheduleSpec struct {
	TransitionID     string
	WorkID           string
	InstanceID       string
	ExpectedSequence uint64
	Definition       Definition
	StepName         string
	Attempt          uint32
	IdempotencyKey   string
	ScheduledAt      time.Time
	Deadline         time.Time
	Input            []byte
	TenantID         string
	CorrelationID    string
}

// NewCompensationSchedule atomically schedules compensation history and work.
func NewCompensationSchedule(spec CompensationScheduleSpec) (Transition, error) {
	step, ok := definitionActivityStep(spec.Definition, spec.StepName)
	if !ok {
		return Transition{}, ErrInvalidCompensation
	}
	if step.Compensation == nil {
		return Transition{}, ErrInvalidCompensation
	}
	if spec.Attempt != 1 {
		return Transition{}, ErrInvalidCompensation
	}
	if !instanceIDPattern.MatchString(spec.IdempotencyKey) {
		return Transition{}, ErrInvalidCompensation
	}
	document := compensationDispatchDocument{
		StepName: spec.StepName, Attempt: spec.Attempt, IdempotencyKey: spec.IdempotencyKey,
	}
	payload, _ := json.Marshal(document)
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.ExpectedSequence + 1, InstanceID: spec.InstanceID,
		Kind: EventCompensationScheduled, OccurredAt: spec.ScheduledAt,
		StepName: spec.StepName, Data: spec.Input,
	})
	if err != nil {
		return Transition{}, ErrInvalidCompensation
	}
	work, err := NewPendingWork(PendingWorkSpec{
		ID: spec.WorkID, Kind: WorkCompensation, InstanceID: spec.InstanceID, Sequence: event.Sequence(),
		AvailableAt: spec.ScheduledAt, Deadline: spec.Deadline, Payload: payload,
		TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
	})
	if err != nil {
		return Transition{}, ErrInvalidCompensation
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: spec.InstanceID, ExpectedSequence: spec.ExpectedSequence,
		Definition: spec.Definition.Reference(), Events: []HistoryEvent{event}, Work: []PendingWork{work},
	})
	if err != nil {
		return Transition{}, ErrInvalidCompensation
	}
	return transition, nil
}

// CompensationAttemptStartSpec supplies the persisted decision required before
// executing one leased compensating side effect.
type CompensationAttemptStartSpec struct {
	TransitionID     string
	Lease            WorkLease
	ExpectedSequence uint64
	Definition       Definition
	StartedAt        time.Time
}

// NewCompensationAttemptStart creates the attempt-start transition that must
// commit before external compensation begins.
func NewCompensationAttemptStart(spec CompensationAttemptStartSpec) (Transition, error) {
	if !spec.Lease.Valid() {
		return Transition{}, ErrInvalidCompensation
	}
	if spec.Lease.Work().Kind() != WorkCompensation {
		return Transition{}, ErrInvalidCompensation
	}
	work := spec.Lease.Work()
	dispatch, err := DecodeCompensationDispatch(work.Payload())
	if err != nil || spec.ExpectedSequence < work.Sequence() {
		return Transition{}, ErrInvalidCompensation
	}
	step, ok := definitionActivityStep(spec.Definition, dispatch.StepName())
	if !ok {
		return Transition{}, ErrInvalidCompensation
	}
	if step.Compensation == nil {
		return Transition{}, ErrInvalidCompensation
	}
	if dispatch.Attempt() > step.Compensation.Retry.MaxAttempts {
		return Transition{}, ErrInvalidCompensation
	}
	startedAt := canonicalTime(spec.StartedAt)
	dueAt := canonicalTime(startedAt.Add(step.Compensation.Timeout))
	if startedAt.Before(work.AvailableAt()) || !startedAt.Before(work.Deadline()) || dueAt.After(work.Deadline()) {
		return Transition{}, ErrInvalidCompensation
	}
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.ExpectedSequence + 1, InstanceID: work.InstanceID(),
		Kind: EventCompensationAttemptStarted, OccurredAt: startedAt,
		StepName: dispatch.StepName(), Attempt: dispatch.Attempt(),
		IdempotencyKey: dispatch.IdempotencyKey(), DueAt: dueAt,
	})
	if err != nil {
		return Transition{}, ErrInvalidCompensation
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: work.InstanceID(), ExpectedSequence: spec.ExpectedSequence,
		Definition: spec.Definition.Reference(), Events: []HistoryEvent{event},
	})
	if err != nil {
		return Transition{}, ErrInvalidCompensation
	}
	return transition, nil
}
