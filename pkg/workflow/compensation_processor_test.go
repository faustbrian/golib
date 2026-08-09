package workflow_test

import (
	"context"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestCompensationWorkProcessorPersistsStartBeforeExecutingAndOutcomeAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	definition := mustCompensationDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	history := compensationProcessorReadyHistory(t, definition, now)
	store := newProcessorStore(t, definitions, history)
	called := false
	compensation, err := workflow.NewActivity("inventory.release", func(_ context.Context, request workflow.ActivityRequest) workflow.ActivityOutcome {
		called = true
		if len(store.transitions) != 1 || store.transitions[0].Events()[0].Kind() != workflow.EventCompensationAttemptStarted {
			t.Fatal("compensation executed before its attempt-start transition committed")
		}
		if request.StepName() != "reserve" || request.Attempt() != 1 ||
			request.IdempotencyKey() != "compensation-attempt-1" || string(request.Input()) != "reservation-1" {
			t.Fatalf("compensation request = %#v", request)
		}
		outcome, outcomeErr := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{Kind: workflow.ActivitySucceeded})
		if outcomeErr != nil {
			t.Fatalf("construct compensation outcome: %v", outcomeErr)
		}
		return outcome
	})
	if err != nil {
		t.Fatalf("construct compensation: %v", err)
	}
	activities, err := workflow.CompileActivities(compensation)
	if err != nil {
		t.Fatalf("compile compensations: %v", err)
	}
	processor, err := workflow.NewCompensationWorkProcessor(workflow.CompensationWorkProcessorConfig{
		Store: store, Definitions: definitions, Compensations: activities,
		Clock: fixedProcessorClock{now: now.Add(5 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), compensationProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process compensation: %v", err)
	}
	if !called || decision.Kind() != workflow.WorkComplete || len(store.transitions) != 2 ||
		store.transitions[1].Events()[0].Kind() != workflow.EventCompensationAttemptSucceeded {
		t.Fatalf("decision = %#v transitions = %#v called = %t", decision, store.transitions, called)
	}
}

func TestCompensationWorkProcessorDoesNotRepeatInFlightRollback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 7, 0, 0, 0, time.UTC)
	definition := mustCompensationDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	history := compensationProcessorReadyHistory(t, definition, now)
	history = append(history, mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted,
		OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1,
		IdempotencyKey: "compensation-attempt-1", DueAt: now.Add(35 * time.Second),
	}))
	store := newProcessorStore(t, definitions, history)
	compensation, err := workflow.NewActivity("inventory.release", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		t.Fatal("redelivery repeated an in-flight compensating side effect")
		return workflow.ActivityOutcome{}
	})
	if err != nil {
		t.Fatalf("construct compensation: %v", err)
	}
	activities, err := workflow.CompileActivities(compensation)
	if err != nil {
		t.Fatalf("compile compensations: %v", err)
	}
	processor, err := workflow.NewCompensationWorkProcessor(workflow.CompensationWorkProcessorConfig{
		Store: store, Definitions: definitions, Compensations: activities,
		Clock: fixedProcessorClock{now: now.Add(6 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), compensationProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process compensation redelivery: %v", err)
	}
	if decision.Kind() != workflow.WorkComplete || len(store.transitions) != 1 ||
		store.transitions[0].Events()[0].Kind() != workflow.EventCompensationAttemptUnknown ||
		store.transitions[0].Events()[0].Code() != "compensation-outcome-unknown" {
		t.Fatalf("decision = %#v transitions = %#v", decision, store.transitions)
	}
}

func TestCompensationWorkProcessorSchedulesIndependentRetryAfterKnownFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	definition := mustCompensationDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	store := newProcessorStore(t, definitions, compensationProcessorReadyHistory(t, definition, now))
	compensation, err := workflow.NewActivity("inventory.release", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		outcome, outcomeErr := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{
			Kind: workflow.ActivityFailed, Code: "temporary", Retryable: true,
		})
		if outcomeErr != nil {
			t.Fatalf("construct compensation failure: %v", outcomeErr)
		}
		return outcome
	})
	if err != nil {
		t.Fatalf("construct compensation: %v", err)
	}
	activities, err := workflow.CompileActivities(compensation)
	if err != nil {
		t.Fatalf("compile compensations: %v", err)
	}
	processor, err := workflow.NewCompensationWorkProcessor(workflow.CompensationWorkProcessorConfig{
		Store: store, Definitions: definitions, Compensations: activities,
		Clock: fixedProcessorClock{now: now.Add(5 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), compensationProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process compensation failure: %v", err)
	}
	if decision.Kind() != workflow.WorkComplete || len(store.transitions) != 3 ||
		store.transitions[1].Events()[0].Kind() != workflow.EventCompensationAttemptFailed ||
		store.transitions[2].Events()[0].Kind() != workflow.EventCompensationRetryScheduled ||
		len(store.transitions[2].Work()) != 1 ||
		store.transitions[2].Work()[0].AvailableAt() != now.Add(6*time.Second) {
		t.Fatalf("decision = %#v transitions = %#v", decision, store.transitions)
	}
}

func TestCompensationWorkProcessorPersistsUnknownAfterHandlerPanic(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	definition := mustCompensationDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	store := newProcessorStore(t, definitions, compensationProcessorReadyHistory(t, definition, now))
	compensation, err := workflow.NewActivity("inventory.release", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		panic("compensation outcome is unknowable")
	})
	if err != nil {
		t.Fatalf("construct compensation: %v", err)
	}
	activities, err := workflow.CompileActivities(compensation)
	if err != nil {
		t.Fatalf("compile compensations: %v", err)
	}
	processor, err := workflow.NewCompensationWorkProcessor(workflow.CompensationWorkProcessorConfig{
		Store: store, Definitions: definitions, Compensations: activities,
		Clock: fixedProcessorClock{now: now.Add(5 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), compensationProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process panicking compensation: %v", err)
	}
	if decision.Kind() != workflow.WorkComplete || len(store.transitions) != 2 ||
		store.transitions[1].Events()[0].Kind() != workflow.EventCompensationAttemptUnknown ||
		store.transitions[1].Events()[0].Code() != "compensation-panic" {
		t.Fatalf("decision = %#v transitions = %#v", decision, store.transitions)
	}
}

func TestCompensationWorkProcessorDeadLettersPoisonDispatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	definition := mustCompensationDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	store := newProcessorStore(t, definitions, compensationProcessorReadyHistory(t, definition, now))
	activities, err := workflow.CompileActivities()
	if err != nil {
		t.Fatalf("compile compensations: %v", err)
	}
	processor, err := workflow.NewCompensationWorkProcessor(workflow.CompensationWorkProcessorConfig{
		Store: store, Definitions: definitions, Compensations: activities,
		Clock: fixedProcessorClock{now: now.Add(5 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "poison-compensation", Kind: workflow.WorkCompensation, InstanceID: "instance-1", Sequence: 5,
		AvailableAt: now.Add(4 * time.Second), Deadline: now.Add(time.Hour), Payload: []byte("not-json"),
	})
	if err != nil {
		t.Fatalf("construct poison work: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(5 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct poison lease: %v", err)
	}

	decision, err := processor.Process(context.Background(), lease)
	if err != nil || decision.Kind() != workflow.WorkDeadLetterDecision ||
		decision.Code() != "invalid-compensation-dispatch" || len(store.transitions) != 0 {
		t.Fatalf("decision = %#v error = %v transitions = %#v", decision, err, store.transitions)
	}
}

func compensationProcessorReadyHistory(t *testing.T, definition workflow.Definition, now time.Time) []workflow.HistoryEvent {
	t.Helper()
	history := successfulActivityHistory(t, definition, now)
	return append(history, mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled,
		OccurredAt: now.Add(4 * time.Second), StepName: "reserve", Data: []byte("reservation-1"),
	}))
}

func compensationProcessorLease(t *testing.T, now time.Time) workflow.WorkLease {
	t.Helper()
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "compensation-work-1", Kind: workflow.WorkCompensation, InstanceID: "instance-1", Sequence: 5,
		AvailableAt: now.Add(4 * time.Second), Deadline: now.Add(time.Hour),
		Payload:  []byte(`{"step_name":"reserve","attempt":1,"idempotency_key":"compensation-attempt-1"}`),
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct compensation work: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(5 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct compensation lease: %v", err)
	}
	return lease
}
