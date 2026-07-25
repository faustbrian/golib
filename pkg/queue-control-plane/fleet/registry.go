package fleet

import (
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// HeartbeatDisposition describes how the registry handled one report.
type HeartbeatDisposition string

const (
	HeartbeatAccepted         HeartbeatDisposition = "accepted"
	HeartbeatDuplicate        HeartbeatDisposition = "duplicate"
	HeartbeatReordered        HeartbeatDisposition = "reordered"
	HeartbeatConflict         HeartbeatDisposition = "conflict"
	HeartbeatInvalid          HeartbeatDisposition = "invalid"
	HeartbeatCapacityExceeded HeartbeatDisposition = "capacity_exceeded"
)

// WorkerSnapshot contains a defensively copied heartbeat and its derived
// fail-safe state.
type WorkerSnapshot struct {
	Heartbeat
	State State
}

// RegistrySnapshot is a deterministic point-in-time view of retained workers.
type RegistrySnapshot struct {
	Workers  []WorkerSnapshot
	Rejected uint64
}

// Registry retains a bounded set of latest worker heartbeats.
type Registry struct {
	mu         sync.RWMutex
	maxWorkers int
	workers    map[workerKey]workerRecord
	rejected   map[string]uint64
}

type workerKey struct {
	tenant string
	worker string
}

type workerRecord struct {
	heartbeat  Heartbeat
	conflicted bool
}

// NewRegistry creates a registry that retains at most maxWorkers identities.
func NewRegistry(maxWorkers int) *Registry {
	if maxWorkers < 0 {
		maxWorkers = 0
	}

	return &Registry{
		maxWorkers: maxWorkers,
		workers:    make(map[workerKey]workerRecord, maxWorkers),
		rejected:   make(map[string]uint64),
	}
}

// Upsert ingests one heartbeat without allowing delayed, duplicate, or
// conflicting reports to falsely advance worker state.
func (r *Registry) Upsert(heartbeat Heartbeat) HeartbeatDisposition {
	if strings.TrimSpace(heartbeat.TenantID) == "" || strings.TrimSpace(heartbeat.WorkerID) == "" {
		return HeartbeatInvalid
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := workerKey{tenant: heartbeat.TenantID, worker: heartbeat.WorkerID}
	record, exists := r.workers[key]
	if !exists && len(r.workers) >= r.maxWorkers {
		r.rejected[heartbeat.TenantID]++

		return HeartbeatCapacityExceeded
	}
	if err := heartbeat.Validate(); err != nil {
		return HeartbeatInvalid
	}
	if !exists {
		r.workers[key] = workerRecord{heartbeat: cloneHeartbeat(heartbeat)}

		return HeartbeatAccepted
	}

	if heartbeat.ObservedAt.Before(record.heartbeat.ObservedAt) {
		return HeartbeatReordered
	}
	if heartbeat.ObservedAt.Equal(record.heartbeat.ObservedAt) {
		if reflect.DeepEqual(heartbeat, record.heartbeat) {
			return HeartbeatDuplicate
		}

		record.conflicted = true
		r.workers[key] = record

		return HeartbeatConflict
	}

	r.workers[key] = workerRecord{heartbeat: cloneHeartbeat(heartbeat)}

	return HeartbeatAccepted
}

// Snapshot returns sorted workers with liveness derived at now.
func (r *Registry) Snapshot(now time.Time, staleAfter time.Duration) RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := r.snapshotLocked("", now, staleAfter)
	for _, rejected := range r.rejected {
		snapshot.Rejected += rejected
	}

	return snapshot
}

// SnapshotTenant returns only one tenant's workers and rejection count.
func (r *Registry) SnapshotTenant(
	tenant string,
	now time.Time,
	staleAfter time.Duration,
) RegistrySnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := r.snapshotLocked(tenant, now, staleAfter)
	snapshot.Rejected = r.rejected[tenant]

	return snapshot
}

func (r *Registry) snapshotLocked(tenant string, now time.Time, staleAfter time.Duration) RegistrySnapshot {
	snapshot := RegistrySnapshot{
		Workers: make([]WorkerSnapshot, 0, len(r.workers)),
	}
	for _, record := range r.workers {
		if tenant != "" && record.heartbeat.TenantID != tenant {
			continue
		}
		state := record.heartbeat.EffectiveState(now, staleAfter)
		if record.conflicted {
			state = StateUnknown
		}
		snapshot.Workers = append(snapshot.Workers, WorkerSnapshot{
			Heartbeat: cloneHeartbeat(record.heartbeat),
			State:     state,
		})
	}
	sort.Slice(snapshot.Workers, func(i, j int) bool {
		if snapshot.Workers[i].TenantID != snapshot.Workers[j].TenantID {
			return snapshot.Workers[i].TenantID < snapshot.Workers[j].TenantID
		}
		return snapshot.Workers[i].WorkerID < snapshot.Workers[j].WorkerID
	})

	return snapshot
}

func cloneHeartbeat(heartbeat Heartbeat) Heartbeat {
	heartbeat.Queues = append([]string(nil), heartbeat.Queues...)
	heartbeat.Capabilities = append([]Capability(nil), heartbeat.Capabilities...)

	return heartbeat
}
