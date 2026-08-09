package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestReplayReconstructsPersistedActivityAttemptsAndRetry(t *testing.T) {
	t.Parallel()

	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute",
			Timeout: time.Minute, InputLimit: 1024, ResultLimit: 1024,
			Retry: workflow.RetryPolicy{MaxAttempts: 2, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		t.Fatalf("construct definition: %v", err)
	}
	registry, err := workflow.CompileRegistry([]workflow.Definition{definition}, nil)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	events := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
			OccurredAt: now, Definition: definition.Reference(), Data: []byte("workflow-input"),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled,
			OccurredAt: now.Add(time.Second), StepName: "execute", Data: []byte("attempt-input"),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted,
			OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1,
			IdempotencyKey: "instance-1/execute/1", DueAt: now.Add(2*time.Second + time.Minute),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed,
			OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1,
			Code: "temporarily-unavailable", Retryable: true, Data: []byte("safe-details"),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled,
			OccurredAt: now.Add(4 * time.Second), StepName: "execute", Attempt: 1,
			DueAt: now.Add(5 * time.Second),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted,
			OccurredAt: now.Add(5 * time.Second), StepName: "execute", Attempt: 2,
			IdempotencyKey: "instance-1/execute/2", DueAt: now.Add(5*time.Second + time.Minute),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded,
			OccurredAt: now.Add(6 * time.Second), StepName: "execute", Attempt: 2,
			Data: []byte("result"),
		}),
	}

	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay activity attempts: %v", err)
	}
	progress, ok := instance.Activity("execute")
	if !ok {
		t.Fatal("activity progress missing")
	}
	if progress.Status() != workflow.ActivityProgressSucceeded || progress.Attempt() != 2 {
		t.Fatalf("activity progress = status %d attempt %d", progress.Status(), progress.Attempt())
	}
	if progress.IdempotencyKey() != "instance-1/execute/2" || string(progress.Input()) != "attempt-input" || string(progress.Result()) != "result" {
		t.Fatal("activity progress lost persisted attempt data")
	}
	if progress.Code() != "" || progress.Retryable() || !progress.DueAt().IsZero() {
		t.Fatal("successful activity retained failure or timer metadata")
	}
	activities := instance.Activities()
	if len(activities) != 1 || activities[0].StepName() != "execute" {
		t.Fatalf("activities = %#v", activities)
	}
	activities[0] = workflow.ActivityProgress{}
	if _, ok := instance.Activity("execute"); !ok {
		t.Fatal("Activities returned mutable instance state")
	}
	startOnly, err := workflow.Replay(registry, events[:1])
	if err != nil {
		t.Fatalf("replay start: %v", err)
	}
	if instance.SnapshotDigest() == startOnly.SnapshotDigest() {
		t.Fatal("activity progress did not affect the diagnostic snapshot")
	}
	changedInput := append([]workflow.HistoryEvent(nil), events...)
	changedInput[1] = mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled,
		OccurredAt: now.Add(time.Second), StepName: "execute", Data: []byte("different-input"),
	})
	changedInstance, err := workflow.Replay(registry, changedInput)
	if err != nil {
		t.Fatalf("replay changed activity input: %v", err)
	}
	if instance.SnapshotDigest() == changedInstance.SnapshotDigest() {
		t.Fatal("activity data did not affect the diagnostic snapshot")
	}
}

