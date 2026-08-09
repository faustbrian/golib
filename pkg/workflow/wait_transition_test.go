package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestTimerScheduleCreatesAtomicDueWorkAndHistory(t *testing.T) {
	t.Parallel()

	definition := mustWaitDefinition(t)
	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	transition, err := workflow.NewTimerSchedule(workflow.TimerScheduleSpec{
		TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1",
		ExpectedSequence: 1, Definition: definition, StepName: "expiry", ScheduledAt: now,
		Deadline: now.Add(time.Hour), TenantID: "tenant-1", CorrelationID: "correlation-1",
	})
	if err != nil {
		t.Fatalf("construct timer schedule: %v", err)
	}
	events := transition.Events()
	work := transition.Work()
	if len(events) != 1 || events[0].Kind() != workflow.EventTimerScheduled ||
		events[0].DueAt() != now.Add(30*time.Second) || len(work) != 1 ||
		work[0].Kind() != workflow.WorkTimer || work[0].AvailableAt() != events[0].DueAt() ||
		string(work[0].Payload()) != "expiry" || work[0].Sequence() != events[0].Sequence() {
		t.Fatalf("timer schedule = events %#v work %#v", events, work)
	}
}

func TestSignalAcceptanceCreatesAnIdempotentDurableTransition(t *testing.T) {
	t.Parallel()

	definition := mustWaitDefinition(t)
	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	transition, err := workflow.NewSignalAcceptance(workflow.SignalAcceptanceSpec{
		InstanceID: "instance-1", ExpectedSequence: 1,
		Definition: definition, StepName: "approved", SignalID: "signal-1",
		ReceivedAt: now, Payload: []byte("1234567890123456"),
	})
	if err != nil {
		t.Fatalf("construct signal acceptance: %v", err)
	}
	events := transition.Events()
	if transition.ID() != "signal-1" || len(events) != 1 || len(transition.Work()) != 0 ||
		events[0].Kind() != workflow.EventSignalReceived || events[0].IdempotencyKey() != "signal-1" ||
		string(events[0].Data()) != "1234567890123456" {
		t.Fatalf("signal transition = %#v events %#v", transition, events)
	}
	payload := events[0].Data()
	payload[0] = 'X'
	if string(transition.Events()[0].Data()) != "1234567890123456" {
		t.Fatal("signal acceptance retained caller-owned payload")
	}
}

func TestTimerFireCreatesThePersistedDecisionBeforeCompletion(t *testing.T) {
	t.Parallel()

	definition := mustWaitDefinition(t)
	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	scheduled, err := workflow.NewTimerSchedule(workflow.TimerScheduleSpec{
		TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1",
		ExpectedSequence: 1, Definition: definition, StepName: "expiry", ScheduledAt: now,
		Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("construct timer schedule: %v", err)
	}
	lease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: scheduled.Work()[0], Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(30 * time.Second), ExpiresAt: now.Add(31 * time.Second),
	})
	if err != nil {
		t.Fatalf("construct timer lease: %v", err)
	}
	fired, err := workflow.NewTimerFire(workflow.TimerFireSpec{
		TransitionID: "transition-timer-fired", Lease: lease, ExpectedSequence: 2,
		Definition: definition, FiredAt: now.Add(30 * time.Second),
	})
	if err != nil {
		t.Fatalf("construct timer fire: %v", err)
	}
	events := fired.Events()
	if len(events) != 1 || events[0].Kind() != workflow.EventTimerFired ||
		events[0].StepName() != "expiry" || events[0].OccurredAt() != now.Add(30*time.Second) ||
		len(fired.Work()) != 0 {
		t.Fatalf("timer fire = %#v", fired)
	}
}

