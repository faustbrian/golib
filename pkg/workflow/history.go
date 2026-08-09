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
)

// HistoryEventSpec supplies one immutable durable history record.
type HistoryEventSpec struct {
	Sequence    uint64
	InstanceID  string
	Kind        EventKind
	OccurredAt  time.Time
	Definition  DefinitionReference
	SuccessorID string
	Data        []byte
}

// HistoryEvent is one validated immutable persisted decision.
type HistoryEvent struct {
	sequence    uint64
	instanceID  string
	kind        EventKind
	occurredAt  time.Time
	definition  DefinitionReference
	successorID string
	data        []byte
}

// NewHistoryEvent validates and owns one durable history record.
func NewHistoryEvent(spec HistoryEventSpec) (HistoryEvent, error) {
	if spec.Sequence == 0 || !instanceIDPattern.MatchString(spec.InstanceID) ||
		spec.Kind < EventInstanceStarted || spec.Kind > EventContinuedAsNew ||
		spec.OccurredAt.IsZero() || len(spec.Data) > MaxPayloadBytes {
		return HistoryEvent{}, ErrInvalidHistoryEvent
	}
	requiresDefinition := spec.Kind == EventInstanceStarted ||
		spec.Kind == EventDefinitionMigrated || spec.Kind == EventContinuedAsNew
	if requiresDefinition && !spec.Definition.valid() {
		return HistoryEvent{}, ErrInvalidHistoryEvent
	}
	if !requiresDefinition && spec.Definition != (DefinitionReference{}) {
		return HistoryEvent{}, ErrInvalidHistoryEvent
	}
	if spec.Kind == EventContinuedAsNew && !instanceIDPattern.MatchString(spec.SuccessorID) {
		return HistoryEvent{}, ErrInvalidHistoryEvent
	}
	if spec.Kind != EventContinuedAsNew && spec.SuccessorID != "" {
		return HistoryEvent{}, ErrInvalidHistoryEvent
	}

	return HistoryEvent{
		sequence:    spec.Sequence,
		instanceID:  spec.InstanceID,
		kind:        spec.Kind,
		occurredAt:  canonicalTime(spec.OccurredAt),
		definition:  spec.Definition,
		successorID: spec.SuccessorID,
		data:        cloneBytes(spec.Data),
	}, nil
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
	id          string
	definition  DefinitionReference
	status      InstanceStatus
	sequence    uint64
	startedAt   time.Time
	updatedAt   time.Time
	input       []byte
	result      []byte
	successorID string
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
	}{
		ID: instance.id, DefinitionName: instance.definition.Name(),
		DefinitionVersion:     instance.definition.Version(),
		DefinitionFingerprint: instance.definition.Fingerprint(), Status: instance.status,
		Sequence: instance.sequence, StartedAt: instance.startedAt,
		UpdatedAt: instance.updatedAt, Input: instance.input, Result: instance.result,
		SuccessorID: instance.successorID,
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
	}

	instance.sequence = event.sequence
	instance.updatedAt = event.occurredAt
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
	return nil
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