func TestReplayPreservesKnownFailureAndUnknownActivityOutcomes(t *testing.T) {
	t.Parallel()

	definition := mustActivityDefinition(t, 2, time.Second, time.Second, 8, 8)
	registry, err := workflow.CompileRegistry([]workflow.Definition{definition}, nil)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	base := activityStartedHistory(t, definition, now)

	tests := []struct {
		name      string
		kind      workflow.EventKind
		code      string
		retryable bool
		status    workflow.ActivityProgressStatus
	}{
		{name: "permanent failure", kind: workflow.EventActivityAttemptFailed, code: "declined", status: workflow.ActivityProgressFailed},
		{name: "retryable failure", kind: workflow.EventActivityAttemptFailed, code: "temporary", retryable: true, status: workflow.ActivityProgressFailed},
		{name: "unknown", kind: workflow.EventActivityAttemptUnknown, code: "transport-lost", status: workflow.ActivityProgressUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := append([]workflow.HistoryEvent(nil), base...)
			events = append(events, mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 4, InstanceID: "instance-1", Kind: test.kind,
				OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1,
				Code: test.code, Retryable: test.retryable, Data: []byte("details"),
			}))
			instance, replayErr := workflow.Replay(registry, events)
			if replayErr != nil {
				t.Fatalf("replay outcome: %v", replayErr)
			}
			progress, ok := instance.Activity("execute")
			if !ok || progress.Status() != test.status || progress.Code() != test.code ||
				progress.Retryable() != test.retryable || string(progress.Result()) != "details" ||
				progress.Attempt() != 1 || progress.DueAt() != (time.Time{}) {
				t.Fatal("outcome classification was not preserved")
			}
		})
	}
}

func TestHistoryActivityEventOwnsCanonicalAttemptMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 11, 0, 0, 123, time.FixedZone("EEST", 3*60*60))
	data := []byte("details")
	event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed,
		OccurredAt: now, StepName: "execute", Attempt: 2,
		Code: "temporary", Retryable: true, Data: data,
	})
	if err != nil {
		t.Fatalf("construct event: %v", err)
	}
	data[0] = 'X'
	if event.StepName() != "execute" || event.Attempt() != 2 || event.IdempotencyKey() != "" ||
		event.DueAt() != (time.Time{}) || event.Code() != "temporary" || !event.Retryable() ||
		!event.OccurredAt().Equal(now.UTC()) || string(event.Data()) != "details" {
		t.Fatal("history event did not preserve canonical activity metadata")
	}
}

func TestHistoryActivityEventRejectsAmbiguousFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	valid := workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted,
		OccurredAt: now, StepName: "execute", Attempt: 1,
		IdempotencyKey: "key-1", DueAt: now.Add(time.Minute),
	}
	tests := map[string]func() workflow.HistoryEventSpec{
		"missing step": func() workflow.HistoryEventSpec { spec := valid; spec.StepName = ""; return spec },
		"activity definition": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Definition = mustDefinitionReference(t, "orders", "1", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
			return spec
		},
		"activity successor": func() workflow.HistoryEventSpec { spec := valid; spec.SuccessorID = "next"; return spec },
		"start zero attempt": func() workflow.HistoryEventSpec { spec := valid; spec.Attempt = 0; return spec },
		"start missing key":  func() workflow.HistoryEventSpec { spec := valid; spec.IdempotencyKey = ""; return spec },
		"start expired":      func() workflow.HistoryEventSpec { spec := valid; spec.DueAt = now; return spec },
		"start code":         func() workflow.HistoryEventSpec { spec := valid; spec.Code = "unexpected"; return spec },
		"start retryable":    func() workflow.HistoryEventSpec { spec := valid; spec.Retryable = true; return spec },
		"start data":         func() workflow.HistoryEventSpec { spec := valid; spec.Data = []byte("unexpected"); return spec },
		"lifecycle activity fields": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventInstancePaused
			return spec
		},
		"schedule attempt": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityScheduled
			spec.Attempt = 1
			spec.IdempotencyKey = ""
			spec.DueAt = time.Time{}
			return spec
		},
		"success code": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityAttemptSucceeded
			spec.IdempotencyKey = ""
			spec.DueAt = time.Time{}
			spec.Code = "unexpected"
			return spec
		},
		"failure missing code": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityAttemptFailed
			spec.IdempotencyKey = ""
			spec.DueAt = time.Time{}
			return spec
		},
		"unknown retryable": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityAttemptUnknown
			spec.IdempotencyKey = ""
			spec.DueAt = time.Time{}
			spec.Code = "unknown"
			spec.Retryable = true
			return spec
		},
		"retry data": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityRetryScheduled
			spec.IdempotencyKey = ""
			spec.Data = []byte("unexpected")
			return spec
		},
		"success zero attempt": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityAttemptSucceeded
			spec.Attempt = 0
			spec.IdempotencyKey = ""
			spec.DueAt = time.Time{}
			return spec
		},
		"failure zero attempt": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityAttemptFailed
			spec.Attempt = 0
			spec.IdempotencyKey = ""
			spec.DueAt = time.Time{}
			spec.Code = "failed"
			return spec
		},
		"unknown zero attempt": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityAttemptUnknown
			spec.Attempt = 0
			spec.IdempotencyKey = ""
			spec.DueAt = time.Time{}
			spec.Code = "unknown"
			return spec
		},
		"retry zero attempt": func() workflow.HistoryEventSpec {
			spec := valid
			spec.Kind = workflow.EventActivityRetryScheduled
			spec.Attempt = 0
			spec.IdempotencyKey = ""
			return spec
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := workflow.NewHistoryEvent(build()); !errors.Is(err, workflow.ErrInvalidHistoryEvent) {
				t.Fatalf("error = %v, want ErrInvalidHistoryEvent", err)
			}
		})
	}

	validKinds := []workflow.HistoryEventSpec{
		{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now, StepName: "execute", Data: []byte("input")},
		valid,
		{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now, StepName: "execute", Attempt: 1, Data: []byte("result")},
		{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now, StepName: "execute", Attempt: 1, Code: "failed"},
		{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptUnknown, OccurredAt: now, StepName: "execute", Attempt: 1, Code: "unknown"},
		{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now, StepName: "execute", Attempt: 1, DueAt: now.Add(time.Second)},
	}
	for _, spec := range validKinds {
		if _, err := workflow.NewHistoryEvent(spec); err != nil {
			t.Fatalf("valid activity event %d rejected: %v", spec.Kind, err)
		}
	}
}

