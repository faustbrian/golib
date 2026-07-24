package redisdb

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

var (
	// ErrManagementStatusDisabled reports a worker without reporter metadata.
	ErrManagementStatusDisabled = errors.New("redisstream: management status disabled")
	// ErrInvalidManagementStatus reports malformed reporter metadata or time.
	ErrInvalidManagementStatus = errors.New("redisstream: invalid management status")
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
	if atomic.LoadInt32(&w.stopFlag) == 1 {
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
		Queues: []string{w.opts.streamName}, Concurrency: metadata.Concurrency,
		State: state, CurrentJobs: w.currentJobs.Load(),
		DrainStatus: management.DrainNotRequested, Backend: w.BackendName(),
		Protocol:     metadata.Protocol,
		Capabilities: capabilities,
	}, nil
}

// ObserveQueue returns honest native Redis Streams measurements.
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
		Pending:   management.Measurement[int64]{Value: stats.Pending, Supported: true},
		OldestAge: management.Measurement[time.Duration]{Value: stats.OldestJobAge, Supported: true},
	}
	if w.groupLagSupported && stats.LagKnown {
		metrics.Depth = management.Measurement[int64]{Value: stats.Depth, Supported: true}
		metrics.Lag = management.Measurement[int64]{Value: stats.Lag, Supported: true}
	}

	return management.QueueStatus{
		Backend: w.BackendName(), Queue: w.opts.streamName,
		ObservedAt: w.statusNow(), Metrics: metrics,
	}, nil
}

func redisGroupLagSupported(info string) bool {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "redis_version:") {
			continue
		}
		version := strings.TrimSpace(strings.TrimPrefix(line, "redis_version:"))
		major, _, found := strings.Cut(version, ".")
		if !found {
			return false
		}
		value, err := strconv.ParseUint(major, 10, 16)

		return err == nil && value >= 7
	}

	return false
}

func (w *Worker) statusNow() time.Time {
	if w.now == nil {
		return time.Now().UTC()
	}

	return w.now().UTC()
}
