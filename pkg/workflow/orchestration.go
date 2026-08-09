package workflow

import (
	"errors"
	"time"
)

var (
	// ErrInvalidOrchestration classifies malformed orchestration input or a
	// definition step whose durable execution semantics are unsupported.
	ErrInvalidOrchestration = errors.New("invalid workflow orchestration decision")
)

// OrchestrationDecisionKind classifies one deterministic next-step decision.
type OrchestrationDecisionKind uint8

const (
	// OrchestrationScheduled persists the next due activity or timer work.
	OrchestrationScheduled OrchestrationDecisionKind = 1
	// OrchestrationWaiting means persisted progress is awaiting an activity,
	// timer, or external signal and no transition should be committed.
	OrchestrationWaiting OrchestrationDecisionKind = 2
	// OrchestrationCompleted persists a successful terminal outcome.
	OrchestrationCompleted OrchestrationDecisionKind = 3
	// OrchestrationFailed persists a known failed terminal outcome.
	OrchestrationFailed OrchestrationDecisionKind = 4
)

// OrchestrationDecisionSpec supplies caller-owned identities, bounded data,
// and deterministic time for one ordered orchestration decision.
type OrchestrationDecisionSpec struct {
	TransitionID   string
	WorkID         string
	Instance       Instance
	Definition     Definition
	DecidedAt      time.Time
	Deadline       time.Time
	IdempotencyKey string
	Input          []byte
	Result         []byte
	TenantID       string
	CorrelationID  string
}

// OrchestrationDecision is one immutable persisted plan or explicit wait.
type OrchestrationDecision struct {
	kind       OrchestrationDecisionKind
	stepName   string
	transition Transition
}

// Kind returns the durable decision classification.
func (decision OrchestrationDecision) Kind() OrchestrationDecisionKind { return decision.kind }

// StepName returns the affected definition step, or empty on completion.
func (decision OrchestrationDecision) StepName() string { return decision.stepName }

// Transition returns the atomic plan. It is invalid for OrchestrationWaiting.
func (decision OrchestrationDecision) Transition() Transition { return decision.transition }

// NewOrchestrationDecision deterministically selects the first incomplete
// ordered step from replayed persisted progress. It does not execute side
// effects and rejects choreography or unsupported control-flow steps.
func NewOrchestrationDecision(spec OrchestrationDecisionSpec) (OrchestrationDecision, error) {
	if spec.Definition.Mode() != Orchestration || spec.Definition.Reference() != spec.Instance.definition ||
		spec.Instance.status != StatusRunning {
		return OrchestrationDecision{}, ErrInvalidOrchestration
	}
	for _, step := range spec.Definition.Steps() {
		switch step.Kind {
		case StepActivity:
			decision, done, err := decideActivityStep(spec, step)
			if err != nil || !done {
				return decision, err
			}
		case StepTimer:
			decision, done, err := decideTimerStep(spec, step)
			if err != nil || !done {
				return decision, err
			}
		case StepSignal, StepApproval:
			if _, received := spec.Instance.Signal(step.Name); !received {
				return orchestrationWait(step.Name), nil
			}
		default:
			return OrchestrationDecision{}, ErrInvalidOrchestration
		}
	}
	return orchestrationTerminal(spec, OrchestrationCompleted, "")
}

func decideActivityStep(
	spec OrchestrationDecisionSpec,
	step StepSpec,
) (OrchestrationDecision, bool, error) {
	progress, exists := spec.Instance.Activity(step.Name)
	if !exists {
		transition, err := NewActivitySchedule(ActivityScheduleSpec{
			TransitionID: spec.TransitionID, WorkID: spec.WorkID, Instance: spec.Instance,
			Definition: spec.Definition, StepName: step.Name, Attempt: 1,
			IdempotencyKey: spec.IdempotencyKey, ScheduledAt: spec.DecidedAt,
			Deadline: spec.Deadline, Input: spec.Input,
			TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
		})
		if err != nil {
			return OrchestrationDecision{}, false, ErrInvalidOrchestration
		}
		return OrchestrationDecision{kind: OrchestrationScheduled, stepName: step.Name, transition: transition}, false, nil
	}
	switch progress.Status() {
	case ActivityProgressSucceeded:
		return OrchestrationDecision{}, true, nil
	case ActivityProgressFailed:
		if progress.Retryable() && progress.Attempt() < step.Retry.MaxAttempts {
			return orchestrationWait(step.Name), false, nil
		}
		decision, err := orchestrationTerminal(spec, OrchestrationFailed, step.Name)
		return decision, false, err
	case ActivityProgressReady, ActivityProgressRunning, ActivityProgressUnknown, ActivityProgressRetryWaiting:
		return orchestrationWait(step.Name), false, nil
	default:
		return OrchestrationDecision{}, false, ErrInvalidOrchestration
	}
}

func decideTimerStep(
	spec OrchestrationDecisionSpec,
	step StepSpec,
) (OrchestrationDecision, bool, error) {
	progress, exists := spec.Instance.Timer(step.Name)
	if !exists {
		transition, err := NewTimerSchedule(TimerScheduleSpec{
			TransitionID: spec.TransitionID, WorkID: spec.WorkID,
			InstanceID: spec.Instance.id, ExpectedSequence: spec.Instance.sequence,
			Definition: spec.Definition, StepName: step.Name, ScheduledAt: spec.DecidedAt,
			Deadline: spec.Deadline, TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
		})
		if err != nil {
			return OrchestrationDecision{}, false, ErrInvalidOrchestration
		}
		return OrchestrationDecision{kind: OrchestrationScheduled, stepName: step.Name, transition: transition}, false, nil
	}
	if progress.Status() == TimerFired {
		return OrchestrationDecision{}, true, nil
	}
	if progress.Status() == TimerWaiting {
		return orchestrationWait(step.Name), false, nil
	}
	return OrchestrationDecision{}, false, ErrInvalidOrchestration
}

func orchestrationTerminal(
	spec OrchestrationDecisionSpec,
	kind OrchestrationDecisionKind,
	stepName string,
) (OrchestrationDecision, error) {
	eventKind := EventInstanceCompleted
	if kind == OrchestrationFailed {
		eventKind = EventInstanceFailed
	}
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.Instance.sequence + 1, InstanceID: spec.Instance.id,
		Kind: eventKind, OccurredAt: spec.DecidedAt, Data: spec.Result,
	})
	if err != nil {
		return OrchestrationDecision{}, ErrInvalidOrchestration
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: spec.Instance.id,
		ExpectedSequence: spec.Instance.sequence, Definition: spec.Instance.definition,
		Events: []HistoryEvent{event},
	})
	if err != nil {
		return OrchestrationDecision{}, ErrInvalidOrchestration
	}
	return OrchestrationDecision{kind: kind, stepName: stepName, transition: transition}, nil
}

func orchestrationWait(stepName string) OrchestrationDecision {
	return OrchestrationDecision{kind: OrchestrationWaiting, stepName: stepName}
}