func TestReplayRejectsIllegalActivityTransitions(t *testing.T) {
	t.Parallel()

	definition := mustActivityDefinition(t, 2, time.Second, time.Second, 8, 8)
	registry, err := workflow.CompileRegistry([]workflow.Definition{definition}, nil)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	start := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()})
	schedule := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "execute", Data: []byte("input")})
	started := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "key-1", DueAt: now.Add(2*time.Second + time.Minute)})

	makeEvent := func(spec workflow.HistoryEventSpec) workflow.HistoryEvent {
		return mustHistoryEvent(t, spec)
	}
	tests := map[string][]workflow.HistoryEvent{
		"missing definition step":   {start, makeEvent(workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "missing", Data: []byte("input")})},
		"oversized scheduled input": {start, makeEvent(workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "execute", Data: make([]byte, 9)})},
		"duplicate schedule":        {start, schedule, makeEvent(workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(2 * time.Second), StepName: "execute"})},
		"schedule paused":           {start, makeEvent(workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(time.Second)}), makeEvent(workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(2 * time.Second), StepName: "execute"})},
		"start without schedule":    {start, makeEvent(workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "key-1", DueAt: now.Add(time.Second + time.Minute)})},
		"start while paused": {start, schedule,
			makeEvent(workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(2 * time.Second)}),
			makeEvent(workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "key-1", DueAt: now.Add(3*time.Second + time.Minute)})},
		"start wrong attempt":   {start, schedule, makeEvent(workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 2, IdempotencyKey: "key-2", DueAt: now.Add(2*time.Second + time.Minute)})},
		"start wrong deadline":  {start, schedule, makeEvent(workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "key-1", DueAt: now.Add(3*time.Second + time.Minute)})},
		"outcome without start": {start, schedule, makeEvent(workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1})},
		"outcome wrong attempt": {start, schedule, started, makeEvent(workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 2})},
		"oversized success":     {start, schedule, started, makeEvent(workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptSucceeded, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Data: make([]byte, 9)})},
		"oversized failure":     {start, schedule, started, makeEvent(workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Code: "failed", Data: make([]byte, 9)})},
		"oversized unknown":     {start, schedule, started, makeEvent(workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptUnknown, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Code: "unknown", Data: make([]byte, 9)})},
	}
	for name, events := range tests {
		t.Run(name, func(t *testing.T) {
			if _, replayErr := workflow.Replay(registry, events); !errors.Is(replayErr, workflow.ErrInvalidTransition) {
				t.Fatalf("error = %v, want ErrInvalidTransition", replayErr)
			}
		})
	}
}

