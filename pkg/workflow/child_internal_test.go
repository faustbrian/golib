package workflow

import (
	"encoding/json"
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
	validDocument := childDispatchDocument{
		StepName: "child", ChildID: "child-1", DefinitionName: child.Reference().Name(),
		DefinitionVersion:     child.Reference().Version(),
		DefinitionFingerprint: child.Reference().Fingerprint(),
		Attempt:               1, IdempotencyKey: "child-1",
	}
	invalidDocuments := []childDispatchDocument{
		func() childDispatchDocument { value := validDocument; value.StepName = " spaces "; return value }(),
		func() childDispatchDocument { value := validDocument; value.ChildID = " spaces "; return value }(),
		func() childDispatchDocument {
			value := validDocument
			value.DefinitionFingerprint = "bad"
			return value
		}(),
		func() childDispatchDocument { value := validDocument; value.Attempt = 0; return value }(),
		func() childDispatchDocument { value := validDocument; value.IdempotencyKey = " spaces "; return value }(),
	}
	for index, document := range invalidDocuments {
		payload, marshalErr := json.Marshal(document)
		if marshalErr != nil {
			t.Fatalf("marshal invalid child dispatch %d: %v", index, marshalErr)
		}
		if _, err := DecodeChildDispatch(payload); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("isolated invalid dispatch %d error = %v", index, err)
		}
	}
	validPayload, err := json.Marshal(validDocument)
	if err != nil {
		t.Fatalf("marshal valid child dispatch: %v", err)
	}
	overLimit := append(append([]byte(nil), validPayload...), make([]byte, MaxChildDispatchBytes-len(validPayload)+1)...)
	for index := len(validPayload); index < len(overLimit); index++ {
		overLimit[index] = ' '
	}
	if _, err := DecodeChildDispatch(overLimit); !errors.Is(err, ErrInvalidChildTransition) {
		t.Fatalf("over-limit valid dispatch error = %v", err)
	}
	atLimit := overLimit[:MaxChildDispatchBytes]
	if _, err := DecodeChildDispatch(atLimit); err != nil {
		t.Fatalf("maximum-size valid dispatch rejected: %v", err)
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
	maximumInput := valid
	maximumInput.TransitionID = "schedule-max-input"
	maximumInput.WorkID = "child-work-max-input"
	maximumInput.Input = make([]byte, 8)
	if _, err := NewChildSchedule(maximumInput); err != nil {
		t.Fatalf("maximum schedule input rejected: %v", err)
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
		func() ChildOutcomeSpec {
			value := validOutcome
			value.StepName = "missing"
			value.Result = nil
			value.Instance.children = map[string]ChildProgress{"missing": {
				stepName: "missing", childID: "child-1", definition: child.Reference(),
				status: ChildScheduled,
			}}
			return value
		}(),
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
	maximumResult := validOutcome
	maximumResult.TransitionID = "complete-max-result"
	maximumResult.Result = make([]byte, 8)
	if _, err := NewChildOutcome(maximumResult); err != nil {
		t.Fatalf("maximum child result rejected: %v", err)
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
	for _, status := range []ChildProgressStatus{
		ChildStartRunning, ChildActive, ChildStartUnknownStatus, ChildStartRetryWaiting,
	} {
		waiting := scheduled
		waiting.children = map[string]ChildProgress{"child": {status: status}}
		decision, _, err := decideChildStep(
			OrchestrationDecisionSpec{Instance: waiting, Definition: parent},
			parent.Steps()[0],
		)
		if err != nil || decision.kind != OrchestrationWaiting {
			t.Fatalf("child status %d decision = %#v, %v", status, decision, err)
		}
	}
	startFailed := scheduled
	startFailed.children = map[string]ChildProgress{"child": {status: ChildStartFailedStatus}}
	decision, _, err = decideChildStep(OrchestrationDecisionSpec{
		TransitionID: "parent-start-failure", Instance: startFailed, Definition: parent,
		DecidedAt: now.Add(4 * time.Second), Result: []byte("parent-failure"),
	}, parent.Steps()[0])
	if err != nil || decision.kind != OrchestrationFailed {
		t.Fatalf("failed child start decision = %#v, %v", decision, err)
	}
	retryParent, _, _ := internalChildRetryDefinitions(t)
	retryableStartFailure := internalChildInstance(retryParent, now)
	retryableStartFailure.children = map[string]ChildProgress{"child": {
		status: ChildStartFailedStatus, attempt: 1, retryable: true,
	}}
	decision, _, err = decideChildStep(
		OrchestrationDecisionSpec{Instance: retryableStartFailure, Definition: retryParent},
		retryParent.Steps()[0],
	)
	if err != nil || decision.kind != OrchestrationWaiting {
		t.Fatalf("retryable child start decision = %#v, %v", decision, err)
	}
	exhaustedStartFailure := internalChildInstance(retryParent, now)
	exhaustedStartFailure.children = map[string]ChildProgress{"child": {
		status: ChildStartFailedStatus, attempt: 2, retryable: true,
	}}
	decision, _, err = decideChildStep(OrchestrationDecisionSpec{
		TransitionID: "parent-exhausted-start", Instance: exhaustedStartFailure,
		Definition: retryParent, DecidedAt: now.Add(4 * time.Second),
		Result: []byte("parent-failure"),
	}, retryParent.Steps()[0])
	if err != nil || decision.kind != OrchestrationFailed {
		t.Fatalf("exhausted child start decision = %#v, %v", decision, err)
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
	other := mustInternalDefinition(t, "other-child", "1")
	registryWithOther, err := CompileDefinitions(parent, child, other)
	if err != nil {
		t.Fatalf("compile alternate child registry: %v", err)
	}
	wrongDefinition := mustInternalHistoryEvent(t, HistoryEventSpec{
		Sequence: 2, InstanceID: "parent-1", Kind: EventChildScheduled,
		OccurredAt: now.Add(time.Second), Definition: other.Reference(),
		SuccessorID: "child-1", StepName: "child",
	})
	value := base
	if err := value.applyChild(registryWithOther, wrongDefinition); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("wrong child definition error = %v", err)
	}
	missingChildRegistry := *registry
	missingChildRegistry.definitions = make(map[definitionKey]Definition, len(registry.definitions))
	for key, definition := range registry.definitions {
		missingChildRegistry.definitions[key] = definition
	}
	delete(missingChildRegistry.definitions, definitionKey{name: child.Reference().Name(), version: child.Reference().Version()})
	value = base
	if err := value.applyChild(&missingChildRegistry, scheduledEvent); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unregistered pinned child error = %v", err)
	}
	maxInput := scheduledEvent
	maxInput.data = make([]byte, 8)
	value = base
	value.children = make(map[string]ChildProgress)
	if err := value.applyChild(registry, maxInput); err != nil {
		t.Fatalf("maximum child input rejected: %v", err)
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

func TestChildEventFieldValidationIsExactForEveryEventKind(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 17, 30, 0, 0, time.UTC)
	_, child, _ := internalChildDefinitions(t)
	valid := map[EventKind]HistoryEventSpec{
		EventChildScheduled: {
			Kind: EventChildScheduled, StepName: "child", Definition: child.Reference(),
		},
		EventChildStartAttempted: {
			Kind: EventChildStartAttempted, StepName: "child", Attempt: 1,
			IdempotencyKey: "child-1", OccurredAt: now, DueAt: now.Add(time.Minute),
		},
		EventChildStarted: {
			Kind: EventChildStarted, StepName: "child", Attempt: 1,
		},
		EventChildStartFailed: {
			Kind: EventChildStartFailed, StepName: "child", Attempt: 1, Code: "failed",
		},
		EventChildStartUnknown: {
			Kind: EventChildStartUnknown, StepName: "child", Attempt: 1, Code: "unknown",
		},
		EventChildStartRetryScheduled: {
			Kind: EventChildStartRetryScheduled, StepName: "child", Attempt: 1,
			OccurredAt: now, DueAt: now.Add(time.Second),
		},
		EventChildCompleted: {Kind: EventChildCompleted, StepName: "child"},
		EventChildFailed:    {Kind: EventChildFailed, StepName: "child", Code: "failed"},
	}
	for kind, spec := range valid {
		if !validChildEventFields(spec) {
			t.Fatalf("valid child event kind %d rejected", kind)
		}
	}
	assertInvalid := func(name string, spec HistoryEventSpec) {
		t.Helper()
		if validChildEventFields(spec) {
			t.Fatalf("invalid child event fields accepted: %s", name)
		}
	}
	for kind, spec := range valid {
		value := spec
		value.StepName = " spaces "
		assertInvalid("step-"+string(rune(kind)), value)
		if kind != EventChildScheduled {
			value = spec
			value.Definition = child.Reference()
			assertInvalid("definition-"+string(rune(kind)), value)
		}
	}
	mutate := func(kind EventKind, name string, change func(*HistoryEventSpec)) {
		t.Helper()
		value := valid[kind]
		change(&value)
		assertInvalid(name, value)
	}
	mutate(EventChildScheduled, "schedule-definition", func(value *HistoryEventSpec) { value.Definition = DefinitionReference{} })
	mutate(EventChildScheduled, "schedule-attempt", func(value *HistoryEventSpec) { value.Attempt = 1 })
	mutate(EventChildScheduled, "schedule-key", func(value *HistoryEventSpec) { value.IdempotencyKey = "child-1" })
	mutate(EventChildScheduled, "schedule-due", func(value *HistoryEventSpec) { value.DueAt = now })
	mutate(EventChildScheduled, "schedule-code", func(value *HistoryEventSpec) { value.Code = "code" })
	mutate(EventChildScheduled, "schedule-retryable", func(value *HistoryEventSpec) { value.Retryable = true })

	for _, kind := range []EventKind{
		EventChildStartAttempted, EventChildStarted, EventChildStartFailed,
		EventChildStartUnknown, EventChildStartRetryScheduled,
	} {
		mutate(kind, "attempt-zero", func(value *HistoryEventSpec) { value.Attempt = 0 })
		mutate(kind, "attempt-data", func(value *HistoryEventSpec) { value.Data = []byte("data") })
	}
	mutate(EventChildStartAttempted, "attempt-key", func(value *HistoryEventSpec) { value.IdempotencyKey = " spaces " })
	mutate(EventChildStartAttempted, "attempt-due", func(value *HistoryEventSpec) { value.DueAt = now })
	mutate(EventChildStartAttempted, "attempt-code", func(value *HistoryEventSpec) { value.Code = "code" })
	mutate(EventChildStartAttempted, "attempt-retryable", func(value *HistoryEventSpec) { value.Retryable = true })
	mutate(EventChildStarted, "started-key", func(value *HistoryEventSpec) { value.IdempotencyKey = "child-1" })
	mutate(EventChildStarted, "started-due", func(value *HistoryEventSpec) { value.DueAt = now })
	mutate(EventChildStarted, "started-code", func(value *HistoryEventSpec) { value.Code = "code" })
	mutate(EventChildStarted, "started-retryable", func(value *HistoryEventSpec) { value.Retryable = true })
	mutate(EventChildStartFailed, "failed-key", func(value *HistoryEventSpec) { value.IdempotencyKey = "child-1" })
	mutate(EventChildStartFailed, "failed-due", func(value *HistoryEventSpec) { value.DueAt = now })
	mutate(EventChildStartFailed, "failed-code", func(value *HistoryEventSpec) { value.Code = " spaces " })
	mutate(EventChildStartUnknown, "unknown-key", func(value *HistoryEventSpec) { value.IdempotencyKey = "child-1" })
	mutate(EventChildStartUnknown, "unknown-due", func(value *HistoryEventSpec) { value.DueAt = now })
	mutate(EventChildStartUnknown, "unknown-code", func(value *HistoryEventSpec) { value.Code = " spaces " })
	mutate(EventChildStartUnknown, "unknown-retryable", func(value *HistoryEventSpec) { value.Retryable = true })
	mutate(EventChildStartRetryScheduled, "retry-key", func(value *HistoryEventSpec) { value.IdempotencyKey = "child-1" })
	mutate(EventChildStartRetryScheduled, "retry-due", func(value *HistoryEventSpec) { value.DueAt = now })
	mutate(EventChildStartRetryScheduled, "retry-code", func(value *HistoryEventSpec) { value.Code = "code" })
	mutate(EventChildStartRetryScheduled, "retry-retryable", func(value *HistoryEventSpec) { value.Retryable = true })
	for _, kind := range []EventKind{EventChildCompleted, EventChildFailed} {
		mutate(kind, "terminal-attempt", func(value *HistoryEventSpec) { value.Attempt = 1 })
		mutate(kind, "terminal-key", func(value *HistoryEventSpec) { value.IdempotencyKey = "child-1" })
		mutate(kind, "terminal-due", func(value *HistoryEventSpec) { value.DueAt = now })
		mutate(kind, "terminal-retryable", func(value *HistoryEventSpec) { value.Retryable = true })
	}
	mutate(EventChildCompleted, "completed-code", func(value *HistoryEventSpec) { value.Code = "failed" })
	mutate(EventChildFailed, "failed-terminal-code", func(value *HistoryEventSpec) { value.Code = " spaces " })
}

func TestChildStartHistoryAndBuildersRejectIncoherentBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 18, 0, 0, 0, time.UTC)
	parent, child, registry := internalChildRetryDefinitions(t)
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
		t.Fatalf("apply schedule: %v", err)
	}
	scheduled.sequence = 2
	scheduled.updatedAt = now.Add(time.Second)
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: schedule.Work()[0], Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("lease child: %v", err)
	}
	validStart := ChildStartAttemptSpec{
		TransitionID: "child-start", Lease: lease, Instance: scheduled,
		Definition: parent, StartedAt: now.Add(2 * time.Second),
	}
	start, err := NewChildStartAttempt(validStart)
	if err != nil {
		t.Fatalf("start child: %v", err)
	}
	running := scheduled
	running.children = map[string]ChildProgress{"child": cloneChildProgress(scheduled.children["child"])}
	if err := running.applyChild(registry, start.Events()[0]); err != nil {
		t.Fatalf("apply start: %v", err)
	}
	running.sequence = 3
	running.updatedAt = now.Add(2 * time.Second)
	startedOutcome, _ := NewChildStartOutcome(ChildStartOutcomeSpec{Kind: ChildStarted})
	validOutcome := ChildStartAttemptOutcomeSpec{
		TransitionID: "child-started", Instance: running, Definition: parent,
		StepName: "child", ChildID: "child-1", Attempt: 1,
		OccurredAt: now.Add(3 * time.Second), Outcome: startedOutcome,
	}
	started, err := NewChildStartAttemptOutcome(validOutcome)
	if err != nil {
		t.Fatalf("complete child start: %v", err)
	}
	active := running
	active.children = map[string]ChildProgress{"child": cloneChildProgress(running.children["child"])}
	if err := active.applyChild(registry, started.Events()[0]); err != nil {
		t.Fatalf("apply child started: %v", err)
	}
	active.sequence = 4
	active.updatedAt = now.Add(3 * time.Second)
	if progress, _ := active.Child("child"); progress.Status() != ChildActive ||
		progress.Attempt() != 1 || progress.IdempotencyKey() != "child-1" ||
		progress.DueAt() != (time.Time{}) || progress.Retryable() {
		t.Fatalf("active child = %#v", progress)
	}

	invalidStarts := []ChildStartAttemptSpec{
		func() ChildStartAttemptSpec { value := validStart; value.Lease = WorkLease{}; return value }(),
		func() ChildStartAttemptSpec { value := validStart; value.StartedAt = now; return value }(),
		func() ChildStartAttemptSpec {
			value := validStart
			value.StartedAt = now.Add(time.Hour)
			return value
		}(),
		func() ChildStartAttemptSpec {
			value := validStart
			value.StartedAt = now.Add(90 * time.Second)
			return value
		}(),
		func() ChildStartAttemptSpec { value := validStart; value.TransitionID = ""; return value }(),
		func() ChildStartAttemptSpec {
			value := validStart
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
	}
	for index, spec := range invalidStarts {
		if _, err := NewChildStartAttempt(spec); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("invalid start %d error = %v", index, err)
		}
	}
	invalidOutcomes := []ChildStartAttemptOutcomeSpec{
		func() ChildStartAttemptOutcomeSpec { value := validOutcome; value.Instance = scheduled; return value }(),
		func() ChildStartAttemptOutcomeSpec {
			value := validOutcome
			value.Instance.status = StatusPaused
			return value
		}(),
		func() ChildStartAttemptOutcomeSpec {
			value := validOutcome
			value.Instance.definition = child.Reference()
			return value
		}(),
		func() ChildStartAttemptOutcomeSpec {
			value := validOutcome
			progress := cloneChildProgress(value.Instance.children["child"])
			progress.status = ChildActive
			value.Instance.children = map[string]ChildProgress{"child": progress}
			return value
		}(),
		func() ChildStartAttemptOutcomeSpec {
			value := validOutcome
			value.ChildID = "other-child"
			return value
		}(),
		func() ChildStartAttemptOutcomeSpec { value := validOutcome; value.Attempt = 2; return value }(),
		func() ChildStartAttemptOutcomeSpec {
			value := validOutcome
			value.Outcome = ChildStartOutcome{}
			return value
		}(),
		func() ChildStartAttemptOutcomeSpec { value := validOutcome; value.OccurredAt = now; return value }(),
		func() ChildStartAttemptOutcomeSpec { value := validOutcome; value.TransitionID = ""; return value }(),
		func() ChildStartAttemptOutcomeSpec {
			value := validOutcome
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
	}
	for index, spec := range invalidOutcomes {
		if _, err := NewChildStartAttemptOutcome(spec); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("invalid start outcome %d error = %v", index, err)
		}
	}

	failedOutcome, _ := NewChildStartOutcome(ChildStartOutcomeSpec{
		Kind: ChildStartFailed, Code: "temporary", Retryable: true,
	})
	failedTransition, err := NewChildStartAttemptOutcome(ChildStartAttemptOutcomeSpec{
		TransitionID: "child-failed", Instance: running, Definition: parent,
		StepName: "child", ChildID: "child-1", Attempt: 1,
		OccurredAt: now.Add(3 * time.Second), Outcome: failedOutcome,
	})
	if err != nil {
		t.Fatalf("fail child start: %v", err)
	}
	failed := running
	failed.children = map[string]ChildProgress{"child": cloneChildProgress(running.children["child"])}
	if err := failed.applyChild(registry, failedTransition.Events()[0]); err != nil {
		t.Fatalf("apply child failure: %v", err)
	}
	failed.sequence = 4
	failed.updatedAt = now.Add(3 * time.Second)
	validRetry := ChildStartRetrySpec{
		TransitionID: "child-retry", WorkID: "child-retry-work", Instance: failed,
		Definition: parent, StepName: "child", ScheduledAt: now.Add(4 * time.Second),
		Deadline: now.Add(time.Hour),
	}
	retry, err := NewChildStartRetry(validRetry)
	if err != nil {
		t.Fatalf("retry child start: %v", err)
	}
	waiting := failed
	waiting.children = map[string]ChildProgress{"child": cloneChildProgress(failed.children["child"])}
	if err := waiting.applyChild(registry, retry.Events()[0]); err != nil {
		t.Fatalf("apply child retry: %v", err)
	}
	waiting.sequence = 5
	waiting.updatedAt = now.Add(4 * time.Second)
	if progress, _ := waiting.Child("child"); progress.Status() != ChildStartRetryWaiting ||
		progress.DueAt() != now.Add(5*time.Second) {
		t.Fatalf("waiting child = %#v", progress)
	}
	invalidRetries := []ChildStartRetrySpec{
		func() ChildStartRetrySpec { value := validRetry; value.Instance = running; return value }(),
		func() ChildStartRetrySpec {
			value := validRetry
			value.Instance.status = StatusPaused
			return value
		}(),
		func() ChildStartRetrySpec {
			value := validRetry
			value.Instance.definition = child.Reference()
			return value
		}(),
		func() ChildStartRetrySpec {
			value := validRetry
			progress := cloneChildProgress(value.Instance.children["child"])
			progress.retryable = false
			value.Instance.children = map[string]ChildProgress{"child": progress}
			return value
		}(),
		func() ChildStartRetrySpec {
			value := validRetry
			progress := cloneChildProgress(value.Instance.children["child"])
			progress.attempt = 2
			value.Instance.children = map[string]ChildProgress{"child": progress}
			return value
		}(),
		func() ChildStartRetrySpec { value := validRetry; value.ScheduledAt = now; return value }(),
		func() ChildStartRetrySpec { value := validRetry; value.WorkID = ""; return value }(),
		func() ChildStartRetrySpec { value := validRetry; value.TransitionID = ""; return value }(),
		func() ChildStartRetrySpec {
			value := validRetry
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
	}
	for index, spec := range invalidRetries {
		if _, err := NewChildStartRetry(spec); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("invalid retry %d error = %v", index, err)
		}
	}

	unknownOutcome, _ := NewChildStartOutcome(ChildStartOutcomeSpec{
		Kind: ChildStartUnknown, Code: "uncertain",
	})
	unknownTransition, err := NewChildStartAttemptOutcome(ChildStartAttemptOutcomeSpec{
		TransitionID: "child-unknown", Instance: running, Definition: parent,
		StepName: "child", ChildID: "child-1", Attempt: 1,
		OccurredAt: now.Add(3 * time.Second), Outcome: unknownOutcome,
	})
	if err != nil {
		t.Fatalf("unknown child start: %v", err)
	}
	unknown := running
	unknown.children = map[string]ChildProgress{"child": cloneChildProgress(running.children["child"])}
	if err := unknown.applyChild(registry, unknownTransition.Events()[0]); err != nil {
		t.Fatalf("apply child unknown: %v", err)
	}
	unknown.sequence = 4
	unknown.updatedAt = now.Add(3 * time.Second)
	if progress, _ := unknown.Child("child"); progress.Status() != ChildStartUnknownStatus ||
		progress.Code() != "uncertain" || progress.Retryable() {
		t.Fatalf("unknown child = %#v", progress)
	}
	for name, observed := range map[string]Instance{"running": running, "unknown": unknown} {
		terminal, err := NewChildOutcome(ChildOutcomeSpec{
			TransitionID: "observed-" + name, Instance: observed, Definition: parent,
			StepName: "child", ChildID: "child-1", CompletedAt: now.Add(4 * time.Second),
			Result: []byte("done"),
		})
		if err != nil {
			t.Fatalf("observe %s child terminal outcome: %v", name, err)
		}
		resolved := observed
		resolved.children = map[string]ChildProgress{"child": cloneChildProgress(observed.children["child"])}
		if err := resolved.applyChild(registry, terminal.Events()[0]); err != nil {
			t.Fatalf("apply %s child terminal outcome: %v", name, err)
		}
		progress, _ := resolved.Child("child")
		if progress.Status() != ChildSucceeded || string(progress.Result()) != "done" {
			t.Fatalf("resolved %s child = %#v", name, progress)
		}
	}

	if validChildEventFields(HistoryEventSpec{Kind: EventChildStarted, StepName: " spaces "}) {
		t.Fatal("invalid child start step fields accepted")
	}
	invalidHistory := []struct {
		instance Instance
		event    HistoryEvent
	}{
		{instance: active, event: start.Events()[0]},
		{instance: scheduled, event: started.Events()[0]},
		{instance: scheduled, event: failedTransition.Events()[0]},
		{instance: scheduled, event: unknownTransition.Events()[0]},
		{instance: running, event: retry.Events()[0]},
	}
	for index, test := range invalidHistory {
		value := test.instance
		value.children = map[string]ChildProgress{"child": cloneChildProgress(test.instance.children["child"])}
		if err := value.applyChild(registry, test.event); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid start history %d error = %v", index, err)
		}
	}
	assertReplayInvalid := func(name string, instance Instance, event HistoryEvent) {
		t.Helper()
		instance.children = map[string]ChildProgress{"child": cloneChildProgress(instance.children["child"])}
		if err := instance.applyChild(registry, event); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid child replay %s error = %v", name, err)
		}
	}
	mutateEvent := func(event HistoryEvent, change func(*HistoryEvent)) HistoryEvent {
		t.Helper()
		change(&event)
		return event
	}
	assertReplayInvalid("start-attempt-gap", scheduled, mutateEvent(start.Events()[0], func(event *HistoryEvent) {
		event.attempt = 2
	}))
	exhaustedSchedule := scheduled
	exhaustedSchedule.children = map[string]ChildProgress{"child": cloneChildProgress(scheduled.children["child"])}
	exhausted := exhaustedSchedule.children["child"]
	exhausted.attempt = 2
	exhaustedSchedule.children["child"] = exhausted
	assertReplayInvalid("start-attempt-exhausted", exhaustedSchedule, mutateEvent(start.Events()[0], func(event *HistoryEvent) {
		event.attempt = 3
	}))
	assertReplayInvalid("start-deadline", scheduled, mutateEvent(start.Events()[0], func(event *HistoryEvent) {
		event.dueAt = event.dueAt.Add(time.Second)
	}))
	earlyWaiting := waiting
	earlyWaiting.children = map[string]ChildProgress{"child": cloneChildProgress(waiting.children["child"])}
	assertReplayInvalid("start-before-retry", earlyWaiting, mutateEvent(start.Events()[0], func(event *HistoryEvent) {
		event.attempt = 2
		event.occurredAt = earlyWaiting.children["child"].dueAt.Add(-time.Nanosecond)
		event.dueAt = event.occurredAt.Add(time.Minute)
	}))
	assertReplayInvalid("started-attempt", running, mutateEvent(started.Events()[0], func(event *HistoryEvent) {
		event.attempt = 2
	}))
	assertReplayInvalid("failed-attempt", running, mutateEvent(failedTransition.Events()[0], func(event *HistoryEvent) {
		event.attempt = 2
	}))
	assertReplayInvalid("unknown-attempt", running, mutateEvent(unknownTransition.Events()[0], func(event *HistoryEvent) {
		event.attempt = 2
	}))
	notRetryable := failed
	notRetryable.children = map[string]ChildProgress{"child": cloneChildProgress(failed.children["child"])}
	notRetryableProgress := notRetryable.children["child"]
	notRetryableProgress.retryable = false
	notRetryable.children["child"] = notRetryableProgress
	assertReplayInvalid("retry-not-retryable", notRetryable, retry.Events()[0])
	exhaustedFailure := failed
	exhaustedFailure.children = map[string]ChildProgress{"child": cloneChildProgress(failed.children["child"])}
	exhaustedProgress := exhaustedFailure.children["child"]
	exhaustedProgress.attempt = 2
	exhaustedFailure.children["child"] = exhaustedProgress
	assertReplayInvalid("retry-exhausted", exhaustedFailure, mutateEvent(retry.Events()[0], func(event *HistoryEvent) {
		event.attempt = 2
		event.dueAt = event.occurredAt.Add(retryDelay(parent.Steps()[0].Retry, 2))
	}))
	assertReplayInvalid("retry-attempt", failed, mutateEvent(retry.Events()[0], func(event *HistoryEvent) {
		event.attempt = 2
		event.dueAt = event.occurredAt.Add(retryDelay(parent.Steps()[0].Retry, 2))
	}))
	assertReplayInvalid("retry-due", failed, mutateEvent(retry.Events()[0], func(event *HistoryEvent) {
		event.dueAt = event.dueAt.Add(time.Second)
	}))
	terminalFromFailedStart := failed
	assertReplayInvalid("terminal-state", terminalFromFailedStart, mustInternalHistoryEvent(t, HistoryEventSpec{
		Sequence: 5, InstanceID: "parent-1", Kind: EventChildCompleted,
		OccurredAt: now.Add(4 * time.Second), SuccessorID: "child-1", StepName: "child",
	}))
	terminalTooLarge := started.Events()[0]
	terminalTooLarge.kind = EventChildCompleted
	terminalTooLarge.attempt = 0
	terminalTooLarge.data = make([]byte, 9)
	assertReplayInvalid("terminal-result-limit", active, terminalTooLarge)
	maxAttemptSchedule := scheduled
	maxAttemptSchedule.children = map[string]ChildProgress{"child": cloneChildProgress(scheduled.children["child"])}
	maxAttemptProgress := maxAttemptSchedule.children["child"]
	maxAttemptProgress.attempt = 1
	maxAttemptProgress.status = ChildStartRetryWaiting
	maxAttemptProgress.dueAt = now.Add(2 * time.Second)
	maxAttemptSchedule.children["child"] = maxAttemptProgress
	maxAttemptEvent := start.Events()[0]
	maxAttemptEvent.attempt = 2
	if err := maxAttemptSchedule.applyChild(registry, maxAttemptEvent); err != nil {
		t.Fatalf("maximum child start attempt rejected: %v", err)
	}
	maxResultEvent := mustInternalHistoryEvent(t, HistoryEventSpec{
		Sequence: 5, InstanceID: "parent-1", Kind: EventChildCompleted,
		OccurredAt: now.Add(4 * time.Second), SuccessorID: "child-1", StepName: "child",
		Data: make([]byte, 8),
	})
	maxResultInstance := active
	maxResultInstance.children = map[string]ChildProgress{"child": cloneChildProgress(active.children["child"])}
	if err := maxResultInstance.applyChild(registry, maxResultEvent); err != nil {
		t.Fatalf("maximum child terminal result rejected: %v", err)
	}
	_ = child
}

