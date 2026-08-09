package workflow_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestCompensationScheduleCreatesOrderedHistoryAndDurableWork(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	transition, err := workflow.NewCompensationSchedule(workflow.CompensationScheduleSpec{
		TransitionID: "compensation-schedule-1", WorkID: "compensation-work-1",
		InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition,
		StepName: "reserve", Attempt: 1, IdempotencyKey: "compensation-attempt-1",
		ScheduledAt: now, Deadline: now.Add(time.Hour), Input: []byte("reservation-1"),
		TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct compensation schedule: %v", err)
	}
	events := transition.Events()
	work := transition.Work()
	if len(events) != 1 || events[0].Kind() != workflow.EventCompensationScheduled ||
		events[0].StepName() != "reserve" || string(events[0].Data()) != "reservation-1" ||
		len(work) != 1 || work[0].Kind() != workflow.WorkCompensation ||
		work[0].Sequence() != events[0].Sequence() || work[0].AvailableAt() != now {
		t.Fatalf("compensation schedule = events %#v work %#v", events, work)
	}
	dispatch, err := workflow.DecodeCompensationDispatch(work[0].Payload())
	if err != nil || dispatch.StepName() != "reserve" || dispatch.Attempt() != 1 ||
		dispatch.IdempotencyKey() != "compensation-attempt-1" {
		t.Fatalf("compensation dispatch = %#v, %v", dispatch, err)
	}
}

func TestCompensationAttemptStartPersistsBeforeExternalExecution(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	scheduled, err := workflow.NewCompensationSchedule(workflow.CompensationScheduleSpec{
		TransitionID: "compensation-schedule-1", WorkID: "compensation-work-1",
		InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition,
		StepName: "reserve", Attempt: 1, IdempotencyKey: "compensation-attempt-1",
		ScheduledAt: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("construct compensation schedule: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: scheduled.Work()[0], Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct compensation lease: %v", err)
	}
	started, err := workflow.NewCompensationAttemptStart(workflow.CompensationAttemptStartSpec{
		TransitionID: "compensation-start-1", Lease: lease, ExpectedSequence: 5,
		Definition: definition, StartedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("construct compensation attempt start: %v", err)
	}
	event := started.Events()[0]
	if event.Kind() != workflow.EventCompensationAttemptStarted || event.Attempt() != 1 ||
		event.IdempotencyKey() != "compensation-attempt-1" ||
		event.DueAt() != now.Add(31*time.Second) || len(started.Work()) != 0 {
		t.Fatalf("compensation attempt start = %#v", started)
	}
}

func TestCompensationBuildersRejectMalformedOrUnboundedDispatch(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	invalid := []workflow.CompensationScheduleSpec{
		{},
		{TransitionID: "schedule-1", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition, StepName: "missing", Attempt: 1, IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "schedule-1", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: 4, Definition: mustUncompensatedReserveDefinition(t), StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "schedule-1", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition, StepName: "reserve", Attempt: 0, IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "schedule-1", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition, StepName: "reserve", Attempt: 2, IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "schedule-1", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition, StepName: "reserve", Attempt: 1, IdempotencyKey: " spaces ", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "schedule-1", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: ^uint64(0), Definition: definition, StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "schedule-1", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition, StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now},
		{TransitionID: " spaces ", WorkID: "work-1", InstanceID: "instance-1", ExpectedSequence: 4, Definition: definition, StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour)},
	}
	for _, spec := range invalid {
		if _, err := workflow.NewCompensationSchedule(spec); !errors.Is(err, workflow.ErrInvalidCompensation) {
			t.Fatalf("invalid compensation schedule error = %v for %#v", err, spec)
		}
	}
	invalidPayloads := [][]byte{
		nil,
		[]byte("not-json"),
		[]byte(`{"step_name":"reserve","attempt":1,"idempotency_key":"attempt-1"}{}`),
		[]byte(`{"step_name":"","attempt":1,"idempotency_key":"attempt-1"}`),
		[]byte(`{"step_name":"reserve","attempt":0,"idempotency_key":"attempt-1"}`),
		[]byte(`{"step_name":"reserve","attempt":1,"idempotency_key":" spaces "}`),
		[]byte(strings.Repeat("x", workflow.MaxCompensationDispatchBytes+1)),
	}
	for _, payload := range invalidPayloads {
		if _, err := workflow.DecodeCompensationDispatch(payload); !errors.Is(err, workflow.ErrInvalidCompensation) {
			t.Fatalf("invalid dispatch error = %v for %q", err, payload)
		}
	}
	maximumPayload := []byte(`{"step_name":"reserve","attempt":1,"idempotency_key":"attempt-1"}`)
	maximumPayload = append(maximumPayload, []byte(strings.Repeat(" ", workflow.MaxCompensationDispatchBytes-len(maximumPayload)))...)
	if _, err := workflow.DecodeCompensationDispatch(maximumPayload); err != nil {
		t.Fatalf("maximum compensation dispatch: %v", err)
	}
	if _, err := workflow.NewCompensationAttemptStart(workflow.CompensationAttemptStartSpec{}); !errors.Is(err, workflow.ErrInvalidCompensation) {
		t.Fatalf("invalid attempt start error = %v", err)
	}
	scheduled, err := workflow.NewCompensationSchedule(workflow.CompensationScheduleSpec{
		TransitionID: "schedule-valid", WorkID: "work-valid", InstanceID: "instance-1",
		ExpectedSequence: 4, Definition: definition, StepName: "reserve", Attempt: 1,
		IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("construct valid schedule: %v", err)
	}
	validLease := mustCompensationLease(t, scheduled.Work()[0], now)
	nonCompensationWork, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "activity-work", Kind: workflow.WorkActivity, InstanceID: "instance-1",
		Sequence: 5, AvailableAt: now, Deadline: now.Add(time.Hour),
		Payload: []byte(`{"step_name":"reserve","attempt":1,"idempotency_key":"attempt-1"}`),
	})
	if err != nil {
		t.Fatalf("construct non-compensation work: %v", err)
	}
	invalidStarts := []workflow.CompensationAttemptStartSpec{
		{TransitionID: "start-1", Lease: mustCompensationLease(t, nonCompensationWork, now), ExpectedSequence: 5, Definition: definition, StartedAt: now},
		{TransitionID: "start-1", Lease: validLease, ExpectedSequence: 4, Definition: definition, StartedAt: now},
		{TransitionID: "start-1", Lease: validLease, ExpectedSequence: 5, Definition: mustDefinition(t, "other", "1"), StartedAt: now},
		{TransitionID: "start-1", Lease: validLease, ExpectedSequence: 5, Definition: mustUncompensatedReserveDefinition(t), StartedAt: now},
		{TransitionID: "start-1", Lease: validLease, ExpectedSequence: 5, Definition: definition, StartedAt: now.Add(-time.Second)},
		{TransitionID: "start-1", Lease: validLease, ExpectedSequence: 5, Definition: definition, StartedAt: now.Add(59*time.Minute + 45*time.Second)},
		{TransitionID: "start-1", Lease: validLease, ExpectedSequence: 5, Definition: definition, StartedAt: now.Add(time.Hour)},
		{TransitionID: "start-1", Lease: validLease, ExpectedSequence: ^uint64(0), Definition: definition, StartedAt: now},
		{TransitionID: " spaces ", Lease: validLease, ExpectedSequence: 5, Definition: definition, StartedAt: now},
		{TransitionID: "start-1", Lease: mustCompensationLease(t, mustCompensationWork(t, now, []byte("bad")), now), ExpectedSequence: 5, Definition: definition, StartedAt: now},
		{TransitionID: "start-1", Lease: mustCompensationLease(t, mustCompensationWork(t, now, []byte(`{"step_name":"reserve","attempt":3,"idempotency_key":"attempt-3"}`)), now), ExpectedSequence: 5, Definition: definition, StartedAt: now},
	}
	for _, spec := range invalidStarts {
		if _, err := workflow.NewCompensationAttemptStart(spec); !errors.Is(err, workflow.ErrInvalidCompensation) {
			t.Fatalf("invalid attempt start error = %v for %#v", err, spec)
		}
	}
	maximumAttemptLease := mustCompensationLease(t, mustCompensationWork(t, now, []byte(`{"step_name":"reserve","attempt":2,"idempotency_key":"attempt-2"}`)), now)
	if _, err := workflow.NewCompensationAttemptStart(workflow.CompensationAttemptStartSpec{
		TransitionID: "start-maximum-attempt", Lease: maximumAttemptLease,
		ExpectedSequence: 5, Definition: definition, StartedAt: now,
	}); err != nil {
		t.Fatalf("maximum compensation attempt: %v", err)
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
