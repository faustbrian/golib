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

func TestOrchestrationValidatesEachInstancePrecondition(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	base := Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
		activities: map[string]ActivityProgress{
			"execute": {stepName: "execute", status: ActivityProgressSucceeded, attempt: 1},
		},
		signals: make(map[string]SignalProgress), timers: make(map[string]TimerProgress),
		compensations: make(map[string]CompensationProgress),
	}
	valid := OrchestrationDecisionSpec{
		TransitionID: "complete-1", Instance: base, Definition: definition,
		DecidedAt: now.Add(time.Second),
	}
	if decision, err := NewOrchestrationDecision(valid); err != nil || decision.Kind() != OrchestrationCompleted {
		t.Fatalf("valid decision = %#v, error %v", decision, err)
	}

	wrongMode := valid
	wrongMode.Definition.spec.Mode = Choreography
	wrongMode.Instance.definition = wrongMode.Definition.Reference()
	wrongDefinition := valid
	wrongDefinition.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: "other"}
	notRunning := valid
	notRunning.Instance.status = StatusPaused
	for name, spec := range map[string]OrchestrationDecisionSpec{
		"mode": wrongMode, "definition": wrongDefinition, "status": notRunning,
	} {
		if _, err := NewOrchestrationDecision(spec); !errors.Is(err, ErrInvalidOrchestration) {
			t.Fatalf("%s precondition error = %v", name, err)
		}
	}
}

func TestOrchestrationFailureRequiresRetryabilityAndRemainingAttempts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	definition := internalParallelOrchestrationDefinition(t)
	parallel, _ := definitionStep(definition, "parallel", StepParallel)
	activity, _ := definitionStep(definition, "reserve", StepActivity)
	base := Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
		signals: make(map[string]SignalProgress), timers: make(map[string]TimerProgress),
		compensations: make(map[string]CompensationProgress),
	}
	for name, progress := range map[string]ActivityProgress{
		"known failure before limit": {stepName: "reserve", status: ActivityProgressFailed, attempt: 1},
		"retryable failure at limit": {stepName: "reserve", status: ActivityProgressFailed, attempt: 2, retryable: true},
	} {
		instance := base
		instance.activities = map[string]ActivityProgress{"reserve": progress}
		spec := OrchestrationDecisionSpec{
			TransitionID: "failure-1", Instance: instance, Definition: definition,
			DecidedAt: now.Add(time.Second),
		}
		parallelDecision, done, err := decideParallelStep(spec, parallel)
		if err != nil || done || parallelDecision.Kind() != OrchestrationFailed {
			t.Fatalf("parallel %s = %#v, done %t, error %v", name, parallelDecision, done, err)
		}
		activityDecision, done, err := decideActivityStep(spec, activity)
		if err != nil || done || activityDecision.Kind() != OrchestrationFailed {
			t.Fatalf("activity %s = %#v, done %t, error %v", name, activityDecision, done, err)
		}
	}
}

func TestParallelSchedulingValidatesEachBranchBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	definition := internalParallelOrchestrationDefinition(t)
	control, _ := definitionStep(definition, "parallel", StepParallel)
	base := OrchestrationDecisionSpec{
		TransitionID: "parallel-1", Instance: Instance{
			id: "instance-1", definition: definition.Reference(), status: StatusRunning,
			sequence: 1, startedAt: now, updatedAt: now,
		},
		Definition: definition, DecidedAt: now.Add(time.Second), Deadline: now.Add(time.Hour),
		Branches: []OrchestrationBranchSpec{{
			StepName: "reserve", WorkID: "work-1", IdempotencyKey: "key-1", Input: []byte("1234"),
		}},
	}
	if transition, err := newParallelActivitySchedule(base, control); err != nil || len(transition.Work()) != 1 {
		t.Fatalf("boundary schedule = %#v, error %v", transition, err)
	}

	missing := base
	missing.Branches = []OrchestrationBranchSpec{{StepName: "other", WorkID: "work-1", IdempotencyKey: "key-1"}}
	invalidStep := base
	invalidStep.Definition = internalParallelOrchestrationDefinition(t)
	invalidStep.Definition.spec.Steps[1].Kind = StepSignal
	invalidKey := base
	invalidKey.Branches = append([]OrchestrationBranchSpec(nil), base.Branches...)
	invalidKey.Branches[0].IdempotencyKey = " spaces "
	oversized := base
	oversized.Branches = append([]OrchestrationBranchSpec(nil), base.Branches...)
	oversized.Branches[0].Input = []byte("12345")
	for name, spec := range map[string]OrchestrationDecisionSpec{
		"missing branch": missing, "invalid step": invalidStep,
		"invalid idempotency key": invalidKey, "oversized input": oversized,
	} {
		if _, err := newParallelActivitySchedule(spec, control); !errors.Is(err, ErrInvalidOrchestration) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestOrchestrationBranchMembershipOnlyIncludesParallelAndRaceBranches(t *testing.T) {
	t.Parallel()

	steps := []StepSpec{
		{Name: "activity", Kind: StepActivity, Branches: []string{"activity-branch"}},
		{Name: "join", Kind: StepJoin, Branches: []string{"join-branch"}},
		{Name: "parallel", Kind: StepParallel, Branches: []string{"parallel-branch"}},
		{Name: "race", Kind: StepRace, Branches: []string{"race-branch"}},
	}
	members := orchestrationBranchMembers(steps)
	if len(members) != 2 {
		t.Fatalf("branch members = %#v", members)
	}
	for _, branch := range []string{"parallel-branch", "race-branch"} {
		if _, exists := members[branch]; !exists {
			t.Fatalf("branch %q absent from %#v", branch, members)
		}
	}
	for _, branch := range []string{"activity-branch", "join-branch"} {
		if _, exists := members[branch]; exists {
			t.Fatalf("non-control branch %q present in %#v", branch, members)
		}
	}
	for kind, want := range map[StepKind]bool{
		StepActivity: true, StepSignal: true, StepApproval: true, StepTimer: true, StepChild: true,
		StepParallel: false, StepJoin: false, StepRace: false,
	} {
		if got := orchestrationBranchLeaf(kind); got != want {
			t.Fatalf("branch leaf %d = %t, want %t", kind, got, want)
		}
	}
}

func TestJoinWaitsForMissingAndNonSuccessfulBranches(t *testing.T) {
	t.Parallel()

	definition := internalParallelOrchestrationDefinition(t)
	join := StepSpec{Name: "join", Kind: StepJoin, Branches: []string{"reserve"}}
	base := Instance{id: "instance-1", definition: definition.Reference(), status: StatusRunning}
	for name, activities := range map[string]map[string]ActivityProgress{
		"missing": {},
		"running": {"reserve": {stepName: "reserve", status: ActivityProgressRunning, attempt: 1}},
	} {
		instance := base
		instance.activities = activities
		decision, done, err := decideJoinStep(OrchestrationDecisionSpec{Instance: instance}, join)
		if err != nil || done || decision.Kind() != OrchestrationWaiting {
			t.Fatalf("%s join = %#v, done %t, error %v", name, decision, done, err)
		}
	}
}

func internalParallelOrchestrationDefinition(t *testing.T) Definition {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: "parallel", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{
			{Name: "parallel", Kind: StepParallel, FanOutLimit: 1, Branches: []string{"reserve"}},
			{Name: "reserve", Kind: StepActivity, Target: "inventory.reserve", Timeout: time.Minute,
				InputLimit: 4, ResultLimit: 4,
				Retry: RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second}},
			{Name: "join", Kind: StepJoin, FanOutLimit: 1, Branches: []string{"reserve"}},
		},
	})
	if err != nil {
		t.Fatalf("construct parallel orchestration definition: %v", err)
	}
	return definition
}
