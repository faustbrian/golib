package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestOrchestrationRejectsUnsupportedAndInvalidStepDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 22, 0, 0, 0, time.UTC)
	activityDefinition := internalActivityTransitionDefinition(t)
	base := Instance{
		id: "instance-1", definition: activityDefinition.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
		activities: make(map[string]ActivityProgress), timers: make(map[string]TimerProgress),
		signals: make(map[string]SignalProgress), compensations: make(map[string]CompensationProgress),
	}
	if _, err := NewOrchestrationDecision(OrchestrationDecisionSpec{
		Instance: base, Definition: activityDefinition,
	}); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid activity schedule error = %v", err)
	}

	step, _ := activityProcessorStep(activityDefinition, "execute")
	retryable := base
	retryable.activities["execute"] = ActivityProgress{
		stepName: "execute", status: ActivityProgressFailed, attempt: 1, retryable: true,
	}
	decision, done, err := decideActivityStep(OrchestrationDecisionSpec{
		Instance: retryable, Definition: activityDefinition,
	}, step)
	if err != nil || done || decision.kind != OrchestrationWaiting {
		t.Fatalf("retryable decision = %#v, done %t, error %v", decision, done, err)
	}
	running := base
	running.activities = map[string]ActivityProgress{"execute": {status: ActivityProgressRunning}}
	decision, done, err = decideActivityStep(OrchestrationDecisionSpec{
		Instance: running, Definition: activityDefinition,
	}, step)
	if err != nil || done || decision.kind != OrchestrationWaiting {
		t.Fatalf("running decision = %#v, done %t, error %v", decision, done, err)
	}
	invalid := base
	invalid.activities = map[string]ActivityProgress{"execute": {status: ActivityProgressStatus(99)}}
	if _, _, err := decideActivityStep(OrchestrationDecisionSpec{
		Instance: invalid, Definition: activityDefinition,
	}, step); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid activity progress error = %v", err)
	}

	timerDefinition, err := NewDefinition(DefinitionSpec{
		Name: "timer", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{{Name: "delay", Kind: StepTimer, Timeout: time.Second}},
	})
	if err != nil {
		t.Fatalf("construct timer definition: %v", err)
	}
	timerInstance := base
	timerInstance.definition = timerDefinition.Reference()
	timerStep, _ := definitionStep(timerDefinition, "delay", StepTimer)
	if _, _, err := decideTimerStep(OrchestrationDecisionSpec{
		Instance: timerInstance, Definition: timerDefinition,
	}, timerStep); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid timer schedule error = %v", err)
	}
	timerInstance.timers = map[string]TimerProgress{"delay": {status: TimerProgressStatus(99)}}
	if _, _, err := decideTimerStep(OrchestrationDecisionSpec{
		Instance: timerInstance, Definition: timerDefinition,
	}, timerStep); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid timer progress error = %v", err)
	}

	unsupportedDefinition, err := NewDefinition(DefinitionSpec{
		Name: "unsupported", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "activity", Kind: StepActivity, Target: "activity.run", Timeout: time.Minute,
			InputLimit: 1, ResultLimit: 1,
			Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct unsupported definition: %v", err)
	}
	unsupportedDefinition.spec.Steps[0].Kind = StepKind(99)
	unsupported := base
	unsupported.definition = unsupportedDefinition.Reference()
	if _, err := NewOrchestrationDecision(OrchestrationDecisionSpec{
		Instance: unsupported, Definition: unsupportedDefinition,
	}); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("unsupported step error = %v", err)
	}
}

func TestOrchestrationTerminalRejectsInvalidEventAndTransition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 23, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	instance := Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
	}
	if _, err := orchestrationTerminal(OrchestrationDecisionSpec{
		TransitionID: "complete", Instance: instance,
	}, OrchestrationCompleted, ""); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid terminal event error = %v", err)
	}
	if _, err := orchestrationTerminal(OrchestrationDecisionSpec{
		Instance: instance, DecidedAt: now.Add(time.Second),
	}, OrchestrationCompleted, ""); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid terminal transition error = %v", err)
	}
}
