package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestActivityTransitionInternalFailureBoundaries(t *testing.T) {
	t.Parallel()

	definition := internalActivityTransitionDefinition(t)
	now := time.Date(2026, 8, 9, 19, 0, 0, 0, time.UTC)
	validSchedule := ActivityScheduleSpec{
		TransitionID: "schedule-1", WorkID: "work-1",
		Instance: Instance{
			id: "instance-1", definition: definition.Reference(), status: StatusRunning,
			sequence: 1, updatedAt: now, activities: make(map[string]ActivityProgress),
		},
		Definition: definition, StepName: "execute", Attempt: 1,
		IdempotencyKey: "attempt-1", ScheduledAt: now, Deadline: now.Add(time.Hour),
	}
	for _, spec := range []ActivityScheduleSpec{
		func() ActivityScheduleSpec { value := validSchedule; value.Attempt = 2; return value }(),
		func() ActivityScheduleSpec { value := validSchedule; value.Input = make([]byte, 65); return value }(),
		func() ActivityScheduleSpec { value := validSchedule; value.IdempotencyKey = " spaces "; return value }(),
		func() ActivityScheduleSpec {
			value := validSchedule
			value.Instance.status = StatusPaused
			return value
		}(),
		func() ActivityScheduleSpec {
			value := validSchedule
			value.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
			return value
		}(),
		func() ActivityScheduleSpec {
			value := validSchedule
			value.Instance.activities = map[string]ActivityProgress{"execute": {}}
			return value
		}(),
		func() ActivityScheduleSpec {
			value := validSchedule
			value.ScheduledAt = now.Add(-time.Second)
			return value
		}(),
		func() ActivityScheduleSpec {
			value := validSchedule
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
		func() ActivityScheduleSpec { value := validSchedule; value.Deadline = now; return value }(),
		func() ActivityScheduleSpec { value := validSchedule; value.TransitionID = " spaces "; return value }(),
	} {
		if _, err := NewActivitySchedule(spec); !errors.Is(err, ErrInvalidActivityTransition) {
			t.Fatalf("invalid activity schedule error = %v for %#v", err, spec)
		}
	}
	maximumSchedule := validSchedule
	maximumSchedule.TransitionID = "schedule-maximum"
	maximumSchedule.WorkID = "work-maximum"
	maximumSchedule.Input = make([]byte, 64)
	if _, err := NewActivitySchedule(maximumSchedule); err != nil {
		t.Fatalf("maximum activity input: %v", err)
	}

	work := internalActivityWork(t, now, WorkActivity, encodeActivityDispatch("execute", 1, "attempt-1"))
	lease := internalActivityLease(t, work, now)
	validStart := ActivityAttemptStartSpec{
		TransitionID: "start-1", Lease: lease,
		Instance:   internalActivityInstance(definition, now, ActivityProgressReady, 0, false),
		Definition: definition, StartedAt: now,
	}
	validStart.Instance.sequence = 2
	for _, spec := range []ActivityAttemptStartSpec{
		{},
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Lease = internalActivityLease(t, internalActivityWork(t, now, WorkTimer, []byte("execute")), now)
			return value
		}(),
		func() ActivityAttemptStartSpec { value := validStart; value.Instance.sequence = 1; return value }(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.id = "other-instance"
			return value
		}(),
		func() ActivityAttemptStartSpec { value := validStart; value.Definition = Definition{}; return value }(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Lease = internalActivityLease(t, internalActivityWork(t, now, WorkActivity, encodeActivityDispatch("execute", 3, "attempt-3")), now)
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.activities = map[string]ActivityProgress{}
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.status = StatusPaused
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.activities = map[string]ActivityProgress{"execute": {stepName: "execute", status: ActivityProgressSucceeded}}
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.activities = map[string]ActivityProgress{"execute": {stepName: "execute", status: ActivityProgressReady, attempt: 1}}
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.StartedAt = now.Add(-time.Second)
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.StartedAt = now.Add(time.Hour)
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.StartedAt = now.Add(59*time.Minute + 45*time.Second)
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.StartedAt = now.Add(2 * time.Minute)
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Lease = internalActivityLease(t, work, now.Add(time.Second))
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.updatedAt = now.Add(time.Second)
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.activities = map[string]ActivityProgress{
				"execute": {stepName: "execute", status: ActivityProgressRetryWaiting, attempt: 0, dueAt: now.Add(time.Second)},
			}
			return value
		}(),
		func() ActivityAttemptStartSpec {
			value := validStart
			value.Instance.sequence = ^uint64(0)
			return value
		}(),
		func() ActivityAttemptStartSpec { value := validStart; value.TransitionID = " spaces "; return value }(),
	} {
		if _, err := NewActivityAttemptStart(spec); !errors.Is(err, ErrInvalidActivityTransition) {
			t.Fatalf("invalid activity start error = %v for %#v", err, spec)
		}
	}
	maximumAttempt := validStart
	maximumAttempt.TransitionID = "start-maximum"
	maximumAttempt.Lease = internalActivityLease(t, internalActivityWork(t, now, WorkActivity, encodeActivityDispatch("execute", 2, "attempt-2")), now)
	maximumAttempt.Instance = internalActivityInstance(definition, now, ActivityProgressRetryWaiting, 1, false)
	maximumAttempt.Instance.sequence = 2
	maximumAttempt.Instance.activities["execute"] = ActivityProgress{
		stepName: "execute", status: ActivityProgressRetryWaiting, attempt: 1, dueAt: now,
	}
	if _, err := NewActivityAttemptStart(maximumAttempt); err != nil {
		t.Fatalf("maximum activity attempt: %v", err)
	}

	running := internalActivityInstance(definition, now, ActivityProgressRunning, 1, true)
	succeeded, _ := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivitySucceeded, Data: []byte("ok")})
	unknown, _ := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivityUnknown, Code: "unknown"})
	for index, outcome := range []ActivityOutcome{succeeded, unknown} {
		transition, err := NewActivityAttemptOutcome(ActivityAttemptOutcomeSpec{
			TransitionID: "outcome-" + string(rune('1'+index)), Instance: running,
			Definition: definition, StepName: "execute", Attempt: 1,
			OccurredAt: now, Outcome: outcome,
		})
		if err != nil || transition.Events()[0].Kind() != activityOutcomeEventKind(outcome.kind) {
			t.Fatalf("activity outcome %d = %#v, %v", index, transition, err)
		}
	}
	if activityOutcomeEventKind(ActivityOutcomeKind(255)) != 0 {
		t.Fatal("invalid activity outcome mapped to an event")
	}
	maximumOutcome, _ := NewActivityOutcome(ActivityOutcomeSpec{Kind: ActivitySucceeded, Data: make([]byte, 64)})
	if _, err := NewActivityAttemptOutcome(ActivityAttemptOutcomeSpec{
		TransitionID: "outcome-maximum", Instance: running, Definition: definition,
		StepName: "execute", Attempt: 1, OccurredAt: now, Outcome: maximumOutcome,
	}); err != nil {
		t.Fatalf("maximum activity outcome: %v", err)
	}
	invalidOutcome := ActivityAttemptOutcomeSpec{
		TransitionID: "outcome-1", Instance: running, Definition: definition,
		StepName: "execute", Attempt: 1, OccurredAt: now, Outcome: succeeded,
	}
	overflowOutcome := invalidOutcome
	overflowOutcome.Instance.sequence = ^uint64(0)
	badTransitionOutcome := invalidOutcome
	badTransitionOutcome.TransitionID = " spaces "
	missingOutcome := invalidOutcome
	missingOutcome.StepName = "missing"
	absentOutcome := invalidOutcome
	absentOutcome.Instance.activities = map[string]ActivityProgress{}
	mismatchedDefinitionOutcome := invalidOutcome
	mismatchedDefinitionOutcome.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
	wrongStateOutcome := invalidOutcome
	wrongStateOutcome.Instance = internalActivityInstance(definition, now, ActivityProgressFailed, 1, false)
	wrongAttemptOutcome := invalidOutcome
	wrongAttemptOutcome.Attempt = 2
	invalidValueOutcome := invalidOutcome
	invalidValueOutcome.Outcome = ActivityOutcome{}
	oversizedOutcome := invalidOutcome
	oversizedOutcome.Outcome = ActivityOutcome{kind: ActivitySucceeded, data: make([]byte, 65)}
	oldOutcome := invalidOutcome
	oldOutcome.OccurredAt = now.Add(-time.Second)
	for _, spec := range []ActivityAttemptOutcomeSpec{
		overflowOutcome, badTransitionOutcome, missingOutcome, absentOutcome,
		mismatchedDefinitionOutcome, wrongStateOutcome, wrongAttemptOutcome,
		invalidValueOutcome, oversizedOutcome, oldOutcome,
	} {
		if _, err := NewActivityAttemptOutcome(spec); !errors.Is(err, ErrInvalidActivityTransition) {
			t.Fatalf("invalid activity outcome transition error = %v", err)
		}
	}

	failed := internalActivityInstance(definition, now, ActivityProgressFailed, 1, true)
	validRetry := ActivityRetrySpec{
		TransitionID: "retry-1", WorkID: "work-2", Instance: failed, Definition: definition,
		StepName: "execute", IdempotencyKey: "attempt-2", ScheduledAt: now,
		Deadline: now.Add(time.Hour),
	}
	overflowRetry := validRetry
	overflowRetry.Instance.sequence = ^uint64(0)
	badWorkRetry := validRetry
	badWorkRetry.Deadline = now.Add(time.Second)
	badTransitionRetry := validRetry
	badTransitionRetry.TransitionID = " spaces "
	missingRetry := validRetry
	missingRetry.StepName = "missing"
	absentRetry := validRetry
	absentRetry.Instance.activities = map[string]ActivityProgress{}
	mismatchedDefinitionRetry := validRetry
	mismatchedDefinitionRetry.Instance.definition = DefinitionReference{name: "other", version: "1", fingerprint: definition.fingerprint}
	wrongStateRetry := validRetry
	wrongStateRetry.Instance = internalActivityInstance(definition, now, ActivityProgressRunning, 1, true)
	nonRetryable := validRetry
	nonRetryable.Instance = internalActivityInstance(definition, now, ActivityProgressFailed, 1, false)
	exhaustedRetry := validRetry
	exhaustedRetry.Instance = internalActivityInstance(definition, now, ActivityProgressFailed, 2, true)
	invalidKeyRetry := validRetry
	invalidKeyRetry.IdempotencyKey = " spaces "
	oldRetry := validRetry
	oldRetry.ScheduledAt = now.Add(-time.Second)
	for _, spec := range []ActivityRetrySpec{
		overflowRetry, badWorkRetry, badTransitionRetry, missingRetry, absentRetry,
		mismatchedDefinitionRetry, wrongStateRetry, nonRetryable, exhaustedRetry,
		invalidKeyRetry, oldRetry,
	} {
		if _, err := NewActivityRetry(spec); !errors.Is(err, ErrInvalidActivityTransition) {
			t.Fatalf("invalid activity retry transition error = %v", err)
		}
	}
}

