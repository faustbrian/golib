package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestOrchestrationSchedulesBoundedParallelActivitiesAndJoinsTheirOutcomes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "parallel-v1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{
			{Name: "parallel", Kind: workflow.StepParallel, FanOutLimit: 2, Branches: []string{"reserve", "charge"}},
			{Name: "reserve", Kind: workflow.StepActivity, Target: "inventory.reserve", Timeout: 30 * time.Second, InputLimit: 64, ResultLimit: 64, Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second}},
			{Name: "charge", Kind: workflow.StepActivity, Target: "payments.charge", Timeout: 30 * time.Second, InputLimit: 64, ResultLimit: 64, Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second}},
			{Name: "join", Kind: workflow.StepJoin, FanOutLimit: 2, Branches: []string{"reserve", "charge"}},
			{Name: "confirm", Kind: workflow.StepSignal, Target: "orders.confirm", Timeout: time.Minute, InputLimit: 64},
		},
	})
	if err != nil {
		t.Fatalf("construct parallel definition: %v", err)
	}
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	history := []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})}
	instance := replaySequential(t, registry, history)
	decision, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "parallel-schedule", Instance: instance, Definition: definition,
		DecidedAt: now.Add(time.Second), Deadline: now.Add(time.Hour),
		Branches: []workflow.OrchestrationBranchSpec{
			{StepName: "reserve", WorkID: "reserve-work", IdempotencyKey: "reserve-key", Input: []byte("order-1")},
			{StepName: "charge", WorkID: "charge-work", IdempotencyKey: "charge-key", Input: []byte("order-1")},
		},
	})
	if err != nil {
		t.Fatalf("plan parallel branches: %v", err)
	}
	transition := decision.Transition()
	if decision.Kind() != workflow.OrchestrationScheduled || decision.StepName() != "parallel" ||
		len(transition.Events()) != 2 || len(transition.Work()) != 2 ||
		transition.Events()[0].StepName() != "reserve" || transition.Events()[1].StepName() != "charge" {
		t.Fatalf("parallel decision = %#v", decision)
	}
	history = append(history, transition.Events()...)
	history = append(history,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "reserve-key", DueAt: now.Add(32 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now.Add(3 * time.Second), StepName: "reserve", Attempt: 1}),
	)
	instance = replaySequential(t, registry, history)
	waiting, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{Instance: instance, Definition: definition})
	if err != nil || waiting.Kind() != workflow.OrchestrationWaiting || waiting.StepName() != "parallel" {
		t.Fatalf("partial join = %#v, %v", waiting, err)
	}
	history = append(history,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(4 * time.Second), StepName: "charge", Attempt: 1, IdempotencyKey: "charge-key", DueAt: now.Add(34 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now.Add(5 * time.Second), StepName: "charge", Attempt: 1}),
	)
	instance = replaySequential(t, registry, history)
	joined, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{Instance: instance, Definition: definition})
	if err != nil || joined.Kind() != workflow.OrchestrationWaiting || joined.StepName() != "confirm" {
		t.Fatalf("joined decision = %#v, %v", joined, err)
	}
}
