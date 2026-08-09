package workflow_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestActivityTransitionsPersistBeforeDispatchAndExternalExecution(t *testing.T) {
	t.Parallel()

	definition := mustActivityTransitionDefinition(t)
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	startedEvent := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	instance, err := workflow.Replay(registry, []workflow.HistoryEvent{startedEvent})
	if err != nil {
		t.Fatalf("replay instance: %v", err)
	}
	scheduled, err := workflow.NewActivitySchedule(workflow.ActivityScheduleSpec{
		TransitionID: "activity-schedule-1", WorkID: "activity-work-1",
		Instance: instance, Definition: definition,
		StepName: "execute", Attempt: 1, IdempotencyKey: "activity-attempt-1",
		ScheduledAt: now, Deadline: now.Add(time.Hour), Input: []byte("input"),
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("schedule activity: %v", err)
	}
	events := scheduled.Events()
	work := scheduled.Work()
	if len(events) != 1 || events[0].Kind() != workflow.EventActivityScheduled ||
		string(events[0].Data()) != "input" || len(work) != 1 ||
		work[0].Kind() != workflow.WorkActivity || work[0].Sequence() != events[0].Sequence() {
		t.Fatalf("activity schedule = events %#v work %#v", events, work)
	}
	dispatch, err := workflow.DecodeActivityDispatch(work[0].Payload())
	if err != nil || dispatch.StepName() != "execute" || dispatch.Attempt() != 1 ||
		dispatch.IdempotencyKey() != "activity-attempt-1" {
		t.Fatalf("activity dispatch = %#v, %v", dispatch, err)
	}
	lease := mustActivityLease(t, work[0], now)
	ready, err := workflow.Replay(registry, append([]workflow.HistoryEvent{startedEvent}, events...))
	if err != nil {
		t.Fatalf("replay scheduled activity: %v", err)
	}
	started, err := workflow.NewActivityAttemptStart(workflow.ActivityAttemptStartSpec{
		TransitionID: "activity-start-1", Lease: lease, Instance: ready,
		Definition: definition, StartedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("start activity: %v", err)
	}
	start := started.Events()[0]
	if start.Kind() != workflow.EventActivityAttemptStarted || start.Attempt() != 1 ||
		start.IdempotencyKey() != "activity-attempt-1" ||
		start.DueAt() != now.Add(31*time.Second) {
		t.Fatalf("activity start = %#v", start)
	}
}

func TestActivityOutcomeAndRetryPreserveSemanticAttemptIdentity(t *testing.T) {
	t.Parallel()

	definition := mustActivityTransitionDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	history := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "execute", Data: []byte("input")}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "activity-attempt-1", DueAt: now.Add(32 * time.Second)}),
	}
	running, err := workflow.Replay(registry, history)
	if err != nil {
		t.Fatalf("replay running activity: %v", err)
	}
	failure, err := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{
		Kind: workflow.ActivityFailed, Code: "temporary", Retryable: true, Data: []byte("safe"),
	})
	if err != nil {
		t.Fatalf("construct failure: %v", err)
	}
	failedTransition, err := workflow.NewActivityAttemptOutcome(workflow.ActivityAttemptOutcomeSpec{
		TransitionID: "activity-outcome-1", Instance: running, Definition: definition,
		StepName: "execute", Attempt: 1, OccurredAt: now.Add(3 * time.Second), Outcome: failure,
	})
	if err != nil {
		t.Fatalf("persist failure: %v", err)
	}
	failed, err := workflow.Replay(registry, append(history, failedTransition.Events()...))
	if err != nil {
		t.Fatalf("replay failure: %v", err)
	}
	retry, err := workflow.NewActivityRetry(workflow.ActivityRetrySpec{
		TransitionID: "activity-retry-1", WorkID: "activity-work-2",
		Instance: failed, Definition: definition, StepName: "execute",
		IdempotencyKey: "activity-attempt-2", ScheduledAt: now.Add(4 * time.Second),
		Deadline: now.Add(time.Hour), TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("schedule retry: %v", err)
	}
	retryEvent := retry.Events()[0]
	retryWork := retry.Work()[0]
	dispatch, err := workflow.DecodeActivityDispatch(retryWork.Payload())
	if err != nil || retryEvent.Kind() != workflow.EventActivityRetryScheduled ||
		retryEvent.Attempt() != 1 || retryWork.AvailableAt() != now.Add(5*time.Second) ||
		dispatch.Attempt() != 2 || dispatch.IdempotencyKey() != "activity-attempt-2" {
		t.Fatalf("activity retry = event %#v work %#v dispatch %#v error %v", retryEvent, retryWork, dispatch, err)
	}
}

