package workflow

import (
	"errors"
	"time"
)

const (
	// MaxTransitionEvents bounds one atomic history append.
	MaxTransitionEvents = 100
	// MaxTransitionWork bounds due-work records created by one atomic append.
	MaxTransitionWork = 1_000
	// MaxTransitionBytes bounds aggregate event and work payload in one append.
	MaxTransitionBytes = MaxPayloadBytes
)

var (
	// ErrInvalidPendingWork classifies incomplete or unbounded durable work.
	ErrInvalidPendingWork = errors.New("invalid workflow pending work")
	// ErrInvalidTransitionPlan classifies a non-atomic or incoherent append plan.
	ErrInvalidTransitionPlan = errors.New("invalid workflow transition plan")
)

// WorkKind identifies one durable unit that may progress an instance only
// after the transition that created it commits.
type WorkKind uint8

const (
	// WorkActivity dispatches one explicit external activity attempt.
	WorkActivity WorkKind = 1
	// WorkTimer observes one durable timer becoming due.
	WorkTimer WorkKind = 2
	// WorkChild progresses one version-pinned child workflow operation.
	WorkChild WorkKind = 3
	// WorkPublication publishes one durably accepted outbound message.
	WorkPublication WorkKind = 4
	// WorkReconciliation resolves an activity with an unknown outcome.
	WorkReconciliation WorkKind = 5
	// WorkCompensation dispatches one explicit compensating activity attempt.
	WorkCompensation WorkKind = 6
)

// PendingWorkSpec supplies one bounded durable work record.
type PendingWorkSpec struct {
	ID            string
	Kind          WorkKind
	InstanceID    string
	Sequence      uint64
	AvailableAt   time.Time
	Deadline      time.Time
	Payload       []byte
	TenantID      string
	CorrelationID string
}

// PendingWork is immutable work created atomically with workflow history.
type PendingWork struct {
	id            string
	kind          WorkKind
	instanceID    string
	sequence      uint64
	availableAt   time.Time
	deadline      time.Time
	payload       []byte
	tenantID      string
	correlationID string
}

// NewPendingWork validates and owns one bounded durable work record.
func NewPendingWork(spec PendingWorkSpec) (PendingWork, error) {
	work := PendingWork{
		id: spec.ID, kind: spec.Kind, instanceID: spec.InstanceID, sequence: spec.Sequence,
		availableAt: canonicalTime(spec.AvailableAt), deadline: canonicalTime(spec.Deadline),
		payload: cloneBytes(spec.Payload), tenantID: spec.TenantID, correlationID: spec.CorrelationID,
	}
	if !work.valid() {
		return PendingWork{}, ErrInvalidPendingWork
	}
	return work, nil
}

// ID returns the stable globally unique durable work identity.
func (work PendingWork) ID() string { return work.id }

// Kind returns the explicit durable work classification.
func (work PendingWork) Kind() WorkKind { return work.kind }

// InstanceID returns the owning workflow instance.
func (work PendingWork) InstanceID() string { return work.instanceID }

// Sequence returns the committed history position that created the work.
func (work PendingWork) Sequence() uint64 { return work.sequence }

// AvailableAt returns the persisted earliest admission time.
func (work PendingWork) AvailableAt() time.Time { return work.availableAt }

// Deadline returns the persisted execution deadline.
func (work PendingWork) Deadline() time.Time { return work.deadline }

// Payload returns an owned copy of bounded work input.
func (work PendingWork) Payload() []byte { return cloneBytes(work.payload) }

// TenantID returns optional tenant propagation data.
func (work PendingWork) TenantID() string { return work.tenantID }

// CorrelationID returns optional correlation propagation data.
func (work PendingWork) CorrelationID() string { return work.correlationID }

func (work PendingWork) valid() bool {
	return instanceIDPattern.MatchString(work.id) &&
		work.kind >= WorkActivity && work.kind <= WorkCompensation &&
		instanceIDPattern.MatchString(work.instanceID) && work.sequence > 0 &&
		!work.availableAt.IsZero() && work.deadline.After(work.availableAt) &&
		len(work.payload) <= MaxPayloadBytes && optionalMetadataValid(work.tenantID) &&
		optionalMetadataValid(work.correlationID)
}

