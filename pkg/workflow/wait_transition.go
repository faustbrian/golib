package workflow

import (
	"errors"
	"time"
)

var (
	// ErrInvalidWait classifies malformed timer or signal persistence plans.
	ErrInvalidWait = errors.New("invalid workflow wait")
)

// TimerScheduleSpec supplies one atomic durable timer admission plan.
type TimerScheduleSpec struct {
	TransitionID     string
	WorkID           string
	InstanceID       string
	ExpectedSequence uint64
	Definition       Definition
	StepName         string
	ScheduledAt      time.Time
	Deadline         time.Time
	TenantID         string
	CorrelationID    string
}

// NewTimerSchedule creates history and due work that must commit atomically.
func NewTimerSchedule(spec TimerScheduleSpec) (Transition, error) {
	step, ok := definitionStep(spec.Definition, spec.StepName, StepTimer)
	if !ok {
		return Transition{}, ErrInvalidWait
	}
	if spec.ScheduledAt.IsZero() {
		return Transition{}, ErrInvalidWait
	}
	scheduledAt := canonicalTime(spec.ScheduledAt)
	dueAt := canonicalTime(scheduledAt.Add(step.Timeout))
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.ExpectedSequence + 1, InstanceID: spec.InstanceID,
		Kind: EventTimerScheduled, OccurredAt: scheduledAt, StepName: spec.StepName, DueAt: dueAt,
	})
	if err != nil {
		return Transition{}, ErrInvalidWait
	}
	work, err := NewPendingWork(PendingWorkSpec{
		ID: spec.WorkID, Kind: WorkTimer, InstanceID: spec.InstanceID, Sequence: event.Sequence(),
		AvailableAt: dueAt, Deadline: spec.Deadline, Payload: []byte(spec.StepName),
		TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
	})
	if err != nil {
		return Transition{}, ErrInvalidWait
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: spec.InstanceID,
		ExpectedSequence: spec.ExpectedSequence, Definition: spec.Definition.Reference(),
		Events: []HistoryEvent{event}, Work: []PendingWork{work},
	})
	if err != nil {
		return Transition{}, ErrInvalidWait
	}
	return transition, nil
}

// TimerFireSpec supplies one persisted due observation from current fenced
// timer work. The returned transition must commit before the lease completes.
type TimerFireSpec struct {
	TransitionID     string
	Lease            WorkLease
	ExpectedSequence uint64
	Definition       Definition
	FiredAt          time.Time
}

// NewTimerFire creates the durable timer-fired decision represented by a
// current timer lease.
func NewTimerFire(spec TimerFireSpec) (Transition, error) {
	if !spec.Lease.Valid() || spec.Lease.Work().Kind() != WorkTimer {
		return Transition{}, ErrInvalidWait
	}
	work := spec.Lease.Work()
	stepName := string(work.Payload())
	if _, ok := definitionStep(spec.Definition, stepName, StepTimer); !ok ||
		spec.ExpectedSequence < work.Sequence() {
		return Transition{}, ErrInvalidWait
	}
	firedAt := canonicalTime(spec.FiredAt)
	if firedAt.Before(work.AvailableAt()) || !firedAt.Before(work.Deadline()) {
		return Transition{}, ErrInvalidWait
	}
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.ExpectedSequence + 1, InstanceID: work.InstanceID(),
		Kind: EventTimerFired, OccurredAt: firedAt, StepName: stepName,
	})
	if err != nil {
		return Transition{}, ErrInvalidWait
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: work.InstanceID(), ExpectedSequence: spec.ExpectedSequence,
		Definition: spec.Definition.Reference(), Events: []HistoryEvent{event},
	})
	if err != nil {
		return Transition{}, ErrInvalidWait
	}
	return transition, nil
}

// SignalAcceptanceSpec supplies one inbound deduplicated signal transition.
type SignalAcceptanceSpec struct {
	InstanceID       string
	ExpectedSequence uint64
	Definition       Definition
	StepName         string
	SignalID         string
	ReceivedAt       time.Time
	Payload          []byte
}

// NewSignalAcceptance creates the transition that must commit before an
// inbound transport acknowledges the signal.
func NewSignalAcceptance(spec SignalAcceptanceSpec) (Transition, error) {
	step, ok := definitionStep(spec.Definition, spec.StepName, StepSignal)
	if !ok || len(spec.Payload) > int(step.InputLimit) {
		return Transition{}, ErrInvalidWait
	}
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.ExpectedSequence + 1, InstanceID: spec.InstanceID,
		Kind: EventSignalReceived, OccurredAt: spec.ReceivedAt, StepName: spec.StepName,
		IdempotencyKey: spec.SignalID, Data: spec.Payload,
	})
	if err != nil {
		return Transition{}, ErrInvalidWait
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.SignalID, InstanceID: spec.InstanceID,
		ExpectedSequence: spec.ExpectedSequence, Definition: spec.Definition.Reference(),
		Events: []HistoryEvent{event},
	})
	if err != nil {
		return Transition{}, ErrInvalidWait
	}
	return transition, nil
}
