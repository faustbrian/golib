package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestParallelOrchestrationClassifiesEveryPersistedBranchState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	definition := internalParallelDefinition(t)
	parallel := definition.Steps()[0]
	join := definition.Steps()[3]
	base := internalParallelInstance(definition, now)
	spec := OrchestrationDecisionSpec{
		TransitionID: "parallel-failure", Instance: base, Definition: definition,
		DecidedAt: now.Add(time.Second), Result: []byte("failed"),
	}

	retryable := base
	retryable.activities = map[string]ActivityProgress{
		"left":  {status: ActivityProgressFailed, attempt: 1, retryable: true},
		"right": {status: ActivityProgressSucceeded},
	}
	decision, done, err := decideParallelStep(specWithInstance(spec, retryable), parallel)
	if err != nil || done || decision.kind != OrchestrationWaiting {
		t.Fatalf("retryable branch = %#v, done %t, error %v", decision, done, err)
	}
	failed := base
	failed.activities = map[string]ActivityProgress{
		"left":  {status: ActivityProgressFailed, attempt: 2},
		"right": {status: ActivityProgressSucceeded},
	}
	decision, done, err = decideParallelStep(specWithInstance(spec, failed), parallel)
	if err != nil || done || decision.kind != OrchestrationFailed || decision.stepName != "left" {
		t.Fatalf("failed branch = %#v, done %t, error %v", decision, done, err)
	}
	running := base
	running.activities = map[string]ActivityProgress{
		"left": {status: ActivityProgressRunning}, "right": {status: ActivityProgressSucceeded},
	}
	decision, done, err = decideParallelStep(specWithInstance(spec, running), parallel)
	if err != nil || done || decision.kind != OrchestrationWaiting {
		t.Fatalf("running branch = %#v, done %t, error %v", decision, done, err)
	}
	invalid := base
	invalid.activities = map[string]ActivityProgress{
		"left": {status: ActivityProgressStatus(99)}, "right": {status: ActivityProgressSucceeded},
	}
	if _, _, err := decideParallelStep(specWithInstance(spec, invalid), parallel); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid branch error = %v", err)
	}
	partial := base
	partial.activities = map[string]ActivityProgress{"left": {status: ActivityProgressSucceeded}}
	if _, _, err := decideParallelStep(specWithInstance(spec, partial), parallel); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("partial branch admission error = %v", err)
	}
	if decision, done, err := decideJoinStep(specWithInstance(spec, partial), join); err != nil || done || decision.kind != OrchestrationWaiting {
		t.Fatalf("partial join = %#v, done %t, error %v", decision, done, err)
	}
	invalidDefinition := definition
	invalidDefinition.spec.Steps = []StepSpec{join, definition.Steps()[1], definition.Steps()[2], parallel}
	invalidJoin := partial
	invalidJoin.definition = invalidDefinition.Reference()
	if decision, err := NewOrchestrationDecision(OrchestrationDecisionSpec{
		Instance: invalidJoin, Definition: invalidDefinition,
	}); err != nil || decision.kind != OrchestrationWaiting || decision.stepName != "join" {
		t.Fatalf("defensive join = %#v, error %v", decision, err)
	}
	if _, _, err := decideParallelStep(spec, parallel); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("missing branch dispatch error = %v", err)
	}
}

func TestParallelScheduleRejectsDuplicateMalformedAndUnboundedPlans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	definition := internalParallelDefinition(t)
	control := definition.Steps()[0]
	base := OrchestrationDecisionSpec{
		TransitionID: "parallel-schedule", Instance: internalParallelInstance(definition, now),
		Definition: definition, DecidedAt: now.Add(time.Second), Deadline: now.Add(time.Hour),
		Branches: []OrchestrationBranchSpec{
			{StepName: "left", WorkID: "left-work", IdempotencyKey: "left-key"},
			{StepName: "right", WorkID: "right-work", IdempotencyKey: "right-key"},
		},
	}
	tests := []OrchestrationDecisionSpec{
		func() OrchestrationDecisionSpec {
			value := base
			value.Branches = []OrchestrationBranchSpec{base.Branches[0], base.Branches[0]}
			return value
		}(),
		func() OrchestrationDecisionSpec {
			value := base
			value.Branches = value.Branches[:1]
			return value
		}(),
		func() OrchestrationDecisionSpec {
			value := base
			value.Branches = append([]OrchestrationBranchSpec(nil), base.Branches...)
			value.Branches[0].IdempotencyKey = " spaces "
			return value
		}(),
		func() OrchestrationDecisionSpec { value := base; value.DecidedAt = time.Time{}; return value }(),
		func() OrchestrationDecisionSpec {
			value := base
			value.Branches = append([]OrchestrationBranchSpec(nil), base.Branches...)
			value.Branches[0].WorkID = ""
			return value
		}(),
		func() OrchestrationDecisionSpec { value := base; value.TransitionID = ""; return value }(),
	}
	for index, spec := range tests {
		if _, err := newParallelActivitySchedule(spec, control); !errors.Is(err, ErrInvalidOrchestration) {
			t.Fatalf("invalid plan %d error = %v", index, err)
		}
	}
}

func internalParallelDefinition(t *testing.T) Definition {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: "parallel", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{
			{Name: "fan-out", Kind: StepParallel, FanOutLimit: 2, Branches: []string{"left", "right"}},
			{Name: "left", Kind: StepActivity, Target: "left.run", Timeout: time.Second, InputLimit: 8, ResultLimit: 8, Retry: RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second}},
			{Name: "right", Kind: StepActivity, Target: "right.run", Timeout: time.Second, InputLimit: 8, ResultLimit: 8, Retry: RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second}},
			{Name: "join", Kind: StepJoin, FanOutLimit: 2, Branches: []string{"left", "right"}},
		},
	})
	if err != nil {
		t.Fatalf("construct parallel definition: %v", err)
	}
	return definition
}

func internalParallelInstance(definition Definition, now time.Time) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
		activities: make(map[string]ActivityProgress), timers: make(map[string]TimerProgress),
		signals: make(map[string]SignalProgress), compensations: make(map[string]CompensationProgress),
	}
}

func specWithInstance(spec OrchestrationDecisionSpec, instance Instance) OrchestrationDecisionSpec {
	spec.Instance = instance
	return spec
}
