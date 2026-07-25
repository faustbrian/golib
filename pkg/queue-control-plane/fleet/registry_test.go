package fleet

import (
	"fmt"
	"testing"
	"time"
)

func TestRegistryClassifiesDuplicateReorderedAndConflictingHeartbeats(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	registry := NewRegistry(10)
	original := validHeartbeat("tenant-1", "worker-1", observedAt)

	if got := registry.Upsert(original); got != HeartbeatAccepted {
		t.Fatalf("Upsert(first) = %q, want %q", got, HeartbeatAccepted)
	}
	if got := registry.Upsert(original); got != HeartbeatDuplicate {
		t.Fatalf("Upsert(duplicate) = %q, want %q", got, HeartbeatDuplicate)
	}
	if got := registry.Upsert(withHeartbeat(original, func(heartbeat *Heartbeat) {
		heartbeat.ObservedAt = heartbeat.ObservedAt.Add(-time.Second)
	})); got != HeartbeatReordered {
		t.Fatalf("Upsert(reordered) = %q, want %q", got, HeartbeatReordered)
	}
	if got := registry.Upsert(withHeartbeat(original, func(heartbeat *Heartbeat) {
		heartbeat.State = StateDraining
	})); got != HeartbeatConflict {
		t.Fatalf("Upsert(conflict) = %q, want %q", got, HeartbeatConflict)
	}

	snapshot := registry.Snapshot(observedAt.Add(time.Second), 30*time.Second)
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].State != StateUnknown {
		t.Fatalf("Snapshot().Workers = %+v, want one unknown worker", snapshot.Workers)
	}

	newer := withHeartbeat(original, func(heartbeat *Heartbeat) {
		heartbeat.ObservedAt = heartbeat.ObservedAt.Add(time.Second)
		heartbeat.State = StateDraining
	})
	if got := registry.Upsert(newer); got != HeartbeatAccepted {
		t.Fatalf("Upsert(newer) = %q, want %q", got, HeartbeatAccepted)
	}
	snapshot = registry.Snapshot(observedAt.Add(2*time.Second), 30*time.Second)
	if snapshot.Workers[0].State != StateDraining {
		t.Fatalf("Snapshot().Workers[0].State = %q, want %q", snapshot.Workers[0].State, StateDraining)
	}
}

func TestRegistryBoundsWorkersAndReportsRejectedHeartbeats(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(1)
	first := validHeartbeat("tenant-1", "worker-1", time.Unix(1, 0))
	second := validHeartbeat("tenant-1", "worker-2", time.Unix(2, 0))

	if got := registry.Upsert(Heartbeat{}); got != HeartbeatInvalid {
		t.Fatalf("Upsert(invalid) = %q, want %q", got, HeartbeatInvalid)
	}
	if got := registry.Upsert(Heartbeat{TenantID: "tenant-1", WorkerID: "malformed"}); got != HeartbeatInvalid {
		t.Fatalf("Upsert(malformed) = %q, want %q", got, HeartbeatInvalid)
	}
	if got := registry.Upsert(first); got != HeartbeatAccepted {
		t.Fatalf("Upsert(first) = %q, want %q", got, HeartbeatAccepted)
	}
	if got := registry.Upsert(second); got != HeartbeatCapacityExceeded {
		t.Fatalf("Upsert(second) = %q, want %q", got, HeartbeatCapacityExceeded)
	}

	snapshot := registry.Snapshot(time.Unix(2, 0), time.Minute)
	if len(snapshot.Workers) != 1 || snapshot.Rejected != 1 {
		t.Fatalf("Snapshot() = %+v, want one worker and one rejected heartbeat", snapshot)
	}
}

func TestRegistrySnapshotsWorkersDeterministicallyWithoutAliasing(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(2)
	queues := []string{"standard", "critical"}
	workerZ := validHeartbeat("tenant-1", "worker-z", time.Unix(1, 0))
	workerZ.Queues = queues
	registry.Upsert(workerZ)
	registry.Upsert(validHeartbeat("tenant-1", "worker-a", time.Unix(1, 0)))
	queues[0] = "mutated"

	snapshot := registry.Snapshot(time.Unix(2, 0), time.Minute)
	if snapshot.Workers[0].WorkerID != "worker-a" || snapshot.Workers[1].WorkerID != "worker-z" {
		t.Fatalf("Snapshot().Workers order = %q, %q", snapshot.Workers[0].WorkerID, snapshot.Workers[1].WorkerID)
	}
	if snapshot.Workers[1].Queues[0] != "standard" {
		t.Fatalf("Snapshot().Workers[1].Queues = %v, want defensive copy", snapshot.Workers[1].Queues)
	}

	snapshot.Workers[1].Queues[0] = "snapshot mutation"
	again := registry.Snapshot(time.Unix(2, 0), time.Minute)
	if again.Workers[1].Queues[0] != "standard" {
		t.Fatalf("second Snapshot().Workers[1].Queues = %v, want defensive copy", again.Workers[1].Queues)
	}
}

func TestRegistryWithNonPositiveCapacityRejectsAllWorkers(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(-1)
	got := registry.Upsert(Heartbeat{TenantID: "tenant-1", WorkerID: "worker-1"})
	if got != HeartbeatCapacityExceeded {
		t.Fatalf("Upsert() = %q, want %q", got, HeartbeatCapacityExceeded)
	}
}

