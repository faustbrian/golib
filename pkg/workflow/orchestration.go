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
	// OrchestrationRecorded persists a durable control-flow decision without
	// creating externally executable work.
	OrchestrationRecorded OrchestrationDecisionKind = 5
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
	ChildID        string
	Branches       []OrchestrationBranchSpec
}

// OrchestrationBranchSpec supplies one bounded parallel branch dispatch. The
// branch name must exactly match its enclosing immutable control step.
type OrchestrationBranchSpec struct {
	StepName       string
	WorkID         string
	IdempotencyKey string
	Input          []byte
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
	steps := spec.Definition.Steps()
	branchMembers := orchestrationBranchMembers(steps)
	for _, step := range steps {
		if _, branch := branchMembers[step.Name]; branch {
			if orchestrationBranchLeaf(step.Kind) {
				continue
			}
		}
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
		case StepParallel:
			decision, done, err := decideParallelStep(spec, step)
			if err != nil || !done {
				return decision, err
			}
		case StepJoin:
			decision, done, err := decideJoinStep(spec, step)
			if err != nil || !done {
				return decision, err
			}
		case StepRace:
			decision, done, err := decideRaceStep(spec, step)
			if err != nil || !done {
				return decision, err
			}
		case StepChild:
			decision, done, err := decideChildStep(spec, step)
			if err != nil || !done {
				return decision, err
			}
		default:
			return OrchestrationDecision{}, ErrInvalidOrchestration
		}
	}
	return orchestrationTerminal(spec, OrchestrationCompleted, "")
}

func decideChildStep(
	spec OrchestrationDecisionSpec,
	step StepSpec,
) (OrchestrationDecision, bool, error) {
	progress, exists := spec.Instance.Child(step.Name)
	if !exists {
		transition, err := NewChildSchedule(ChildScheduleSpec{
			TransitionID: spec.TransitionID, WorkID: spec.WorkID, ChildID: spec.ChildID,
			Instance: spec.Instance, Definition: spec.Definition, StepName: step.Name,
			ScheduledAt: spec.DecidedAt, Deadline: spec.Deadline, Input: spec.Input,
			TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
		})
		if err != nil {
			return OrchestrationDecision{}, false, ErrInvalidOrchestration
		}
		return OrchestrationDecision{
			kind: OrchestrationScheduled, stepName: step.Name, transition: transition,
		}, false, nil
	}
	switch progress.Status() {
	case ChildScheduled:
		return orchestrationWait(step.Name), false, nil
	case ChildStartRunning, ChildActive, ChildStartUnknownStatus, ChildStartRetryWaiting:
		return orchestrationWait(step.Name), false, nil
	case ChildStartFailedStatus:
		if progress.Retryable() && progress.Attempt() < step.Retry.MaxAttempts {
			return orchestrationWait(step.Name), false, nil
		}
		decision, err := orchestrationTerminal(spec, OrchestrationFailed, step.Name)
		return decision, false, err
	case ChildSucceeded:
		return OrchestrationDecision{}, true, nil
	case ChildFailed:
		decision, err := orchestrationTerminal(spec, OrchestrationFailed, step.Name)
		return decision, false, err
	default:
		return OrchestrationDecision{}, false, ErrInvalidOrchestration
	}
}

func decideRaceStep(
	spec OrchestrationDecisionSpec,
	step StepSpec,
) (OrchestrationDecision, bool, error) {
	if _, decided := spec.Instance.Race(step.Name); decided {
		return OrchestrationDecision{}, true, nil
	}
	winner := ""
	winnerAt := time.Time{}
	for _, branchName := range step.Branches {
		progress, exists := spec.Instance.Signal(branchName)
		if exists && (winnerAt.IsZero() || progress.ReceivedAt().Before(winnerAt)) {
			winner = branchName
			winnerAt = progress.ReceivedAt()
		}
	}
	if winner == "" {
		return orchestrationWait(step.Name), false, nil
	}
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: spec.Instance.sequence + 1, InstanceID: spec.Instance.id,
		Kind: EventRaceWon, OccurredAt: spec.DecidedAt, StepName: step.Name, Data: []byte(winner),
	})
	if err != nil {
		return OrchestrationDecision{}, false, ErrInvalidOrchestration
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: spec.Instance.id,
		ExpectedSequence: spec.Instance.sequence, Definition: spec.Instance.definition,
		Events: []HistoryEvent{event},
	})
	if err != nil {
		return OrchestrationDecision{}, false, ErrInvalidOrchestration
	}
	return OrchestrationDecision{
		kind: OrchestrationRecorded, stepName: step.Name, transition: transition,
	}, false, nil
}

