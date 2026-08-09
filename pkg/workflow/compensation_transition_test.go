package workflow_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestCompensationTransitionsPersistIndependentRetryLifecycle(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	history := successfulActivityHistory(t, definition, now)
	instance := replayCompensationTransition(t, registry, history)

	scheduled, err := workflow.NewCompensationSchedule(workflow.CompensationScheduleSpec{
		TransitionID: "compensation-schedule-1", WorkID: "compensation-work-1",
		Instance: instance, Definition: definition, StepName: "reserve", Attempt: 1,
		IdempotencyKey: "compensation-attempt-1", ScheduledAt: now.Add(4 * time.Second),
		Deadline: now.Add(time.Hour), Input: []byte("reservation-1"),
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct compensation schedule: %v", err)
	}
	events := scheduled.Events()
	work := scheduled.Work()
	if len(events) != 1 || events[0].Kind() != workflow.EventCompensationScheduled ||
		events[0].StepName() != "reserve" || string(events[0].Data()) != "reservation-1" ||
		len(work) != 1 || work[0].Kind() != workflow.WorkCompensation ||
		work[0].Sequence() != events[0].Sequence() || work[0].AvailableAt() != now.Add(4*time.Second) {
		t.Fatalf("compensation schedule = events %#v work %#v", events, work)
	}
	dispatch, err := workflow.DecodeCompensationDispatch(work[0].Payload())
	if err != nil || dispatch.StepName() != "reserve" || dispatch.Attempt() != 1 ||
		dispatch.IdempotencyKey() != "compensation-attempt-1" {
		t.Fatalf("compensation dispatch = %#v, %v", dispatch, err)
	}

	history = append(history, events...)
	ready := replayCompensationTransition(t, registry, history)
	lease := mustCompensationLease(t, work[0], now.Add(4*time.Second))
	started, err := workflow.NewCompensationAttemptStart(workflow.CompensationAttemptStartSpec{
		TransitionID: "compensation-start-1", Lease: lease, Instance: ready,
		Definition: definition, StartedAt: now.Add(5 * time.Second),
	})
	if err != nil {
		t.Fatalf("construct compensation attempt start: %v", err)
	}
	startEvent := started.Events()[0]
	if startEvent.Kind() != workflow.EventCompensationAttemptStarted || startEvent.Attempt() != 1 ||
		startEvent.IdempotencyKey() != "compensation-attempt-1" ||
		startEvent.DueAt() != now.Add(35*time.Second) || len(started.Work()) != 0 {
		t.Fatalf("compensation attempt start = %#v", started)
	}

	history = append(history, started.Events()...)
	running := replayCompensationTransition(t, registry, history)
	failure, err := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{
		Kind: workflow.ActivityFailed, Code: "temporary", Retryable: true, Data: []byte("safe"),
	})
	if err != nil {
		t.Fatalf("construct compensation failure: %v", err)
	}
	failedTransition, err := workflow.NewCompensationAttemptOutcome(workflow.CompensationAttemptOutcomeSpec{
		TransitionID: "compensation-outcome-1", Instance: running, Definition: definition,
		StepName: "reserve", Attempt: 1, OccurredAt: now.Add(6 * time.Second), Outcome: failure,
	})
	if err != nil {
		t.Fatalf("persist compensation failure: %v", err)
	}
	history = append(history, failedTransition.Events()...)
	failed := replayCompensationTransition(t, registry, history)
	retry, err := workflow.NewCompensationRetry(workflow.CompensationRetrySpec{
		TransitionID: "compensation-retry-1", WorkID: "compensation-work-2",
		Instance: failed, Definition: definition, StepName: "reserve",
		IdempotencyKey: "compensation-attempt-2", ScheduledAt: now.Add(7 * time.Second),
		Deadline: now.Add(time.Hour), TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("schedule compensation retry: %v", err)
	}
	retryEvent := retry.Events()[0]
	retryWork := retry.Work()[0]
	retryDispatch, err := workflow.DecodeCompensationDispatch(retryWork.Payload())
	if err != nil || retryEvent.Kind() != workflow.EventCompensationRetryScheduled ||
		retryEvent.Attempt() != 1 || retryWork.AvailableAt() != now.Add(8*time.Second) ||
		retryDispatch.Attempt() != 2 || retryDispatch.IdempotencyKey() != "compensation-attempt-2" {
		t.Fatalf("compensation retry = event %#v work %#v dispatch %#v error %v", retryEvent, retryWork, retryDispatch, err)
	}
}

func TestCompensationBuildersRejectMalformedOrUnboundedInput(t *testing.T) {
	t.Parallel()

	if _, err := workflow.NewCompensationSchedule(workflow.CompensationScheduleSpec{}); !errors.Is(err, workflow.ErrInvalidCompensation) {
		t.Fatalf("zero schedule error = %v", err)
	}
	if _, err := workflow.NewCompensationAttemptStart(workflow.CompensationAttemptStartSpec{}); !errors.Is(err, workflow.ErrInvalidCompensation) {
		t.Fatalf("zero start error = %v", err)
	}
	if _, err := workflow.NewCompensationAttemptOutcome(workflow.CompensationAttemptOutcomeSpec{}); !errors.Is(err, workflow.ErrInvalidCompensation) {
		t.Fatalf("zero outcome error = %v", err)
	}
	if _, err := workflow.NewCompensationRetry(workflow.CompensationRetrySpec{}); !errors.Is(err, workflow.ErrInvalidCompensation) {
		t.Fatalf("zero retry error = %v", err)
	}
	for _, payload := range [][]byte{
		nil,
		[]byte("not-json"),
		[]byte(`{"step_name":"reserve","attempt":1,"idempotency_key":"attempt-1"}{}`),
		[]byte(`{"step_name":"","attempt":1,"idempotency_key":"attempt-1"}`),
		[]byte(`{"step_name":"reserve","attempt":0,"idempotency_key":"attempt-1"}`),
		[]byte(`{"step_name":"reserve","attempt":1,"idempotency_key":" spaces "}`),
		[]byte(strings.Repeat("x", workflow.MaxCompensationDispatchBytes+1)),
	} {
		if _, err := workflow.DecodeCompensationDispatch(payload); !errors.Is(err, workflow.ErrInvalidCompensation) {
			t.Fatalf("invalid dispatch error = %v for %q", err, payload)
		}
	}
	maximumPayload := []byte(`{"step_name":"reserve","attempt":1,"idempotency_key":"attempt-1"}`)
	maximumPayload = append(maximumPayload, []byte(strings.Repeat(" ", workflow.MaxCompensationDispatchBytes-len(maximumPayload)))...)
	if _, err := workflow.DecodeCompensationDispatch(maximumPayload); err != nil {
		t.Fatalf("maximum compensation dispatch: %v", err)
	}
}

func mustUncompensatedReserveDefinition(t *testing.T) workflow.Definition {
	t.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "uncompensated-v1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "reserve", Kind: workflow.StepActivity, Target: "inventory.reserve",
			Timeout: time.Minute, InputLimit: 64, ResultLimit: 64,
			Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct uncompensated definition: %v", err)
	}
	return definition
}

func mustCompensationWork(t *testing.T, now time.Time, payload []byte) workflow.PendingWork {
	t.Helper()
	work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "compensation-work", Kind: workflow.WorkCompensation, InstanceID: "instance-1",
		Sequence: 5, AvailableAt: now, Deadline: now.Add(time.Hour), Payload: payload,
	})
	if err != nil {
		t.Fatalf("construct compensation work: %v", err)
	}
	return work
}

func mustCompensationLease(t *testing.T, work workflow.PendingWork, now time.Time) workflow.WorkLease {
	t.Helper()
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct compensation lease: %v", err)
	}
	return lease
}

func replayCompensationTransition(t *testing.T, registry *workflow.Registry, events []workflow.HistoryEvent) workflow.Instance {
	t.Helper()
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay compensation transition: %v", err)
	}
	return instance
}
