package management

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	// MaxIdentityBytes bounds worker, backend, queue, and capability labels.
	MaxIdentityBytes = 256
	// MaxQueuesPerWorker bounds one heartbeat's queue cardinality.
	MaxQueuesPerWorker = 256
	// MaxCapabilitiesPerWorker bounds one heartbeat's capability cardinality.
	MaxCapabilitiesPerWorker = 64
	// MaxWorkerConcurrency bounds reported goroutine concurrency.
	MaxWorkerConcurrency = 1_000_000
	// MaxStatusPageSize bounds one worker or queue status response.
	MaxStatusPageSize = 200
)

// WorkerState is the state reported by a live worker.
type WorkerState string

const (
	WorkerRunning  WorkerState = "running"
	WorkerPaused   WorkerState = "paused"
	WorkerDraining WorkerState = "draining"
	WorkerStopped  WorkerState = "stopped"
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

// WorkerStatus is the bounded status contract emitted by a worker.
type WorkerStatus struct {
	ID           string
	Version      string
	StartedAt    time.Time
	HeartbeatAt  time.Time
	Queues       []string
	Concurrency  uint32
	State        WorkerState
	CurrentJobs  uint32
	DrainStatus  DrainState
	Backend      string
	Protocol     ProtocolVersion
	Capabilities []Capability
}

// StatusMetadata configures stable identity for a native worker reporter.
type StatusMetadata struct {
	ID          string
	Version     string
	Concurrency uint32
	Protocol    ProtocolVersion
}

// Validate rejects incomplete or unbounded native reporter metadata.
func (m StatusMetadata) Validate() error {
	if invalidIdentity(m.ID) {
		return invalid("id", "is required and must be bounded")
	}
	if invalidIdentity(m.Version) {
		return invalid("version", "is required and must be bounded")
	}
	if m.Concurrency == 0 || m.Concurrency > MaxWorkerConcurrency {
		return invalid("concurrency", "must be within the worker limit")
	}
	if m.Protocol == (ProtocolVersion{}) {
		return invalid("protocol", "is required")
	}

	return nil
}

// Measurement distinguishes an honestly measured zero from an unsupported
// backend metric.
type Measurement[T any] struct {
	Value     T
	Supported bool
}

// QueueMetrics reports current gauges and monotonic lifecycle counters where
// the backend can measure them honestly.
type QueueMetrics struct {
	Depth            Measurement[int64]
	Lag              Measurement[int64]
	Pending          Measurement[int64]
	OldestAge        Measurement[time.Duration]
	Throughput       Measurement[float64]
	Runtime          Measurement[time.Duration]
	Succeeded        Measurement[uint64]
	Failed           Measurement[uint64]
	Retried          Measurement[uint64]
	Reclaimed        Measurement[uint64]
	DeadLettered     Measurement[uint64]
	SettlementErrors Measurement[uint64]
}

// QueueStatus is a point-in-time backend-neutral queue observation.
type QueueStatus struct {
	Backend    string
	Queue      string
	ObservedAt time.Time
	Metrics    QueueMetrics
}

// StatusPageRequest defines bounded opaque-cursor status pagination.
type StatusPageRequest struct {
	Cursor string
	Limit  uint32
}

// Validate rejects unbounded status requests.
func (r StatusPageRequest) Validate() error {
	if r.Limit == 0 || r.Limit > MaxStatusPageSize {
		return invalid("limit", "must be within the page limit")
	}
	if len(r.Cursor) > MaxCursorBytes {
		return invalid("cursor", "exceeds the cursor limit")
	}

	return nil
}

// WorkerStatusPage is one bounded page of worker observations.
type WorkerStatusPage struct {
	Items      []WorkerStatus
	NextCursor string
}

// Validate rejects malformed or oversized worker adapter output.
func (p WorkerStatusPage) Validate() error {
	if len(p.Items) > MaxStatusPageSize {
		return invalid("items", "exceeds the page limit")
	}
	for index, item := range p.Items {
		if err := item.Validate(); err != nil {
			var validationError *ValidationError
			_ = errors.As(err, &validationError)

			return invalid(
				fmt.Sprintf("items[%d].%s", index, validationError.Field),
				validationError.Problem,
			)
		}
	}
	if len(p.NextCursor) > MaxCursorBytes {
		return invalid("next_cursor", "exceeds the cursor limit")
	}

	return nil
}

// QueueStatusPage is one bounded page of queue observations.
type QueueStatusPage struct {
	Items      []QueueStatus
	NextCursor string
}

// Validate rejects malformed or oversized queue adapter output.
func (p QueueStatusPage) Validate() error {
	if len(p.Items) > MaxStatusPageSize {
		return invalid("items", "exceeds the page limit")
	}
	for index, item := range p.Items {
		if err := item.Validate(); err != nil {
			var validationError *ValidationError
			_ = errors.As(err, &validationError)

			return invalid(
				fmt.Sprintf("items[%d].%s", index, validationError.Field),
				validationError.Problem,
			)
		}
	}
	if len(p.NextCursor) > MaxCursorBytes {
		return invalid("next_cursor", "exceeds the cursor limit")
	}

	return nil
}

// StatusReader is implemented by queue adapters that expose bounded
// backend-neutral worker and queue observations.
type StatusReader interface {
	ListWorkers(context.Context, StatusPageRequest) (WorkerStatusPage, error)
	ListQueues(context.Context, StatusPageRequest) (QueueStatusPage, error)
}

// Validate rejects malformed or unbounded worker reports.
func (s WorkerStatus) Validate() error {
	if invalidIdentity(s.ID) {
		return invalid("id", "is required and must be bounded")
	}
	if invalidIdentity(s.Version) {
		return invalid("version", "is required and must be bounded")
	}
	if s.StartedAt.IsZero() {
		return invalid("started_at", "is required")
	}
	if s.HeartbeatAt.IsZero() {
		return invalid("heartbeat_at", "is required")
	}
	if s.HeartbeatAt.Before(s.StartedAt) {
		return invalid("heartbeat_at", "cannot precede started_at")
	}
	if len(s.Queues) > MaxQueuesPerWorker {
		return invalid("queues", "exceeds the worker queue limit")
	}
	for index, queue := range s.Queues {
		if invalidIdentity(queue) {
			return invalid(fmt.Sprintf("queues[%d]", index), "is invalid")
		}
	}
	if s.Concurrency == 0 || s.Concurrency > MaxWorkerConcurrency {
		return invalid("concurrency", "must be within the worker limit")
	}
	if s.CurrentJobs > s.Concurrency {
		return invalid("current_jobs", "cannot exceed concurrency")
	}
	if !s.State.valid() {
		return invalid("state", "is unsupported")
	}
	if !s.DrainStatus.valid() {
		return invalid("drain_status", "is unsupported")
	}
	if invalidIdentity(s.Backend) {
		return invalid("backend", "is required and must be bounded")
	}
	if s.Protocol == (ProtocolVersion{}) {
		return invalid("protocol", "is required")
	}
	if len(s.Capabilities) > MaxCapabilitiesPerWorker {
		return invalid("capabilities", "exceeds the worker capability limit")
	}
	for index, capability := range s.Capabilities {
		if invalidIdentity(string(capability)) {
			return invalid(fmt.Sprintf("capabilities[%d]", index), "is invalid")
		}
	}

	return nil
}

// Validate rejects malformed queue observations.
func (s QueueStatus) Validate() error {
	if invalidIdentity(s.Backend) {
		return invalid("backend", "is required and must be bounded")
	}
	if invalidIdentity(s.Queue) {
		return invalid("queue", "is required and must be bounded")
	}
	if s.ObservedAt.IsZero() {
		return invalid("observed_at", "is required")
	}
	if s.Metrics.Depth.Supported && s.Metrics.Depth.Value < 0 {
		return invalid("metrics.depth", "cannot be negative")
	}
	if s.Metrics.Lag.Supported && s.Metrics.Lag.Value < 0 {
		return invalid("metrics.lag", "cannot be negative")
	}
	if s.Metrics.Pending.Supported && s.Metrics.Pending.Value < 0 {
		return invalid("metrics.pending", "cannot be negative")
	}
	if s.Metrics.OldestAge.Supported && s.Metrics.OldestAge.Value < 0 {
		return invalid("metrics.oldest_age", "cannot be negative")
	}
	if s.Metrics.Throughput.Supported &&
		(s.Metrics.Throughput.Value < 0 || math.IsNaN(s.Metrics.Throughput.Value) ||
			math.IsInf(s.Metrics.Throughput.Value, 0)) {
		return invalid("metrics.throughput", "must be finite and non-negative")
	}
	if s.Metrics.Runtime.Supported && s.Metrics.Runtime.Value < 0 {
		return invalid("metrics.runtime", "cannot be negative")
	}

	return nil
}

func (s WorkerState) valid() bool {
	switch s {
	case WorkerRunning, WorkerPaused, WorkerDraining, WorkerStopped:
		return true
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

func invalidIdentity(value string) bool {
	return strings.TrimSpace(value) == "" || len(value) > MaxIdentityBytes
}

// ValidationError identifies one invalid management-contract field.
type ValidationError struct {
	Field   string
	Problem string
}

// Error implements error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Problem)
}

func invalid(field, problem string) error {
	return &ValidationError{Field: field, Problem: problem}
}