func TestRegistryScopesMatchingWorkerIdentitiesByTenant(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(2)
	for _, tenant := range []string{"tenant-a", "tenant-b"} {
		if got := registry.Upsert(validHeartbeat(tenant, "worker-1", time.Unix(1, 0))); got != HeartbeatAccepted {
			t.Fatalf("Upsert(%q) = %q, want accepted", tenant, got)
		}
	}

	snapshot := registry.SnapshotTenant("tenant-b", time.Unix(2, 0), time.Minute)
	if len(snapshot.Workers) != 1 || snapshot.Workers[0].TenantID != "tenant-b" {
		t.Fatalf("SnapshotTenant() = %+v, want only tenant-b", snapshot.Workers)
	}
	if workers := registry.SnapshotTenant("missing", time.Unix(2, 0), time.Minute).Workers; len(workers) != 0 {
		t.Fatalf("SnapshotTenant(missing) = %+v, want empty", workers)
	}
	global := registry.Snapshot(time.Unix(2, 0), time.Minute)
	if len(global.Workers) != 2 || global.Workers[0].TenantID != "tenant-a" {
		t.Fatalf("Snapshot() tenant order = %+v, want tenant-a then tenant-b", global.Workers)
	}

	if got := registry.Upsert(Heartbeat{WorkerID: "worker-2"}); got != HeartbeatInvalid {
		t.Fatalf("Upsert(missing tenant) = %q, want invalid", got)
	}
}

func TestRegistryRepresentsPartitionAndReconnectWithoutFalseState(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	registry := NewRegistry(1)
	current := validHeartbeat("tenant-1", "worker-1", observedAt)
	current.Protocol = ProtocolVersion{Major: 1, Minor: 1}
	if got := registry.Upsert(current); got != HeartbeatAccepted {
		t.Fatalf("Upsert(current) = %q", got)
	}
	partitioned := registry.SnapshotTenant("tenant-1", observedAt.Add(time.Minute), 30*time.Second)
	if len(partitioned.Workers) != 1 || partitioned.Workers[0].State != StateStale ||
		partitioned.Workers[0].Protocol != current.Protocol {
		t.Fatalf("partitioned snapshot = %+v", partitioned.Workers)
	}

	newer := withHeartbeat(current, func(heartbeat *Heartbeat) {
		heartbeat.ObservedAt = observedAt.Add(61 * time.Second)
		heartbeat.Protocol = ProtocolVersion{Major: 2}
	})
	if got := registry.Upsert(newer); got != HeartbeatAccepted {
		t.Fatalf("Upsert(reconnected) = %q", got)
	}
	reconnected := registry.SnapshotTenant("tenant-1", newer.ObservedAt, 30*time.Second)
	if len(reconnected.Workers) != 1 || reconnected.Workers[0].State != StateRunning ||
		reconnected.Workers[0].Protocol != newer.Protocol {
		t.Fatalf("reconnected snapshot = %+v", reconnected.Workers)
	}
	compatibility := Negotiate(
		ProtocolRange{Minimum: ProtocolVersion{Major: 1}, Maximum: ProtocolVersion{Major: 1, Minor: 2}},
		reconnected.Workers[0].Protocol,
		reconnected.Workers[0].Capabilities,
		[]Capability{CapabilityPause},
	)
	if compatibility.State != CompatibilityWorkerNewer || len(compatibility.Enabled) != 0 {
		t.Fatalf("compatibility = %+v", compatibility)
	}
}

func TestRegistryBoundsTenThousandWorkerStaleAndReconnectStorms(t *testing.T) {
	t.Parallel()

	const workers = 10_000
	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	registry := NewRegistry(workers)
	for index := range workers {
		heartbeat := validHeartbeat(
			fmt.Sprintf("tenant-%02d", index%100),
			fmt.Sprintf("worker-%05d", index),
			observedAt,
		)
		if got := registry.Upsert(heartbeat); got != HeartbeatAccepted {
			t.Fatalf("Upsert(initial %d) = %q, want accepted", index, got)
		}
	}

	stale := registry.Snapshot(observedAt.Add(time.Minute), 30*time.Second)
	if len(stale.Workers) != workers {
		t.Fatalf("stale workers = %d, want %d", len(stale.Workers), workers)
	}
	for index, worker := range stale.Workers {
		if worker.State != StateStale {
			t.Fatalf("stale worker %d state = %q, want stale", index, worker.State)
		}
	}

	reconnectedAt := observedAt.Add(61 * time.Second)
	for index := range workers {
		heartbeat := validHeartbeat(
			fmt.Sprintf("tenant-%02d", index%100),
			fmt.Sprintf("worker-%05d", index),
			reconnectedAt,
		)
		if got := registry.Upsert(heartbeat); got != HeartbeatAccepted {
			t.Fatalf("Upsert(reconnect %d) = %q, want accepted", index, got)
		}
	}
	running := registry.Snapshot(reconnectedAt, 30*time.Second)
	if len(running.Workers) != workers {
		t.Fatalf("reconnected workers = %d, want %d", len(running.Workers), workers)
	}
	for index, worker := range running.Workers {
		if worker.State != StateRunning {
			t.Fatalf("reconnected worker %d state = %q, want running", index, worker.State)
		}
	}
}

func withHeartbeat(heartbeat Heartbeat, mutate func(*Heartbeat)) Heartbeat {
	mutate(&heartbeat)

	return heartbeat
}