func decideParallelStep(
	spec OrchestrationDecisionSpec,
	step StepSpec,
) (OrchestrationDecision, bool, error) {
	missing := 0
	for _, branchName := range step.Branches {
		progress, exists := spec.Instance.Activity(branchName)
		if !exists {
			missing++
			continue
		}
		switch progress.Status() {
		case ActivityProgressSucceeded:
		case ActivityProgressFailed:
			branch, _ := definitionActivityStep(spec.Definition, branchName)
			if progress.Retryable() && progress.Attempt() < branch.Retry.MaxAttempts {
				return orchestrationWait(step.Name), false, nil
			}
			decision, err := orchestrationTerminal(spec, OrchestrationFailed, branchName)
			return decision, false, err
		case ActivityProgressReady, ActivityProgressRunning, ActivityProgressUnknown, ActivityProgressRetryWaiting:
			return orchestrationWait(step.Name), false, nil
		default:
			return OrchestrationDecision{}, false, ErrInvalidOrchestration
		}
	}
	if missing == 0 {
		return OrchestrationDecision{}, true, nil
	}
	if missing != len(step.Branches) {
		return OrchestrationDecision{}, false, ErrInvalidOrchestration
	}
	transition, err := newParallelActivitySchedule(spec, step)
	if err != nil {
		return OrchestrationDecision{}, false, ErrInvalidOrchestration
	}
	return OrchestrationDecision{
		kind: OrchestrationScheduled, stepName: step.Name, transition: transition,
	}, false, nil
}

func decideJoinStep(
	spec OrchestrationDecisionSpec,
	step StepSpec,
) (OrchestrationDecision, bool, error) {
	for _, branchName := range step.Branches {
		progress, _ := spec.Instance.Activity(branchName)
		if progress.Status() != ActivityProgressSucceeded {
			return orchestrationWait(step.Name), false, nil
		}
	}
	return OrchestrationDecision{}, true, nil
}

func newParallelActivitySchedule(spec OrchestrationDecisionSpec, control StepSpec) (Transition, error) {
	branches := make(map[string]OrchestrationBranchSpec, len(spec.Branches))
	for _, branch := range spec.Branches {
		if _, exists := branches[branch.StepName]; exists {
			return Transition{}, ErrInvalidOrchestration
		}
		branches[branch.StepName] = branch
	}
	if len(branches) != len(control.Branches) {
		return Transition{}, ErrInvalidOrchestration
	}
	events := make([]HistoryEvent, 0, len(control.Branches))
	workItems := make([]PendingWork, 0, len(control.Branches))
	for index, branchName := range control.Branches {
		branchSpec, exists := branches[branchName]
		if !exists {
			return Transition{}, ErrInvalidOrchestration
		}
		branchStep, validStep := definitionActivityStep(spec.Definition, branchName)
		if !validStep || !instanceIDPattern.MatchString(branchSpec.IdempotencyKey) ||
			len(branchSpec.Input) > int(branchStep.InputLimit) {
			return Transition{}, ErrInvalidOrchestration
		}
		event, err := NewHistoryEvent(HistoryEventSpec{
			Sequence: spec.Instance.sequence + uint64(index) + 1, InstanceID: spec.Instance.id,
			Kind: EventActivityScheduled, OccurredAt: spec.DecidedAt,
			StepName: branchName, Data: branchSpec.Input,
		})
		if err != nil {
			return Transition{}, ErrInvalidOrchestration
		}
		work, err := NewPendingWork(PendingWorkSpec{
			ID: branchSpec.WorkID, Kind: WorkActivity, InstanceID: spec.Instance.id,
			Sequence: event.Sequence(), AvailableAt: spec.DecidedAt, Deadline: spec.Deadline,
			Payload:  encodeActivityDispatch(branchName, 1, branchSpec.IdempotencyKey),
			TenantID: spec.TenantID, CorrelationID: spec.CorrelationID,
		})
		if err != nil {
			return Transition{}, ErrInvalidOrchestration
		}
		events = append(events, event)
		workItems = append(workItems, work)
	}
	transition, err := NewTransition(TransitionSpec{
		ID: spec.TransitionID, InstanceID: spec.Instance.id,
		ExpectedSequence: spec.Instance.sequence, Definition: spec.Instance.definition,
		Events: events, Work: workItems,
	})
	if err != nil {
		return Transition{}, ErrInvalidOrchestration
	}
	return transition, nil
}

func orchestrationBranchMembers(steps []StepSpec) map[string]struct{} {
	members := make(map[string]struct{})
	for _, step := range steps {
		switch step.Kind {
		case StepParallel, StepRace:
		default:
			continue
		}
		for _, branch := range step.Branches {
			members[branch] = struct{}{}
		}
	}
	return members
}

func orchestrationBranchLeaf(kind StepKind) bool {
	switch kind {
	case StepParallel, StepJoin, StepRace:
		return false
	default:
		return true
	}
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
