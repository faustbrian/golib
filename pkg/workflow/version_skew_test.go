package workflow_test

import (
	"errors"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestMixedVersionRollingDeployKeepsWorkersPinnedToDurableDefinitions(t *testing.T) {
	t.Parallel()

	versionOne := mustDefinition(t, "rolling.workflow", "1")
	versionTwo := mustDefinition(t, "rolling.workflow", "2")
	migration := workflow.Migration{
		Name: "rolling.workflow", FromVersion: "1", ToVersion: "2",
		Apply: func(state workflow.MigrationState) (workflow.MigrationState, error) {
			return state, nil
		},
	}
	oldWorker, err := workflow.CompileDefinitions(versionOne)
	if err != nil {
		t.Fatalf("compile old-worker definitions: %v", err)
	}
	newWorker, err := workflow.CompileRegistry(
		[]workflow.Definition{versionOne, versionTwo},
		[]workflow.Migration{migration},
	)
	if err != nil {
		t.Fatalf("compile new-worker definitions: %v", err)
	}

	now := time.Date(2036, 8, 11, 12, 0, 0, 0, time.UTC)
	started := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "rolling-instance", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: versionOne.Reference(),
	})
	paused := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "rolling-instance", Kind: workflow.EventInstancePaused,
		OccurredAt: now.Add(time.Second),
	})
	versionOneHistory := []workflow.HistoryEvent{started, paused}

	oldSnapshot, err := workflow.Replay(oldWorker, versionOneHistory)
	if err != nil {
		t.Fatalf("old worker replay version one: %v", err)
	}
	newSnapshot, err := workflow.Replay(newWorker, versionOneHistory)
	if err != nil {
		t.Fatalf("new worker replay version one: %v", err)
	}
	if oldSnapshot.SnapshotDigest() != newSnapshot.SnapshotDigest() ||
		newSnapshot.Definition() != versionOne.Reference() {
		t.Fatal("rolling deploy reinterpreted the pinned version-one history")
	}

	migrated := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 3, InstanceID: "rolling-instance", Kind: workflow.EventDefinitionMigrated,
		OccurredAt: now.Add(2 * time.Second), Definition: versionTwo.Reference(), Data: []byte("v2"),
	})
	migratedHistory := append(append([]workflow.HistoryEvent(nil), versionOneHistory...), migrated)
	if _, err := workflow.Replay(oldWorker, migratedHistory); !errors.Is(err, workflow.ErrDefinitionNotFound) {
		t.Fatalf("old worker accepted an unavailable version-two decision: %v", err)
	}
	migratedSnapshot, err := workflow.Replay(newWorker, migratedHistory)
	if err != nil {
		t.Fatalf("new worker replay migrated history: %v", err)
	}
	if migratedSnapshot.Definition() != versionTwo.Reference() || string(migratedSnapshot.Input()) != "v2" {
		t.Fatal("new worker did not use the persisted migration decision")
	}

	incompatibleVersionOne, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "rolling.workflow", Version: "1", Mode: workflow.Orchestration,
		Deprecated: true, Steps: versionOne.Steps(),
	})
	if err != nil {
		t.Fatalf("construct incompatible version one: %v", err)
	}
	incompatibleWorker, err := workflow.CompileDefinitions(incompatibleVersionOne)
	if err != nil {
		t.Fatalf("compile incompatible worker: %v", err)
	}
	if _, err := workflow.Replay(incompatibleWorker, versionOneHistory); !errors.Is(err, workflow.ErrDefinitionMismatch) {
		t.Fatalf("incompatible worker reinterpreted version-one history: %v", err)
	}

	continued := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 2, InstanceID: "continued-instance", Kind: workflow.EventContinuedAsNew,
		OccurredAt: now.Add(time.Second), Definition: versionTwo.Reference(), SuccessorID: "successor-instance",
	})
	continuedStart := mustHistoryEvent(t, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "continued-instance", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: versionOne.Reference(),
	})
	continuedHistory := []workflow.HistoryEvent{continuedStart, continued}
	if _, err := workflow.Replay(oldWorker, continuedHistory); !errors.Is(err, workflow.ErrDefinitionNotFound) {
		t.Fatalf("old worker accepted unavailable continue-as-new target: %v", err)
	}
	continuedSnapshot, err := workflow.Replay(newWorker, continuedHistory)
	if err != nil {
		t.Fatalf("new worker replay continue-as-new: %v", err)
	}
	if continuedSnapshot.Status() != workflow.StatusContinuedAsNew ||
		continuedSnapshot.SuccessorID() != "successor-instance" {
		t.Fatal("continue-as-new did not preserve the persisted successor")
	}
}