func TestTimerAndSignalBuildersRejectWrongStepsAndBounds(t *testing.T) {
	t.Parallel()

	definition := mustWaitDefinition(t)
	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	if _, err := workflow.NewTimerSchedule(workflow.TimerScheduleSpec{
		TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1",
		ExpectedSequence: 1, Definition: definition, StepName: "approved", ScheduledAt: now,
		Deadline: now.Add(time.Hour),
	}); !errors.Is(err, workflow.ErrInvalidWait) {
		t.Fatalf("wrong timer step error = %v", err)
	}
	if _, err := workflow.NewSignalAcceptance(workflow.SignalAcceptanceSpec{
		InstanceID: "instance-1", ExpectedSequence: 1,
		Definition: definition, StepName: "approved", SignalID: "signal-1",
		ReceivedAt: now, Payload: make([]byte, 17),
	}); !errors.Is(err, workflow.ErrInvalidWait) {
		t.Fatalf("oversized signal error = %v", err)
	}
	invalidTimers := []workflow.TimerScheduleSpec{
		{TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1", ExpectedSequence: 0, Definition: definition, StepName: "expiry", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1", ExpectedSequence: 1, Definition: definition, StepName: "expiry", Deadline: now.Add(time.Hour)},
		{TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1", ExpectedSequence: ^uint64(0), Definition: definition, StepName: "expiry", ScheduledAt: now, Deadline: now.Add(time.Hour)},
		{TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1", ExpectedSequence: 1, Definition: definition, StepName: "expiry", ScheduledAt: now, Deadline: now.Add(30 * time.Second)},
		{TransitionID: " spaces ", WorkID: "timer-work-1", InstanceID: "instance-1", ExpectedSequence: 1, Definition: definition, StepName: "expiry", ScheduledAt: now, Deadline: now.Add(time.Hour)},
	}
	for _, spec := range invalidTimers {
		if _, err := workflow.NewTimerSchedule(spec); !errors.Is(err, workflow.ErrInvalidWait) {
			t.Fatalf("invalid timer schedule error = %v for %#v", err, spec)
		}
	}
	invalidSignals := []workflow.SignalAcceptanceSpec{
		{InstanceID: "instance-1", ExpectedSequence: 0, Definition: definition, StepName: "approved", SignalID: "signal-1", ReceivedAt: now},
		{InstanceID: "instance-1", ExpectedSequence: ^uint64(0), Definition: definition, StepName: "approved", SignalID: "signal-1", ReceivedAt: now},
		{InstanceID: "instance-1", ExpectedSequence: 1, Definition: definition, StepName: "approved", SignalID: "signal-1"},
		{InstanceID: "instance-1", ExpectedSequence: 1, Definition: definition, StepName: "approved", SignalID: " spaces ", ReceivedAt: now},
	}
	for _, spec := range invalidSignals {
		if _, err := workflow.NewSignalAcceptance(spec); !errors.Is(err, workflow.ErrInvalidWait) {
			t.Fatalf("invalid signal acceptance error = %v for %#v", err, spec)
		}
	}
	activityWork, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
		ID: "activity-work-1", Kind: workflow.WorkActivity, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(time.Hour), Payload: []byte("expiry"),
	})
	if err != nil {
		t.Fatalf("construct activity work: %v", err)
	}
	activityLease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: activityWork, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct activity lease: %v", err)
	}
	timerSchedule, err := workflow.NewTimerSchedule(workflow.TimerScheduleSpec{
		TransitionID: "transition-timer", WorkID: "timer-work-1", InstanceID: "instance-1",
		ExpectedSequence: 1, Definition: definition, StepName: "expiry", ScheduledAt: now,
		Deadline: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("construct timer schedule: %v", err)
	}
	timerLease, err := workflow.NewWorkLease(workflow.WorkLeaseSpec{
		Work: timerSchedule.Work()[0], Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now.Add(30 * time.Second), ExpiresAt: now.Add(31 * time.Second),
	})
	if err != nil {
		t.Fatalf("construct timer lease: %v", err)
	}
	invalidFires := []workflow.TimerFireSpec{
		{TransitionID: "transition-fired", Lease: activityLease, ExpectedSequence: 2, Definition: definition, FiredAt: now},
		{TransitionID: "transition-fired", Lease: workflow.WorkLease{}, ExpectedSequence: 2, Definition: definition, FiredAt: now},
		{TransitionID: "transition-fired", Lease: timerLease, ExpectedSequence: 2, Definition: mustDefinition(t, "other", "1"), FiredAt: now.Add(30 * time.Second)},
		{TransitionID: "transition-fired", Lease: timerLease, ExpectedSequence: 1, Definition: definition, FiredAt: now.Add(30 * time.Second)},
		{TransitionID: "transition-fired", Lease: timerLease, ExpectedSequence: 2, Definition: definition, FiredAt: time.Time{}},
		{TransitionID: "transition-fired", Lease: timerLease, ExpectedSequence: 2, Definition: definition, FiredAt: now.Add(time.Hour)},
		{TransitionID: "transition-fired", Lease: timerLease, ExpectedSequence: ^uint64(0), Definition: definition, FiredAt: now.Add(30 * time.Second)},
		{TransitionID: " spaces ", Lease: timerLease, ExpectedSequence: 2, Definition: definition, FiredAt: now.Add(30 * time.Second)},
	}
	for _, spec := range invalidFires {
		if _, err := workflow.NewTimerFire(spec); !errors.Is(err, workflow.ErrInvalidWait) {
			t.Fatalf("invalid timer fire error = %v for %#v", err, spec)
		}
	}
}
