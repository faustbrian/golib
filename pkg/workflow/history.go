package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

var (
	// ErrInvalidDefinitionReference classifies malformed persisted definition
	// identities.
	ErrInvalidDefinitionReference = errors.New("invalid workflow definition reference")
	// ErrInvalidHistoryEvent classifies malformed durable history records.
	ErrInvalidHistoryEvent = errors.New("invalid workflow history event")
	// ErrEmptyHistory reports that replay has no persisted instance start.
	ErrEmptyHistory = errors.New("workflow history is empty")
	// ErrHistoryConflict classifies gaps, mixed instances, or non-monotonic time.
	ErrHistoryConflict = errors.New("workflow history conflict")
	// ErrInvalidTransition classifies an event that is illegal for current state.
	ErrInvalidTransition = errors.New("invalid workflow transition")
	// ErrDefinitionMismatch reports silent behavior reinterpretation for a pinned
	// name and version.
	ErrDefinitionMismatch = errors.New("workflow definition fingerprint mismatch")
)

var (
	instanceIDPattern  = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]{0,255}$`)
	fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// DefinitionReference is a comparable exact immutable behavior identity.
// Its zero value is invalid.
type DefinitionReference struct {
	name        string
	version     string
	fingerprint string
}

// NewDefinitionReference validates one persisted definition identity.
func NewDefinitionReference(name, version, fingerprint string) (DefinitionReference, error) {
	reference := DefinitionReference{name: name, version: version, fingerprint: fingerprint}
	if !reference.valid() {
		return DefinitionReference{}, ErrInvalidDefinitionReference
	}
	return reference, nil
}

// Name returns the stable definition name.
func (reference DefinitionReference) Name() string { return reference.name }

// Version returns the immutable definition version.
func (reference DefinitionReference) Version() string { return reference.version }

// Fingerprint returns the exact behavior digest.
func (reference DefinitionReference) Fingerprint() string { return reference.fingerprint }

func (reference DefinitionReference) valid() bool {
	return stableName.MatchString(reference.name) &&
		stableName.MatchString(reference.version) &&
		fingerprintPattern.MatchString(reference.fingerprint)
}

// EventKind identifies one persisted instance-lifecycle decision.
type EventKind uint8

const (
	// EventInstanceStarted creates one version-pinned instance.
	EventInstanceStarted EventKind = 1
	// EventInstancePaused stops new workflow progression.
	EventInstancePaused EventKind = 2
	// EventInstanceResumed permits workflow progression after a pause.
	EventInstanceResumed EventKind = 3
	// EventCancellationRequested begins durable cancellation.
	EventCancellationRequested EventKind = 4
	// EventInstanceCancelled records completed cancellation.
	EventInstanceCancelled EventKind = 5
	// EventInstanceCompleted records a successful terminal outcome.
	EventInstanceCompleted EventKind = 6
	// EventInstanceFailed records a known failed terminal outcome.
	EventInstanceFailed EventKind = 7
	// EventInstanceTerminated records forced operator termination.
	EventInstanceTerminated EventKind = 8
	// EventDefinitionMigrated records already-produced migrated persisted state.
	EventDefinitionMigrated EventKind = 9
	// EventContinuedAsNew closes history in favor of an explicit successor.
	EventContinuedAsNew EventKind = 10
	// EventActivityScheduled records durable activity input before dispatch.
	EventActivityScheduled EventKind = 11
	// EventActivityAttemptStarted records one externally observable attempt.
	EventActivityAttemptStarted EventKind = 12
	// EventActivityAttemptSucceeded records a known successful attempt result.
	EventActivityAttemptSucceeded EventKind = 13
	// EventActivityAttemptFailed records a known failed attempt result.
	EventActivityAttemptFailed EventKind = 14
	// EventActivityAttemptUnknown records an attempt that may have committed.
	EventActivityAttemptUnknown EventKind = 15
	// EventActivityRetryScheduled records the deterministic next-attempt time.
	EventActivityRetryScheduled EventKind = 16
	// EventTimerScheduled records a durable timer deadline before admission.
	EventTimerScheduled EventKind = 17
	// EventTimerFired records the persisted observation that a timer became due.
	EventTimerFired EventKind = 18
	// EventSignalReceived records one durably accepted deduplicated signal.
	EventSignalReceived EventKind = 19
	// EventCompensationScheduled records an explicit compensating activity.
	EventCompensationScheduled EventKind = 20
	// EventCompensationAttemptStarted records one compensating side-effect attempt.
	EventCompensationAttemptStarted EventKind = 21
	// EventCompensationAttemptSucceeded records known successful compensation.
	EventCompensationAttemptSucceeded EventKind = 22
	// EventCompensationAttemptFailed records known failed compensation.
	EventCompensationAttemptFailed EventKind = 23
	// EventCompensationAttemptUnknown records an uncertain compensation outcome.
	EventCompensationAttemptUnknown EventKind = 24
	// EventCompensationRetryScheduled records deterministic retry admission.
	EventCompensationRetryScheduled EventKind = 25
	// EventCompensationManuallyResolved records explicit operator resolution.
	EventCompensationManuallyResolved EventKind = 26
	// EventOperatorCommandRecorded audits one caller-authorized intervention.
	EventOperatorCommandRecorded EventKind = 27
)

// HistoryEventSpec supplies one immutable durable history record.
type HistoryEventSpec struct {
	Sequence       uint64
	InstanceID     string
	Kind           EventKind
	OccurredAt     time.Time
	Definition     DefinitionReference
	SuccessorID    string
	StepName       string
	Attempt        uint32
	IdempotencyKey string
	DueAt          time.Time
	Code           string
	Retryable      bool
	Data           []byte
}

// HistoryEvent is one validated immutable persisted decision.
type HistoryEvent struct {
	sequence       uint64
	instanceID     string
	kind           EventKind
	occurredAt     time.Time
	definition     DefinitionReference
	successorID    string
	stepName       string
	attempt        uint32
	idempotencyKey string
	dueAt          time.Time
	code           string
	retryable      bool
	data           []byte
}

// NewHistoryEvent validates and owns one durable history record.
func NewHistoryEvent(spec HistoryEventSpec) (HistoryEvent, error) {
	if spec.OccurredAt.IsZero() {
		return HistoryEvent{}, ErrInvalidHistoryEvent
	}
	spec.OccurredAt = canonicalTime(spec.OccurredAt)
	spec.DueAt = canonicalTime(spec.DueAt)
	if !validHistoryEventSpec(spec) {
		return HistoryEvent{}, ErrInvalidHistoryEvent
	}

	return HistoryEvent{
		sequence:    spec.Sequence,
		instanceID:  spec.InstanceID,
		kind:        spec.Kind,
		occurredAt:  spec.OccurredAt,
		definition:  spec.Definition,
		successorID: spec.SuccessorID,
		stepName:    spec.StepName, attempt: spec.Attempt,
		idempotencyKey: spec.IdempotencyKey, dueAt: spec.DueAt,
		code: spec.Code, retryable: spec.Retryable,
		data: cloneBytes(spec.Data),
	}, nil
}

func validHistoryEventSpec(spec HistoryEventSpec) bool {
	if spec.Sequence == 0 || !instanceIDPattern.MatchString(spec.InstanceID) ||
		spec.Kind < EventInstanceStarted || spec.Kind > EventOperatorCommandRecorded ||
		spec.OccurredAt.IsZero() || len(spec.Data) > MaxPayloadBytes {
		return false
	}
	requiresDefinition := spec.Kind == EventInstanceStarted ||
		spec.Kind == EventDefinitionMigrated || spec.Kind == EventContinuedAsNew
	if requiresDefinition && !spec.Definition.valid() {
		return false
	}
	if !requiresDefinition && spec.Definition != (DefinitionReference{}) {
		return false
	}
	if spec.Kind == EventContinuedAsNew && !instanceIDPattern.MatchString(spec.SuccessorID) {
		return false
	}
	if spec.Kind != EventContinuedAsNew && spec.SuccessorID != "" {
		return false
	}
	return validEventFields(spec)
}

// Sequence returns the contiguous instance-history position.
func (event HistoryEvent) Sequence() uint64 { return event.sequence }

// InstanceID returns the durable instance identity.
func (event HistoryEvent) InstanceID() string { return event.instanceID }

// Kind returns the persisted transition kind.
func (event HistoryEvent) Kind() EventKind { return event.kind }

// OccurredAt returns canonical UTC persisted decision time.
func (event HistoryEvent) OccurredAt() time.Time { return event.occurredAt }

// Definition returns the target definition for start, migration, or
// continue-as-new decisions.
func (event HistoryEvent) Definition() DefinitionReference { return event.definition }

// SuccessorID returns the explicit continue-as-new successor identity.
func (event HistoryEvent) SuccessorID() string { return event.successorID }

// StepName returns the definition step selected by a step event.
func (event HistoryEvent) StepName() string { return event.stepName }

// Attempt returns the one-based activity attempt selected by an attempt event.
func (event HistoryEvent) Attempt() uint32 { return event.attempt }

// IdempotencyKey returns the persisted external-attempt or signal identity.
func (event HistoryEvent) IdempotencyKey() string { return event.idempotencyKey }

// DueAt returns a persisted attempt, retry, or timer deadline.
func (event HistoryEvent) DueAt() time.Time { return event.dueAt }

// Code returns a stable known-failure or unknown-outcome code.
func (event HistoryEvent) Code() string { return event.code }

// Retryable reports the persisted known-failure retry classification.
func (event HistoryEvent) Retryable() bool { return event.retryable }

// Data returns an owned copy of persisted event data.
func (event HistoryEvent) Data() []byte { return cloneBytes(event.data) }

// InstanceStatus identifies one durable instance lifecycle state.
type InstanceStatus uint8

const (
	// StatusRunning permits normal workflow progression.
	StatusRunning InstanceStatus = 1
	// StatusPaused prevents normal progression until explicitly resumed.
	StatusPaused InstanceStatus = 2
	// StatusCancelling records a durable cancellation request in progress.
	StatusCancelling InstanceStatus = 3
	// StatusCompleted is a successful terminal outcome.
	StatusCompleted InstanceStatus = 4
	// StatusFailed is a known failed terminal outcome.
	StatusFailed InstanceStatus = 5
	// StatusCancelled is a completed cancellation terminal outcome.
	StatusCancelled InstanceStatus = 6
	// StatusTerminated is a forced terminal outcome.
	StatusTerminated InstanceStatus = 7
	// StatusContinuedAsNew is a terminal outcome with a named successor.
	StatusContinuedAsNew InstanceStatus = 8
)

// Instance is an immutable replay result derived only from persisted history.
type Instance struct {
	id              string
	definition      DefinitionReference
	status          InstanceStatus
	sequence        uint64
	startedAt       time.Time
	updatedAt       time.Time
	input           []byte
	result          []byte
	successorID     string
	activities      map[string]ActivityProgress
	timers          map[string]TimerProgress
	signals         map[string]SignalProgress
	compensations   map[string]CompensationProgress
	operatorActions []OperatorActionRecord
	pendingOperator OperatorAction
}

// Replay deterministically reconstructs one instance from validated persisted
// decisions. It resolves definitions and migration edges but never executes
// workflow or migration code.
func Replay(registry *Registry, events []HistoryEvent) (Instance, error) {
	if len(events) == 0 {
		return Instance{}, ErrEmptyHistory
	}

	var instance Instance
	for _, event := range events {
		if err := instance.apply(registry, event); err != nil {
			return Instance{}, err
		}
	}
	if instance.pendingOperator != 0 {
		return Instance{}, ErrInvalidTransition
	}
	return instance, nil
}

// ID returns the durable instance identity.
func (instance Instance) ID() string { return instance.id }

// Definition returns the currently pinned exact behavior identity.
func (instance Instance) Definition() DefinitionReference { return instance.definition }

// Status returns the reconstructed lifecycle state.
func (instance Instance) Status() InstanceStatus { return instance.status }

// Sequence returns the final contiguous history position.
func (instance Instance) Sequence() uint64 { return instance.sequence }

// StartedAt returns canonical persisted creation time.
func (instance Instance) StartedAt() time.Time { return instance.startedAt }

// UpdatedAt returns canonical time of the last persisted decision.
func (instance Instance) UpdatedAt() time.Time { return instance.updatedAt }

// Input returns an owned copy of current persisted workflow state.
func (instance Instance) Input() []byte { return cloneBytes(instance.input) }

// Result returns an owned copy of terminal result or failure data.
func (instance Instance) Result() []byte { return cloneBytes(instance.result) }

// SuccessorID returns the explicit continue-as-new successor, when present.
func (instance Instance) SuccessorID() string { return instance.successorID }

// Activity returns immutable replayed progress for one definition activity.
func (instance Instance) Activity(stepName string) (ActivityProgress, bool) {
	progress, exists := instance.activities[stepName]
	return cloneActivityProgress(progress), exists
}

// Activities returns replayed activity progress in stable step-name order.
func (instance Instance) Activities() []ActivityProgress {
	return sortedActivityProgress(instance.activities)
}

// Timer returns immutable replayed progress for one definition timer.
func (instance Instance) Timer(stepName string) (TimerProgress, bool) {
	progress, exists := instance.timers[stepName]
	return progress, exists
}

// Timers returns replayed timer progress in stable step-name order.
func (instance Instance) Timers() []TimerProgress { return sortedTimerProgress(instance.timers) }

// Signal returns immutable replayed progress for one definition signal wait.
func (instance Instance) Signal(stepName string) (SignalProgress, bool) {
	progress, exists := instance.signals[stepName]
	return cloneSignalProgress(progress), exists
}

// Signals returns replayed signal progress in stable step-name order.
func (instance Instance) Signals() []SignalProgress { return sortedSignalProgress(instance.signals) }

// Compensation returns immutable replayed progress for one activity's
// explicit compensating action.
func (instance Instance) Compensation(stepName string) (CompensationProgress, bool) {
	progress, exists := instance.compensations[stepName]
	return cloneCompensationProgress(progress), exists
}

// Compensations returns replayed compensation progress in persisted schedule
// order.
func (instance Instance) Compensations() []CompensationProgress {
	return sortedCompensationProgress(instance.compensations)
}

// OperatorActions returns an owned ordered audit trail of interventions.
func (instance Instance) OperatorActions() []OperatorActionRecord {
	return append([]OperatorActionRecord(nil), instance.operatorActions...)
}

// SnapshotDigest returns a deterministic digest of reconstructed persisted
// state for diagnostics and replay comparison.
func (instance Instance) SnapshotDigest() string {
	encoded, _ := json.Marshal(struct {
		ID                    string
		DefinitionName        string
		DefinitionVersion     string
		DefinitionFingerprint string
		Status                InstanceStatus
		Sequence              uint64
		StartedAt             time.Time
		UpdatedAt             time.Time
		Input                 []byte
		Result                []byte
		SuccessorID           string
		Activities            []activityProgressSnapshot
		Timers                []timerProgressSnapshot        `json:",omitempty"`
		Signals               []signalProgressSnapshot       `json:",omitempty"`
		Compensations         []compensationProgressSnapshot `json:",omitempty"`
		OperatorActions       []operatorActionSnapshot       `json:",omitempty"`
	}{
		ID: instance.id, DefinitionName: instance.definition.Name(),
		DefinitionVersion:     instance.definition.Version(),
		DefinitionFingerprint: instance.definition.Fingerprint(), Status: instance.status,
		Sequence: instance.sequence, StartedAt: instance.startedAt,
		UpdatedAt: instance.updatedAt, Input: instance.input, Result: instance.result,
		SuccessorID: instance.successorID, Activities: activityProgressSnapshots(instance.activities),
		Timers: timerProgressSnapshots(instance.timers), Signals: signalProgressSnapshots(instance.signals),
		Compensations:   compensationProgressSnapshots(instance.compensations),
		OperatorActions: operatorActionSnapshots(instance.operatorActions),
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (instance *Instance) apply(registry *Registry, event HistoryEvent) error {
	if instance.sequence == 0 {
		return instance.applyStart(registry, event)
	}
	if event.instanceID != instance.id || event.sequence != instance.sequence+1 ||
		event.occurredAt.Before(instance.updatedAt) {
		return ErrHistoryConflict
	}
	if terminalStatus(instance.status) {
		return ErrInvalidTransition
	}
	completesOperator := instance.pendingOperator != 0 &&
		event.kind == operatorEventKind(instance.pendingOperator)
	if instance.pendingOperator != 0 && !completesOperator {
		return ErrInvalidTransition
	}

	switch event.kind {
	case EventInstanceStarted:
		return ErrInvalidTransition
	case EventInstancePaused:
		if instance.status != StatusRunning {
			return ErrInvalidTransition
		}
		instance.status = StatusPaused
	case EventInstanceResumed:
		if instance.status != StatusPaused {
			return ErrInvalidTransition
		}
		instance.status = StatusRunning
	case EventCancellationRequested:
		if instance.status != StatusRunning && instance.status != StatusPaused {
			return ErrInvalidTransition
		}
		instance.status = StatusCancelling
	case EventInstanceCancelled:
		if instance.status != StatusCancelling {
			return ErrInvalidTransition
		}
		instance.status = StatusCancelled
		instance.result = cloneBytes(event.data)
	case EventInstanceCompleted:
		if instance.status != StatusRunning {
			return ErrInvalidTransition
		}
		instance.status = StatusCompleted
		instance.result = cloneBytes(event.data)
	case EventInstanceFailed:
		if instance.status != StatusRunning {
			return ErrInvalidTransition
		}
		instance.status = StatusFailed
		instance.result = cloneBytes(event.data)
	case EventInstanceTerminated:
		instance.status = StatusTerminated
		instance.result = cloneBytes(event.data)
	case EventDefinitionMigrated:
		if instance.status != StatusPaused {
			return ErrInvalidTransition
		}
		if event.definition.Name() != instance.definition.Name() {
			return ErrInvalidMigration
		}
		if err := validateReference(registry, event.definition); err != nil {
			return err
		}
		if _, err := registry.Migration(
			instance.definition.Name(), instance.definition.Version(), event.definition.Version(),
		); err != nil {
			return err
		}
		instance.definition = event.definition
		instance.input = cloneBytes(event.data)
	case EventContinuedAsNew:
		if instance.status != StatusRunning {
			return ErrInvalidTransition
		}
		if err := validateReference(registry, event.definition); err != nil {
			return err
		}
		instance.status = StatusContinuedAsNew
		instance.successorID = event.successorID
	case EventActivityScheduled, EventActivityAttemptStarted,
		EventActivityAttemptSucceeded, EventActivityAttemptFailed,
		EventActivityAttemptUnknown, EventActivityRetryScheduled:
		if err := instance.applyActivity(registry, event); err != nil {
			return err
		}
	case EventTimerScheduled, EventTimerFired, EventSignalReceived:
		if err := instance.applyWait(registry, event); err != nil {
			return err
		}
	case EventCompensationScheduled, EventCompensationAttemptStarted,
		EventCompensationAttemptSucceeded, EventCompensationAttemptFailed,
		EventCompensationAttemptUnknown, EventCompensationRetryScheduled,
		EventCompensationManuallyResolved:
		if err := instance.applyCompensation(registry, event); err != nil {
			return err
		}
	case EventOperatorCommandRecorded:
		if err := instance.applyOperator(event); err != nil {
			return err
		}
	}

	instance.sequence = event.sequence
	instance.updatedAt = event.occurredAt
	if completesOperator {
		instance.pendingOperator = 0
	}
	return nil
}

func (instance *Instance) applyStart(registry *Registry, event HistoryEvent) error {
	if event.kind != EventInstanceStarted {
		return ErrInvalidTransition
	}
	if event.sequence != 1 {
		return ErrHistoryConflict
	}
	if err := validateReference(registry, event.definition); err != nil {
		return err
	}

	instance.id = event.instanceID
	instance.definition = event.definition
	instance.status = StatusRunning
	instance.sequence = event.sequence
	instance.startedAt = event.occurredAt
	instance.updatedAt = event.occurredAt
	instance.input = cloneBytes(event.data)
	instance.activities = make(map[string]ActivityProgress)
	instance.timers = make(map[string]TimerProgress)
	instance.signals = make(map[string]SignalProgress)
	instance.compensations = make(map[string]CompensationProgress)
	instance.operatorActions = nil
	instance.pendingOperator = 0
	return nil
}

func validEventFields(spec HistoryEventSpec) bool {
	if spec.Kind >= EventActivityScheduled && spec.Kind <= EventActivityRetryScheduled {
		return validActivityEventFields(spec)
	}
	if spec.Kind >= EventTimerScheduled && spec.Kind <= EventSignalReceived {
		return validWaitEventFields(spec)
	}
	if spec.Kind >= EventCompensationScheduled && spec.Kind <= EventCompensationManuallyResolved {
		return validCompensationEventFields(spec)
	}
	if spec.Kind == EventOperatorCommandRecorded {
		return validOperatorEventFields(spec)
	}
	return spec.StepName == "" && spec.Attempt == 0 && spec.IdempotencyKey == "" &&
		spec.DueAt.IsZero() && spec.Code == "" && !spec.Retryable
}

func validActivityEventFields(spec HistoryEventSpec) bool {
	if !stableName.MatchString(spec.StepName) || spec.Definition != (DefinitionReference{}) || spec.SuccessorID != "" {
		return false
	}
	switch spec.Kind {
	case EventActivityScheduled:
		return spec.Attempt == 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			spec.Code == "" && !spec.Retryable
	case EventActivityAttemptStarted:
		return spec.Attempt > 0 && instanceIDPattern.MatchString(spec.IdempotencyKey) &&
			spec.DueAt.After(spec.OccurredAt) && spec.Code == "" && !spec.Retryable && len(spec.Data) == 0
	case EventActivityAttemptSucceeded:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			spec.Code == "" && !spec.Retryable
	case EventActivityAttemptFailed:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			stableName.MatchString(spec.Code)
	case EventActivityAttemptUnknown:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			stableName.MatchString(spec.Code) && !spec.Retryable
	default: // The bounded activity-kind check makes this retry scheduling.
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.After(spec.OccurredAt) &&
			spec.Code == "" && !spec.Retryable && len(spec.Data) == 0
	}
}

func validateReference(registry *Registry, reference DefinitionReference) error {
	definition, err := registry.Resolve(reference.Name(), reference.Version())
	if err != nil {
		return err
	}
	if definition.Fingerprint() != reference.Fingerprint() {
		return ErrDefinitionMismatch
	}
	return nil
}

func terminalStatus(status InstanceStatus) bool {
	return status == StatusCompleted || status == StatusFailed ||
		status == StatusCancelled || status == StatusTerminated ||
		status == StatusContinuedAsNew
}

func canonicalTime(value time.Time) time.Time {
	return value.Round(0).UTC()
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}
