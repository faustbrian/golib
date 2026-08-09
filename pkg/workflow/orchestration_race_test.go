package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestOrchestrationPersistsTheFirstObservedRaceWinner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 3, 0, 0, 0, time.UTC)
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "approval-race", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{
			{Name: "decision", Kind: workflow.StepRace, FanOutLimit: 2, Branches: []string{"primary", "fallback"}},
			{Name: "primary", Kind: workflow.StepSignal, Target: "approval.primary", Timeout: time.Hour, InputLimit: 8},
			{Name: "fallback", Kind: workflow.StepSignal, Target: "approval.fallback", Timeout: time.Hour, InputLimit: 8},
			{Name: "confirmed", Kind: workflow.StepSignal, Target: "approval.confirmed", Timeout: time.Hour, InputLimit: 8},
		},
	})
	if err != nil {
		t.Fatalf("construct race definition: %v", err)
	}
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile race definition: %v", err)
	}
	history := []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
	}
	instance := replaySequential(t, registry, history)
	waiting, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{Instance: instance, Definition: definition})
	if err != nil || waiting.Kind() != workflow.OrchestrationWaiting || waiting.StepName() != "decision" {
		t.Fatalf("empty race decision = %#v, error %v", waiting, err)
	}
	history = append(history,
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventSignalReceived, OccurredAt: now.Add(time.Second), StepName: "fallback", IdempotencyKey: "fallback-1"}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 3, InstanceID: "instance-1", Kind: workflow.EventSignalReceived, OccurredAt: now.Add(2 * time.Second), StepName: "primary", IdempotencyKey: "primary-1"}),
	)
	instance = replaySequential(t, registry, history)
	decision, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "race-winner", Instance: instance, Definition: definition,
		DecidedAt: now.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("persist race winner: %v", err)
	}
	events := decision.Transition().Events()
	if decision.Kind() != workflow.OrchestrationRecorded || decision.StepName() != "decision" ||
		len(events) != 1 || events[0].Kind() != workflow.EventRaceWon || string(events[0].Data()) != "fallback" {
		t.Fatalf("race decision = %#v events %#v", decision, events)
	}
	history = append(history, events...)
	instance = replaySequential(t, registry, history)
	winner, ok := instance.Race("decision")
	if !ok || winner.StepName() != "decision" || winner.WinnerStepName() != "fallback" ||
		winner.DecidedAt() != now.Add(3*time.Second) {
		t.Fatalf("race winner = %#v, exists %t", winner, ok)
	}
	if races := instance.Races(); len(races) != 1 || races[0] != winner || instance.SnapshotDigest() == "" {
		t.Fatalf("race snapshot = %#v", races)
	}
	next, err := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{Instance: instance, Definition: definition})
	if err != nil || next.Kind() != workflow.OrchestrationWaiting || next.StepName() != "confirmed" {
		t.Fatalf("post-race decision = %#v, error %v", next, err)
	}
}

func TestReplayRejectsRaceWinnerWithoutObservedBranch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 11, 3, 30, 0, 0, time.UTC)
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "race", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{
			{Name: "decision", Kind: workflow.StepRace, FanOutLimit: 1, Branches: []string{"signal"}},
			{Name: "signal", Kind: workflow.StepSignal, Target: "race.signal", Timeout: time.Hour, InputLimit: 1},
		},
	})
	if err != nil {
		t.Fatalf("construct race definition: %v", err)
	}
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		t.Fatalf("compile race definition: %v", err)
	}
	_, err = workflow.Replay(registry, []workflow.HistoryEvent{
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 1, InstanceID: "instance-1", Kind: workflow.EventInstanceStarted, OccurredAt: now, Definition: definition.Reference()}),
		mustHistoryEvent(t, workflow.HistoryEventSpec{Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventRaceWon, OccurredAt: now.Add(time.Second), StepName: "decision", Data: []byte("signal")}),
	})
	if !errors.Is(err, workflow.ErrInvalidTransition) {
		t.Fatalf("unobserved race winner error = %v", err)
	}
}
