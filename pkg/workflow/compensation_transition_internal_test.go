package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestCompensationTransitionInternalBoundaries(t *testing.T) {
	t.Parallel()

	definition := internalCompensationTransitionDefinition(t)
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	validSchedule := CompensationScheduleSpec{
		TransitionID: "schedule-1", WorkID: "work-1",
		Instance:       internalCompensationCandidate(definition, now),
		Definition:     definition,
		StepName:       "reserve",
		Attempt:        1,
		IdempotencyKey: "attempt-1",
		ScheduledAt:    now,
		Deadline:       now.Add(time.Hour),
	}
	for _, spec := range []CompensationScheduleSpec{
		{},
		func() CompensationScheduleSpec { value := validSchedule; value.StepName = "missing"; return value }(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.Definition = internalUncompensatedDefinition(t)
			return value
		}(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.Instance.status = StatusPaused
			return value
		}(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
			return value
		}(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.Instance.activities = map[string]ActivityProgress{}
			return value
		}(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.Instance.activities = map[string]ActivityProgress{"reserve": {stepName: "reserve", status: ActivityProgressFailed}}
			return value
		}(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.Instance.compensations = map[string]CompensationProgress{"reserve": {stepName: "reserve"}}
			return value
		}(),
		func() CompensationScheduleSpec { value := validSchedule; value.Attempt = 2; return value }(),
		func() CompensationScheduleSpec { value := validSchedule; value.Input = make([]byte, 65); return value }(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.IdempotencyKey = " spaces "
			return value
		}(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.ScheduledAt = now.Add(-time.Second)
			return value
		}(),
		func() CompensationScheduleSpec {
			value := validSchedule
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
		func() CompensationScheduleSpec { value := validSchedule; value.Deadline = now; return value }(),
		func() CompensationScheduleSpec { value := validSchedule; value.TransitionID = " spaces "; return value }(),
	} {
		if _, err := NewCompensationSchedule(spec); !errors.Is(err, ErrInvalidCompensation) {
			t.Fatalf("invalid compensation schedule error = %v for %#v", err, spec)
		}
	}
	maximumSchedule := validSchedule
	maximumSchedule.TransitionID = "schedule-maximum"
	maximumSchedule.WorkID = "work-maximum"
	maximumSchedule.Input = make([]byte, 64)
	if _, err := NewCompensationSchedule(maximumSchedule); err != nil {
		t.Fatalf("maximum compensation input: %v", err)
	}

	work := internalCompensationWork(t, now, WorkCompensation, encodeCompensationDispatch("reserve", 1, "attempt-1"))
	lease := internalCompensationLease(t, work, now)
	validStart := CompensationAttemptStartSpec{
		TransitionID: "start-1", Lease: lease,
		Instance:   internalCompensationProgressInstance(definition, now, CompensationReady, 0, false),
		Definition: definition, StartedAt: now,
	}
	validStart.Instance.sequence = 5
	for _, spec := range []CompensationAttemptStartSpec{
		{},
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Lease = internalCompensationLease(t, internalCompensationWork(t, now, WorkActivity, encodeCompensationDispatch("reserve", 1, "attempt-1")), now)
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Lease = internalCompensationLease(t, internalCompensationWork(t, now, WorkCompensation, []byte("bad")), now)
			return value
		}(),
		func() CompensationAttemptStartSpec { value := validStart; value.Instance.sequence = 4; return value }(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.id = "other-instance"
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Definition = Definition{}
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Definition = internalUncompensatedDefinition(t)
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.status = StatusPaused
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.compensations = map[string]CompensationProgress{}
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.compensations = map[string]CompensationProgress{"reserve": {stepName: "reserve", status: CompensationSucceeded}}
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.compensations = map[string]CompensationProgress{"reserve": {stepName: "reserve", status: CompensationReady, attempt: 1}}
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Lease = internalCompensationLease(t, internalCompensationWork(t, now, WorkCompensation, encodeCompensationDispatch("reserve", 3, "attempt-3")), now)
			value.Instance.compensations = map[string]CompensationProgress{
				"reserve": {stepName: "reserve", status: CompensationRetryWaiting, attempt: 2, dueAt: now},
			}
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.StartedAt = now.Add(time.Hour)
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.StartedAt = now.Add(59*time.Minute + 45*time.Second)
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.StartedAt = now.Add(time.Minute)
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Lease = internalCompensationLease(t, work, now.Add(time.Second))
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.updatedAt = now.Add(time.Second)
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.compensations = map[string]CompensationProgress{
				"reserve": {stepName: "reserve", status: CompensationRetryWaiting, dueAt: now.Add(time.Second)},
			}
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
		func() CompensationAttemptStartSpec {
			value := validStart
			value.TransitionID = " spaces "
			return value
		}(),
	} {
		if _, err := NewCompensationAttemptStart(spec); !errors.Is(err, ErrInvalidCompensation) {
			t.Fatalf("invalid compensation start error = %v for %#v", err, spec)
		}
	}
	maximumAttempt := validStart
	maximumAttempt.TransitionID = "start-maximum"
	maximumAttempt.Lease = internalCompensationLease(t, internalCompensationWork(t, now, WorkCompensation, encodeCompensationDispatch("reserve", 2, "attempt-2")), now)
	maximumAttempt.Instance = internalCompensationProgressInstance(definition, now, CompensationRetryWaiting, 1, false)
	maximumAttempt.Instance.sequence = 5
	maximumAttempt.Instance.compensations["reserve"] = CompensationProgress{
		stepName: "reserve", status: CompensationRetryWaiting, attempt: 1, dueAt: now,
	}
	if _, err := NewCompensationAttemptStart(maximumAttempt); err != nil {
		t.Fatalf("maximum compensation attempt: %v", err)
	}

	running := internalCompensationProgressInstance(definition, now, CompensationRunning, 1, false)
	succeeded, _ := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivitySucceeded, Data: []byte("ok")})
	unknown, _ := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivityUnknown, Code: "unknown"})
	for index, outcome := range []ActivityOutcome{succeeded, unknown} {
		transition, err := NewCompensationAttemptOutcome(CompensationAttemptOutcomeSpec{
			TransitionID: "outcome-" + string(rune('1'+index)), Instance: running,
			Definition: definition, StepName: "reserve", Attempt: 1,
			OccurredAt: now, Outcome: outcome,
		})
		if err != nil || transition.Events()[0].Kind() != compensationOutcomeEventKind(outcome.kind) {
			t.Fatalf("compensation outcome %d = %#v, %v", index, transition, err)
		}
	}
	if compensationOutcomeEventKind(ActivityOutcomeKind(255)) != 0 {
		t.Fatal("invalid compensation outcome mapped to an event")
	}
	validOutcome := CompensationAttemptOutcomeSpec{
		TransitionID: "outcome-1", Instance: running, Definition: definition,
		StepName: "reserve", Attempt: 1, OccurredAt: now, Outcome: succeeded,
	}
	maximumOutcome := validOutcome
	maximumOutcome.TransitionID = "outcome-maximum"
	maximumOutcome.Outcome = ActivityOutcome{kind: ActivitySucceeded, data: make([]byte, 64)}
	if _, err := NewCompensationAttemptOutcome(maximumOutcome); err != nil {
		t.Fatalf("maximum compensation outcome: %v", err)
	}
	for _, spec := range []CompensationAttemptOutcomeSpec{
		func() CompensationAttemptOutcomeSpec { value := validOutcome; value.StepName = "missing"; return value }(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.Definition = internalUncompensatedDefinition(t)
			return value
		}(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.Instance.compensations = map[string]CompensationProgress{}
			return value
		}(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
			return value
		}(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.Instance.compensations = map[string]CompensationProgress{"reserve": {stepName: "reserve", status: CompensationFailed, attempt: 1}}
			return value
		}(),
		func() CompensationAttemptOutcomeSpec { value := validOutcome; value.Attempt = 2; return value }(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.Outcome = ActivityOutcome{}
			return value
		}(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.Outcome = ActivityOutcome{kind: ActivitySucceeded, data: make([]byte, 65)}
			return value
		}(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.OccurredAt = now.Add(-time.Second)
			return value
		}(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
		func() CompensationAttemptOutcomeSpec {
			value := validOutcome
			value.TransitionID = " spaces "
			return value
		}(),
	} {
		if _, err := NewCompensationAttemptOutcome(spec); !errors.Is(err, ErrInvalidCompensation) {
			t.Fatalf("invalid compensation outcome error = %v for %#v", err, spec)
		}
	}

	failed := internalCompensationProgressInstance(definition, now, CompensationFailed, 1, true)
	validRetry := CompensationRetrySpec{
		TransitionID: "retry-1", WorkID: "work-2", Instance: failed, Definition: definition,
		StepName: "reserve", IdempotencyKey: "attempt-2", ScheduledAt: now, Deadline: now.Add(time.Hour),
	}
	for _, spec := range []CompensationRetrySpec{
		func() CompensationRetrySpec { value := validRetry; value.StepName = "missing"; return value }(),
		func() CompensationRetrySpec {
			value := validRetry
			value.Definition = internalUncompensatedDefinition(t)
			return value
		}(),
		func() CompensationRetrySpec {
			value := validRetry
			value.Instance.compensations = map[string]CompensationProgress{}
			return value
		}(),
		func() CompensationRetrySpec {
			value := validRetry
			value.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
			return value
		}(),
		func() CompensationRetrySpec { value := validRetry; value.Instance.status = StatusPaused; return value }(),
		func() CompensationRetrySpec {
			value := validRetry
			value.Instance = internalCompensationProgressInstance(definition, now, CompensationRunning, 1, true)
			return value
		}(),
		func() CompensationRetrySpec {
			value := validRetry
			value.Instance = internalCompensationProgressInstance(definition, now, CompensationFailed, 1, false)
			return value
		}(),
		func() CompensationRetrySpec {
			value := validRetry
			value.Instance = internalCompensationProgressInstance(definition, now, CompensationFailed, 2, true)
			return value
		}(),
		func() CompensationRetrySpec { value := validRetry; value.IdempotencyKey = " spaces "; return value }(),
		func() CompensationRetrySpec {
			value := validRetry
			value.ScheduledAt = now.Add(-time.Second)
			return value
		}(),
		func() CompensationRetrySpec { value := validRetry; value.Deadline = now.Add(time.Second); return value }(),
		func() CompensationRetrySpec { value := validRetry; value.Instance.sequence = ^uint64(0); return value }(),
		func() CompensationRetrySpec { value := validRetry; value.TransitionID = " spaces "; return value }(),
	} {
		if _, err := NewCompensationRetry(spec); !errors.Is(err, ErrInvalidCompensation) {
			t.Fatalf("invalid compensation retry error = %v for %#v", err, spec)
		}
	}
}

