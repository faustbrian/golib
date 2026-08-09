package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestChildWorkflowScheduleAndOutcomeRemainVersionPinned(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 6, 0, 0, 0, time.UTC)
	child := mustDefinition(t, "shipment", "3")
	parent, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "order", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "shipment", Kind: workflow.StepChild, Target: "shipment",
			ChildDefinition: child.Reference(), Timeout: time.Minute,
			InputLimit: 16, ResultLimit: 16,
			Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct parent definition: %v", err)
	}
	if _, err := workflow.CompileDefinitions(parent); !errors.Is(err, workflow.ErrDefinitionNotFound) {
		t.Fatalf("unregistered child definition error = %v", err)
	}
	incompatibleChild, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "shipment", Version: "3", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "different", Kind: workflow.StepActivity, Target: "different.run",
			Timeout: time.Second, InputLimit: 1, ResultLimit: 1,
			Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct incompatible child: %v", err)
	}
	if _, err := workflow.CompileDefinitions(parent, incompatibleChild); !errors.Is(err, workflow.ErrDefinitionMismatch) {
		t.Fatalf("incompatible child definition error = %v", err)
	}
	registry, err := workflow.CompileDefinitions(parent, child)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	started := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "parent-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: parent.Reference(),
	})
	instance := replaySequential(t, registry, []workflow.HistoryEvent{started})
	decision, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "child-schedule", WorkID: "child-work", ChildID: "child-1",
		Instance: instance, Definition: parent,
		DecidedAt: now.Add(time.Second), Deadline: now.Add(time.Hour), Input: []byte("order-1"),
	})
	if err != nil {
		t.Fatalf("schedule child: %v", err)
	}
	if decision.Kind() != workflow.OrchestrationScheduled || decision.StepName() != "shipment" {
		t.Fatalf("child decision = %#v", decision)
	}
	scheduled := decision.Transition()
	events := scheduled.Events()
	work := scheduled.Work()
	if len(events) != 1 || events[0].Kind() != workflow.EventChildScheduled ||
		events[0].Definition() != child.Reference() || events[0].SuccessorID() != "child-1" ||
		len(work) != 1 || work[0].Kind() != workflow.WorkChild {
		t.Fatalf("child schedule = events %#v work %#v", events, work)
	}
	dispatch, err := workflow.DecodeChildDispatch(work[0].Payload())
	if err != nil || dispatch.StepName() != "shipment" || dispatch.ChildID() != "child-1" ||
		dispatch.Definition() != child.Reference() {
		t.Fatalf("child dispatch = %#v, error %v", dispatch, err)
	}
	instance = replaySequential(t, registry, append([]workflow.HistoryEvent{started}, events...))
	progress, ok := instance.Child("shipment")
	if !ok || progress.StepName() != "shipment" || progress.Status() != workflow.ChildScheduled ||
		progress.ChildID() != "child-1" || progress.Code() != "" ||
		progress.Definition() != child.Reference() || string(progress.Input()) != "order-1" {
		t.Fatalf("scheduled child = %#v, exists %t", progress, ok)
	}
	if children := instance.Children(); len(children) != 1 || children[0].ChildID() != "child-1" || instance.SnapshotDigest() == "" {
		t.Fatalf("children = %#v", children)
	}
	waiting, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{Instance: instance, Definition: parent})
	if err != nil || waiting.Kind() != workflow.OrchestrationWaiting || waiting.StepName() != "shipment" {
		t.Fatalf("scheduled child decision = %#v, error %v", waiting, err)
	}
	completed, err := workflow.NewChildOutcome(workflow.ChildOutcomeSpec{
		TransitionID: "child-complete", Instance: instance, Definition: parent,
		StepName: "shipment", ChildID: "child-1", CompletedAt: now.Add(2 * time.Second),
		Result: []byte("shipped"),
	})
	if err != nil {
		t.Fatalf("complete child: %v", err)
	}
	instance = replaySequential(t, registry, append(append([]workflow.HistoryEvent{started}, events...), completed.Events()...))
	progress, ok = instance.Child("shipment")
	if !ok || progress.Status() != workflow.ChildSucceeded || string(progress.Result()) != "shipped" {
		t.Fatalf("completed child = %#v, exists %t", progress, ok)
	}
	completedDecision, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "parent-complete", Instance: instance, Definition: parent,
		DecidedAt: now.Add(3 * time.Second), Result: []byte("done"),
	})
	if err != nil || completedDecision.Kind() != workflow.OrchestrationCompleted {
		t.Fatalf("completed parent decision = %#v, error %v", completedDecision, err)
	}
}