// TransitionSpec supplies one idempotent atomic history-and-work append.
type TransitionSpec struct {
	ID               string
	InstanceID       string
	ExpectedSequence uint64
	Definition       DefinitionReference
	Events           []HistoryEvent
	Work             []PendingWork
}

// Transition is one immutable atomic persistence plan. A store must append all
// events and create all work in one transaction or make none of them visible.
type Transition struct {
	id               string
	instanceID       string
	expectedSequence uint64
	definition       DefinitionReference
	events           []HistoryEvent
	work             []PendingWork
}

// NewTransition validates and owns one bounded atomic persistence plan.
func NewTransition(spec TransitionSpec) (Transition, error) {
	transition := Transition{
		id: spec.ID, instanceID: spec.InstanceID, expectedSequence: spec.ExpectedSequence,
		definition: spec.Definition,
		events:     append([]HistoryEvent(nil), spec.Events...),
		work:       clonePendingWork(spec.Work),
	}
	if !transition.valid() {
		return Transition{}, ErrInvalidTransitionPlan
	}
	return transition, nil
}

// ID returns the idempotency identity for this complete persistence plan.
func (transition Transition) ID() string { return transition.id }

// InstanceID returns the owning workflow instance.
func (transition Transition) InstanceID() string { return transition.instanceID }

// ExpectedSequence returns the optimistic-concurrency precondition.
func (transition Transition) ExpectedSequence() uint64 { return transition.expectedSequence }

// Definition returns the exact behavior identity that made this decision.
func (transition Transition) Definition() DefinitionReference { return transition.definition }

// Events returns an owned ordered copy of the atomic history append.
func (transition Transition) Events() []HistoryEvent {
	return append([]HistoryEvent(nil), transition.events...)
}

// Work returns owned durable work created by the atomic append.
func (transition Transition) Work() []PendingWork { return clonePendingWork(transition.work) }

func (transition Transition) valid() bool {
	if !instanceIDPattern.MatchString(transition.id) ||
		!instanceIDPattern.MatchString(transition.instanceID) || !transition.definition.valid() ||
		transition.expectedSequence == ^uint64(0) || len(transition.events) == 0 ||
		len(transition.events) > MaxTransitionEvents || len(transition.work) > MaxTransitionWork {
		return false
	}

	firstSequence := transition.expectedSequence + 1
	previousTime := time.Time{}
	totalBytes := uint64(0)
	eventTimes := make(map[uint64]time.Time, len(transition.events))
	for index, event := range transition.events {
		if !historyEventValid(event) || event.instanceID != transition.instanceID ||
			event.sequence != firstSequence+uint64(index) ||
			(!previousTime.IsZero() && event.occurredAt.Before(previousTime)) {
			return false
		}
		previousTime = event.occurredAt
		eventTimes[event.sequence] = event.occurredAt
		totalBytes += uint64(len(event.data))
	}
	if transition.expectedSequence == 0 {
		first := transition.events[0]
		if first.kind != EventInstanceStarted || first.definition != transition.definition {
			return false
		}
	} else if transition.events[0].kind == EventInstanceStarted {
		return false
	}

	workIDs := make(map[string]struct{}, len(transition.work))
	for _, work := range transition.work {
		totalBytes += uint64(len(work.payload))
		eventTime, exists := eventTimes[work.sequence]
		if !work.valid() || work.instanceID != transition.instanceID || !exists ||
			work.availableAt.Before(eventTime) {
			return false
		}
		if _, duplicate := workIDs[work.id]; duplicate {
			return false
		}
		workIDs[work.id] = struct{}{}
	}
	return totalBytes <= MaxTransitionBytes
}

func historyEventValid(event HistoryEvent) bool {
	return validHistoryEventSpec(HistoryEventSpec{
		Sequence: event.sequence, InstanceID: event.instanceID, Kind: event.kind,
		OccurredAt: event.occurredAt, Definition: event.definition, SuccessorID: event.successorID,
		StepName: event.stepName, Attempt: event.attempt, IdempotencyKey: event.idempotencyKey,
		DueAt: event.dueAt, Code: event.code, Retryable: event.retryable, Data: event.data,
	})
}

func clonePendingWork(items []PendingWork) []PendingWork {
	if items == nil {
		return nil
	}
	cloned := make([]PendingWork, len(items))
	for index, work := range items {
		cloned[index] = work
		cloned[index].payload = cloneBytes(work.payload)
	}
	return cloned
}