func TestReplayAcceptsExactActivityPayloadLimits(t *testing.T) {
	t.Parallel()

	definition := mustActivityDefinition(t, 2, time.Second, time.Second, 8, 8)
	registry, err := workflow.CompileRegistry([]workflow.Definition{definition}, nil)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		kind      workflow.EventKind
		code      string
		retryable bool
	}{
		{name: "success", kind: workflow.EventActivityAttemptSucceeded},
		{name: "failure", kind: workflow.EventActivityAttemptFailed, code: "failed"},
		{name: "unknown", kind: workflow.EventActivityAttemptUnknown, code: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := activityStartedHistory(t, definition, now)
			events[1] = mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled,
				OccurredAt: now.Add(time.Second), StepName: "execute", Data: make([]byte, 8),
			})
			events = append(events, mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 4, InstanceID: "instance-1", Kind: test.kind,
				OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1,
				Code: test.code, Retryable: test.retryable, Data: make([]byte, 8),
			}))
			instance, replayErr := workflow.Replay(registry, events)
			if replayErr != nil {
				t.Fatalf("exact payload limit rejected: %v", replayErr)
			}
			progress, ok := instance.Activity("execute")
			if !ok || len(progress.Input()) != 8 || len(progress.Result()) != 8 {
				t.Fatal("exact payload limit was not preserved")
			}
		})
	}
}

func TestReplayEnforcesActivityRetryPolicyAndDueTime(t *testing.T) {
	t.Parallel()

	definition := mustActivityDefinition(t, 3, 3*time.Second, 5*time.Second, 8, 8)
	registry, err := workflow.CompileRegistry([]workflow.Definition{definition}, nil)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	base := activityStartedHistory(t, definition, now)
	failure := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Code: "temporary", Retryable: true})
	retry := mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "execute", Attempt: 1, DueAt: now.Add(7 * time.Second)})

	illegal := map[string][]workflow.HistoryEvent{
		"retry wrong attempt":           append(append([]workflow.HistoryEvent(nil), base...), failure, mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "execute", Attempt: 2, DueAt: now.Add(9 * time.Second)})),
		"retry wrong due time":          append(append([]workflow.HistoryEvent(nil), base...), failure, mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "execute", Attempt: 1, DueAt: now.Add(8 * time.Second)})),
		"start before retry due":        append(append([]workflow.HistoryEvent(nil), base...), failure, retry, mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(6 * time.Second), StepName: "execute", Attempt: 2, IdempotencyKey: "key-2", DueAt: now.Add(6*time.Second + time.Minute)})),
		"retry after permanent failure": append(append([]workflow.HistoryEvent(nil), base...), mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Code: "permanent"}), mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "execute", Attempt: 1, DueAt: now.Add(7 * time.Second)})),
		"retry while paused": append(append([]workflow.HistoryEvent(nil), base...), failure,
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventInstancePaused, OccurredAt: now.Add(4 * time.Second)}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(5 * time.Second), StepName: "execute", Attempt: 1, DueAt: now.Add(8 * time.Second)})),
	}
	for name, events := range illegal {
		t.Run(name, func(t *testing.T) {
			if _, replayErr := workflow.Replay(registry, events); !errors.Is(replayErr, workflow.ErrInvalidTransition) {
				t.Fatalf("error = %v, want ErrInvalidTransition", replayErr)
			}
		})
	}

	events := append(append([]workflow.HistoryEvent(nil), base...), failure, retry,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(7 * time.Second), StepName: "execute", Attempt: 2, IdempotencyKey: "key-2", DueAt: now.Add(7*time.Second + time.Minute)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(8 * time.Second), StepName: "execute", Attempt: 2, Code: "temporary", Retryable: true}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 8, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(9 * time.Second), StepName: "execute", Attempt: 2, DueAt: now.Add(14 * time.Second)}),
	)
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay capped retry: %v", err)
	}
	progress, ok := instance.Activity("execute")
	if !ok || progress.Status() != workflow.ActivityProgressRetryWaiting || !progress.DueAt().Equal(now.Add(14*time.Second)) {
		t.Fatal("capped retry delay was not replayed")
	}
}

