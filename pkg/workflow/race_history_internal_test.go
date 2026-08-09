package workflow

import (
	"errors"
	"testing"
	"time"
)

func TestRaceHistoryRejectsUnobservedMismatchedAndDuplicateWinners(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 4, 0, 0, 0, time.UTC)
	definition := internalRaceDefinition(t)
	registry, err := CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile race definition: %v", err)
	}
	event := internalRaceEvent(t, now, "decision", "fallback")
	base := internalRaceInstance(definition, now)
	base.signals["fallback"] = SignalProgress{stepName: "fallback", receivedAt: now}
	if err := base.applyRace(registry, event); err != nil {
		t.Fatalf("apply race winner: %v", err)
	}
	if err := base.applyRace(registry, event); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("duplicate race error = %v", err)
	}

	tests := []struct {
		instance Instance
		event    HistoryEvent
	}{
		{instance: internalRaceInstance(definition, now), event: event},
		{instance: func() Instance {
			value := internalRaceInstance(definition, now)
			value.status = StatusPaused
			value.signals["fallback"] = SignalProgress{stepName: "fallback"}
			return value
		}(), event: event},
		{instance: func() Instance {
			value := internalRaceInstance(definition, now)
			value.signals["fallback"] = SignalProgress{stepName: "fallback"}
			return value
		}(), event: internalRaceEvent(t, now, "missing", "fallback")},
		{instance: func() Instance {
			value := internalRaceInstance(definition, now)
			value.signals["outsider"] = SignalProgress{stepName: "outsider"}
			return value
		}(), event: internalRaceEvent(t, now, "decision", "outsider")},
	}
	for index, test := range tests {
		if err := test.instance.applyRace(registry, test.event); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("invalid race %d error = %v", index, err)
		}
	}
}

func TestRaceDecisionAndEventRejectMalformedPersistencePlans(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 5, 0, 0, 0, time.UTC)
	definition := internalRaceDefinition(t)
	instance := internalRaceInstance(definition, now)
	instance.signals["primary"] = SignalProgress{stepName: "primary", receivedAt: now}
	step := definition.Steps()[0]
	if _, _, err := decideRaceStep(OrchestrationDecisionSpec{
		TransitionID: "race", Instance: instance, Definition: definition,
	}, step); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("missing decision time error = %v", err)
	}
	if _, _, err := decideRaceStep(OrchestrationDecisionSpec{
		Instance: instance, Definition: definition, DecidedAt: now.Add(time.Second),
	}, step); !errors.Is(err, ErrInvalidOrchestration) {
		t.Fatalf("missing transition identity error = %v", err)
	}

	valid := HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: EventRaceWon,
		OccurredAt: now, StepName: "decision", Data: []byte("primary"),
	}
	invalid := []HistoryEventSpec{
		func() HistoryEventSpec { value := valid; value.StepName = " spaces "; return value }(),
		func() HistoryEventSpec { value := valid; value.Data = []byte(" spaces "); return value }(),
		func() HistoryEventSpec { value := valid; value.Attempt = 1; return value }(),
		func() HistoryEventSpec { value := valid; value.IdempotencyKey = "key"; return value }(),
		func() HistoryEventSpec { value := valid; value.DueAt = now; return value }(),
		func() HistoryEventSpec { value := valid; value.Code = "code"; return value }(),
		func() HistoryEventSpec { value := valid; value.Retryable = true; return value }(),
	}
	for index, spec := range invalid {
		if _, err := NewHistoryEvent(spec); !errors.Is(err, ErrInvalidHistoryEvent) {
			t.Fatalf("invalid race event %d error = %v", index, err)
		}
	}
	if races := (Instance{}).Races(); races != nil || raceProgressSnapshots(nil) != nil {
		t.Fatalf("empty races = %#v", races)
	}
}

func internalRaceDefinition(t *testing.T) Definition {
	t.Helper()
	definition, err := NewDefinition(DefinitionSpec{
		Name: "race", Version: "1", Mode: Orchestration,
		Steps: []StepSpec{
			{Name: "decision", Kind: StepRace, FanOutLimit: 2, Branches: []string{"primary", "fallback"}},
			{Name: "primary", Kind: StepSignal, Target: "race.primary", Timeout: time.Minute, InputLimit: 8},
			{Name: "fallback", Kind: StepSignal, Target: "race.fallback", Timeout: time.Minute, InputLimit: 8},
		},
	})
	if err != nil {
		t.Fatalf("construct race definition: %v", err)
	}
	return definition
}

func internalRaceInstance(definition Definition, now time.Time) Instance {
	return Instance{
		id: "instance-1", definition: definition.Reference(), status: StatusRunning,
		sequence: 1, startedAt: now, updatedAt: now,
		activities: make(map[string]ActivityProgress), timers: make(map[string]TimerProgress),
		signals: make(map[string]SignalProgress), races: make(map[string]RaceProgress),
		compensations: make(map[string]CompensationProgress),
	}
}

func internalRaceEvent(t *testing.T, now time.Time, stepName, winner string) HistoryEvent {
	t.Helper()
	event, err := NewHistoryEvent(HistoryEventSpec{
		Sequence: 2, InstanceID: "instance-1", Kind: EventRaceWon,
		OccurredAt: now.Add(time.Second), StepName: stepName, Data: []byte(winner),
	})
	if err != nil {
		t.Fatalf("construct race event: %v", err)
	}
	return event
}