func internalActivityTransitionDefinition(t *testing.T) Definition {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: "orders", Version: "activity-transitions-v1", Mode: Orchestration,
		Steps: []StepSpec{{
			Name: "execute", Kind: StepActivity, Target: "orders.execute",
			Timeout: 30 * time.Second, InputLimit: 64, ResultLimit: 64,
			Retry: RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct activity definition: %v", err)
	}
	return definition
}

func internalActivityInstance(
	definition Definition,
	now time.Time,
	status ActivityProgressStatus,
	attempt uint32,
	retryable bool,
) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 4, updatedAt: now,
		activities: map[string]ActivityProgress{
			"execute": {stepName: "execute", status: status, attempt: attempt, retryable: retryable},
		},
	}
}

func internalActivityWork(t *testing.T, now time.Time, kind WorkKind, payload []byte) PendingWork {
	t.Helper()
	work, err := NewPendingWork(PendingWorkSpec{
		ID: "work-1", Kind: kind, InstanceID: "instance-1", Sequence: 2,
		AvailableAt: now, Deadline: now.Add(time.Hour), Payload: payload,
	})
	if err != nil {
		t.Fatalf("construct activity work: %v", err)
	}
	return work
}

func internalActivityLease(t *testing.T, work PendingWork, now time.Time) WorkLease {
	t.Helper()
	lease, err := NewWorkLease(WorkLeaseSpec{
		Work: work, Owner: "worker-1", Token: 1, Attempt: 1,
		ClaimedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("construct activity lease: %v", err)
	}
	return lease
}
