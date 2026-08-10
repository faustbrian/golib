package workflow_test

import (
	"context"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestActivityWorkProcessorPersistsStartBeforeExecutingAndPersistsOutcome(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 9, 21, 0, 0, 0, time.UTC)
	definition := mustActivityTransitionDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	store := newProcessorStore(t, definitions, activityProcessorReadyHistory(t, definition, now))
	called := false
	activity, err := workflow.NewActivity("orders.execute", func(_ context.Context, request workflow.ActivityRequest) workflow.ActivityOutcome {
		called = true
		if len(store.transitions) != 1 || store.transitions[0].Events()[0].Kind() != workflow.EventActivityAttemptStarted {
			t.Fatal("activity executed before its attempt-start transition committed")
		}
		if request.Attempt() != 1 || request.IdempotencyKey() != "activity-attempt-1" ||
			string(request.Input()) != "input" || request.TenantID() != "tenant-1" ||
			request.CorrelationID() != "correlation-1" {
			t.Fatalf("activity request = %#v", request)
		}
		outcome, outcomeErr := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{
			Kind: workflow.ActivitySucceeded, Data: []byte("result"),
		})
		if outcomeErr != nil {
			t.Fatalf("construct outcome: %v", outcomeErr)
		}
		return outcome
	})
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}
	activities, err := workflow.CompileActivities(activity)
	if err != nil {
		t.Fatalf("compile activities: %v", err)
	}
	processor, err := workflow.NewActivityWorkProcessor(workflow.ActivityWorkProcessorConfig{
		Store: store, Definitions: definitions, Activities: activities,
		Clock:    fixedProcessorClock{now: now.Add(2 * time.Second)},
		PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), activityProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process activity: %v", err)
	}
	if !called || decision.Kind() != workflow.WorkComplete || len(store.transitions) != 2 ||
		store.transitions[1].Events()[0].Kind() != workflow.EventActivityAttemptSucceeded ||
		string(store.transitions[1].Events()[0].Data()) != "result" {
		t.Fatalf("decision = %#v transitions = %#v called = %t", decision, store.transitions, called)
	}
}

