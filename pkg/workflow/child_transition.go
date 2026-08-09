package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const (
	// MaxChildDispatchBytes bounds internal version-pinned child metadata.
	MaxChildDispatchBytes = 1 << 10
)

var (
	// ErrInvalidChildTransition classifies malformed or incoherent child plans.
	ErrInvalidChildTransition = errors.New("invalid workflow child transition")
)

// ChildDispatch is immutable durable metadata for starting one pinned child.
type ChildDispatch struct {
	stepName   string
	childID    string
	definition DefinitionReference
}

// StepName returns the parent definition step.
func (dispatch ChildDispatch) StepName() string { return dispatch.stepName }

// ChildID returns the stable child instance identity.
func (dispatch ChildDispatch) ChildID() string { return dispatch.childID }

// Definition returns the exact child behavior identity.
func (dispatch ChildDispatch) Definition() DefinitionReference { return dispatch.definition }

type childDispatchDocument struct {
	StepName              string `json:"step_name"`
	ChildID               string `json:"child_id"`
	DefinitionName        string `json:"definition_name"`
	DefinitionVersion     string `json:"definition_version"`
	DefinitionFingerprint string `json:"definition_fingerprint"`
}

// DecodeChildDispatch validates bounded durable work metadata without starting
// or otherwise observing the child workflow.
func DecodeChildDispatch(payload []byte) (ChildDispatch, error) {
	if len(payload) == 0 || len(payload) > MaxChildDispatchBytes {
		return ChildDispatch{}, ErrInvalidChildTransition
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var document childDispatchDocument
	if err := decoder.Decode(&document); err != nil {
		return ChildDispatch{}, ErrInvalidChildTransition
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ChildDispatch{}, ErrInvalidChildTransition
	}
	reference, err := NewDefinitionReference(document.DefinitionName, document.DefinitionVersion, document.DefinitionFingerprint)
	dispatch := ChildDispatch{stepName: document.StepName, childID: document.ChildID, definition: reference}
	if err != nil || !stableName.MatchString(dispatch.stepName) || !instanceIDPattern.MatchString(dispatch.childID) {
		return ChildDispatch{}, ErrInvalidChildTransition
	}
	return dispatch, nil
}

// ChildScheduleSpec supplies atomic parent history and durable child work.
type ChildScheduleSpec struct {
	TransitionID  string
	WorkID        string
	ChildID       string
	Instance      Instance
	Definition    Definition
	StepName      string
	ScheduledAt   time.Time
	Deadline      time.Time
	Input         []byte
	TenantID      string
	CorrelationID string
}

// NewChildSchedule records the pinned child identity and bounded input before
// any child-start adapter may act.
func NewChildSchedule(spec ChildScheduleSpec) (Transition, error) {
	step, ok := definitionStep(spec.Definition, spec.StepName, StepChild)
	if !ok || spec.Instance.status != StatusRunning || spec.Definition.Reference() != spec.Instance.definition ||
		len(spec.Input) > int(step.InputLimit) || !instanceIDPattern.MatchString(spec.ChildID) ||
		spec.ScheduledAt.Before(spec.Instance.updatedAt) {
		return Transition{}, ErrInvalidChildTransition
	}
	if _, exists := spec.Instance.Child(spec.StepName); exists {
		return Transition{}, ErrInvalidChildTransition
	}
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.Instance.sequence + 1, InstanceID: spec.Instance.id, Kind: EventChildScheduled,
		OccurredAt: spec.ScheduledAt, Definition: step.ChildDefinition, SuccessorID: spec.ChildID,
		StepName: spec.StepName, Data: spec.Input,
	})
	if err != nil {
		return Transition{}, ErrInvalidChildTransition
	}
	payload := encodeChildDispatch(spec.StepName, spec.ChildID, step.ChildDefinition)
	work, err := NewPendingWork(PendingWorkSpec{
		ID: spec.WorkID, Kind: WorkChild, InstanceID: spec.Instance.id, Sequence: event.Sequence(),
		AvailableAt: spec.ScheduledAt, Deadline: spec.Deadline, Payload: payload,
		TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
	})
	if err != nil {
		return Transition{}, ErrInvalidChildTransition
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: spec.Instance.id, ExpectedSequence: spec.Instance.sequence,
		Definition: spec.Instance.definition, Events: []HistoryEvent{event}, Work: []PendingWork{work},
	})
	if err != nil {
		return Transition{}, ErrInvalidChildTransition
	}
	return transition, nil
}

// ChildOutcomeSpec supplies one known terminal child result observed by the
// parent. FailureCode selects failure; an empty code selects success.
type ChildOutcomeSpec struct {
	TransitionID string
	Instance     Instance
	Definition   Definition
	StepName     string
	ChildID      string
	CompletedAt  time.Time
	Result       []byte
	FailureCode  string
}

// NewChildOutcome records a known child terminal result before parent
// orchestration advances.
func NewChildOutcome(spec ChildOutcomeSpec) (Transition, error) {
	step, ok := definitionStep(spec.Definition, spec.StepName, StepChild)
	progress, exists := spec.Instance.Child(spec.StepName)
	if !ok || !exists || progress.Status() != ChildScheduled || progress.ChildID() != spec.ChildID ||
		spec.Instance.status != StatusRunning || spec.Definition.Reference() != spec.Instance.definition ||
		len(spec.Result) > int(step.ResultLimit) || spec.CompletedAt.Before(spec.Instance.updatedAt) {
		return Transition{}, ErrInvalidChildTransition
	}
	kind := EventChildCompleted
	if spec.FailureCode != "" {
		if !stableName.MatchString(spec.FailureCode) {
			return Transition{}, ErrInvalidChildTransition
		}
		kind = EventChildFailed
	}
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.Instance.sequence + 1, InstanceID: spec.Instance.id, Kind: kind,
		OccurredAt: spec.CompletedAt, SuccessorID: spec.ChildID, StepName: spec.StepName,
		Code: spec.FailureCode, Data: spec.Result,
	})
	if err != nil {
		return Transition{}, ErrInvalidChildTransition
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: spec.Instance.id, ExpectedSequence: spec.Instance.sequence,
		Definition: spec.Instance.definition, Events: []HistoryEvent{event},
	})
	if err != nil {
		return Transition{}, ErrInvalidChildTransition
	}
	return transition, nil
}

func encodeChildDispatch(stepName, childID string, definition DefinitionReference) []byte {
	payload, _ := json.Marshal(childDispatchDocument{
		StepName: stepName, ChildID: childID, DefinitionName: definition.Name(),
		DefinitionVersion: definition.Version(), DefinitionFingerprint: definition.Fingerprint(),
	})
	return payload
}
