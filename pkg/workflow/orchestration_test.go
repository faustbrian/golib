package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestOrchestrationDecisionAdvancesPersistedSequentialSteps(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 19, 0, 0, 0, time.UTC)
	definition := mustSequentialDefinition(t, workflow.Orchestration)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	history := []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})}
	instance := replaySequential(t, registry, history)

	activity, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "orchestration-activity", WorkID: "orchestration-activity-work",
		Instance: instance, Definition: definition, DecidedAt: now.Add(time.Second),
		Deadline: now.Add(time.Hour), IdempotencyKey: "activity-key-1", Input: []byte("order-1"),
	})
	if err != nil {
		t.Fatalf("plan activity: %v", err)
	}
	if activity.Kind() != workflow.OrchestrationScheduled || activity.StepName() != "execute" ||
		activity.Transition().Events()[0].Kind() != workflow.EventActivityScheduled {
		t.Fatalf("activity decision = %#v", activity)
	}
	history = append(history, activity.Transition().Events()...)
	history = append(history,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "activity-key-1", DueAt: now.Add(32 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1}),
	)
	instance = replaySequential(t, registry, history)

	timer, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "orchestration-timer", WorkID: "orchestration-timer-work",
		Instance: instance, Definition: definition, DecidedAt: now.Add(4 * time.Second),
		Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("plan timer: %v", err)
	}
	if timer.Kind() != workflow.OrchestrationScheduled || timer.StepName() != "delay" ||
		timer.Transition().Events()[0].Kind() != workflow.EventTimerScheduled {
		t.Fatalf("timer decision = %#v", timer)
	}
	history = append(history, timer.Transition().Events()...)
	instance = replaySequential(t, registry, history)
	waiting, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		Instance: instance, Definition: definition,
	})
	if err != nil || waiting.Kind() != workflow.OrchestrationWaiting || waiting.StepName() != "delay" || waiting.Transition().Valid() {
		t.Fatalf("waiting decision = %#v, %v", waiting, err)
	}
	history = append(history, mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
		OccurredAt: now.Add(10 * time.Second), StepName: "delay",
	}))
	instance = replaySequential(t, registry, history)

	signalWait, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		Instance: instance, Definition: definition,
	})
	if err != nil || signalWait.Kind() != workflow.OrchestrationWaiting || signalWait.StepName() != "confirm" {
		t.Fatalf("signal wait = %#v, %v", signalWait, err)
	}
	signal, err := workflow.NewSignalAcceptance(workflow.SignalAcceptanceSpec{
		InstanceID: "instance-1", ExpectedSequence: 6, Definition: definition,
		StepName: "confirm", SignalID: "signal-1", ReceivedAt: now.Add(11 * time.Second),
	})
	if err != nil {
		t.Fatalf("accept signal: %v", err)
	}
	history = append(history, signal.Events()...)
	instance = replaySequential(t, registry, history)

	completed, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "orchestration-complete", Instance: instance, Definition: definition,
		DecidedAt: now.Add(12 * time.Second), Result: []byte("complete"),
	})
	if err != nil {
		t.Fatalf("plan completion: %v", err)
	}
	if completed.Kind() != workflow.OrchestrationCompleted || completed.StepName() != "" ||
		completed.Transition().Events()[0].Kind() != workflow.EventInstanceCompleted {
		t.Fatalf("completion decision = %#v", completed)
	}
	history = append(history, completed.Transition().Events()...)
	if terminal := replaySequential(t, registry, history); terminal.Status() != workflow.StatusCompleted || string(terminal.Result()) != "complete" {
		t.Fatalf("terminal instance = %#v", terminal)
	}
}

