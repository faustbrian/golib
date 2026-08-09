package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestOperatorActivityRetryIsAuditedAndAtomicallyCreatesWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)
	definition := mustActivityTransitionDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	history := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "execute"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "attempt-1", DueAt: now.Add(32 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Code: "temporary", Retryable: true}),
	}
	instance, err := workflow.Replay(registry, history)
	if err != nil {
		t.Fatalf("replay failure: %v", err)
	}
	command, err := workflow.NewOperatorActivityRetry(workflow.OperatorActivityRetrySpec{
		CommandID: "operator-retry-1", WorkID: "operator-retry-work-1",
		Instance: instance, Definition: definition, StepName: "execute",
		IdempotencyKey: "operator-attempt-2", Actor: "operator-1", Reason: "incident-recovery",
		OccurredAt: now.Add(4 * time.Second), Deadline: now.Add(time.Hour),
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct operator retry: %v", err)
	}
	events := command.Events()
	work := command.Work()
	if len(events) != 2 || events[0].Kind() != workflow.EventOperatorCommandRecorded ||
		events[1].Kind() != workflow.EventActivityRetryScheduled || len(work) != 1 ||
		work[0].Kind() != workflow.WorkActivity || work[0].Sequence() != events[1].Sequence() {
		t.Fatalf("operator retry = events %#v work %#v", events, work)
	}
	history = append(history, events...)
	replayed, err := workflow.Replay(registry, history)
	if err != nil {
		t.Fatalf("replay operator retry: %v", err)
	}
	progress, ok := replayed.Activity("execute")
	actions := replayed.OperatorActions()
	if !ok || progress.Status() != workflow.ActivityProgressRetryWaiting ||
		len(actions) != 1 || actions[0].Action() != workflow.OperatorRetryActivity ||
		actions[0].Actor() != "operator-1" || actions[0].Reason() != "incident-recovery" {
		t.Fatalf("progress = %#v actions = %#v", progress, actions)
	}
}

func TestOperatorCompensationScheduleIsAuditedBeforeDueWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)
	definition := mustCompensationDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	history := successfulActivityHistory(t, definition, now)
	instance, err := workflow.Replay(registry, history)
	if err != nil {
		t.Fatalf("replay activity success: %v", err)
	}
	command, err := workflow.NewOperatorCompensation(workflow.OperatorCompensationSpec{
		CommandID: "operator-compensate-1", WorkID: "operator-compensation-work-1",
		Instance: instance, Definition: definition, StepName: "reserve", Attempt: 1,
		IdempotencyKey: "compensation-attempt-1", Actor: "operator-1", Reason: "cancel-order",
		OccurredAt: now.Add(4 * time.Second), Deadline: now.Add(time.Hour), Input: []byte("reservation-1"),
	})
	if err != nil {
		t.Fatalf("construct operator compensation: %v", err)
	}
	events := command.Events()
	if len(events) != 2 || events[0].Kind() != workflow.EventOperatorCommandRecorded ||
		events[1].Kind() != workflow.EventCompensationScheduled || len(command.Work()) != 1 {
		t.Fatalf("operator compensation = %#v", command)
	}
	replayed, err := workflow.Replay(registry, append(history, events...))
	if err != nil {
		t.Fatalf("replay operator compensation: %v", err)
	}
	progress, ok := replayed.Compensation("reserve")
	if !ok || progress.Status() != workflow.CompensationReady ||
		len(replayed.OperatorActions()) != 1 || replayed.OperatorActions()[0].Action() != workflow.OperatorCompensate {
		t.Fatalf("progress = %#v actions = %#v", progress, replayed.OperatorActions())
	}
}

func TestOperatorCompensationResolutionRemainsDistinctFromSuccessfulRollback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 16, 0, 0, 0, time.UTC)
	definition := mustCompensationDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	history := append(successfulActivityHistory(t, definition, now),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "reserve"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted, OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "key-1", DueAt: now.Add(35 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptUnknown, OccurredAt: now.Add(6 * time.Second), StepName: "reserve", Attempt: 1, Code: "commit-unknown"}),
	)
	instance, err := workflow.Replay(registry, history)
	if err != nil {
		t.Fatalf("replay unknown compensation: %v", err)
	}
	command, err := workflow.NewOperatorCompensationResolution(workflow.OperatorCompensationResolutionSpec{
		CommandID: "operator-resolution-1", Instance: instance, Definition: definition,
		StepName: "reserve", Actor: "operator-1", Reason: "manual-reconciliation",
		Code: "accepted-loss", Evidence: []byte("ticket-123"), OccurredAt: now.Add(7 * time.Second),
	})
	if err != nil {
		t.Fatalf("construct manual resolution: %v", err)
	}
	events := command.Events()
	if len(events) != 2 || events[0].Kind() != workflow.EventOperatorCommandRecorded ||
		events[1].Kind() != workflow.EventCompensationManuallyResolved || len(command.Work()) != 0 {
		t.Fatalf("resolution = %#v", command)
	}
	replayed, err := workflow.Replay(registry, append(history, events...))
	if err != nil {
		t.Fatalf("replay manual resolution: %v", err)
	}
	progress, ok := replayed.Compensation("reserve")
	if !ok || progress.Status() != workflow.CompensationManuallyResolved ||
		progress.Status() == workflow.CompensationSucceeded || progress.Code() != "accepted-loss" ||
		len(replayed.OperatorActions()) != 1 ||
		replayed.OperatorActions()[0].Action() != workflow.OperatorResolveCompensation {
		t.Fatalf("progress = %#v actions = %#v", progress, replayed.OperatorActions())
	}
}