func TestChildStartAttemptValidatesEachFencedBoundary(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 21, 0, 0, 0, time.UTC)
	parent, child, _ := internalChildRetryDefinitions(t)
	scheduled := internalChildInstance(parent, now)
	scheduled.sequence = 2
	scheduled.updatedAt = now.Add(time.Second)
	scheduled.children["child"] = ChildProgress{
		stepName: "child", childID: "child-1", definition: child.Reference(),
		status: ChildScheduled,
	}
	makeLease := func(
		kind WorkKind,
		instanceID string,
		sequence uint64,
		payload []byte,
		availableAt, deadline, claimedAt, expiresAt time.Time,
	) WorkLease {
		t.Helper()
		work, err := NewPendingWork(PendingWorkSpec{
			ID: "child-boundary-work", Kind: kind, InstanceID: instanceID, Sequence: sequence,
			AvailableAt: availableAt, Deadline: deadline, Payload: payload,
		})
		if err != nil {
			t.Fatalf("construct boundary work: %v", err)
		}
		lease, err := NewWorkLease(WorkLeaseSpec{
			Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
			ClaimedAt: claimedAt, ExpiresAt: expiresAt,
		})
		if err != nil {
			t.Fatalf("construct boundary lease: %v", err)
		}
		return lease
	}
	payload := encodeChildDispatch("child", "child-1", child.Reference(), 1, "child-1")
	validLease := makeLease(
		WorkChild, "parent-1", 2, payload, now.Add(time.Second), now.Add(time.Hour),
		now.Add(2*time.Second), now.Add(30*time.Second),
	)
	valid := ChildStartAttemptSpec{
		TransitionID: "child-boundary-start", Lease: validLease, Instance: scheduled,
		Definition: parent, StartedAt: now.Add(2 * time.Second),
	}
	if _, err := NewChildStartAttempt(valid); err != nil {
		t.Fatalf("valid child start boundary: %v", err)
	}
	assertInvalid := func(name string, spec ChildStartAttemptSpec) {
		t.Helper()
		if _, err := NewChildStartAttempt(spec); !errors.Is(err, ErrInvalidChildTransition) {
			t.Fatalf("invalid child start boundary %s error = %v", name, err)
		}
	}
	value := valid
	value.Lease = makeLease(
		WorkActivity, "parent-1", 2, payload, now.Add(time.Second), now.Add(time.Hour),
		now.Add(2*time.Second), now.Add(30*time.Second),
	)
	assertInvalid("work-kind", value)
	value = valid
	value.Lease = makeLease(
		WorkChild, "other-parent", 2, payload, now.Add(time.Second), now.Add(time.Hour),
		now.Add(2*time.Second), now.Add(30*time.Second),
	)
	assertInvalid("instance-id", value)
	value = valid
	value.Instance.status = StatusPaused
	assertInvalid("instance-status", value)
	value = valid
	value.Instance.definition = child.Reference()
	assertInvalid("parent-definition", value)
	value = valid
	value.Instance.sequence = 1
	assertInvalid("history-behind-work", value)
	value = valid
	progress := value.Instance.children["child"]
	progress.childID = "other-child"
	value.Instance.children = map[string]ChildProgress{"child": progress}
	assertInvalid("child-id", value)
	value = valid
	progress = value.Instance.children["child"]
	progress.definition = parent.Reference()
	value.Instance.children = map[string]ChildProgress{"child": progress}
	assertInvalid("child-definition", value)
	value = valid
	progress = value.Instance.children["child"]
	progress.status = ChildActive
	value.Instance.children = map[string]ChildProgress{"child": progress}
	assertInvalid("progress-status", value)
	value = valid
	value.Lease = makeLease(
		WorkChild, "parent-1", 2,
		encodeChildDispatch("child", "child-1", child.Reference(), 2, "child-2"),
		now.Add(time.Second), now.Add(time.Hour), now.Add(2*time.Second), now.Add(30*time.Second),
	)
	assertInvalid("attempt-gap", value)
	value = valid
	progress = value.Instance.children["child"]
	progress.attempt = 2
	value.Instance.children = map[string]ChildProgress{"child": progress}
	value.Lease = makeLease(
		WorkChild, "parent-1", 2,
		encodeChildDispatch("child", "child-1", child.Reference(), 3, "child-3"),
		now.Add(time.Second), now.Add(time.Hour), now.Add(2*time.Second), now.Add(30*time.Second),
	)
	assertInvalid("attempt-limit", value)
	value = valid
	value.Lease = makeLease(
		WorkChild, "parent-1", 2, payload, now.Add(3*time.Second), now.Add(time.Hour),
		now.Add(2*time.Second), now.Add(30*time.Second),
	)
	assertInvalid("before-available", value)
	value = valid
	value.Lease = makeLease(
		WorkChild, "parent-1", 2, payload, now.Add(time.Second), now.Add(30*time.Second),
		now.Add(2*time.Second), now.Add(20*time.Second),
	)
	assertInvalid("due-after-deadline", value)
	value = valid
	value.Lease = makeLease(
		WorkChild, "parent-1", 2, payload, now.Add(time.Second), now.Add(time.Hour),
		now.Add(3*time.Second), now.Add(30*time.Second),
	)
	assertInvalid("before-claim", value)
	value = valid
	value.StartedAt = now.Add(30 * time.Second)
	assertInvalid("at-lease-expiry", value)
	value = valid
	value.Instance.updatedAt = now.Add(3 * time.Second)
	assertInvalid("before-instance-update", value)
	value = valid
	progress = value.Instance.children["child"]
	progress.status = ChildStartRetryWaiting
	progress.attempt = 1
	progress.dueAt = now.Add(3 * time.Second)
	value.Instance.children = map[string]ChildProgress{"child": progress}
	value.Lease = makeLease(
		WorkChild, "parent-1", 2,
		encodeChildDispatch("child", "child-1", child.Reference(), 2, "child-2"),
		now.Add(time.Second), now.Add(time.Hour), now.Add(2*time.Second), now.Add(30*time.Second),
	)
	assertInvalid("before-retry-due", value)
	value.StartedAt = now.Add(3 * time.Second)
	if _, err := NewChildStartAttempt(value); err != nil {
		t.Fatalf("retry at due boundary: %v", err)
	}
	value = valid
	value.Lease = makeLease(
		WorkChild, "parent-1", 2, payload, now.Add(time.Second), now.Add(62*time.Second),
		now.Add(2*time.Second), now.Add(30*time.Second),
	)
	if _, err := NewChildStartAttempt(value); err != nil {
		t.Fatalf("attempt deadline equality boundary: %v", err)
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

func internalChildRetryDefinitions(t *testing.T) (Definition, Definition, *Registry) {
	t.Helper()
	child := mustInternalDefinition(t, "child", "1")
	parent, err := NewDefinition(DefinitionSpec{
		Name: "parent", Version: "retry", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "child", Kind: StepChild, Target: "child", ChildDefinition: child.Reference(),
			Timeout: time.Minute, InputLimit: 8, ResultLimit: 8,
			Retry: RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct retry parent: %v", err)
	}
	registry, err := CompileDefinitions(parent, child)
	if err != nil {
		t.Fatalf("compile retry child definitions: %v", err)
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
