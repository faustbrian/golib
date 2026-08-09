package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCompensationProcessorRejectsInvalidConstructionAndCalls(t *testing.T) {
	t.Parallel()

	if processor, err := NewCompensationWorkProcessor(CompensationWorkProcessorConfig{}); processor != nil || !errors.Is(err, ErrInvalidCompensationProcessor) {
		t.Fatalf("zero processor = %#v, %v", processor, err)
	}
	var processor *CompensationWorkProcessor
	if _, err := processor.Process(context.Background(), WorkLease{}); !errors.Is(err, ErrInvalidCompensationProcessor) {
		t.Fatalf("nil processor error = %v", err)
	}
	processor = &CompensationWorkProcessor{}
	if _, err := processor.Process(nil, WorkLease{}); !errors.Is(err, ErrInvalidCompensationProcessor) {
		t.Fatalf("nil context error = %v", err)
	}
}

func TestExecuteCompensationSafelyClassifiesExecutionErrorsAsUnknown(t *testing.T) {
	t.Parallel()

	outcome := executeCompensationSafely(context.Background(), Activity{}, ActivityRequest{})
	if outcome.Kind() != ActivityUnknown || outcome.Code() != "compensation-execution-unknown" {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestCompensationProcessorLoadedStateDecisions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 11, 0, 0, 0, time.UTC)
	definition := internalCompensationTransitionDefinition(t)
	work := internalActivityWork(t, now, WorkCompensation, encodeCompensationDispatch("reserve", 1, "key-1"))
	lease := internalActivityLease(t, work, now)
	step, _ := activityProcessorStep(definition, "reserve")
	processor := &CompensationWorkProcessor{}
	dispatch := CompensationDispatch{stepName: "reserve", attempt: 1, idempotencyKey: "key-1"}
	tests := []struct {
		name     string
		progress CompensationProgress
		wantKind WorkDecisionKind
		wantCode string
	}{
		{name: "ready wrong attempt", progress: CompensationProgress{status: CompensationReady, attempt: 1}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-state"},
		{name: "running wrong identity", progress: CompensationProgress{status: CompensationRunning, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-state"},
		{name: "failed wrong identity", progress: CompensationProgress{status: CompensationFailed, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-state"},
		{name: "failed exhausted", progress: CompensationProgress{status: CompensationFailed, attempt: 1, idempotencyKey: "key-1"}, wantKind: WorkComplete},
		{name: "succeeded wrong identity", progress: CompensationProgress{status: CompensationSucceeded, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-state"},
		{name: "succeeded", progress: CompensationProgress{status: CompensationSucceeded, attempt: 1, idempotencyKey: "key-1"}, wantKind: WorkComplete},
		{name: "unknown", progress: CompensationProgress{status: CompensationUnknown, attempt: 1, idempotencyKey: "key-1"}, wantKind: WorkComplete},
		{name: "manual resolution", progress: CompensationProgress{status: CompensationManuallyResolved, attempt: 1, idempotencyKey: "key-1"}, wantKind: WorkComplete},
		{name: "invalid status", progress: CompensationProgress{}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-state"},
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

func TestCompensationProcessorClassifiesLoadAndStateFailures(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	definition := internalCompensationTransitionDefinition(t)
	definitions, err := CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	compensation, err := NewActivity("inventory.release", func(context.Context, ActivityRequest) ActivityOutcome {
		return ActivityOutcome{}
	})
	if err != nil {
		t.Fatalf("construct compensation: %v", err)
	}
	compensations, err := CompileActivities(compensation)
	if err != nil {
		t.Fatalf("compile compensations: %v", err)
	}
	readyHistory := internalCompensationProcessorReadyHistory(t, definition, now)
	failure := errors.New("history unavailable")
	tests := []struct {
		name          string
		store         *internalProcessorStore
		compensations *ActivityRegistry
		work          PendingWork
		wantKind      WorkDecisionKind
		wantCode      string
		wantErr       error
	}{
		{name: "history failure", store: &internalProcessorStore{historyErr: failure}, compensations: compensations, work: internalCompensationProcessorWork(t, now, 5, encodeCompensationDispatch("reserve", 1, "key-1")), wantErr: failure},
		{name: "missing step", store: &internalProcessorStore{history: readyHistory}, compensations: compensations, work: internalCompensationProcessorWork(t, now, 5, encodeCompensationDispatch("missing", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-definition"},
		{name: "attempt above definition", store: &internalProcessorStore{history: readyHistory}, compensations: compensations, work: internalCompensationProcessorWork(t, now, 5, encodeCompensationDispatch("reserve", 3, "key-3")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-definition"},
		{name: "history behind work", store: &internalProcessorStore{history: readyHistory[:4]}, compensations: compensations, work: internalCompensationProcessorWork(t, now, 5, encodeCompensationDispatch("reserve", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-definition"},
		{name: "missing compensation registration", store: &internalProcessorStore{history: readyHistory}, compensations: &ActivityRegistry{activities: map[string]Activity{}}, work: internalCompensationProcessorWork(t, now, 5, encodeCompensationDispatch("reserve", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-definition"},
		{name: "missing compensation progress", store: &internalProcessorStore{history: readyHistory[:4]}, compensations: compensations, work: internalCompensationProcessorWork(t, now, 4, encodeCompensationDispatch("reserve", 1, "key-1")), wantKind: WorkDeadLetterDecision, wantCode: "invalid-compensation-state"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			processor, constructErr := NewCompensationWorkProcessor(CompensationWorkProcessorConfig{
				Store: test.store, Definitions: definitions, Compensations: test.compensations,
				Clock: internalProcessorClock{now: now.Add(5 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
			})
			if constructErr != nil {
				t.Fatalf("construct processor: %v", constructErr)
			}
			lease := internalProcessorLease(t, test.work, now.Add(5*time.Second))
			decision, processErr := processor.Process(context.Background(), lease)
			if !errors.Is(processErr, test.wantErr) || decision.Kind() != test.wantKind || decision.Code() != test.wantCode {
				t.Fatalf("decision = %#v, error = %v", decision, processErr)
			}
		})
	}
}

func TestCompensationProcessorPersistenceFailureBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)
	definition := internalCompensationTransitionDefinition(t)
	definitions, err := CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}
	step, _ := activityProcessorStep(definition, "reserve")
	dispatch := CompensationDispatch{stepName: "reserve", attempt: 1, idempotencyKey: "key-1"}
	work := internalCompensationProcessorWork(t, now, 5, encodeCompensationDispatch("reserve", 1, "key-1"))
	lease := internalProcessorLease(t, work, now.Add(5*time.Second))
	ready := internalCompensationProcessorReadyInstance(definition, now)
	failure := errors.New("persistence failure")

	t.Run("invalid start", func(t *testing.T) {
		processor := &CompensationWorkProcessor{config: CompensationWorkProcessorConfig{Clock: internalProcessorClock{now: now}}}
		if _, err := processor.execute(context.Background(), lease, ready, definition, step, Activity{}, dispatch); !errors.Is(err, ErrInvalidCompensationProcessor) {
			t.Fatalf("execute error = %v", err)
		}
	})
	t.Run("start commit failure", func(t *testing.T) {
		store := &internalProcessorStore{commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)}}
		processor := internalConfiguredCompensationProcessor(store, definitions, now.Add(5*time.Second))
		if _, err := processor.execute(context.Background(), lease, ready, definition, step, Activity{}, dispatch); !errors.Is(err, failure) {
			t.Fatalf("execute error = %v", err)
		}
	})
	t.Run("reload after start failure", func(t *testing.T) {
		store := &internalProcessorStore{historyErr: failure}
		processor := internalConfiguredCompensationProcessor(store, definitions, now.Add(5*time.Second))
		if _, err := processor.execute(context.Background(), lease, ready, definition, step, Activity{}, dispatch); !errors.Is(err, failure) {
			t.Fatalf("execute error = %v", err)
		}
	})
	t.Run("invalid request after durable start", func(t *testing.T) {
		store := &internalProcessorStore{history: internalCompensationProcessorReadyHistory(t, definition, now)}
		processor := internalConfiguredCompensationProcessor(store, definitions, now.Add(5*time.Second))
		badStep := step
		badStep.InputLimit = 0
		if _, err := processor.execute(context.Background(), lease, ready, definition, badStep, Activity{}, dispatch); !errors.Is(err, ErrInvalidCompensationProcessor) {
			t.Fatalf("execute error = %v", err)
		}
	})

	running := internalCompensationProcessorRunningInstance(definition, now)
	success, err := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivitySucceeded})
	if err != nil {
		t.Fatalf("construct success: %v", err)
	}
	t.Run("outcome time follows history", func(t *testing.T) {
		store := &internalProcessorStore{historyErr: failure}
		processor := internalConfiguredCompensationProcessor(store, definitions, now)
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, success); !errors.Is(err, failure) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})
	t.Run("invalid outcome transition", func(t *testing.T) {
		processor := internalConfiguredCompensationProcessor(&internalProcessorStore{}, definitions, now.Add(6*time.Second))
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, ActivityOutcome{}); !errors.Is(err, ErrInvalidCompensationProcessor) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})
	t.Run("outcome commit failure", func(t *testing.T) {
		store := &internalProcessorStore{commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)}}
		processor := internalConfiguredCompensationProcessor(store, definitions, now.Add(6*time.Second))
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, success); !errors.Is(err, failure) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})
	t.Run("reload after outcome failure", func(t *testing.T) {
		store := &internalProcessorStore{historyErr: failure}
		processor := internalConfiguredCompensationProcessor(store, definitions, now.Add(6*time.Second))
		if _, err := processor.persistOutcome(context.Background(), work, running, definition, dispatch, success); !errors.Is(err, failure) {
			t.Fatalf("persist outcome error = %v", err)
		}
	})

	failed := internalCompensationProcessorFailedInstance(definition, now)
	t.Run("retry time follows history", func(t *testing.T) {
		processor := internalConfiguredCompensationProcessor(&internalProcessorStore{}, definitions, now)
		if _, err := processor.scheduleRetry(context.Background(), work, failed, definition, step, dispatch); err != nil {
			t.Fatalf("schedule retry: %v", err)
		}
	})
	t.Run("retry deadline exhausted", func(t *testing.T) {
		processor := internalConfiguredCompensationProcessor(&internalProcessorStore{}, definitions, now.Add(6*time.Second))
		expiring := work
		expiring.deadline = now.Add(7 * time.Second)
		decision, err := processor.scheduleRetry(context.Background(), expiring, failed, definition, step, dispatch)
		if err != nil || decision.Kind() != WorkComplete {
			t.Fatalf("decision = %#v, error = %v", decision, err)
		}
	})
	t.Run("invalid retry transition", func(t *testing.T) {
		processor := internalConfiguredCompensationProcessor(&internalProcessorStore{}, definitions, now.Add(6*time.Second))
		mismatched := failed
		mismatched.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
		if _, err := processor.scheduleRetry(context.Background(), work, mismatched, definition, step, dispatch); !errors.Is(err, ErrInvalidCompensationProcessor) {
			t.Fatalf("schedule retry error = %v", err)
		}
	})
	t.Run("retry commit failure", func(t *testing.T) {
		store := &internalProcessorStore{commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)}}
		processor := internalConfiguredCompensationProcessor(store, definitions, now.Add(6*time.Second))
		if _, err := processor.scheduleRetry(context.Background(), work, failed, definition, step, dispatch); !errors.Is(err, failure) {
			t.Fatalf("schedule retry error = %v", err)
		}
	})
}

func internalConfiguredCompensationProcessor(store ActivityExecutionStore, definitions *Registry, now time.Time) *CompensationWorkProcessor {
	return &CompensationWorkProcessor{config: CompensationWorkProcessorConfig{
		Store: store, Definitions: definitions, Clock: internalProcessorClock{now: now},
		PageSize: 10, MaxHistoryEvents: 100,
	}}
}

func internalCompensationProcessorReadyHistory(t *testing.T, definition Definition, now time.Time) []HistoryEvent {
	t.Helper()
	return []HistoryEvent{
		internalCompensationProcessorEvent(t, HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		internalCompensationProcessorEvent(t, HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "reserve"}),
		internalCompensationProcessorEvent(t, HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "activity-1", DueAt: now.Add(62 * time.Second)}),
		internalCompensationProcessorEvent(t, HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: EventActivityAttemptSucceeded, OccurredAt: now.Add(3 * time.Second), StepName: "reserve", Attempt: 1}),
		internalCompensationProcessorEvent(t, HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "reserve", Data: []byte("input")}),
	}
}

func internalCompensationProcessorEvent(t *testing.T, spec HistoryEventSpec) HistoryEvent {
	t.Helper()
	event, err := NewHistoryEvent(spec)
	if err != nil {
		t.Fatalf("construct history event: %v", err)
	}
	return event
}

func internalCompensationProcessorWork(t *testing.T, now time.Time, sequence uint64, payload []byte) PendingWork {
	t.Helper()
	work, err := NewPendingWork(PendingWorkSpec{
		ID: "compensation-processor-work", Kind: WorkCompensation, InstanceID: "instance-1", Sequence: sequence,
		AvailableAt: now.Add(4 * time.Second), Deadline: now.Add(time.Hour), Payload: payload,
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	return work
}

func internalCompensationProcessorReadyInstance(definition Definition, now time.Time) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 5, startedAt: now, updatedAt: now.Add(4 * time.Second),
		activities: map[string]ActivityProgress{"reserve": {stepName: "reserve", status: ActivityProgressSucceeded, attempt: 1}},
		compensations: map[string]CompensationProgress{
			"reserve": {stepName: "reserve", status: CompensationReady, scheduledSequence: 5, input: []byte("input")},
		},
	}
}

func internalCompensationProcessorRunningInstance(definition Definition, now time.Time) Instance {
	instance := internalCompensationProcessorReadyInstance(definition, now)
	instance.sequence = 6
	instance.updatedAt = now.Add(5 * time.Second)
	instance.compensations["reserve"] = CompensationProgress{
		stepName: "reserve", status: CompensationRunning, scheduledSequence: 5, attempt: 1,
		idempotencyKey: "key-1", dueAt: now.Add(35 * time.Second), input: []byte("input"),
	}
	return instance
}

func internalCompensationProcessorFailedInstance(definition Definition, now time.Time) Instance {
	instance := internalCompensationProcessorReadyInstance(definition, now)
	instance.sequence = 7
	instance.updatedAt = now.Add(6 * time.Second)
	instance.compensations["reserve"] = CompensationProgress{
		stepName: "reserve", status: CompensationFailed, scheduledSequence: 5,
		attempt: 1, idempotencyKey: "key-1", retryable: true,
	}
	return instance
}
