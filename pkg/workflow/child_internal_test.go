package workflow

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestChildBuildersRejectMalformedDispatchSchedulesAndOutcomes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	parent, child, registry := internalChildDefinitions(t)
	base := internalChildInstance(parent, now)
	valid := ChildScheduleSpec{
		TransitionID: "schedule", WorkID: "child-work", ChildID: "child-1",
		Instance: base, Definition: parent, StepName: "child", ScheduledAt: now.Add(time.Second),
		Deadline: now.Add(time.Hour), Input: []byte("input"),
	}
	transition, err := NewChildSchedule(valid)
	if err != nil {
		t.Fatalf("construct child schedule: %v", err)
	}
	dispatchPayload := transition.Work()[0].Payload()
	invalidDispatches := [][]byte{
		nil,
		make([]byte, MaxChildDispatchBytes+1),
		[]byte("{"),
		append(append([]byte(nil), dispatchPayload...), []byte("{}")...),
		[]byte(`{"step_name":" spaces ","child_id":"child-1","definition_name":"child","definition_version":"1","definition_fingerprint":"` + strings.Repeat("0", 64) + `"}`),
		[]byte(`{"step_name":"child","child_id":" spaces ","definition_name":"child","definition_version":"1","definition_fingerprint":"` + strings.Repeat("0", 64) + `"}`),
		[]byte(`{"step_name":"child","child_id":"child-1","definition_name":"child","definition_version":"1","definition_fingerprint":"bad"}`),
	}
	for index, payload := range invalidDispatches {
		if _, err := DecodeChildDispatch(payload); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("invalid dispatch %d error = %v", index, err)
		}
	}

	other := mustInternalDefinition(t, "other", "1")
	invalidSchedules := []ChildScheduleSpec{
		func() ChildScheduleSpec { value := valid; value.StepName = "missing"; return value }(),
		func() ChildScheduleSpec { value := valid; value.Instance.status = StatusPaused; return value }(),
		func() ChildScheduleSpec { value := valid; value.Definition = other; return value }(),
		func() ChildScheduleSpec { value := valid; value.Input = make([]byte, 9); return value }(),
		func() ChildScheduleSpec { value := valid; value.ChildID = " spaces "; return value }(),
		func() ChildScheduleSpec { value := valid; value.ScheduledAt = now.Add(-time.Second); return value }(),
		func() ChildScheduleSpec {
			value := valid
			value.Instance = internalChildInstance(parent, now)
			value.Instance.children["child"] = ChildProgress{status: ChildScheduled}
			return value
		}(),
		func() ChildScheduleSpec { value := valid; value.WorkID = ""; return value }(),
		func() ChildScheduleSpec { value := valid; value.TransitionID = ""; return value }(),
		func() ChildScheduleSpec { value := valid; value.Instance.sequence = ^uint64(0); return value }(),
	}
	for index, spec := range invalidSchedules {
		if _, err := NewChildSchedule(spec); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("invalid schedule %d error = %v", index, err)
		}
	}

	scheduled := internalChildInstance(parent, now)
	if err := scheduled.applyChild(registry, transition.Events()[0]); err != nil {
		t.Fatalf("apply child schedule: %v", err)
	}
	validOutcome := ChildOutcomeSpec{
		TransitionID: "complete", Instance: scheduled, Definition: parent,
		StepName: "child", ChildID: "child-1", CompletedAt: now.Add(2 * time.Second), Result: []byte("result"),
	}
	invalidOutcomes := []ChildOutcomeSpec{
		func() ChildOutcomeSpec { value := validOutcome; value.StepName = "missing"; return value }(),
		func() ChildOutcomeSpec { value := validOutcome; value.Instance = base; return value }(),
		func() ChildOutcomeSpec {
			value := validOutcome
			value.Instance.children = map[string]ChildProgress{"child": value.Instance.children["child"]}
			progress := value.Instance.children["child"]
			progress.status = ChildSucceeded
			value.Instance.children["child"] = progress
			return value
		}(),
		func() ChildOutcomeSpec { value := validOutcome; value.ChildID = "other-child"; return value }(),
		func() ChildOutcomeSpec { value := validOutcome; value.Instance.status = StatusPaused; return value }(),
		func() ChildOutcomeSpec { value := validOutcome; value.Definition = other; return value }(),
		func() ChildOutcomeSpec { value := validOutcome; value.Result = make([]byte, 9); return value }(),
		func() ChildOutcomeSpec {
			value := validOutcome
			value.CompletedAt = now.Add(-time.Second)
			return value
		}(),
		func() ChildOutcomeSpec { value := validOutcome; value.FailureCode = " spaces "; return value }(),
		func() ChildOutcomeSpec { value := validOutcome; value.TransitionID = ""; return value }(),
		func() ChildOutcomeSpec { value := validOutcome; value.Instance.sequence = ^uint64(0); return value }(),
	}
	for index, spec := range invalidOutcomes {
		if _, err := NewChildOutcome(spec); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("invalid outcome %d error = %v", index, err)
		}
	}
	_ = child
	if _, _, err := decideChildStep(OrchestrationDecisionSpec{Instance: base, Definition: parent}, parent.Steps()[0]); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid child orchestration error = %v", err)
	}
}

