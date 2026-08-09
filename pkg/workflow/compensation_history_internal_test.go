package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestCompensationHelpersRejectNonCompensationAndRetainScheduleOrder(t *testing.T) {
	t.Parallel()

	instance := Instance{}
	if err := instance.applyCompensation(nil, HistoryEvent{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("non-compensation event error = %v", err)
	}
	if compensationProgressSnapshots(nil) != nil {
		t.Fatal("nil compensation snapshots did not remain nil")
	}
	progress := map[string]CompensationProgress{
		"second": {stepName: "second", scheduledSequence: 9},
		"first":  {stepName: "first", scheduledSequence: 5},
	}
	ordered := sortedCompensationProgress(progress)
	snapshots := compensationProgressSnapshots(progress)
	if len(ordered) != 2 || ordered[0].stepName != "first" || ordered[1].stepName != "second" ||
		len(snapshots) != 2 || snapshots[0].StepName != "first" {
		t.Fatalf("compensation order = %#v snapshots %#v", ordered, snapshots)
	}
}

func TestCompensationTransitionModelRejectsEveryInvalidStateBoundary(t *testing.T) {
	t.Parallel()

	registry, reference := internalCompensationRegistry(t)
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	base := func() Instance {
		return Instance{
			id: "instance-1", definition: reference, status: StatusRunning,
			activities: map[string]ActivityProgress{
				"reserve": {stepName: "reserve", status: ActivityProgressSucceeded},
			},
			compensations: make(map[string]CompensationProgress),
		}
	}
	scheduled := HistoryEvent{kind: EventCompensationScheduled, stepName: "reserve", occurredAt: now, sequence: 5}
	invalidSchedule := []Instance{
		func() Instance { instance := base(); instance.status = StatusPaused; return instance }(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{}
			return instance
		}(),
		func() Instance { instance := base(); delete(instance.activities, "reserve"); return instance }(),
		func() Instance {
			instance := base()
			instance.activities["reserve"] = ActivityProgress{status: ActivityProgressFailed}
			return instance
		}(),
	}
	for _, instance := range invalidSchedule {
		if err := instance.applyCompensation(registry, scheduled); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid schedule error = %v for %#v", err, instance)
		}
	}
	cancelling := base()
	cancelling.status = StatusCancelling
	if err := cancelling.applyCompensation(registry, scheduled); err != nil {
		t.Fatalf("schedule while cancelling: %v", err)
	}
	missingStep := base()
	missingEvent := scheduled
	missingEvent.stepName = "missing"
	if err := missingStep.applyCompensation(registry, missingEvent); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("missing compensation step error = %v", err)
	}
	plainStep := base()
	plainEvent := scheduled
	plainEvent.stepName = "plain"
	if err := plainStep.applyCompensation(registry, plainEvent); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("uncompensated step error = %v", err)
	}
	aboveKind := base()
	if err := aboveKind.applyCompensation(registry, HistoryEvent{kind: EventCompensationManuallyResolved + 1}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("above compensation kind error = %v", err)
	}

	started := HistoryEvent{
		kind: EventCompensationAttemptStarted, stepName: "reserve", occurredAt: now,
		attempt: 1, dueAt: now.Add(30 * time.Second), idempotencyKey: "attempt-1",
	}
	invalidStarts := []Instance{
		base(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationSucceeded}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationReady, attempt: 1}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationRetryWaiting, attempt: 2, dueAt: now}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationReady}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationRetryWaiting, dueAt: now.Add(time.Second)}
			return instance
		}(),
	}
	invalidStartEvents := []HistoryEvent{
		started,
		started,
		started,
		func() HistoryEvent { event := started; event.attempt = 3; return event }(),
		func() HistoryEvent { event := started; event.dueAt = now.Add(29 * time.Second); return event }(),
		started,
	}
	for index, instance := range invalidStarts {
		if err := instance.applyCompensation(registry, invalidStartEvents[index]); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid start %d error = %v", index, err)
		}
	}

	outcome := HistoryEvent{kind: EventCompensationAttemptSucceeded, stepName: "reserve", attempt: 1}
	invalidOutcomes := []Instance{
		base(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationReady, attempt: 1}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationRunning, attempt: 2}
			return instance
		}(),
	}
	for index, instance := range invalidOutcomes {
		if err := instance.applyCompensation(registry, outcome); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid outcome %d error = %v", index, err)
		}
	}

	retry := HistoryEvent{kind: EventCompensationRetryScheduled, stepName: "reserve", occurredAt: now, attempt: 1, dueAt: now.Add(time.Second)}
	invalidRetries := []Instance{
		base(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationReady, attempt: 1, retryable: true}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationFailed, attempt: 1}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationFailed, attempt: 2, retryable: true}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationFailed, attempt: 1, retryable: true}
			return instance
		}(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationFailed, attempt: 1, retryable: true}
			return instance
		}(),
	}
	invalidRetryEvents := []HistoryEvent{
		retry,
		retry,
		retry,
		func() HistoryEvent { event := retry; event.attempt = 2; return event }(),
		func() HistoryEvent { event := retry; event.attempt = 2; return event }(),
		func() HistoryEvent { event := retry; event.dueAt = now.Add(2 * time.Second); return event }(),
	}
	for index, instance := range invalidRetries {
		if err := instance.applyCompensation(registry, invalidRetryEvents[index]); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid retry %d error = %v", index, err)
		}
	}

	manual := HistoryEvent{kind: EventCompensationManuallyResolved, stepName: "reserve", code: "accepted-loss"}
	invalidManual := []Instance{
		base(),
		func() Instance {
			instance := base()
			instance.compensations["reserve"] = CompensationProgress{status: CompensationReady}
			return instance
		}(),
	}
	for index, instance := range invalidManual {
		if err := instance.applyCompensation(registry, manual); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid manual resolution %d error = %v", index, err)
		}
	}
	unknown := base()
	unknown.compensations["reserve"] = CompensationProgress{status: CompensationUnknown}
	if err := unknown.applyCompensation(registry, manual); err != nil {
		t.Fatalf("resolve unknown compensation: %v", err)
	}
}

func internalCompensationRegistry(t *testing.T) (*Registry, DefinitionReference) {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: "orders", Version: "compensation-v1", Mode: Orchestration,
		Steps: []StepSpec{
			{
				Name: "reserve", Kind: StepActivity, Target: "inventory.reserve",
				Timeout: time.Minute, InputLimit: 64, ResultLimit: 64,
				Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
				Compensation: &CompensationSpec{
					Target: "inventory.release", Timeout: 30 * time.Second, ResultLimit: 64,
					Retry: RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
				},
			},
			{
				Name: "plain", Kind: StepActivity, Target: "plain.run", Timeout: time.Minute,
				InputLimit: 64, ResultLimit: 64,
				Retry: RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
			},
		},
	})
	if err != nil {
		t.Fatalf("construct definition: %v", err)
	}
	registry, err := CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	return registry, definition.Reference()
}
