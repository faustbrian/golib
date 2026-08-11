package workflow_test

import (
	"fmt"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

type lifecycleModelCommand uint8

const (
	modelPause lifecycleModelCommand = iota
	modelResume
	modelRequestCancellation
	modelCancel
	modelComplete
	modelFail
	modelTerminate
	modelMigrate
	modelContinueAsNew
	lifecycleModelCommandCount
)

type lifecycleModelState struct {
	status  workflow.InstanceStatus
	version string
}

func TestLifecycleTransitionModelExhaustivelyMatchesReplay(t *testing.T) {
	t.Parallel()

	first := mustDefinition(t, "model.workflow", "1")
	second := mustDefinition(t, "model.workflow", "2")
	registry, err := workflow.CompileRegistry(
		[]workflow.Definition{first, second},
		[]workflow.Migration{{
			Name: "model.workflow", FromVersion: "1", ToVersion: "2",
			Apply: func(state workflow.MigrationState) (workflow.MigrationState, error) { return state, nil },
		}},
	)
	if err != nil {
		t.Fatalf("compile model registry: %v", err)
	}
	now := time.Date(2026, 8, 11, 13, 0, 0, 0, time.UTC)
	start := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "model-instance", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: first.Reference(),
	})
	definitions := map[string]workflow.DefinitionReference{
		"1": first.Reference(),
		"2": second.Reference(),
	}
	observedLegalEdges := make(map[string]bool)

	for firstCommand := lifecycleModelCommand(0); firstCommand < lifecycleModelCommandCount; firstCommand++ {
		for secondCommand := lifecycleModelCommand(0); secondCommand < lifecycleModelCommandCount; secondCommand++ {
			for thirdCommand := lifecycleModelCommand(0); thirdCommand < lifecycleModelCommandCount; thirdCommand++ {
				for fourthCommand := lifecycleModelCommand(0); fourthCommand < lifecycleModelCommandCount; fourthCommand++ {
					commands := [...]lifecycleModelCommand{firstCommand, secondCommand, thirdCommand, fourthCommand}
					events := []workflow.HistoryEvent{start}
					state := lifecycleModelState{status: workflow.StatusRunning, version: "1"}
					for _, command := range commands {
						candidate := lifecycleModelEvent(t, command, uint64(len(events)+1), now, definitions)
						next, allowed := applyLifecycleModel(state, command)
						replayed, replayErr := workflow.Replay(registry, append(events, candidate))
						if !allowed {
							if replayErr == nil {
								t.Fatalf("model accepted command %d from status %d version %s", command, state.status, state.version)
							}
							_, replayAgainErr := workflow.Replay(registry, append(events, candidate))
							if replayAgainErr == nil || replayAgainErr.Error() != replayErr.Error() {
								t.Fatalf("command %d rejection is not deterministic: %v then %v", command, replayErr, replayAgainErr)
							}
							break
						}
						if replayErr != nil {
							t.Fatalf("model rejected command %d from status %d version %s: %v", command, state.status, state.version, replayErr)
						}
						if replayed.Status() != next.status || replayed.Definition().Version() != next.version {
							t.Fatalf("command %d state = status %d version %s, want status %d version %s", command, replayed.Status(), replayed.Definition().Version(), next.status, next.version)
						}
						again, replayAgainErr := workflow.Replay(registry, append(events, candidate))
						if replayAgainErr != nil || again.SnapshotDigest() != replayed.SnapshotDigest() {
							t.Fatalf("command %d replay is not deterministic: %v", command, replayAgainErr)
						}
						observedLegalEdges[modelEdge(state, command)] = true
						events = append(events, candidate)
						state = next
					}
				}
			}
		}
	}

	for _, edge := range []string{
		"1/1/0", "1/1/2", "1/1/4", "1/1/5", "1/1/6", "1/1/8",
		"1/2/0", "1/2/2", "1/2/4", "1/2/5", "1/2/6", "1/2/8",
		"2/1/1", "2/1/2", "2/1/6", "2/1/7", "2/2/1", "2/2/2", "2/2/6",
		"3/1/3", "3/1/6", "3/2/3", "3/2/6",
	} {
		if !observedLegalEdges[edge] {
			t.Fatalf("legal lifecycle edge %s was not exercised", edge)
		}
	}
}

func applyLifecycleModel(state lifecycleModelState, command lifecycleModelCommand) (lifecycleModelState, bool) {
	if state.status >= workflow.StatusCompleted {
		return state, false
	}
	next := state
	switch command {
	case modelPause:
		if state.status != workflow.StatusRunning {
			return state, false
		}
		next.status = workflow.StatusPaused
	case modelResume:
		if state.status != workflow.StatusPaused {
			return state, false
		}
		next.status = workflow.StatusRunning
	case modelRequestCancellation:
		if state.status != workflow.StatusRunning && state.status != workflow.StatusPaused {
			return state, false
		}
		next.status = workflow.StatusCancelling
	case modelCancel:
		if state.status != workflow.StatusCancelling {
			return state, false
		}
		next.status = workflow.StatusCancelled
	case modelComplete:
		if state.status != workflow.StatusRunning {
			return state, false
		}
		next.status = workflow.StatusCompleted
	case modelFail:
		if state.status != workflow.StatusRunning {
			return state, false
		}
		next.status = workflow.StatusFailed
	case modelTerminate:
		next.status = workflow.StatusTerminated
	case modelMigrate:
		if state.status != workflow.StatusPaused || state.version != "1" {
			return state, false
		}
		next.version = "2"
	case modelContinueAsNew:
		if state.status != workflow.StatusRunning {
			return state, false
		}
		next.status = workflow.StatusContinuedAsNew
	default:
		return state, false
	}
	return next, true
}

func lifecycleModelEvent(
	t *testing.T,
	command lifecycleModelCommand,
	sequence uint64,
	startedAt time.Time,
	definitions map[string]workflow.DefinitionReference,
) workflow.HistoryEvent {
	t.Helper()
	spec := workflow.HistoryEventSpec{
		Sequence: sequence, InstanceID: "model-instance",
		OccurredAt: startedAt.Add(time.Duration(sequence-1) * time.Second),
	}
	switch command {
	case modelPause:
		spec.Kind = workflow.EventInstancePaused
	case modelResume:
		spec.Kind = workflow.EventInstanceResumed
	case modelRequestCancellation:
		spec.Kind = workflow.EventCancellationRequested
	case modelCancel:
		spec.Kind = workflow.EventInstanceCancelled
	case modelComplete:
		spec.Kind = workflow.EventInstanceCompleted
	case modelFail:
		spec.Kind = workflow.EventInstanceFailed
	case modelTerminate:
		spec.Kind = workflow.EventInstanceTerminated
	case modelMigrate:
		spec.Kind = workflow.EventDefinitionMigrated
		spec.Definition = definitions["2"]
	case modelContinueAsNew:
		spec.Kind = workflow.EventContinuedAsNew
		spec.Definition = definitions["2"]
		spec.SuccessorID = "model-successor"
	default:
		t.Fatalf("construct unknown lifecycle model command %d", command)
	}
	return mustHistoryEvent(t, spec)
}

func modelEdge(state lifecycleModelState, command lifecycleModelCommand) string {
	return fmt.Sprintf("%d/%s/%d", state.status, state.version, command)
}
