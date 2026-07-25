package fleet

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkRegistrySnapshotTenThousandWorkers(b *testing.B) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	queues := make([]string, 64)
	for index := range queues {
		queues[index] = fmt.Sprintf("queue-%02d", index)
	}
	registry := NewRegistry(10_000)
	for index := range 10_000 {
		disposition := registry.Upsert(Heartbeat{
			TenantID:     fmt.Sprintf("tenant-%02d", index%100),
			WorkerID:     fmt.Sprintf("worker-%05d", index),
			Version:      "1.0.0",
			StartedAt:    now.Add(-time.Hour),
			ObservedAt:   now,
			Queues:       queues,
			Concurrency:  32,
			State:        StateRunning,
			CurrentJobs:  16,
			DrainStatus:  DrainNotRequested,
			Backend:      "redis",
			Protocol:     ProtocolVersion{Major: 1},
			Capabilities: []Capability{CapabilityDrain, CapabilityPause},
		})
		if disposition != HeartbeatAccepted {
			b.Fatalf("Upsert(%d) = %q", index, disposition)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if snapshot := registry.Snapshot(now, 30*time.Second); len(snapshot.Workers) != 10_000 {
			b.Fatalf("Snapshot() workers = %d", len(snapshot.Workers))
		}
	}
}

func BenchmarkRegistryReconnectStormTenThousandWorkers(b *testing.B) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	registry := NewRegistry(10_000)
	heartbeats := make([]Heartbeat, 10_000)
	for index := range heartbeats {
		heartbeats[index] = Heartbeat{
			TenantID: fmt.Sprintf("tenant-%02d", index%100),
			WorkerID: fmt.Sprintf("worker-%05d", index), Version: "1.0.0",
			StartedAt: now.Add(-time.Hour), ObservedAt: now,
			Queues: []string{"critical"}, Concurrency: 32, State: StateRunning,
			DrainStatus: DrainNotRequested, Backend: "redis",
			Protocol: ProtocolVersion{Major: 1},
		}
		if registry.Upsert(heartbeats[index]) != HeartbeatAccepted {
			b.Fatalf("seed Upsert(%d) failed", index)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for iteration := range b.N {
		observedAt := now.Add(time.Duration(iteration+1) * time.Second)
		for index := range heartbeats {
			heartbeats[index].ObservedAt = observedAt
			if registry.Upsert(heartbeats[index]) != HeartbeatAccepted {
				b.Fatalf("reconnect Upsert(%d) failed", index)
			}
		}
	}
}
