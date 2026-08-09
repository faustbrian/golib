package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestReplayReconstructsDurableTimersAndSignals(t *testing.T) {
	t.Parallel()

	definition := mustWaitDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	events := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
			OccurredAt: now, Definition: definition.Reference(),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
			OccurredAt: now.Add(time.Second), StepName: "expiry", DueAt: now.Add(31 * time.Second),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventSignalReceived,
			OccurredAt: now.Add(2 * time.Second), StepName: "approved",
			IdempotencyKey: "signal-1", Data: []byte("1234567890123456"),
		}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
			OccurredAt: now.Add(31 * time.Second), StepName: "expiry",
		}),
	}
	instance, err := workflow.Replay(registry, events)
	if err != nil {
		t.Fatalf("replay waits: %v", err)
	}
	timer, ok := instance.Timer("expiry")
	if !ok || timer.Status() != workflow.TimerFired || timer.DueAt() != now.Add(31*time.Second) ||
		timer.FiredAt() != now.Add(31*time.Second) || timer.StepName() != "expiry" {
		t.Fatalf("timer progress = %#v, %t", timer, ok)
	}
	signal, ok := instance.Signal("approved")
	if !ok || signal.SignalID() != "signal-1" || signal.ReceivedAt() != now.Add(2*time.Second) ||
		string(signal.Payload()) != "1234567890123456" || signal.StepName() != "approved" {
		t.Fatalf("signal progress = %#v, %t", signal, ok)
	}
	if len(instance.Timers()) != 1 || len(instance.Signals()) != 1 {
		t.Fatal("stable wait progress was not exposed")
	}
	if instance.SnapshotDigest() == "" {
		t.Fatal("wait progress was omitted from the deterministic snapshot")
	}
	startOnly, err := workflow.Replay(registry, events[:1])
	if err != nil {
		t.Fatalf("replay start only: %v", err)
	}
	if instance.SnapshotDigest() == startOnly.SnapshotDigest() {
		t.Fatal("wait progress did not affect the deterministic snapshot")
	}
	payload := signal.Payload()
	payload[0] = 'X'
	if string(signal.Payload()) != "1234567890123456" {
		t.Fatal("signal payload is caller mutable")
	}
}

func TestHistoryEventsRejectInvalidTimerAndSignalFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	validTimer := workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
		OccurredAt: now, StepName: "expiry", DueAt: now.Add(time.Second),
	}
	validFired := workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
		OccurredAt: now, StepName: "expiry",
	}
	validSignal := workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventSignalReceived,
		OccurredAt: now, StepName: "approved", IdempotencyKey: "signal-1",
	}
	invalid := []workflow.HistoryEventSpec{
		func() workflow.HistoryEventSpec { value := validTimer; value.StepName = ""; return value }(),
		func() workflow.HistoryEventSpec { value := validTimer; value.Attempt = 1; return value }(),
		func() workflow.HistoryEventSpec { value := validTimer; value.Code = "code"; return value }(),
		func() workflow.HistoryEventSpec { value := validTimer; value.Retryable = true; return value }(),
		func() workflow.HistoryEventSpec { value := validTimer; value.IdempotencyKey = "key"; return value }(),
		func() workflow.HistoryEventSpec { value := validTimer; value.DueAt = now; return value }(),
		func() workflow.HistoryEventSpec { value := validTimer; value.Data = []byte("data"); return value }(),
		func() workflow.HistoryEventSpec { value := validFired; value.IdempotencyKey = "key"; return value }(),
		func() workflow.HistoryEventSpec {
			value := validFired
			value.DueAt = now.Add(time.Second)
			return value
		}(),
		func() workflow.HistoryEventSpec { value := validFired; value.Data = []byte("data"); return value }(),
		func() workflow.HistoryEventSpec {
			value := validSignal
			value.IdempotencyKey = " spaces "
			return value
		}(),
		func() workflow.HistoryEventSpec {
			value := validSignal
			value.DueAt = now.Add(time.Second)
			return value
		}(),
	}
	for _, spec := range invalid {
		if _, err := workflow.NewHistoryEvent(spec); !errors.Is(err, workflow.ErrInvalidHistoryEvent) {
			t.Fatalf("invalid wait event error = %v for %#v", err, spec)
		}
	}
}

