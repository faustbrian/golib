package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestActivityProcessorCommitReconciliationBoundaries(t *testing.T) {
	t.Parallel()

	transition := internalProcessorTransition(t)
	failure := errors.New("store failure")
	reconcileFailure := errors.New("reconcile failure")
	tests := []struct {
		name      string
		commits   []error
		outcome   TransitionReconciliationOutcome
		reconcile error
		want      error
	}{
		{name: "committed", commits: []error{nil}},
		{name: "classified committed", commits: []error{NewStoreCommitError(StoreCommitCommitted, failure)}},
		{name: "not committed", commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)}, want: failure},
		{name: "reconciliation fails", commits: []error{failure}, reconcile: reconcileFailure, want: reconcileFailure},
		{name: "reconciled committed", commits: []error{failure}, outcome: TransitionCommitted},
		{name: "reconciled conflict", commits: []error{failure}, outcome: TransitionConflicting, want: ErrDuplicateTransition},
		{name: "reconciled missing retry succeeds", commits: []error{failure, nil}, outcome: TransitionMissing},
		{name: "reconciled missing retry fails", commits: []error{failure, reconcileFailure}, outcome: TransitionMissing, want: reconcileFailure},
		{name: "invalid reconciliation output", commits: []error{failure}, outcome: 99, want: ErrInvalidStoreRequest},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := &internalProcessorStore{commits: append([]error(nil), test.commits...), outcome: test.outcome, reconcileErr: test.reconcile}
			err := commitActivityTransition(context.Background(), store, transition)
			if !errors.Is(err, test.want) {
				t.Fatalf("commit error = %v, want %v", err, test.want)
			}
			if store.reconciled != (StoreCommitOutcomeOf(test.commits[0]) == StoreCommitUnknown && test.commits[0] != nil) {
				t.Fatalf("reconciled = %t", store.reconciled)
			}
		})
	}
}

func TestActivityProcessorRejectsInvalidConstructionAndCalls(t *testing.T) {
	t.Parallel()

	if processor, err := NewActivityWorkProcessor(ActivityWorkProcessorConfig{}); processor != nil || !errors.Is(err, ErrInvalidActivityProcessor) {
		t.Fatalf("zero processor = %#v, %v", processor, err)
	}
	var processor *ActivityWorkProcessor
	if _, err := processor.Process(context.Background(), WorkLease{}); !errors.Is(err, ErrInvalidActivityProcessor) {
		t.Fatalf("nil processor error = %v", err)
	}
	processor = &ActivityWorkProcessor{}
	if _, err := processor.Process(nil, WorkLease{}); !errors.Is(err, ErrInvalidActivityProcessor) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, ok := activityProcessorStep(Definition{}, "missing"); ok {
		t.Fatal("zero definition resolved a processor step")
	}
}

