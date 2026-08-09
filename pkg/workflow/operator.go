package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const (
	// MaxOperatorAuditBytes bounds persisted operator identity and reason data.
	MaxOperatorAuditBytes = 1 << 10
)

var (
	// ErrInvalidOperatorCommand classifies malformed, unauthorized-by-state, or
	// unbounded operator command input. Callers must authorize actors before
	// constructing a command.
	ErrInvalidOperatorCommand = errors.New("invalid workflow operator command")
)

// OperatorAction identifies one audited lifecycle intervention.
type OperatorAction uint8

const (
	// OperatorPause stops new progression of a running instance.
	OperatorPause OperatorAction = 1
	// OperatorResume resumes a paused instance.
	OperatorResume OperatorAction = 2
	// OperatorCancel requests durable cooperative cancellation.
	OperatorCancel OperatorAction = 3
	// OperatorTerminate forcibly ends a non-terminal instance.
	OperatorTerminate OperatorAction = 4
	// OperatorRetryActivity durably admits an explicit activity retry.
	OperatorRetryActivity OperatorAction = 5
	// OperatorCompensate durably schedules an explicit compensation.
	OperatorCompensate OperatorAction = 6
	// OperatorResolveCompensation records explicit manual reconciliation without
	// reporting successful rollback.
	OperatorResolveCompensation OperatorAction = 7
	// OperatorApprove records a caller-authorized human approval decision.
	OperatorApprove OperatorAction = 8
)

// String returns the stable persisted action name.
func (action OperatorAction) String() string {
	switch action {
	case OperatorPause:
		return "pause"
	case OperatorResume:
		return "resume"
	case OperatorCancel:
		return "cancel"
	case OperatorTerminate:
		return "terminate"
	case OperatorRetryActivity:
		return "retry-activity"
	case OperatorCompensate:
		return "compensate"
	case OperatorResolveCompensation:
		return "resolve-compensation"
	case OperatorApprove:
		return "approve"
	default:
		return ""
	}
}

// OperatorActionRecord is immutable audit state reconstructed from history.
type OperatorActionRecord struct {
	commandID  string
	action     OperatorAction
	actor      string
	reason     string
	occurredAt time.Time
}

// CommandID returns the idempotent operator command identity.
func (record OperatorActionRecord) CommandID() string { return record.commandID }

// Action returns the requested lifecycle intervention.
func (record OperatorActionRecord) Action() OperatorAction { return record.action }

// Actor returns the caller-authorized principal identity supplied to the command.
func (record OperatorActionRecord) Actor() string { return record.actor }

// Reason returns the bounded caller-supplied audit reason code.
func (record OperatorActionRecord) Reason() string { return record.reason }

// OccurredAt returns the persisted command time.
func (record OperatorActionRecord) OccurredAt() time.Time { return record.occurredAt }

type operatorAuditDocument struct {
	Action OperatorAction `json:"action"`
	Actor  string         `json:"actor"`
	Reason string         `json:"reason"`
}

