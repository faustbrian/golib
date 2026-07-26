// Package fleet models worker liveness and compatibility without supervising
// worker processes or implementing queue delivery semantics.
package fleet

import (
	"fmt"
	"strings"
	"time"
)

const (
	// MaxIdentityBytes bounds worker status labels.
	MaxIdentityBytes = 256
	// MaxQueuesPerWorker bounds one heartbeat's queue cardinality.
	MaxQueuesPerWorker = 256
	// MaxCapabilitiesPerWorker bounds negotiated capability cardinality.
	MaxCapabilitiesPerWorker = 64
	// MaxWorkerConcurrency bounds reported goroutine concurrency.
	MaxWorkerConcurrency = 1_000_000
)

// State is the worker state reported to administrative consumers.
type State string

const (
	StateRunning  State = "running"
	StatePaused   State = "paused"
	StateDraining State = "draining"
	StateStopped  State = "stopped"
	StateStale    State = "stale"
	StateUnknown  State = "unknown"
)

// DrainState describes progress of a graceful drain request.
type DrainState string

const (
	DrainNotRequested DrainState = "not_requested"
	DrainRequested    DrainState = "requested"
	DrainInProgress   DrainState = "in_progress"
	DrainCompleted    DrainState = "completed"
	DrainTimedOut     DrainState = "timed_out"
)

// Heartbeat is the latest status reported by a worker.
type Heartbeat struct {
	TenantID     string
	WorkerID     string
	Version      string
	StartedAt    time.Time
	ObservedAt   time.Time
	Queues       []string
	Concurrency  uint32
	State        State
	CurrentJobs  uint32
	DrainStatus  DrainState
	Backend      string
	Protocol     ProtocolVersion
	Capabilities []Capability
}

// Validate rejects malformed or unbounded worker status reports.
func (h Heartbeat) Validate() error {
	identities := []struct {
		field string
		value string
	}{
		{field: "tenant_id", value: h.TenantID},
		{field: "worker_id", value: h.WorkerID},
		{field: "version", value: h.Version},
		{field: "backend", value: h.Backend},
	}
	for _, identity := range identities {
		if invalidHeartbeatIdentity(identity.value) {
			return invalidHeartbeat(identity.field, "is required and must be bounded")
		}
	}
	if h.ObservedAt.IsZero() {
		return invalidHeartbeat("observed_at", "is required")
	}
	if h.StartedAt.IsZero() || h.StartedAt.After(h.ObservedAt) {
		return invalidHeartbeat("started_at", "is required and cannot follow observation")
	}
	if len(h.Queues) > MaxQueuesPerWorker {
		return invalidHeartbeat("queues", "exceeds the worker queue limit")
	}
	for index, queue := range h.Queues {
		if invalidHeartbeatIdentity(queue) {
			return invalidHeartbeat(fmt.Sprintf("queues[%d]", index), "is invalid")
		}
	}
	if h.Concurrency == 0 || h.Concurrency > MaxWorkerConcurrency {
		return invalidHeartbeat("concurrency", "must be within the worker limit")
	}
	if h.CurrentJobs > h.Concurrency {
		return invalidHeartbeat("current_jobs", "cannot exceed concurrency")
	}
	if !h.State.reported() {
		return invalidHeartbeat("state", "is unsupported")
	}
	if !h.DrainStatus.valid() {
		return invalidHeartbeat("drain_status", "is unsupported")
	}
	if h.Protocol == (ProtocolVersion{}) {
		return invalidHeartbeat("protocol", "is required")
	}
	if len(h.Capabilities) > MaxCapabilitiesPerWorker {
		return invalidHeartbeat("capabilities", "exceeds the worker capability limit")
	}
	for index, capability := range h.Capabilities {
		if invalidHeartbeatIdentity(string(capability)) {
			return invalidHeartbeat(fmt.Sprintf("capabilities[%d]", index), "is invalid")
		}
	}

	return nil
}

// EffectiveState safely classifies a heartbeat at the supplied observation
// time. Missing, future, and malformed reports are never treated as healthy.
func (h Heartbeat) EffectiveState(now time.Time, staleAfter time.Duration) State {
	if h.ObservedAt.IsZero() || h.ObservedAt.After(now) || !h.State.reported() {
		return StateUnknown
	}

	if !h.ObservedAt.After(now.Add(-staleAfter)) {
		return StateStale
	}

	return h.State
}

func (s State) reported() bool {
	switch s {
	case StateRunning, StatePaused, StateDraining, StateStopped:
		return true
	case StateStale, StateUnknown:
		return false
	default:
		return false
	}
}

func (s DrainState) valid() bool {
	switch s {
	case DrainNotRequested, DrainRequested, DrainInProgress, DrainCompleted, DrainTimedOut:
		return true
	default:
		return false
	}
}

func invalidHeartbeatIdentity(value string) bool {
	return strings.TrimSpace(value) == "" || len(value) > MaxIdentityBytes
}

// HeartbeatValidationError identifies one invalid worker-status field.
type HeartbeatValidationError struct {
	Field   string
	Problem string
}

func (e *HeartbeatValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Problem)
}

func invalidHeartbeat(field string, problem string) error {
	return &HeartbeatValidationError{Field: field, Problem: problem}
}