func TestOrchestrationDecisionFailsKnownTerminalActivityFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 20, 0, 0, 0, time.UTC)
	definition := mustSequentialDefinition(t, workflow.Orchestration)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	history := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "execute"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "activity-key-1", DueAt: now.Add(32 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Code: "permanent"}),
	}
	instance := replaySequential(t, registry, history)
	decision, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "orchestration-fail", Instance: instance, Definition: definition,
		DecidedAt: now.Add(4 * time.Second), Result: []byte("permanent"),
	})
	if err != nil {
		t.Fatalf("plan failure: %v", err)
	}
	if decision.Kind() != workflow.OrchestrationFailed || decision.StepName() != "execute" ||
		decision.Transition().Events()[0].Kind() != workflow.EventInstanceFailed {
		t.Fatalf("failure decision = %#v", decision)
	}
}

func TestOrchestrationDecisionRejectsChoreographyDefinitions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	definition := mustSequentialDefinition(t, workflow.Choreography)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	instance := replaySequential(t, registry, []workflow.HistoryEvent{mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})})
	if _, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		Instance: instance, Definition: definition,
	}); !errors.Is(err, workflow.ErrInvalidOrchestration) {
		t.Fatalf("choreography orchestration error = %v", err)
	}
}

func TestOrchestrationApprovalRequiresAuditedOperatorAcceptance(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 22, 0, 0, 0, time.UTC)
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "payments", Version: "approval-v1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "approve", Kind: workflow.StepApproval, Target: "finance.approval",
			Timeout: time.Hour, InputLimit: 64,
		}},
	})
	if err != nil {
		t.Fatalf("construct approval definition: %v", err)
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
	waiting, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		Instance: instance, Definition: definition,
	})
	if err != nil || waiting.Kind() != workflow.OrchestrationWaiting || waiting.StepName() != "approve" {
		t.Fatalf("approval wait = %#v, %v", waiting, err)
	}
	approval, err := workflow.NewOperatorApproval(workflow.OperatorApprovalSpec{
		CommandID: "approval-command-1", Instance: instance, Definition: definition,
		StepName: "approve", Actor: "approver-1", Reason: "within-policy",
		OccurredAt: now.Add(time.Second), Payload: []byte("approved"),
	})
	if err != nil {
		t.Fatalf("construct approval: %v", err)
	}
	events := approval.Events()
	if len(events) != 2 || events[0].Kind() != workflow.EventOperatorCommandRecorded ||
		events[1].Kind() != workflow.EventSignalReceived {
		t.Fatalf("approval events = %#v", events)
	}
	history = append(history, events...)
	instance = replaySequential(t, registry, history)
	progress, ok := instance.Signal("approve")
	if !ok || progress.SignalID() != "approval-command-1" || string(progress.Payload()) != "approved" ||
		len(instance.OperatorActions()) != 1 || instance.OperatorActions()[0].Action() != workflow.OperatorApprove {
		t.Fatalf("approval progress = %#v actions = %#v", progress, instance.OperatorActions())
	}
	completed, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "approval-complete", Instance: instance, Definition: definition,
		DecidedAt: now.Add(2 * time.Second),
	})
	if err != nil || completed.Kind() != workflow.OrchestrationCompleted {
		t.Fatalf("approval completion = %#v, %v", completed, err)
	}
}

func mustSequentialDefinition(t *testing.T, mode workflow.ExecutionMode) workflow.Definition {
	t.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "sequential-v1", Mode: mode,
		Steps: []workflow.StepSpec{
			{Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute", Timeout: 30 * time.Second, InputLimit: 64, ResultLimit: 64, Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second}},
			{Name: "delay", Kind: workflow.StepTimer, Timeout: 5 * time.Second},
			{Name: "confirm", Kind: workflow.StepSignal, Target: "orders.confirm", Timeout: time.Minute, InputLimit: 64},
		},
	})
	if err != nil {
		t.Fatalf("construct sequential definition: %v", err)
	}
	return definition
}

func replaySequential(t *testing.T, registry *workflow.Registry, history []workflow.HistoryEvent) workflow.Instance {
	t.Helper()
	instance, err := workflow.Replay(registry, history)
	if err != nil {
		t.Fatalf("replay sequential instance: %v", err)
	}
	return instance
}