func TestReplayDoublesActivityRetryDelayBeforeTheCap(t *testing.T) {
	t.Parallel()

	definition := mustActivityDefinition(t, 3, time.Second, 5*time.Second, 8, 8)
	registry, err := workflow.CompileRegistry([]workflow.Definition{definition}, nil)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	events := append(activityStartedHistory(t, definition, now),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1, Code: "temporary", Retryable: true}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(4 * time.Second), StepName: "execute", Attempt: 1, DueAt: now.Add(5 * time.Second)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 6, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(5 * time.Second), StepName: "execute", Attempt: 2, IdempotencyKey: "key-2", DueAt: now.Add(5*time.Second + time.Minute)}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 7, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed, OccurredAt: now.Add(6 * time.Second), StepName: "execute", Attempt: 2, Code: "temporary", Retryable: true}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 8, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled, OccurredAt: now.Add(7 * time.Second), StepName: "execute", Attempt: 2, DueAt: now.Add(9 * time.Second)}),
	)
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay doubled retry: %v", err)
	}
	progress, ok := instance.Activity("execute")
	if !ok || !progress.DueAt().Equal(now.Add(9*time.Second)) {
		t.Fatal("doubled retry delay was not replayed")
	}
}

func TestReplayPreservesRetryableFailureWhenAttemptsAreExhausted(t *testing.T) {
	t.Parallel()

	definition := mustActivityDefinition(t, 1, time.Second, time.Second, 8, 8)
	registry, err := workflow.CompileRegistry([]workflow.Definition{definition}, nil)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	events := append(activityStartedHistory(t, definition, now),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptFailed,
			OccurredAt: now.Add(3 * time.Second), StepName: "execute", Attempt: 1,
			Code: "temporary", Retryable: true,
		}),
	)
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay exhausted retryable failure: %v", err)
	}
	progress, ok := instance.Activity("execute")
	if !ok || !progress.Retryable() || progress.Status() != workflow.ActivityProgressFailed {
		t.Fatal("exhausted attempt lost the truthful retryability classification")
	}

	events = append(events, mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 5, InstanceID: "instance-1", Kind: workflow.EventActivityRetryScheduled,
		OccurredAt: now.Add(4 * time.Second), StepName: "execute", Attempt: 1,
		DueAt: now.Add(5 * time.Second),
	}))
	if _, err := workflow.Replay(registry, events); !errors.Is(err, workflow.ErrInvalidTransition) {
		t.Fatalf("exhausted retry scheduling error = %v", err)
	}
}

func mustActivityDefinition(t *testing.T, attempts uint32, initialDelay, maxDelay time.Duration, inputLimit, resultLimit uint32) workflow.Definition {
	t.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute",
			Timeout: time.Minute, InputLimit: inputLimit, ResultLimit: resultLimit,
			Retry: workflow.RetryPolicy{MaxAttempts: attempts, InitialDelay: initialDelay, MaxDelay: maxDelay},
		}},
	})
	if err != nil {
		t.Fatalf("construct definition: %v", err)
	}
	return definition
}

func activityStartedHistory(t *testing.T, definition workflow.Definition, now time.Time) []workflow.HistoryEvent {
	t.Helper()
	return []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventActivityScheduled, OccurredAt: now.Add(time.Second), StepName: "execute", Data: []byte("input")}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventActivityAttemptStarted, OccurredAt: now.Add(2 * time.Second), StepName: "execute", Attempt: 1, IdempotencyKey: "key-1", DueAt: now.Add(2*time.Second + time.Minute)}),
	}
}
