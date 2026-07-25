package valkeystream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValkeyStreamNativeManagementStatus(t *testing.T) {
	t.Parallel()

	now := time.Unix(20, 0).UTC()
	transport := &fakeTransport{state: streamqueue.GroupState{
		Pending: 2, Lag: 3, OldestPendingID: "10000-0",
	}}
	worker := &Worker{
		opts: options{
			stream: "critical", group: "workers", consumer: "worker-1",
			management: &management.StatusMetadata{
				ID: "worker-1", Version: "v1.2.3", Concurrency: 4,
				Protocol: management.ProtocolVersion{Major: 1},
			},
		},
		transport: transport, startedAt: now.Add(-time.Hour),
		now: func() time.Time { return now },
	}
	worker.currentJobs.Store(2)
	worker.metrics.acknowledged.Store(7)
	worker.metrics.reclaimed.Store(3)
	worker.metrics.retries.Store(4)
	worker.metrics.deadLettered.Store(2)
	worker.metrics.settlementFailures.Store(1)

	status, err := worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "worker-1", status.ID)
	assert.Equal(t, uint32(2), status.CurrentJobs)
	assert.Equal(t, management.WorkerRunning, status.State)
	assert.Contains(t, status.Capabilities, management.CapabilityFailures)
	assert.Contains(t, status.Capabilities, management.CapabilityDeadLetters)
	assert.Contains(t, status.Capabilities, management.CapabilityRetry)
	assert.Contains(t, status.Capabilities, management.CapabilityBulkRetry)
	assert.Contains(t, status.Capabilities, management.CapabilityDelete)
	assert.Contains(t, status.Capabilities, management.CapabilityPurge)
	assert.NotContains(t, status.Capabilities, management.CapabilityReplay)

	queueStatus, err := worker.ObserveQueue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(5), queueStatus.Metrics.Depth.Value)
	assert.Equal(t, uint64(7), queueStatus.Metrics.Succeeded.Value)
	assert.Equal(t, uint64(3), queueStatus.Metrics.Reclaimed.Value)
	assert.Equal(t, uint64(4), queueStatus.Metrics.Retried.Value)
	assert.Equal(t, uint64(2), queueStatus.Metrics.DeadLettered.Value)
	assert.Equal(t, uint64(1), queueStatus.Metrics.SettlementErrors.Value)

	worker.stopped.Store(true)
	worker.opts.replayDestinations = map[string]struct{}{"archive": {}}
	status, err = worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.Equal(t, management.WorkerStopped, status.State)
	assert.Contains(t, status.Capabilities, management.CapabilityReplay)
}

func TestValkeyStreamReportsConfiguredRetentionCapabilities(t *testing.T) {
	t.Parallel()

	worker := &Worker{opts: options{
		stream: "jobs", recordMaxLength: 100,
		management: &management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		},
	}, startedAt: time.Now().Add(-time.Minute), now: time.Now}
	status, err := worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.Contains(t, status.Capabilities, management.CapabilityRetentionCount)
	assert.NotContains(t, status.Capabilities, management.CapabilityRetentionTime)
	assert.NotContains(t, status.Capabilities, management.CapabilityRetentionBytes)

	worker.opts.recordMaxLength = 0
	status, err = worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, status.Capabilities, management.CapabilityRetentionCount)
}

func TestValkeyStreamManagementStatusFailsClosed(t *testing.T) {
	t.Parallel()

	worker := &Worker{}
	_, err := worker.ObserveWorker(context.Background())
	assert.ErrorIs(t, err, ErrManagementStatusDisabled)
	_, err = worker.ObserveQueue(context.Background())
	assert.ErrorIs(t, err, ErrManagementStatusDisabled)

	worker.opts.management = &management.StatusMetadata{
		ID: "worker-1", Version: "v1", Concurrency: 1,
		Protocol: management.ProtocolVersion{Major: 1},
	}
	worker.startedAt = time.Unix(1, 0).UTC()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = worker.ObserveWorker(cancelled)
	assert.ErrorIs(t, err, context.Canceled)
	_, err = worker.ObserveQueue(cancelled)
	assert.ErrorIs(t, err, context.Canceled)

	worker.now = func() time.Time { return worker.startedAt.Add(-time.Second) }
	_, err = worker.ObserveWorker(context.Background())
	assert.ErrorIs(t, err, ErrInvalidManagementStatus)
	worker.now = nil
	_, err = worker.ObserveWorker(context.Background())
	require.NoError(t, err)

	statsErr := errors.New("stats unavailable")
	worker.transport = &fakeTransport{stateErr: statsErr}
	_, err = worker.ObserveQueue(context.Background())
	assert.ErrorIs(t, err, statsErr)
}

func TestValkeyStreamManagementOptionRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	_, err := newOptions(WithManagementStatus(management.StatusMetadata{}))
	assert.ErrorIs(t, err, ErrInvalidManagementStatus)
	metadata := management.StatusMetadata{
		ID: "worker-1", Version: "v1", Concurrency: 1,
		Protocol: management.ProtocolVersion{Major: 1},
	}
	opts, err := newOptions(WithAddress("127.0.0.1:6379"), WithManagementStatus(metadata))
	require.NoError(t, err)
	assert.Equal(t, metadata, *opts.management)
}
