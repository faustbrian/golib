package workflow_test

import (
	"context"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestChildWorkProcessorPersistsStartBeforeCreatingPinnedChild(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 10, 0, 0, 0, time.UTC)
	parent, child, definitions, history, lease := childProcessorFixture(t, now)
	store := newProcessorStore(t, definitions, history)
	called := false
	starter := workflow.ChildStartFunc(func(_ context.Context, request workflow.ChildStartRequest) workflow.ChildStartOutcome {
		called = true
		if len(store.transitions) != 1 || store.transitions[0].Events()[0].Kind() != workflow.EventChildStartAttempted {
			t.Fatal("child creation observed before its start-attempt transition committed")
		}
		if request.ParentInstanceID() != "instance-1" || request.ParentDefinition() != parent.Reference() ||
			request.StepName() != "shipment" || request.ChildID() != "child-1" ||
			request.ChildDefinition() != child.Reference() || request.Attempt() != 1 ||
			request.IdempotencyKey() == "" || string(request.Input()) != "order-1" ||
			request.TenantID() != "tenant-1" || request.CorrelationID() != "correlation-1" {
			t.Fatalf("child start request = %#v", request)
		}
		outcome, err := workflow.NewChildStartOutcome(workflow.ChildStartOutcomeSpec{Kind: workflow.ChildStarted})
		if err != nil {
			t.Fatalf("construct child outcome: %v", err)
		}
		return outcome
	})
	processor, err := workflow.NewChildWorkProcessor(workflow.ChildWorkProcessorConfig{
		Store: store, Definitions: definitions, Starter: starter,
		Clock: fixedProcessorClock{now: now.Add(2 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct child processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), lease)
	if err != nil {
		t.Fatalf("process child: %v", err)
	}
	if !called || decision.Kind() != workflow.WorkComplete || len(store.transitions) != 2 ||
		store.transitions[1].Events()[0].Kind() != workflow.EventChildStarted {
		t.Fatalf("decision = %#v transitions = %#v called = %t", decision, store.transitions, called)
	}
}

func TestChildWorkProcessorDoesNotRepeatInFlightUnknownStart(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 11, 0, 0, 0, time.UTC)
	_, _, definitions, history, lease := childProcessorFixture(t, now)
	history = append(history, mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventChildStartAttempted,
		OccurredAt: now.Add(2 * time.Second), StepName: "shipment", Attempt: 1,
		IdempotencyKey: "child-1", SuccessorID: "child-1", DueAt: now.Add(62 * time.Second),
	}))
	store := newProcessorStore(t, definitions, history)
	processor, err := workflow.NewChildWorkProcessor(workflow.ChildWorkProcessorConfig{
		Store: store, Definitions: definitions,
		Starter: workflow.ChildStartFunc(func(context.Context, workflow.ChildStartRequest) workflow.ChildStartOutcome {
			t.Fatal("redelivery repeated a child start with an unknown outcome")
			return workflow.ChildStartOutcome{}
		}),
		Clock: fixedProcessorClock{now: now.Add(3 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct child processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), lease)
	if err != nil {
		t.Fatalf("process redelivery: %v", err)
	}
	if decision.Kind() != workflow.WorkComplete || len(store.transitions) != 1 ||
		store.transitions[0].Events()[0].Kind() != workflow.EventChildStartUnknown ||
		store.transitions[0].Events()[0].Code() != "child-start-outcome-unknown" {
		t.Fatalf("decision = %#v transitions = %#v", decision, store.transitions)
	}
}

func childProcessorFixture(t *testing.T, now time.Time) (workflow.Definition, workflow.Definition, *workflow.Registry, []workflow.HistoryEvent, workflow.WorkLease) {
	t.Helper()
	child := mustDefinition(t, "shipment", "3")
	parent, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "order", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "shipment", Kind: workflow.StepChild, Target: "shipment", ChildDefinition: child.Reference(),
			Timeout: time.Minute, InputLimit: 16, ResultLimit: 16,
			Retry: workflow.RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct parent: %v", err)
	}
	definitions, err := workflow.CompileDefinitions(parent, child)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	started := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: parent.Reference(),
	})
	base := replaySequential(t, definitions, []workflow.HistoryEvent{started})
	schedule, err := workflow.NewChildSchedule(workflow.ChildScheduleSpec{
		TransitionID: "schedule-child", WorkID: "child-work", ChildID: "child-1",
		Instance: base, Definition: parent, StepName: "shipment", ScheduledAt: now.Add(time.Second),
		Deadline: now.Add(time.Hour), Input: []byte("order-1"), TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("schedule child: %v", err)
	}
	history := append([]workflow.HistoryEvent{started}, schedule.Events()...)
	work := schedule.Work()[0]
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("lease child: %v", err)
	}
	return parent, child, definitions, history, lease
}