func TestReplayRejectsInvalidTimerAndSignalTransitions(t *testing.T) {
	t.Parallel()

	definition := mustWaitDefinition(t)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile definition: %v", err)
	}
	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	start := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	tests := map[string][]workflow.HistoryEvent{
		"unknown timer step": {start, mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
			OccurredAt: now.Add(time.Second), StepName: "missing", DueAt: now.Add(31 * time.Second),
		})},
		"duplicate timer": {start,
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
				OccurredAt: now.Add(time.Second), StepName: "expiry", DueAt: now.Add(31 * time.Second),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
				OccurredAt: now.Add(2 * time.Second), StepName: "expiry", DueAt: now.Add(32 * time.Second),
			}),
		},
		"timer not scheduled": {start, mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
			OccurredAt: now.Add(time.Second), StepName: "expiry",
		})},
		"timer fired early": {start,
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
				OccurredAt: now.Add(time.Second), StepName: "expiry", DueAt: now.Add(31 * time.Second),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
				OccurredAt: now.Add(30 * time.Second), StepName: "expiry",
			}),
		},
		"timer fired while paused": {start,
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
				OccurredAt: now.Add(time.Second), StepName: "expiry", DueAt: now.Add(31 * time.Second),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventInstancePaused,
				OccurredAt: now.Add(2 * time.Second),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
				OccurredAt: now.Add(31 * time.Second), StepName: "expiry",
			}),
		},
		"timer fired twice": {start,
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventTimerScheduled,
				OccurredAt: now.Add(time.Second), StepName: "expiry", DueAt: now.Add(31 * time.Second),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
				OccurredAt: now.Add(31 * time.Second), StepName: "expiry",
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 4, InstanceID: "instance-1", Kind: workflow.EventTimerFired,
				OccurredAt: now.Add(32 * time.Second), StepName: "expiry",
			}),
		},
		"duplicate signal": {start,
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventSignalReceived,
				OccurredAt: now.Add(time.Second), StepName: "approved", IdempotencyKey: "signal-1",
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventSignalReceived,
				OccurredAt: now.Add(2 * time.Second), StepName: "approved", IdempotencyKey: "signal-2",
			}),
		},
		"unknown signal step": {start, mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventSignalReceived,
			OccurredAt: now.Add(time.Second), StepName: "missing", IdempotencyKey: "signal-1",
		})},
		"oversized signal": {start, mustHistoryEvent(t, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventSignalReceived,
			OccurredAt: now.Add(time.Second), StepName: "approved", IdempotencyKey: "signal-1",
			Data: make([]byte, 17),
		})},
		"signal while cancelling": {start,
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventCancellationRequested,
				OccurredAt: now.Add(time.Second),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventSignalReceived,
				OccurredAt: now.Add(2 * time.Second), StepName: "approved", IdempotencyKey: "signal-1",
			}),
		},
	}
	for name, events := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := workflow.Replay(registry, events); !errors.Is(err, workflow.ErrInvalidTransition) {
				t.Fatalf("replay error = %v", err)
			}
		})
	}
}

func mustWaitDefinition(t *testing.T) workflow.Definition {
	t.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "waits-v1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{
			{Name: "expiry", Kind: workflow.StepTimer, Timeout: 30 * time.Second},
			{Name: "approved", Kind: workflow.StepSignal, Target: "orders.approved", Timeout: time.Hour, InputLimit: 16},
		},
	})
	if err != nil {
		t.Fatalf("construct wait definition: %v", err)
	}
	return definition
}
