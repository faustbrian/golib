package valkeystream

import (
	"context"
	"errors"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

var (
	// ErrManagementStatusDisabled reports a worker without reporter metadata.
	ErrManagementStatusDisabled = errors.New("valkeystream: management status disabled")
	// ErrInvalidManagementStatus reports malformed reporter metadata or time.
	ErrInvalidManagementStatus = errors.New("valkeystream: invalid management status")
)

var _ management.StatusProvider = (*Worker)(nil)

// ObserveWorker returns this worker's bounded native management observation.
func (w *Worker) ObserveWorker(ctx context.Context) (management.WorkerStatus, error) {
	if w.opts.management == nil || w.startedAt.IsZero() {
		return management.WorkerStatus{}, ErrManagementStatusDisabled
	}
	if err := ctx.Err(); err != nil {
		return management.WorkerStatus{}, err
	}
	now := w.statusNow()
	if now.Before(w.startedAt) {
		return management.WorkerStatus{}, ErrInvalidManagementStatus
	}
	state := management.WorkerRunning
	if w.stopped.Load() {
		state = management.WorkerStopped
	}
	metadata := w.opts.management

	capabilities := []management.Capability{
		management.CapabilityWorkerStatus,
		management.CapabilityQueueStatus,
		management.CapabilityFailures,
		management.CapabilityDeadLetters,
		management.CapabilityRetry,
		management.CapabilityBulkRetry,
		management.CapabilityDelete,
		management.CapabilityPurge,
	}
	if len(w.opts.replayDestinations) > 0 {
		capabilities = append(capabilities, management.CapabilityReplay)
	}
	if w.opts.recordMaxLength > 0 {
		capabilities = append(capabilities, management.CapabilityRetentionCount)
	}

	return management.WorkerStatus{
		ID: metadata.ID, Version: metadata.Version,
		StartedAt: w.startedAt, HeartbeatAt: now,
		Queues: []string{w.opts.stream}, Concurrency: metadata.Concurrency,
		State: state, CurrentJobs: w.currentJobs.Load(),
		DrainStatus: management.DrainNotRequested, Backend: w.BackendName(),
		Protocol:     metadata.Protocol,
		Capabilities: capabilities,
	}, nil
}

// ObserveQueue returns honest native Valkey Streams measurements and counters.
func (w *Worker) ObserveQueue(ctx context.Context) (management.QueueStatus, error) {
	if w.opts.management == nil {
		return management.QueueStatus{}, ErrManagementStatusDisabled
	}
	if err := ctx.Err(); err != nil {
		return management.QueueStatus{}, err
	}
	stats, err := w.Stats(ctx)
	if err != nil {
		return management.QueueStatus{}, err
	}
	metrics := management.QueueMetrics{
		Pending: management.Measurement[int64]{Value: stats.Pending, Supported: true},
		OldestAge: management.Measurement[time.Duration]{
			Value: stats.OldestPendingAge, Supported: true,
		},
		Succeeded:        management.Measurement[uint64]{Value: stats.Acknowledged, Supported: true},
		Retried:          management.Measurement[uint64]{Value: stats.Retries, Supported: true},
		Reclaimed:        management.Measurement[uint64]{Value: stats.Reclaimed, Supported: true},
		DeadLettered:     management.Measurement[uint64]{Value: stats.DeadLettered, Supported: true},
		SettlementErrors: management.Measurement[uint64]{Value: stats.SettlementFailures, Supported: true},
	}
	if stats.LagKnown {
		metrics.Depth = management.Measurement[int64]{Value: stats.Depth, Supported: true}
		metrics.Lag = management.Measurement[int64]{Value: stats.Lag, Supported: true}
	}

	return management.QueueStatus{
		Backend: w.BackendName(), Queue: w.opts.stream,
		ObservedAt: w.statusNow(), Metrics: metrics,
	}, nil
}

func (w *Worker) statusNow() time.Time {
	if w.now == nil {
		return time.Now().UTC()
	}

	return w.now().UTC()
}
