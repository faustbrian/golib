package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestApplyWaitRejectsNonWaitEvents(t *testing.T) {
	t.Parallel()

	instance := Instance{}
	if err := instance.applyWait(nil, HistoryEvent{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("non-wait event error = %v", err)
	}
}

func TestWaitExtensionPreservesLegacySnapshotDigestsWhenNoWaitsExist(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 9, 15, 0, 0, 0, time.UTC)
	instance := Instance{
		id: "instance-1", definition: DefinitionReference{name: "orders", version: "1", fingerprint: string(make([]byte, 64))},
		status: StatusRunning, sequence: 1, startedAt: now, updatedAt: now,
		activities: make(map[string]ActivityProgress), timers: make(map[string]TimerProgress),
		signals: make(map[string]SignalProgress),
	}
	if instance.SnapshotDigest() != legacySnapshotDigest(instance) {
		t.Fatal("adding wait progress reinterpreted a legacy snapshot digest")
	}
}

func TestWaitSnapshotsPreserveNilAndPersistedProgress(t *testing.T) {
	t.Parallel()

	if timerProgressSnapshots(nil) != nil || signalProgressSnapshots(nil) != nil {
		t.Fatal("nil wait progress did not remain nil")
	}
	timers := timerProgressSnapshots(map[string]TimerProgress{
		"timer": {stepName: "timer", status: TimerWaiting},
	})
	signals := signalProgressSnapshots(map[string]SignalProgress{
		"signal": {stepName: "signal", signalID: "signal-1"},
	})
	if len(timers) != 1 || timers[0].StepName != "timer" ||
		len(signals) != 1 || signals[0].SignalID != "signal-1" {
		t.Fatalf("wait snapshots = timers %#v signals %#v", timers, signals)
	}
}

func legacySnapshotDigest(instance Instance) string {
	encoded, _ := json.Marshal(struct {
		ID                    string
		DefinitionName        string
		DefinitionVersion     string
		DefinitionFingerprint string
		Status                InstanceStatus
		Sequence              uint64
		StartedAt             time.Time
		UpdatedAt             time.Time
		Input                 []byte
		Result                []byte
		SuccessorID           string
		Activities            []activityProgressSnapshot
	}{
		ID: instance.id, DefinitionName: instance.definition.Name(),
		DefinitionVersion:     instance.definition.Version(),
		DefinitionFingerprint: instance.definition.Fingerprint(), Status: instance.status,
		Sequence: instance.sequence, StartedAt: instance.startedAt,
		UpdatedAt: instance.updatedAt, Input: instance.input, Result: instance.result,
		SuccessorID: instance.successorID, Activities: activityProgressSnapshots(instance.activities),
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