func TestActivityTransitionBuildersRejectInvalidStateAndBoundaries(t *testing.T) {
	t.Parallel()

	definition := mustActivityTransitionDefinition(t)
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	if _, err := workflow.NewActivitySchedule(workflow.ActivityScheduleSpec{}); !errors.Is(err, workflow.ErrInvalidActivityTransition) {
		t.Fatalf("zero schedule error = %v", err)
	}
	if _, err := workflow.DecodeActivityDispatch(nil); !errors.Is(err, workflow.ErrInvalidActivityTransition) {
		t.Fatalf("empty dispatch error = %v", err)
	}
	for _, payload := range [][]byte{
		[]byte("bad"),
		[]byte(`{"step_name":"execute","attempt":1,"idempotency_key":"attempt-1"}{}`),
		[]byte(`{"step_name":"","attempt":1,"idempotency_key":"attempt-1"}`),
		[]byte(`{"step_name":"execute","attempt":0,"idempotency_key":"attempt-1"}`),
		[]byte(`{"step_name":"execute","attempt":1,"idempotency_key":" spaces "}`),
		[]byte(strings.Repeat("x", workflow.MaxActivityDispatchBytes+1)),
	} {
		if _, err := workflow.DecodeActivityDispatch(payload); !errors.Is(err, workflow.ErrInvalidActivityTransition) {
			t.Fatalf("invalid dispatch error = %v for %q", err, payload)
		}
	}
	maximumPayload := []byte(`{"step_name":"execute","attempt":1,"idempotency_key":"attempt-1"}`)
	maximumPayload = append(maximumPayload, []byte(strings.Repeat(" ", workflow.MaxActivityDispatchBytes-len(maximumPayload)))...)
	if _, err := workflow.DecodeActivityDispatch(maximumPayload); err != nil {
		t.Fatalf("maximum activity dispatch: %v", err)
	}
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "activity-work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1",
		Sequence: 2, AvailableAt: now, Deadline: now.Add(time.Hour), Payload: []byte("bad"),
	})
	if err != nil {
		t.Fatalf("construct work: %v", err)
	}
	if _, err := workflow.NewActivityAttemptStart(workflow.ActivityAttemptStartSpec{
		TransitionID: "activity-start-1", Lease: mustActivityLease(t, work, now),
		Definition: definition, StartedAt: now,
	}); !errors.Is(err, workflow.ErrInvalidActivityTransition) {
		t.Fatalf("invalid start error = %v", err)
	}
	if _, err := workflow.NewActivityAttemptOutcome(workflow.ActivityAttemptOutcomeSpec{}); !errors.Is(err, workflow.ErrInvalidActivityTransition) {
		t.Fatalf("zero outcome error = %v", err)
	}
	if _, err := workflow.NewActivityRetry(workflow.ActivityRetrySpec{}); !errors.Is(err, workflow.ErrInvalidActivityTransition) {
		t.Fatalf("zero retry error = %v", err)
	}
}

func mustActivityTransitionDefinition(t *testing.T) workflow.Definition {
	t.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "activity-transitions-v1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute",
			Timeout: 30 * time.Second, InputLimit: 64, ResultLimit: 64,
			Retry: workflow.RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct activity transition definition: %v", err)
	}
	return definition
}

func mustActivityLease(t *testing.T, work workflow.PendingWork, now time.Time) workflow.WorkLease {
	t.Helper()
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct activity lease: %v", err)
	}
	return lease
}
