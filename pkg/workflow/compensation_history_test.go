package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestReplayReconstructsIndependentlyRetryableCompensation(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	events := successfulActivityHistory(t, definition, now)
	events = append(events,
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled,
			OccurredAt: now.Add(4 * time.Second), StepName: "reserve", Data: []byte("undo-input"),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted,
			OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1,
			IdempotencyKey: "instance-1/reserve/compensate/1", DueAt: now.Add(35 * time.Second),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptFailed,
			OccurredAt: now.Add(6 * time.Second), StepName: "reserve", Attempt: 1,
			Code: "temporary", Retryable: true, Data: []byte("safe-details"),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 8, InstanceID: "instance-1", Kind: workflow.EventCompensationRetryScheduled,
			OccurredAt: now.Add(7 * time.Second), StepName: "reserve", Attempt: 1,
			DueAt: now.Add(8 * time.Second),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 9, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted,
			OccurredAt: now.Add(8 * time.Second), StepName: "reserve", Attempt: 2,
			IdempotencyKey: "instance-1/reserve/compensate/2", DueAt: now.Add(38 * time.Second),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 10, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptSucceeded,
			OccurredAt: now.Add(9 * time.Second), StepName: "reserve", Attempt: 2,
			Data: make([]byte, 64),
		}),
	)
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay compensation: %v", err)
	}
	progress, ok := instance.Compensation("reserve")
	if !ok || progress.Status() != workflow.CompensationSucceeded || progress.Attempt() != 2 ||
		progress.IdempotencyKey() != "instance-1/reserve/compensate/2" ||
		string(progress.Input()) != "undo-input" || len(progress.Result()) != 64 ||
		progress.StepName() != "reserve" || progress.Code() != "" || progress.Retryable() ||
		!progress.DueAt().IsZero() || progress.ScheduledSequence() != 5 {
		t.Fatalf("compensation progress = %#v, %t", progress, ok)
	}
	if len(instance.Compensations()) != 1 || instance.Compensations()[0].StepName() != "reserve" {
		t.Fatal("compensation order was not retained")
	}
	if instance.SnapshotDigest() == "" {
		t.Fatal("compensation state was omitted from replay diagnostics")
	}
}

func TestReplayPreservesUnknownCompensationForManualResolution(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	events := successfulActivityHistory(t, definition, now)
	events = append(events,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "reserve"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted, OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", DueAt: now.Add(35 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptUnknown, OccurredAt: now.Add(6 * time.Second), StepName: "reserve", Attempt: 1, Code: "commit-unknown"}),
	)
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay unknown compensation: %v", err)
	}
	progress, ok := instance.Compensation("reserve")
	if !ok || progress.Status() != workflow.CompensationUnknown || progress.Code() != "commit-unknown" {
		t.Fatalf("unknown compensation = %#v, %t", progress, ok)
	}
}

func TestHistoryRejectsInvalidCompensationEventFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	base := workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", OccurredAt: now, StepName: "reserve",
	}
	valid := []workflow.HistoryEventSpec{
		func() workflow.HistoryEventSpec {
			value := base
			value.Kind = workflow.EventCompensationScheduled
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := base
			value.Kind = workflow.EventCompensationAttemptStarted
			value.Attempt = 1
			value.IdempotencyKey = "attempt-1"
			value.DueAt = now.Add(time.Second)
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := base
			value.Kind = workflow.EventCompensationAttemptSucceeded
			value.Attempt = 1
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := base
			value.Kind = workflow.EventCompensationAttemptFailed
			value.Attempt = 1
			value.Code = "failed"
			value.Retryable = true
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := base
			value.Kind = workflow.EventCompensationAttemptUnknown
			value.Attempt = 1
			value.Code = "unknown"
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := base
			value.Kind = workflow.EventCompensationRetryScheduled
			value.Attempt = 1
			value.DueAt = now.Add(time.Second)
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := base
			value.Kind = workflow.EventCompensationManuallyResolved
			value.Code = "accepted-loss"
			return value
		}(),
	}
	for _, spec := range valid {
		if _, err := workflow.NewHistoryEvent(spec); err != nil {
			t.Fatalf("valid compensation event error = %v for %#v", err, spec)
		}
	}
	invalid := []workflow.HistoryEventSpec{
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) { value.StepName = "" }),
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) {
			value.Definition = mustDefinitionReference(t, "orders", "1", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
		}),
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) { value.SuccessorID = "instance-2" }),
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) { value.Attempt = 1 }),
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) { value.IdempotencyKey = "key" }),
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) { value.DueAt = now.Add(time.Second) }),
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) { value.Code = "code" }),
		mutateCompensationEvent(valid[0], func(value *workflow.HistoryEventSpec) { value.Retryable = true }),
		mutateCompensationEvent(valid[1], func(value *workflow.HistoryEventSpec) { value.Attempt = 0 }),
		mutateCompensationEvent(valid[1], func(value *workflow.HistoryEventSpec) { value.IdempotencyKey = " spaces " }),
		mutateCompensationEvent(valid[1], func(value *workflow.HistoryEventSpec) { value.DueAt = now }),
		mutateCompensationEvent(valid[1], func(value *workflow.HistoryEventSpec) { value.Code = "code" }),
		mutateCompensationEvent(valid[1], func(value *workflow.HistoryEventSpec) { value.Retryable = true }),
		mutateCompensationEvent(valid[1], func(value *workflow.HistoryEventSpec) { value.Data = []byte("data") }),
		mutateCompensationEvent(valid[2], func(value *workflow.HistoryEventSpec) { value.Attempt = 0 }),
		mutateCompensationEvent(valid[2], func(value *workflow.HistoryEventSpec) { value.IdempotencyKey = "key" }),
		mutateCompensationEvent(valid[2], func(value *workflow.HistoryEventSpec) { value.DueAt = now.Add(time.Second) }),
		mutateCompensationEvent(valid[2], func(value *workflow.HistoryEventSpec) { value.Code = "code" }),
		mutateCompensationEvent(valid[2], func(value *workflow.HistoryEventSpec) { value.Retryable = true }),
		mutateCompensationEvent(valid[3], func(value *workflow.HistoryEventSpec) { value.Attempt = 0 }),
		mutateCompensationEvent(valid[3], func(value *workflow.HistoryEventSpec) { value.IdempotencyKey = "key" }),
		mutateCompensationEvent(valid[3], func(value *workflow.HistoryEventSpec) { value.DueAt = now.Add(time.Second) }),
		mutateCompensationEvent(valid[3], func(value *workflow.HistoryEventSpec) { value.Code = "" }),
		mutateCompensationEvent(valid[4], func(value *workflow.HistoryEventSpec) { value.Attempt = 0 }),
		mutateCompensationEvent(valid[4], func(value *workflow.HistoryEventSpec) { value.IdempotencyKey = "key" }),
		mutateCompensationEvent(valid[4], func(value *workflow.HistoryEventSpec) { value.DueAt = now.Add(time.Second) }),
		mutateCompensationEvent(valid[4], func(value *workflow.HistoryEventSpec) { value.Code = "" }),
		mutateCompensationEvent(valid[4], func(value *workflow.HistoryEventSpec) { value.Retryable = true }),
		mutateCompensationEvent(valid[5], func(value *workflow.HistoryEventSpec) { value.Attempt = 0 }),
		mutateCompensationEvent(valid[5], func(value *workflow.HistoryEventSpec) { value.IdempotencyKey = "key" }),
		mutateCompensationEvent(valid[5], func(value *workflow.HistoryEventSpec) { value.DueAt = now }),
		mutateCompensationEvent(valid[5], func(value *workflow.HistoryEventSpec) { value.Code = "code" }),
		mutateCompensationEvent(valid[5], func(value *workflow.HistoryEventSpec) { value.Retryable = true }),
		mutateCompensationEvent(valid[5], func(value *workflow.HistoryEventSpec) { value.Data = []byte("data") }),
		mutateCompensationEvent(valid[6], func(value *workflow.HistoryEventSpec) { value.Attempt = 1 }),
		mutateCompensationEvent(valid[6], func(value *workflow.HistoryEventSpec) { value.IdempotencyKey = "key" }),
		mutateCompensationEvent(valid[6], func(value *workflow.HistoryEventSpec) { value.DueAt = now.Add(time.Second) }),
		mutateCompensationEvent(valid[6], func(value *workflow.HistoryEventSpec) { value.Code = "" }),
		mutateCompensationEvent(valid[6], func(value *workflow.HistoryEventSpec) { value.Retryable = true }),
	}
	for _, spec := range invalid {
		if _, err := workflow.NewHistoryEvent(spec); !errors.Is(err, workflow.ErrInvalidHistoryEvent) {
			t.Fatalf("invalid compensation event error = %v for %#v", err, spec)
		}
	}
}

