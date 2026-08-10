package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestChildProcessorRejectsInvalidConstructionCallsAndLoadedStates(t *testing.T) {
	t.Parallel()

	if processor, err := NewChildWorkProcessor(ChildWorkProcessorConfig{}); processor != nil ||
		!errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("zero processor = %#v, %v", processor, err)
	}
	var nilProcessor *ChildWorkProcessor
	if _, err := nilProcessor.Process(context.Background(), WorkLease{}); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("nil processor error = %v", err)
	}
	processor := &ChildWorkProcessor{}
	if _, err := processor.Process(nil, WorkLease{}); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("nil context error = %v", err)
	}

	now := time.Date(2036, 8, 11, 16, 0, 0, 0, time.UTC)
	parent, child, _ := internalChildDefinitions(t)
	step := parent.Steps()[0]
	work, err := NewPendingWork(PendingWorkSpec{
		ID: "child-work", Kind: WorkChild, InstanceID: "parent-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(time.Hour),
		Payload: encodeChildDispatch("child", "child-1", child.Reference(), 1, "child-key"),
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct lease: %v", err)
	}
	dispatch, err := DecodeChildDispatch(work.Payload())
	if err != nil {
		t.Fatalf("decode dispatch: %v", err)
	}
	tests := []struct {
		name     string
		progress ChildProgress
		wantKind WorkDecisionKind
		wantCode string
	}{
		{name: "scheduled wrong attempt", progress: ChildProgress{status: ChildScheduled, attempt: 1}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-child-state"},
		{name: "running wrong key", progress: ChildProgress{status: ChildStartRunning, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-child-state"},
		{name: "failed wrong key", progress: ChildProgress{status: ChildStartFailedStatus, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-child-state"},
		{name: "failed exhausted", progress: ChildProgress{status: ChildStartFailedStatus, attempt: 1, idempotencyKey: "child-key", retryable: true}, wantKind: WorkComplete},
		{name: "active wrong key", progress: ChildProgress{status: ChildActive, attempt: 1, idempotencyKey: "other"}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-child-state"},
		{name: "active", progress: ChildProgress{status: ChildActive, attempt: 1, idempotencyKey: "child-key"}, wantKind: WorkComplete},
		{name: "unknown", progress: ChildProgress{status: ChildStartUnknownStatus, attempt: 1, idempotencyKey: "child-key"}, wantKind: WorkComplete},
		{name: "succeeded", progress: ChildProgress{status: ChildSucceeded, attempt: 1, idempotencyKey: "child-key"}, wantKind: WorkComplete},
		{name: "failed child", progress: ChildProgress{status: ChildFailed, attempt: 1, idempotencyKey: "child-key"}, wantKind: WorkComplete},
		{name: "invalid", progress: ChildProgress{}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-child-state"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision, err := processor.processProgress(
				context.Background(), lease, Instance{}, parent, step, dispatch, test.progress,
			)
			if err != nil || decision.Kind() != test.wantKind || decision.Code() != test.wantCode {
				t.Fatalf("decision = %#v, error = %v", decision, err)
			}
		})
	}
}

func TestChildProcessorLoadAndPersistenceFailuresRemainFenced(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 17, 0, 0, 0, time.UTC)
	parent, child, registry := internalChildDefinitions(t)
	base := internalChildInstance(parent, now)
	schedule, err := NewChildSchedule(ChildScheduleSpec{
		TransitionID: "schedule", WorkID: "child-work", ChildID: "child-1",
		Instance: base, Definition: parent, StepName: "child", ScheduledAt: now.Add(time.Second),
		Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("schedule child: %v", err)
	}
	history := []HistoryEvent{
		mustInternalHistoryEvent(t, HistoryEventSpec{
			Sequence: 1, InstanceID: "parent-1", Kind: EventInstanceStarted,
			OccurredAt: now, Definition: parent.Reference(),
		}),
		schedule.Events()[0],
	}
	failure := errors.New("history unavailable")
	store := &internalProcessorStore{historyErr: failure}
	processor, err := NewChildWorkProcessor(ChildWorkProcessorConfig{
		Store: store, Definitions: registry,
		Starter: ChildStartFunc(func(context.Context, ChildStartRequest) ChildStartOutcome {
			return ChildStartOutcome{}
		}),
		Clock: internalProcessorClock{now: now.Add(2 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	})
	if err != nil {
		t.Fatalf("construct processor: %v", err)
	}
	work := schedule.Work()[0]
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("lease child: %v", err)
	}
	if _, err := processor.Process(context.Background(), lease); !errors.Is(err, failure) {
		t.Fatalf("history failure = %v", err)
	}

	store = &internalProcessorStore{
		history: history,
		commits: []error{NewStoreCommitError(StoreCommitNotCommitted, failure)},
	}
	processor.config.Store = store
	if _, err := processor.Process(context.Background(), lease); !errors.Is(err, failure) {
		t.Fatalf("start commit failure = %v", err)
	}

	_ = child
}