func internalCompensationTransitionDefinition(t *testing.T) Definition {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: "orders", Version: "compensation-transitions-v1", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "reserve", Kind: StepActivity, Target: "inventory.reserve",
			Timeout: time.Minute, InputLimit: 64, ResultLimit: 64,
			Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
			Compensation: &CompensationSpec{
				Target: "inventory.release", Timeout: 30 * time.Second, ResultLimit: 64,
				Retry: RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
			},
		}},
	})
	if err != nil {
		t.Fatalf("construct compensation transition definition: %v", err)
	}
	return definition
}

func internalUncompensatedDefinition(t *testing.T) Definition {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: "orders", Version: "uncompensated-v1", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "reserve", Kind: StepActivity, Target: "inventory.reserve", Timeout: time.Minute,
			InputLimit: 64, ResultLimit: 64,
			Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct uncompensated definition: %v", err)
	}
	return definition
}

func internalCompensationCandidate(definition Definition, now time.Time) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 4, updatedAt: now,
		activities: map[string]ActivityProgress{
			"reserve": {stepName: "reserve", status: ActivityProgressSucceeded},
		},
		compensations: make(map[string]CompensationProgress),
	}
}

func internalCompensationProgressInstance(
	definition Definition,
	now time.Time,
	status CompensationProgressStatus,
	attempt uint32,
	retryable bool,
) Instance {
	instance := internalCompensationCandidate(definition, now)
	instance.sequence = 7
	instance.compensations = map[string]CompensationProgress{
		"reserve": {stepName: "reserve", status: status, attempt: attempt, retryable: retryable},
	}
	return instance
}

func internalCompensationWork(t *testing.T, now time.Time, kind WorkKind, payload []byte) PendingWork {
	t.Helper()
	work, err := NewPendingWork(PendingWorkSpec{
		ID: "work-1", Kind: kind, InstanceID: "instance-1", Sequence: 5,
		AvailableAt: now, Deadline: now.Add(time.Hour), Payload: payload,
	})
	if err != nil {
		t.Fatalf("construct compensation work: %v", err)
	}
	return work
}

func internalCompensationLease(t *testing.T, work PendingWork, now time.Time) WorkLease {
	t.Helper()
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct compensation lease: %v", err)
	}
	return lease
}