func mutateCompensationEvent(
	spec workflow.HistoryEventSpec,
	mutate func(*workflow.HistoryEventSpec),
) workflow.HistoryEventSpec {
	mutate(&spec)
	return spec
}

func TestManualCompensationResolutionNeverReportsSuccessfulRollback(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	events := successfulActivityHistory(t, definition, now)
	events = append(events,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "reserve"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted, OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", DueAt: now.Add(35 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptFailed, OccurredAt: now.Add(6 * time.Second), StepName: "reserve", Attempt: 1, Code: "permanent"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 8, InstanceID: "instance-1", Kind: workflow.EventCompensationManuallyResolved, OccurredAt: now.Add(7 * time.Second), StepName: "reserve", Code: "accepted-loss", Data: make([]byte, 64)}),
	)
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay manual resolution: %v", err)
	}
	progress, ok := instance.Compensation("reserve")
	if !ok || progress.Status() != workflow.CompensationManuallyResolved ||
		progress.Status() == workflow.CompensationSucceeded || progress.Code() != "accepted-loss" ||
		len(progress.Result()) != 64 {
		t.Fatalf("manual compensation = %#v, %t", progress, ok)
	}
}

func TestReplayRejectsCompensationWithoutSuccessfulActivityOrValidOrder(t *testing.T) {
	t.Parallel()

	definition := mustCompensationDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 16, 0, 0, 0, time.UTC)
	start := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()})
	tests := [][]workflow.HistoryEvent{
		{start, mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(time.Second), StepName: "reserve"})},
		append(successfulActivityHistory(t, definition, now), mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted, OccurredAt: now.Add(4 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", DueAt: now.Add(34 * time.Second)})),
		append(successfulActivityHistory(t, definition, now), mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "missing"})),
		append(successfulActivityHistory(t, definition, now),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "reserve"}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted, OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", DueAt: now.Add(35 * time.Second)}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptSucceeded, OccurredAt: now.Add(6 * time.Second), StepName: "reserve", Attempt: 1, Data: make([]byte, 65)}),
		),
		append(successfulActivityHistory(t, definition, now),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "reserve"}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted, OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", DueAt: now.Add(35 * time.Second)}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptFailed, OccurredAt: now.Add(6 * time.Second), StepName: "reserve", Attempt: 1, Code: "temporary", Retryable: true}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 8, InstanceID: "instance-1", Kind: workflow.EventCompensationRetryScheduled, OccurredAt: now.Add(7 * time.Second), StepName: "reserve", Attempt: 1, DueAt: now.Add(9 * time.Second)}),
		),
		append(successfulActivityHistory(t, definition, now),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventCompensationScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "reserve"}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptStarted, OccurredAt: now.Add(5 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "attempt-1", DueAt: now.Add(35 * time.Second)}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventCompensationAttemptFailed, OccurredAt: now.Add(6 * time.Second), StepName: "reserve", Attempt: 1, Code: "permanent"}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 8, InstanceID: "instance-1", Kind: workflow.EventCompensationManuallyResolved, OccurredAt: now.Add(7 * time.Second), StepName: "reserve", Code: "accepted-loss", Data: make([]byte, 65)}),
		),
	}
	for _, events := range tests {
		if _, err := workflow.Replay(registry, events); !errors.Is(err, workflow.ErrInvalidTransition) {
			t.Fatalf("invalid compensation replay error = %v", err)
		}
	}
}

func mustCompensationDefinition(t *testing.T) workflow.Definition {
	t.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "compensation-v1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "reserve", Kind: workflow.StepActivity, Target: "inventory.reserve",
			Timeout: time.Minute, InputLimit: 64, ResultLimit: 64,
			Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
			Compensation: &workflow.CompensationSpec{
				Target: "inventory.release", Timeout: 30 * time.Second, ResultLimit: 64,
				Retry: workflow.RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
			},
		}},
	})
	if err != nil {
		t.Fatalf("construct compensation definition: %v", err)
	}
	return definition
}

func successfulActivityHistory(t *testing.T, definition workflow.Definition, now time.Time) []workflow.HistoryEvent {
	t.Helper()
	return []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "reserve"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "reserve", Attempt: 1, IdempotencyKey: "activity-1", DueAt: now.Add(62 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now.Add(3 * time.Second), StepName: "reserve", Attempt: 1}),
	}
}