func decodeOperatorAudit(data []byte) (operatorAuditDocument, error) {
	if len(data) == 0 {
		return operatorAuditDocument{}, ErrInvalidOperatorCommand
	}
	if len(data) > MaxOperatorAuditBytes {
		return operatorAuditDocument{}, ErrInvalidOperatorCommand
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document operatorAuditDocument
	if err := decoder.Decode(&document); err != nil {
		return operatorAuditDocument{}, ErrInvalidOperatorCommand
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return operatorAuditDocument{}, ErrInvalidOperatorCommand
	}
	if document.Action.String() == "" {
		return operatorAuditDocument{}, ErrInvalidOperatorCommand
	}
	if !instanceIDPattern.MatchString(document.Actor) {
		return operatorAuditDocument{}, ErrInvalidOperatorCommand
	}
	if !stableName.MatchString(document.Reason) {
		return operatorAuditDocument{}, ErrInvalidOperatorCommand
	}
	return document, nil
}

func validOperatorEventFields(spec HistoryEventSpec) bool {
	if spec.StepName != "" {
		return false
	}
	if spec.Attempt != 0 {
		return false
	}
	if !spec.DueAt.IsZero() {
		return false
	}
	if spec.Code != "" {
		return false
	}
	if spec.Retryable {
		return false
	}
	if !instanceIDPattern.MatchString(spec.IdempotencyKey) {
		return false
	}
	_, err := decodeOperatorAudit(spec.Data)
	return err == nil
}

func (instance *Instance) applyOperator(event HistoryEvent) error {
	if instance.pendingOperator != 0 {
		return ErrInvalidTransition
	}
	document, err := decodeOperatorAudit(event.data)
	if err != nil {
		return ErrInvalidTransition
	}
	instance.operatorActions = append(instance.operatorActions, OperatorActionRecord{
		commandID: event.idempotencyKey, action: document.Action, actor: document.Actor,
		reason: document.Reason, occurredAt: event.occurredAt,
	})
	instance.pendingOperator = document.Action
	return nil
}

type operatorActionSnapshot struct {
	CommandID  string
	Action     OperatorAction
	Actor      string
	Reason     string
	OccurredAt time.Time
}

func operatorActionSnapshots(records []OperatorActionRecord) []operatorActionSnapshot {
	if records == nil {
		return nil
	}
	result := make([]operatorActionSnapshot, len(records))
	for index, record := range records {
		result[index] = operatorActionSnapshot{
			CommandID: record.commandID, Action: record.action, Actor: record.actor,
			Reason: record.reason, OccurredAt: record.occurredAt,
		}
	}
	return result
}

// OperatorLifecycleCommandSpec supplies one caller-authorized audited command.
// Authorization remains the caller's policy boundary and is not inferred by
// this package.
type OperatorLifecycleCommandSpec struct {
	CommandID  string
	Instance   Instance
	Action     OperatorAction
	Actor      string
	Reason     string
	OccurredAt time.Time
}

// NewOperatorLifecycleCommand atomically records audit identity before the
// requested lifecycle action. Concurrent commands are serialized by the
// instance sequence and exact command replay is idempotent by CommandID.
func NewOperatorLifecycleCommand(spec OperatorLifecycleCommandSpec) (Transition, error) {
	if !validOperatorLifecycleState(spec.Instance.status, spec.Action) {
		return Transition{}, ErrInvalidOperatorCommand
	}
	if !instanceIDPattern.MatchString(spec.CommandID) {
		return Transition{}, ErrInvalidOperatorCommand
	}
	if spec.OccurredAt.IsZero() || spec.OccurredAt.Before(spec.Instance.updatedAt) {
		return Transition{}, ErrInvalidOperatorCommand
	}
	if spec.Instance.sequence > ^uint64(0)-2 {
		return Transition{}, ErrInvalidOperatorCommand
	}
	if !instanceIDPattern.MatchString(spec.Instance.id) || !spec.Instance.definition.valid() {
		return Transition{}, ErrInvalidOperatorCommand
	}
	document := operatorAuditDocument{Action: spec.Action, Actor: spec.Actor, Reason: spec.Reason}
	auditData, _ := json.Marshal(document)
	if _, err := decodeOperatorAudit(auditData); err != nil {
		return Transition{}, ErrInvalidOperatorCommand
	}
	occurredAt := canonicalTime(spec.OccurredAt)
	audit, _ := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.Instance.sequence + 1, InstanceID: spec.Instance.id,
		Kind: EventOperatorCommandRecorded, OccurredAt: occurredAt,
		IdempotencyKey: spec.CommandID, Data: auditData,
	})
	action, _ := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.Instance.sequence + 2, InstanceID: spec.Instance.id,
		Kind: operatorEventKind(spec.Action), OccurredAt: occurredAt,
	})
	transition, _ := NewTransition(TransitionSpec{
		ID: spec.CommandID, InstanceID: spec.Instance.id,
		ExpectedSequence: spec.Instance.sequence, Definition: spec.Instance.definition,
		Events: []HistoryEvent{audit, action},
	})
	return transition, nil
}

func validOperatorLifecycleState(status InstanceStatus, action OperatorAction) bool {
	switch action {
	case OperatorPause:
		return status == StatusRunning
	case OperatorResume:
		return status == StatusPaused
	case OperatorCancel:
		return status == StatusRunning || status == StatusPaused
	case OperatorTerminate:
		return status == StatusRunning || status == StatusPaused || status == StatusCancelling
	default:
		return false
	}
}

func operatorEventKind(action OperatorAction) EventKind {
	switch action {
	case OperatorPause:
		return EventInstancePaused
	case OperatorResume:
		return EventInstanceResumed
	case OperatorCancel:
		return EventCancellationRequested
	case OperatorTerminate:
		return EventInstanceTerminated
	case OperatorRetryActivity:
		return EventActivityRetryScheduled
	case OperatorCompensate:
		return EventCompensationScheduled
	case OperatorResolveCompensation:
		return EventCompensationManuallyResolved
	case OperatorApprove:
		return EventSignalReceived
	default:
		return 0
	}
}