func TestExecuteActivitySafelyClassifiesExecutionErrorsAsUnknown(t *testing.T) {
	t.Parallel()

	outcome := executeActivitySafely(context.Background(), Activity{}, ActivityRequest{})
	if outcome.Kind() != ActivityUnknown || outcome.Code() != "activity-execution-unknown" {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestActivityProcessorLoadedStateDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	work := internalActivityWork(t, now, WorkActivity, encodeActivityDispatch("execute", 1, "key-1"))
	lease := internalActivityLease(t, work, now)
	step, _ := activityProcessorStep(definition, "execute")
	processor := &ActivityWorkProcessor{}
	dispatch := ActivityDispatch{stepName: "execute", attempt: 1, idempotencyKey: "key-1"}
	tests := []struct {
		name     string
		progress ActivityProgress
		wantKind WorkDecisionKind
		wantCode string
	}{
		{name: "ready wrong attempt", progress: ActivityProgress{status: ActivityProgressReady, attempt: 1}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-state"},
		{name: "running wrong identity", progress: ActivityProgress{status: ActivityProgressRunning, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-state"},
		{name: "failed wrong identity", progress: ActivityProgress{status: ActivityProgressFailed, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-state"},
		{name: "failed exhausted", progress: ActivityProgress{status: ActivityProgressFailed, attempt: 1, idempotencyKey: "key-1"}, wantKind: WorkComplete},
		{name: "succeeded wrong identity", progress: ActivityProgress{status: ActivityProgressSucceeded, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-state"},
		{name: "succeeded", progress: ActivityProgress{status: ActivityProgressSucceeded, attempt: 1, idempotencyKey: "key-1"}, wantKind: WorkComplete},
		{name: "unknown", progress: ActivityProgress{status: ActivityProgressUnknown, attempt: 1, idempotencyKey: "key-1"}, wantKind: WorkComplete},
		{name: "invalid status", progress: ActivityProgress{}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-state"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision, err := processor.processProgress(
				context.Background(), lease, Instance{}, definition, step, Activity{}, dispatch, test.progress,
			)
			if err != nil || decision.Kind() != test.wantKind || decision.Code() != test.wantCode {
				t.Fatalf("decision = %#v, error = %v", decision, err)
			}
		})
	}
}

func TestActivityProcessorClassifiesLoadAndStateFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	definitions, err := CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	activity, err := NewActivity("orders.execute", func(context.Context, ActivityRequest) ActivityOutcome {
		return ActivityOutcome{}
	})
	if err != nil {
		t.Fatalf("construct activity: %v", err)
	}
	activities, err := CompileActivities(activity)
	if err != nil {
		t.Fatalf("compile activities: %v", err)
	}
	started := internalProcessorStartedEvent(t, definition, now)
	scheduled := internalProcessorScheduledEvent(t, now.Add(time.Second))
	failure := errors.New("history unavailable")
	tests := []struct {
		name       string
		store      *internalProcessorStore
		activities *ActivityRegistry
		work       PendingWork
		wantKind   WorkDecisionKind
		wantCode   string
		wantErr    error
	}{
		{name: "history failure", store: &internalProcessorStore{historyErr: failure}, activities: activities, work: internalProcessorWork(t, now, 1, encodeActivityDispatch("execute", 1, "key-1")), wantErr: failure},
		{name: "missing step", store: &internalProcessorStore{history: []HistoryEvent{started}}, activities: activities, work: internalProcessorWork(t, now, 1, encodeActivityDispatch("missing", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-definition"},
		{name: "attempt above definition", store: &internalProcessorStore{history: []HistoryEvent{started, scheduled}}, activities: activities, work: internalProcessorWork(t, now, 2, encodeActivityDispatch("execute", 3, "key-3")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-definition"},
		{name: "history behind work", store: &internalProcessorStore{history: []HistoryEvent{started}}, activities: activities, work: internalProcessorWork(t, now, 2, encodeActivityDispatch("execute", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-definition"},
		{name: "missing activity registration", store: &internalProcessorStore{history: []HistoryEvent{started, scheduled}}, activities: &ActivityRegistry{activities: map[string]Activity{}}, work: internalProcessorWork(t, now, 2, encodeActivityDispatch("execute", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-definition"},
		{name: "missing scheduled progress", store: &internalProcessorStore{history: []HistoryEvent{started}}, activities: activities, work: internalProcessorWork(t, now, 1, encodeActivityDispatch("execute", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-activity-state"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			processor, constructErr := NewActivityWorkProcessor(ActivityWorkProcessorConfig{
				Store: test.store, Definitions: definitions, Activities: test.activities,
				Clock: internalProcessorClock{now: now.Add(2 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
			})
			if constructErr != nil {
				t.Fatalf("construct processor: %v", constructErr)
			}
			lease := internalProcessorLease(t, test.work, now.Add(2*time.Second))
			decision, processErr := processor.Process(context.Background(), lease)
			if !errors.Is(processErr, test.wantErr) || decision.Kind() != test.wantKind || decision.Code() != test.wantCode {
				t.Fatalf("decision = %#v, error = %v", decision, processErr)
			}
		})
	}
}

func TestActivityProcessorPersistenceFailureBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 5, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	definitions, err := CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	step, _ := activityProcessorStep(definition, "execute")
	dispatch := ActivityDispatch{stepName: "execute", attempt: 1, idempotencyKey: "key-1"}
	work := internalProcessorWork(t, now, 2, encodeActivityDispatch("execute", 1, "key-1"))
	lease := internalProcessorLease(t, work, now.Add(2*time.Second))
	ready := internalProcessorReadyInstance(definition, now)
	failure := errors.New("persistence failure")

	t.Run("invalid start", func(t *testing.T) {
		processor := &ActivityWorkProcessor{config: ActivityWorkProcessorConfig{Clock: internalProcessorClock{now: now}}}
		if _, err := processor.execute(context.Background(), lease, ready, definition, step, Activity{}, dispatch); !errors.Is(err, ErrInvalidActivityProcessor) {
			t.Fatalf("execute error = %v", err)
		}
	})
	t.Run("start commit failure", func(t *testing.T) {
		store := &internalProcessorStore{commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)}}
		processor := internalConfiguredProcessor(store, definitions, now.Add(2*time.Second))
		if _, err := processor.execute(context.Background(), lease, ready, definition, step, Activity{}, dispatch); !errors.Is(err, failure) {
			t.Fatalf("execute error = %v", err)
		}
	})
	t.Run("reload after start failure", func(t *testing.T) {
		store := &internalProcessorStore{historyErr: failure}
		processor := internalConfiguredProcessor(store, definitions, now.Add(2*time.Second))
		if _, err := processor.execute(context.Background(), lease, ready, definition, step, Activity{}, dispatch); !errors.Is(err, failure) {
			t.Fatalf("execute error = %v", err)
		}
	})
	t.Run("invalid request after durable start", func(t *testing.T) {
		store := &internalProcessorStore{history: internalProcessorReadyHistory(t, definition, now)}
		processor := internalConfiguredProcessor(store, definitions, now.Add(2*time.Second))
		if _, err := processor.execute(context.Background(), lease, ready, definition, StepSpec{}, Activity{}, dispatch); !errors.Is(err, ErrInvalidActivityProcessor) {
			t.Fatalf("execute error = %v", err)
		}
	})

	running := internalProcessorRunningInstance(definition, now)
	success, err := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivitySucceeded})
	if err != nil {
		t.Fatalf("construct success: %v", err)
	}
	t.Run("outcome time follows history", func(t *testing.T) {
		store := &internalProcessorStore{historyErr: failure}
		processor := internalConfiguredProcessor(store, definitions, now)
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, success); !errors.Is(err, failure) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})
	t.Run("invalid outcome transition", func(t *testing.T) {
		processor := internalConfiguredProcessor(&internalProcessorStore{}, definitions, now.Add(3*time.Second))
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, ActivityOutcome{}); !errors.Is(err, ErrInvalidActivityProcessor) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})
	t.Run("outcome commit failure", func(t *testing.T) {
		store := &internalProcessorStore{commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)}}
		processor := internalConfiguredProcessor(store, definitions, now.Add(3*time.Second))
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, success); !errors.Is(err, failure) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})
	t.Run("reload after outcome failure", func(t *testing.T) {
		store := &internalProcessorStore{historyErr: failure}
		processor := internalConfiguredProcessor(store, definitions, now.Add(3*time.Second))
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, success); !errors.Is(err, failure) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})

	failed := internalProcessorFailedInstance(definition, now)
	t.Run("retry time follows history", func(t *testing.T) {
		processor := internalConfiguredProcessor(&internalProcessorStore{}, definitions, now)
		lateDeadline := work
		lateDeadline.deadline = now.Add(time.Hour)
		if _, err := processor.scheduleRetry(context.Background(), lateDeadline, failed, definition, step, dispatch); err != nil {
			t.Fatalf("schedule retry: %v", err)
		}
	})
	t.Run("retry deadline exhausted", func(t *testing.T) {
		processor := internalConfiguredProcessor(&internalProcessorStore{}, definitions, now.Add(3*time.Second))
		expiring := work
		expiring.deadline = now.Add(4 * time.Second)
		decision, err := processor.scheduleRetry(context.Background(), expiring, failed, definition, step, dispatch)
		if err != nil || decision.Kind() != WorkComplete {
			t.Fatalf("decision = %#v, error = %v", decision, err)
		}
	})
	t.Run("invalid retry transition", func(t *testing.T) {
		processor := internalConfiguredProcessor(&internalProcessorStore{}, definitions, now.Add(3*time.Second))
		mismatched := failed
		mismatched.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
		if _, err := processor.scheduleRetry(context.Background(), work, mismatched, definition, step, dispatch); !errors.Is(err, ErrInvalidActivityProcessor) {
			t.Fatalf("schedule retry error = %v", err)
		}
	})
	t.Run("retry commit failure", func(t *testing.T) {
		store := &internalProcessorStore{commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)}}
		processor := internalConfiguredProcessor(store, definitions, now.Add(3*time.Second))
		if _, err := processor.scheduleRetry(context.Background(), work, failed, definition, step, dispatch); !errors.Is(err, failure) {
			t.Fatalf("schedule retry error = %v", err)
		}
	})
}

type internalProcessorStore struct {
	commits      []error
	outcome      TransitionReconciliationOutcome
	reconcileErr error
	reconciled   bool
	historyErr   error
	history      []HistoryEvent
}

func (store *internalProcessorStore) Commit(_ context.Context, transition Transition) error {
	if len(store.commits) == 0 {
		store.history = append(store.history, transition.Events()...)
		return nil
	}
	err := store.commits[0]
	store.commits = store.commits[1:]
	if err == nil {
		store.history = append(store.history, transition.Events()...)
	}
	return err
}

func (store *internalProcessorStore) History(_ context.Context, query HistoryQuery) (HistoryPage, error) {
	if store.historyErr != nil {
		return HistoryPage{}, store.historyErr
	}
	start := int(query.AfterSequence())
	end := min(start+int(query.Limit()), len(store.history))
	return NewHistoryPage(query, store.history[start:end], end < len(store.history))
}

func (store *internalProcessorStore) ReconcileTransition(context.Context, TransitionReconciliation) (TransitionReconciliationOutcome, error) {
	store.reconciled = true
	return store.outcome, store.reconcileErr
}

func internalProcessorTransition(t *testing.T) Transition {
	t.Helper()
	now := time.Date(2026, 8, 10, 2, 0, 0, 0, time.UTC)
	definition := internalActivityTransitionDefinition(t)
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	if err != nil {
		t.Fatalf("construct event: %v", err)
	}
	transition, err := NewTransition(TransitionSpec{
		ID: "processor-transition", InstanceID: "instance-1", ExpectedSequence: 0,
		Definition: definition.Reference(), Events: []HistoryEvent{event},
	})
	if err != nil {
		t.Fatalf("construct transition: %v", err)
	}
	return transition
}

type internalProcessorClock struct{ now time.Time }

func (clock internalProcessorClock) Now() time.Time { return clock.now }
func (internalProcessorClock) NewTimer(time.Duration) ClockTimer {
	panic("processor test clock does not create timers")
}

func internalProcessorStartedEvent(t *testing.T, definition Definition, now time.Time) HistoryEvent {
	t.Helper()
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	if err != nil {
		t.Fatalf("construct start event: %v", err)
	}
	return event
}

func internalProcessorScheduledEvent(t *testing.T, at time.Time) HistoryEvent {
	t.Helper()
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: EventActivityScheduled,
		OccurredAt: at, StepName: "execute", Data: []byte("input"),
	})
	if err != nil {
		t.Fatalf("construct scheduled event: %v", err)
	}
	return event
}

func internalProcessorWork(t *testing.T, now time.Time, sequence uint64, payload []byte) PendingWork {
	t.Helper()
	work, err := NewPendingWork(PendingWorkSpec{
		ID: "processor-work", Kind: WorkActivity, InstanceID: "instance-1", Sequence: sequence,
		AvailableAt: now.Add(time.Second), Deadline: now.Add(time.Hour), Payload: payload,
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	return work
}

func internalProcessorLease(t *testing.T, work PendingWork, claimedAt time.Time) WorkLease {
	t.Helper()
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: claimedAt, ExpiresAt: claimedAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct lease: %v", err)
	}
	return lease
}

func internalConfiguredProcessor(store ActivityExecutionStore, definitions *Registry, now time.Time) *ActivityWorkProcessor {
	return &ActivityWorkProcessor{config: ActivityWorkProcessorConfig{
		Store: store, Definitions: definitions, Clock: internalProcessorClock{now: now},
		PageSize: 10, MaxHistoryEvents: 100,
	}}
}

func internalProcessorReadyHistory(t *testing.T, definition Definition, now time.Time) []HistoryEvent {
	t.Helper()
	return []HistoryEvent{
		internalProcessorStartedEvent(t, definition, now),
		internalProcessorScheduledEvent(t, now.Add(time.Second)),
	}
}

func internalProcessorReadyInstance(definition Definition, now time.Time) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 2, startedAt: now, updatedAt: now.Add(time.Second),
		activities: map[string]ActivityProgress{
			"execute": {stepName: "execute", status: ActivityProgressReady, input: []byte("input")},
		},
	}
}

func internalProcessorRunningInstance(definition Definition, now time.Time) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 3, startedAt: now, updatedAt: now.Add(2 * time.Second),
		activities: map[string]ActivityProgress{
			"execute": {
				stepName: "execute", status: ActivityProgressRunning, attempt: 1,
				idempotencyKey: "key-1", dueAt: now.Add(32 * time.Second), input: []byte("input"),
			},
		},
	}
}

func internalProcessorFailedInstance(definition Definition, now time.Time) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 4, startedAt: now, updatedAt: now.Add(3 * time.Second),
		activities: map[string]ActivityProgress{
			"execute": {
				stepName: "execute", status: ActivityProgressFailed, attempt: 1,
				idempotencyKey: "key-1", retryable: true,
			},
		},
	}
}