func TestActivityWorkProcessorDoesNotRepeatInFlightUnknownSideEffect(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 9, 22, 0, 0, 0, time.UTC)
	definition := mustActivityTransitionDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	history := activityProcessorReadyHistory(t, definition, now)
	history = append(history, mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted,
		OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1,
		IdempotencyKey: "activity-attempt-1", DueAt: now.Add(32 * time.Second),
	}))
	store := newProcessorStore(t, definitions, history)
	activity, err := workflow.NewActivity("orders.execute", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		t.Fatal("redelivery repeated an in-flight external side effect")
		return workflow.ActivityOutcome{}
	})
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}
	activities, err := workflow.CompileActivities(activity)
	if err != nil {
		t.Fatalf("compile activities: %v", err)
	}
	processor, err := workflow.NewActivityWorkProcessor(workflow.ActivityWorkProcessorConfig{
		Store: store, Definitions: definitions, Activities: activities,
		Clock: fixedProcessorClock{now: now.Add(3 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), activityProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process redelivery: %v", err)
	}
	if decision.Kind() != workflow.WorkComplete || len(store.transitions) != 1 ||
		store.transitions[0].Events()[0].Kind() != workflow.EventActivityAttemptUnknown ||
		store.transitions[0].Events()[0].Code() != "activity-outcome-unknown" {
		t.Fatalf("decision = %#v transitions = %#v", decision, store.transitions)
	}
}

func TestActivityWorkProcessorSchedulesDurableRetryAfterKnownFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 9, 23, 0, 0, 0, time.UTC)
	definition := mustActivityTransitionDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	store := newProcessorStore(t, definitions, activityProcessorReadyHistory(t, definition, now))
	activity, err := workflow.NewActivity("orders.execute", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		outcome, outcomeErr := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{
			Kind: workflow.ActivityFailed, Code: "temporary", Retryable: true,
		})
		if outcomeErr != nil {
			t.Fatalf("construct failure: %v", outcomeErr)
		}
		return outcome
	})
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}
	activities, err := workflow.CompileActivities(activity)
	if err != nil {
		t.Fatalf("compile activities: %v", err)
	}
	processor, err := workflow.NewActivityWorkProcessor(workflow.ActivityWorkProcessorConfig{
		Store: store, Definitions: definitions, Activities: activities,
		Clock: fixedProcessorClock{now: now.Add(2 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), activityProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process retryable failure: %v", err)
	}
	if decision.Kind() != workflow.WorkComplete || len(store.transitions) != 3 ||
		store.transitions[1].Events()[0].Kind() != workflow.EventActivityAttemptFailed ||
		store.transitions[2].Events()[0].Kind() != workflow.EventActivityRetryScheduled ||
		len(store.transitions[2].Work()) != 1 ||
		store.transitions[2].Work()[0].AvailableAt() != now.Add(3*time.Second) {
		t.Fatalf("decision = %#v transitions = %#v", decision, store.transitions)
	}
}

func TestActivityWorkProcessorPersistsUnknownAfterHandlerPanic(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 10, 0, 0, 0, 0, time.UTC)
	definition := mustActivityTransitionDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	store := newProcessorStore(t, definitions, activityProcessorReadyHistory(t, definition, now))
	activity, err := workflow.NewActivity("orders.execute", func(context.Context, workflow.ActivityRequest) workflow.ActivityOutcome {
		panic("outcome is unknowable")
	})
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}
	activities, err := workflow.CompileActivities(activity)
	if err != nil {
		t.Fatalf("compile activities: %v", err)
	}
	processor, err := workflow.NewActivityWorkProcessor(workflow.ActivityWorkProcessorConfig{
		Store: store, Definitions: definitions, Activities: activities,
		Clock: fixedProcessorClock{now: now.Add(2 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}

	decision, err := processor.Process(context.Background(), activityProcessorLease(t, now))
	if err != nil {
		t.Fatalf("process panicking activity: %v", err)
	}
	if decision.Kind() != workflow.WorkComplete || len(store.transitions) != 2 ||
		store.transitions[1].Events()[0].Kind() != workflow.EventActivityAttemptUnknown ||
		store.transitions[1].Events()[0].Code() != "activity-panic" {
		t.Fatalf("decision = %#v transitions = %#v", decision, store.transitions)
	}
}

func TestActivityWorkProcessorDeadLettersPoisonDispatchWithoutSideEffects(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 10, 1, 0, 0, 0, time.UTC)
	definition := mustActivityTransitionDefinition(t)
	definitions, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	store := newProcessorStore(t, definitions, activityProcessorReadyHistory(t, definition, now))
	activities, err := workflow.CompileActivities()
	if err != nil {
		t.Fatalf("compile activities: %v", err)
	}
	processor, err := workflow.NewActivityWorkProcessor(workflow.ActivityWorkProcessorConfig{
		Store: store, Definitions: definitions, Activities: activities,
		Clock: fixedProcessorClock{now: now.Add(2 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "poison-work", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now.Add(time.Second), Deadline: now.Add(time.Hour), Payload: []byte("not-json"),
	})
	if err != nil {
		t.Fatalf("construct poison work: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct poison lease: %v", err)
	}

	decision, err := processor.Process(context.Background(), lease)
	if err != nil || decision.Kind() != workflow.WorkDeadLetterDecision ||
		decision.Code() != "invalid-activity-dispatch" || len(store.transitions) != 0 {
		t.Fatalf("decision = %#v error = %v transitions = %#v", decision, err, store.transitions)
	}
}

type processorStore struct {
	t           *testing.T
	definitions *workflow.Registry
	history     []workflow.HistoryEvent
	transitions []workflow.Transition
}

func newProcessorStore(t *testing.T, definitions *workflow.Registry, history []workflow.HistoryEvent) *processorStore {
	t.Helper()
	return &processorStore{t: t, definitions: definitions, history: append([]workflow.HistoryEvent(nil), history...)}
}

func (store *processorStore) History(_ context.Context, query workflow.HistoryQuery) (workflow.HistoryPage, error) {
	if query.InstanceID() != "instance-1" {
		return workflow.HistoryPage{}, workflow.ErrStoreNotFound
	}
	start := int(query.AfterSequence())
	end := min(start+int(query.Limit()), len(store.history))
	return workflow.NewHistoryPage(query, store.history[start:end], end < len(store.history))
}

func (store *processorStore) Commit(_ context.Context, transition workflow.Transition) error {
	instance, err := workflow.Replay(store.definitions, store.history)
	if err != nil {
		store.t.Fatalf("replay before commit: %v", err)
	}
	if transition.ExpectedSequence() != instance.Sequence() {
		return workflow.NewStoreCommitError(workflow.StoreCommitNotCommitted, workflow.ErrStoreConflict)
	}
	store.transitions = append(store.transitions, transition)
	store.history = append(store.history, transition.Events()...)
	return nil
}

func (store *processorStore) ReconcileTransition(_ context.Context, reconciliation workflow.TransitionReconciliation) (workflow.TransitionReconciliationOutcome, error) {
	for _, transition := range store.transitions {
		if transition.ID() == reconciliation.TransitionID() {
			if transition.Fingerprint() == reconciliation.Fingerprint() {
				return workflow.TransitionCommitted, nil
			}
			return workflow.TransitionConflicting, nil
		}
	}
	return workflow.TransitionMissing, nil
}

type fixedProcessorClock struct{ now time.Time }

func (clock fixedProcessorClock) Now() time.Time { return clock.now }
func (fixedProcessorClock) NewTimer(time.Duration) workflow.ClockTimer {
	panic("activity processor does not own process timers")
}

func activityProcessorReadyHistory(t *testing.T, definition workflow.Definition, now time.Time) []workflow.HistoryEvent {
	t.Helper()
	return []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "execute", Data: []byte("input")}),
	}
}

func activityProcessorLease(t *testing.T, now time.Time) workflow.WorkLease {
	t.Helper()
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "activity-work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now.Add(time.Second), Deadline: now.Add(time.Hour),
		Payload:  []byte(`{"step_name":"execute","attempt":1,"idempotency_key":"activity-attempt-1"}`),
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct lease: %v", err)
	}
	return lease
}

var _ workflow.ActivityExecutionStore = (*processorStore)(nil)