func TestChildReplayAndOrchestrationPreserveKnownFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	parent, _, registry := internalChildDefinitions(t)
	base := internalChildInstance(parent, now)
	schedule, err := NewChildSchedule(ChildScheduleSpec{
		TransitionID: "schedule", WorkID: "child-work", ChildID: "child-1",
		Instance: base, Definition: parent, StepName: "child", ScheduledAt: now.Add(time.Second),
		Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("schedule child: %v", err)
	}
	scheduled := base
	if err := scheduled.applyChild(registry, schedule.Events()[0]); err != nil {
		t.Fatalf("apply child schedule: %v", err)
	}
	failure, err := NewChildOutcome(ChildOutcomeSpec{
		TransitionID: "failure", Instance: scheduled, Definition: parent,
		StepName: "child", ChildID: "child-1", CompletedAt: now.Add(2 * time.Second),
		FailureCode: "child.failed", Result: []byte("failure"),
	})
	if err != nil {
		t.Fatalf("construct child failure: %v", err)
	}
	if err := scheduled.applyChild(registry, failure.Events()[0]); err != nil {
		t.Fatalf("apply child failure: %v", err)
	}
	progress, _ := scheduled.Child("child")
	if progress.Status() != ChildFailed || progress.Code() != "child.failed" || string(progress.Result()) != "failure" {
		t.Fatalf("child failure = %#v", progress)
	}
	decision, _, err := decideChildStep(OrchestrationDecisionSpec{
		TransitionID: "parent-failure", Instance: scheduled, Definition: parent,
		DecidedAt: now.Add(3 * time.Second), Result: []byte("parent-failure"),
	}, parent.Steps()[0])
	if err != nil || decision.kind != OrchestrationFailed {
		t.Fatalf("failed child decision = %#v, error %v", decision, err)
	}
	invalid := scheduled
	invalid.children["child"] = ChildProgress{status: ChildProgressStatus(99)}
	if _, _, err := decideChildStep(OrchestrationDecisionSpec{Instance: invalid, Definition: parent}, parent.Steps()[0]); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("invalid child status error = %v", err)
	}
}

func TestChildHistoryRejectsIncoherentTransitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	parent, child, registry := internalChildDefinitions(t)
	base := internalChildInstance(parent, now)
	scheduledEvent := mustInternalHistoryEvent(t, HistoryEventSpec{
		Sequence: 2, InstanceID: "parent-1", Kind: EventChildScheduled, OccurredAt: now.Add(time.Second),
		Definition: child.Reference(), SuccessorID: "child-1", StepName: "child", Data: []byte("input"),
	})
	invalidSchedules := []struct {
		instance Instance
		event    HistoryEvent
	}{
		{instance: func() Instance { value := base; value.status = StatusPaused; return value }(), event: scheduledEvent},
		{instance: base, event: mustInternalHistoryEvent(t, HistoryEventSpec{Sequence: 2, InstanceID: "parent-1", Kind: EventChildScheduled, OccurredAt: now.Add(time.Second), Definition: child.Reference(), SuccessorID: "child-1", StepName: "missing"})},
		{instance: base, event: mustInternalHistoryEvent(t, HistoryEventSpec{Sequence: 2, InstanceID: "parent-1", Kind: EventChildScheduled, OccurredAt: now.Add(time.Second), Definition: child.Reference(), SuccessorID: "child-1", StepName: "child", Data: make([]byte, 9)})},
	}
	for index, test := range invalidSchedules {
		if err := test.instance.applyChild(registry, test.event); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid child schedule %d error = %v", index, err)
		}
	}
	scheduled := base
	if err := scheduled.applyChild(registry, scheduledEvent); err != nil {
		t.Fatalf("apply child schedule: %v", err)
	}
	if err := scheduled.applyChild(registry, scheduledEvent); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("duplicate child schedule error = %v", err)
	}
	completed := mustInternalHistoryEvent(t, HistoryEventSpec{
		Sequence: 3, InstanceID: "parent-1", Kind: EventChildCompleted, OccurredAt: now.Add(2 * time.Second),
		SuccessorID: "child-1", StepName: "child", Data: []byte("result"),
	})
	invalidOutcomes := []HistoryEvent{
		mustInternalHistoryEvent(t, HistoryEventSpec{Sequence: 3, InstanceID: "parent-1", Kind: EventChildCompleted, OccurredAt: now.Add(2 * time.Second), SuccessorID: "other", StepName: "child"}),
		mustInternalHistoryEvent(t, HistoryEventSpec{Sequence: 3, InstanceID: "parent-1", Kind: EventChildCompleted, OccurredAt: now.Add(2 * time.Second), SuccessorID: "child-1", StepName: "child", Data: make([]byte, 9)}),
	}
	for index, event := range invalidOutcomes {
		value := scheduled
		if err := value.applyChild(registry, event); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid child outcome %d error = %v", index, err)
		}
	}
	if err := scheduled.applyChild(registry, completed); err != nil {
		t.Fatalf("apply child completion: %v", err)
	}
	if err := scheduled.applyChild(registry, completed); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("duplicate child completion error = %v", err)
	}
	if children := (Instance{}).Children(); children != nil || childProgressSnapshots(nil) != nil {
		t.Fatalf("empty children = %#v", children)
	}
	invalidFields := []HistoryEventSpec{
		{Kind: EventChildCompleted, StepName: "child", SuccessorID: "child-1", IdempotencyKey: "key"},
		{Kind: EventChildCompleted, StepName: "child", SuccessorID: "child-1", DueAt: now},
		{Kind: EventChildCompleted, StepName: "child", SuccessorID: "child-1", Retryable: true},
		{Kind: EventChildCompleted, StepName: "child", SuccessorID: "child-1", Definition: child.Reference()},
	}
	for index, spec := range invalidFields {
		if validChildEventFields(spec) {
			t.Fatalf("invalid child fields %d accepted", index)
		}
	}
	if err := base.apply(registry, mustInternalHistoryEvent(t, HistoryEventSpec{
		Sequence: 2, InstanceID: "parent-1", Kind: EventChildCompleted, OccurredAt: now.Add(time.Second),
		SuccessorID: "child-1", StepName: "child",
	})); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("orphan child outcome error = %v", err)
	}
}

func internalChildDefinitions(t *testing.T) (Definition, Definition, *Registry) {
	t.Helper()
	child := mustInternalDefinition(t, "child", "1")
	parent, err := NewDefinition(DefinitionSpec{
		Name: "parent", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "child", Kind: StepChild, Target: "child", ChildDefinition: child.Reference(),
			Timeout: time.Minute, InputLimit: 8, ResultLimit: 8,
			Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct parent definition: %v", err)
	}
	registry, err := CompileDefinitions(parent, child)
	if err != nil {
		t.Fatalf("compile child definitions: %v", err)
	}
	return parent, child, registry
}

func internalChildInstance(parent Definition, now time.Time) Instance {
	return Instance{
		id: "parent-1", definition: parent.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
		activities: make(map[string]ActivityProgress), timers: make(map[string]TimerProgress),
		signals: make(map[string]SignalProgress), races: make(map[string]RaceProgress),
		children: make(map[string]ChildProgress), compensations: make(map[string]CompensationProgress),
	}
}

func mustInternalDefinition(t *testing.T, name, version string) Definition {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: name, Version: version, Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "activity", Kind: StepActivity, Target: "activity.run", Timeout: time.Minute,
			InputLimit: 1, ResultLimit: 1,
			Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct internal definition: %v", err)
	}
	return definition
}

func mustInternalHistoryEvent(t *testing.T, spec HistoryEventSpec) HistoryEvent {
	t.Helper()
	event, err := NewHistoryEvent(spec)
	if err != nil {
		t.Fatalf("construct internal history event: %v", err)
	}
	return event
}
