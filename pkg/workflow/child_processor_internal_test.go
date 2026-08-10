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
	parentForConfig, _, definitionsForConfig := internalChildDefinitions(t)
	_ = parentForConfig
	validConfig := ChildWorkProcessorConfig{
		Store: &childProcessorBoundaryStore{}, Definitions: definitionsForConfig,
		Starter: ChildStartFunc(func(context.Context, ChildStartRequest) ChildStartOutcome {
			outcome, _ := NewChildStartOutcome(ChildStartOutcomeSpec{Kind: ChildStarted})
			return outcome
		}),
		Clock:    internalProcessorClock{now: time.Date(2036, 8, 11, 15, 30, 0, 0, time.UTC)},
		PageSize: 10, MaxHistoryEvents: 100,
	}
	invalidConfigs := []ChildWorkProcessorConfig{
		func() ChildWorkProcessorConfig { value := validConfig; value.Store = nil; return value }(),
		func() ChildWorkProcessorConfig { value := validConfig; value.Definitions = nil; return value }(),
		func() ChildWorkProcessorConfig { value := validConfig; value.Starter = nil; return value }(),
		func() ChildWorkProcessorConfig { value := validConfig; value.Clock = nil; return value }(),
		func() ChildWorkProcessorConfig { value := validConfig; value.PageSize = 0; return value }(),
	}
	for index, config := range invalidConfigs {
		if processor, err := NewChildWorkProcessor(config); processor != nil ||
			!errors.Is(err, ErrInvalidChildProcessor) {
			t.Fatalf("invalid config %d = %#v, %v", index, processor, err)
		}
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
	nilProcessor = nil
	if _, err := nilProcessor.Process(context.Background(), lease); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("nil processor with valid lease error = %v", err)
	}
	processor = &ChildWorkProcessor{}
	if _, err := processor.Process(nil, lease); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("nil context with valid lease error = %v", err)
	}
	if _, err := processor.Process(context.Background(), WorkLease{}); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("invalid lease error = %v", err)
	}
	activityWork, err := NewPendingWork(PendingWorkSpec{
		ID: "activity-work", Kind: WorkActivity, InstanceID: "parent-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(time.Hour), Payload: []byte("activity"),
	})
	if err != nil {
		t.Fatalf("construct wrong-kind work: %v", err)
	}
	activityLease, err := NewWorkLease(WorkLeaseSpec{
		Work: activityWork, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct wrong-kind lease: %v", err)
	}
	if _, err := processor.Process(context.Background(), activityLease); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("wrong work kind error = %v", err)
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
		{name: "succeeded", progress: ChildProgress{status: ChildSucceeded, childID: "child-1", definition: child.Reference(), attempt: 1, idempotencyKey: "child-key"}, wantKind: WorkComplete},
		{name: "failed child", progress: ChildProgress{status: ChildFailed, childID: "child-1", definition: child.Reference(), attempt: 1, idempotencyKey: "child-key"}, wantKind: WorkComplete},
		{name: "legacy succeeded", progress: ChildProgress{status: ChildSucceeded, childID: "child-1", definition: child.Reference()}, wantKind: WorkComplete},
		{name: "terminal wrong child", progress: ChildProgress{status: ChildSucceeded, childID: "other", definition: child.Reference()}, wantKind: WorkDeadLetterDecision, wantCode: "invalid-child-state"},
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

func TestChildProcessorTreatsPreInvocationCancellationAsKnownAbsent(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 16, 30, 0, 0, time.UTC)
	parent, child, _ := internalChildDefinitions(t)
	request, err := NewChildStartRequest(ChildStartRequestSpec{
		ParentInstanceID: "parent-1", ParentDefinition: parent.Reference(),
		StepName: "child", ChildID: "child-1", ChildDefinition: child.Reference(),
		Attempt: 1, MaxAttempts: 1, IdempotencyKey: "child-1",
		StartedAt: now, Deadline: now.Add(time.Minute), InputLimit: 8,
	})
	if err != nil {
		t.Fatalf("construct child request: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := executeChildStartSafely(ctx, ChildStartFunc(
		func(context.Context, ChildStartRequest) ChildStartOutcome {
			t.Fatal("cancelled child start invoked adapter")
			return ChildStartOutcome{}
		},
	), request)
	if outcome.Kind() != ChildStartFailed || outcome.Code() != "child-start-context-done" ||
		!outcome.Retryable() {
		t.Fatalf("cancelled child outcome = %#v", outcome)
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

func TestChildProcessorCoversPoisonAndStoreFailureBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Date(2036, 8, 11, 19, 0, 0, 0, time.UTC)
	parent, child, registry := internalChildRetryDefinitions(t)
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
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: schedule.Work()[0], Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("lease child: %v", err)
	}
	dispatch, _ := DecodeChildDispatch(lease.Work().Payload())
	scheduled := base
	scheduled.sequence = 2
	scheduled.updatedAt = now.Add(time.Second)
	scheduled.children["child"] = ChildProgress{
		stepName: "child", childID: "child-1", definition: child.Reference(),
		status: ChildScheduled,
	}
	running := scheduled
	running.sequence = 3
	running.updatedAt = now.Add(2 * time.Second)
	running.children = map[string]ChildProgress{"child": {
		stepName: "child", childID: "child-1", definition: child.Reference(),
		status: ChildStartRunning, attempt: 1, idempotencyKey: "child-1",
		dueAt: now.Add(62 * time.Second),
	}}
	failed := running
	failed.sequence = 4
	failed.updatedAt = now.Add(3 * time.Second)
	failed.children = map[string]ChildProgress{"child": {
		stepName: "child", childID: "child-1", definition: child.Reference(),
		status: ChildStartFailedStatus, attempt: 1, idempotencyKey: "child-1",
		retryable: true,
	}}
	step := parent.Steps()[0]
	started, _ := NewChildStartOutcome(ChildStartOutcomeSpec{Kind: ChildStarted})
	knownFailure := errors.New("store failed")

	store := &internalProcessorStore{history: history}
	processor := &ChildWorkProcessor{config: ChildWorkProcessorConfig{
		Store: store, Definitions: registry,
		Starter: ChildStartFunc(func(context.Context, ChildStartRequest) ChildStartOutcome { return started }),
		Clock:   internalProcessorClock{now: now.Add(2 * time.Second)}, PageSize: 10, MaxHistoryEvents: 100,
	}}
	missingDispatch := encodeChildDispatch("missing", "child-1", child.Reference(), 1, "child-1")
	missingWork, _ := NewPendingWork(PendingWorkSpec{
		ID: "missing-definition", Kind: WorkChild, InstanceID: "parent-1", Sequence: 2,
		AvailableAt: now.Add(time.Second), Deadline: now.Add(time.Hour), Payload: missingDispatch,
	})
	missingLease, _ := NewWorkLease(WorkLeaseSpec{
		Work: missingWork, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if decision, err := processor.Process(context.Background(), missingLease); err != nil ||
		decision.Code() != "invalid-child-definition" {
		t.Fatalf("missing definition decision = %#v, %v", decision, err)
	}
	otherChild := mustInternalDefinition(t, "other-child", "1")
	mismatchedWork, _ := NewPendingWork(PendingWorkSpec{
		ID: "mismatched-definition", Kind: WorkChild, InstanceID: "parent-1", Sequence: 2,
		AvailableAt: now.Add(time.Second), Deadline: now.Add(time.Hour),
		Payload: encodeChildDispatch("child", "child-1", otherChild.Reference(), 1, "child-1"),
	})
	mismatchedLease, _ := NewWorkLease(WorkLeaseSpec{
		Work: mismatchedWork, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if decision, err := processor.Process(context.Background(), mismatchedLease); err != nil ||
		decision.Code() != "invalid-child-definition" {
		t.Fatalf("mismatched definition decision = %#v, %v", decision, err)
	}

	startOnlyStore := &internalProcessorStore{history: history[:1]}
	processor.config.Store = startOnlyStore
	workAtStart, _ := NewPendingWork(PendingWorkSpec{
		ID: "missing-state", Kind: WorkChild, InstanceID: "parent-1", Sequence: 1,
		AvailableAt: now, Deadline: now.Add(time.Hour),
		Payload: encodeChildDispatch("child", "child-1", child.Reference(), 1, "child-1"),
	})
	leaseAtStart, _ := NewWorkLease(WorkLeaseSpec{
		Work: workAtStart, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(2 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if decision, err := processor.Process(context.Background(), leaseAtStart); err != nil ||
		decision.Code() != "invalid-child-state" {
		t.Fatalf("missing state decision = %#v, %v", decision, err)
	}

	processor.config.Store = &childProcessorBoundaryStore{historyErr: knownFailure}
	if _, err := processor.execute(
		context.Background(), lease, scheduled, parent, step, dispatch,
	); !errors.Is(err, knownFailure) {
		t.Fatalf("post-start inspection failure = %v", err)
	}
	processor.config.Store = &internalProcessorStore{history: history, commits: []error{nil}}
	invalidStep := step
	invalidStep.InputLimit = 0
	if _, err := processor.execute(
		context.Background(), lease, scheduled, parent, invalidStep, dispatch,
	); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("invalid request error = %v", err)
	}
	if _, err := processor.execute(
		context.Background(), lease, Instance{}, parent, step, dispatch,
	); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("invalid start transition error = %v", err)
	}

	processor.config.Clock = internalProcessorClock{now: now}
	processor.config.Store = &childProcessorBoundaryStore{}
	if decision, err := processor.persistOutcome(
		context.Background(), lease.Work(), running, parent, dispatch, started,
	); err != nil || decision.Kind() != WorkComplete {
		t.Fatalf("clamped outcome decision = %#v, %v", decision, err)
	}
	if _, err := processor.persistOutcome(
		context.Background(), lease.Work(), scheduled, parent, dispatch, started,
	); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("invalid persisted outcome error = %v", err)
	}
	processor.config.Clock = internalProcessorClock{now: now.Add(3 * time.Second)}
	processor.config.Store = &childProcessorBoundaryStore{commitErr: knownFailure}
	if _, err := processor.persistOutcome(
		context.Background(), lease.Work(), running, parent, dispatch, started,
	); !errors.Is(err, knownFailure) {
		t.Fatalf("outcome commit failure = %v", err)
	}
	failedOutcome, _ := NewChildStartOutcome(ChildStartOutcomeSpec{
		Kind: ChildStartFailed, Code: "temporary", Retryable: true,
	})
	processor.config.Store = &childProcessorBoundaryStore{historyErr: knownFailure}
	if _, err := processor.persistOutcome(
		context.Background(), lease.Work(), running, parent, dispatch, failedOutcome,
	); !errors.Is(err, knownFailure) {
		t.Fatalf("failed-outcome inspection failure = %v", err)
	}

	processor.config.Store = &childProcessorBoundaryStore{}
	processor.config.Clock = internalProcessorClock{now: now}
	if decision, err := processor.scheduleRetry(
		context.Background(), lease.Work(), failed, parent, step, dispatch,
	); err != nil || decision.Kind() != WorkComplete {
		t.Fatalf("clamped retry decision = %#v, %v", decision, err)
	}
	shortWork, _ := NewPendingWork(PendingWorkSpec{
		ID: "short-deadline", Kind: WorkChild, InstanceID: "parent-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(4 * time.Second), Payload: lease.Work().Payload(),
	})
	processor.config.Clock = internalProcessorClock{now: now.Add(3 * time.Second)}
	if decision, err := processor.scheduleRetry(
		context.Background(), shortWork, failed, parent, step, dispatch,
	); err != nil || decision.Kind() != WorkComplete {
		t.Fatalf("deadline retry decision = %#v, %v", decision, err)
	}
	if _, err := processor.scheduleRetry(
		context.Background(), lease.Work(), failed, Definition{}, step, dispatch,
	); !errors.Is(err, ErrInvalidChildProcessor) {
		t.Fatalf("invalid retry transition error = %v", err)
	}
	exhaustedFailed := failed
	exhaustedFailed.children = map[string]ChildProgress{"child": cloneChildProgress(failed.children["child"])}
	exhaustedProgress := exhaustedFailed.children["child"]
	exhaustedProgress.attempt = step.Retry.MaxAttempts
	exhaustedFailed.children["child"] = exhaustedProgress
	processor.config.Store = &childProcessorBoundaryStore{commitErr: knownFailure}
	if decision, err := processor.scheduleRetry(
		context.Background(), lease.Work(), exhaustedFailed, parent, step, dispatch,
	); err != nil || decision.Kind() != WorkComplete {
		t.Fatalf("exhausted retry decision = %#v, %v", decision, err)
	}
	processor.config.Store = &childProcessorBoundaryStore{commitErr: knownFailure}
	if _, err := processor.scheduleRetry(
		context.Background(), lease.Work(), failed, parent, step, dispatch,
	); !errors.Is(err, knownFailure) {
		t.Fatalf("retry commit failure = %v", err)
	}
}

type childProcessorBoundaryStore struct {
	commitErr  error
	historyErr error
}

func (store *childProcessorBoundaryStore) Commit(context.Context, Transition) error {
	return store.commitErr
}

func (store *childProcessorBoundaryStore) History(context.Context, HistoryQuery) (HistoryPage, error) {
	return HistoryPage{}, store.historyErr
}

func (*childProcessorBoundaryStore) ReconcileTransition(
	context.Context,
	TransitionReconciliation,
) (TransitionReconciliationOutcome, error) {
	return TransitionMissing, nil
}
