package redisdb

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisStreamNativeManagementStatus(t *testing.T) {
	t.Parallel()

	now := time.Unix(20, 0).UTC()
	worker := &Worker{
		opts: options{
			streamName: "critical", group: "workers", consumer: "worker-1",
			management: &management.StatusMetadata{
				ID: "worker-1", Version: "v1.2.3", Concurrency: 4,
				Protocol: management.ProtocolVersion{Major: 1},
			},
		},
		startedAt: now.Add(-time.Hour),
		readGroups: func(context.Context, string) ([]redis.XInfoGroup, error) {
			return []redis.XInfoGroup{{Name: "workers", Pending: 2, Lag: 3}}, nil
		},
		readPending: func(context.Context, *redis.XPendingExtArgs) ([]redis.XPendingExt, error) {
			return []redis.XPendingExt{{ID: "10000-0"}}, nil
		},
		readRange: func(context.Context, string, string, string, int64) ([]redis.XMessage, error) {
			return nil, nil
		},
		now:               func() time.Time { return now },
		groupLagSupported: true,
	}
	worker.currentJobs.Store(2)

	status, err := worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "worker-1", status.ID)
	assert.Equal(t, uint32(2), status.CurrentJobs)
	assert.Equal(t, management.WorkerRunning, status.State)
	assert.Equal(t, []management.Capability{
		management.CapabilityWorkerStatus, management.CapabilityQueueStatus,
		management.CapabilityFailures, management.CapabilityDeadLetters,
		management.CapabilityRetry, management.CapabilityBulkRetry,
		management.CapabilityDelete, management.CapabilityPurge,
	}, status.Capabilities)

	queueStatus, err := worker.ObserveQueue(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(5), queueStatus.Metrics.Depth.Value)
	assert.True(t, queueStatus.Metrics.Depth.Supported)
	assert.Equal(t, int64(2), queueStatus.Metrics.Pending.Value)
	assert.True(t, queueStatus.Metrics.OldestAge.Supported)

	atomic.StoreInt32(&worker.stopFlag, 1)
	status, err = worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.Equal(t, management.WorkerStopped, status.State)
}

func TestRedisGroupLagCapabilityRequiresRedisSeven(t *testing.T) {
	t.Parallel()

	assert.True(t, redisGroupLagSupported("# Server\r\nredis_version:7.4.1\r\n"))
	for _, info := range []string{
		"redis_version:6.2.22\n", "redis_version:invalid\n",
		"redis_version:7\n", "server_version:8.0.0\n",
	} {
		assert.False(t, redisGroupLagSupported(info))
	}
}

func TestRedisStreamManagementStatusFailsClosed(t *testing.T) {
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
	worker.readGroups = func(context.Context, string) ([]redis.XInfoGroup, error) {
		return nil, statsErr
	}
	_, err = worker.ObserveQueue(context.Background())
	assert.ErrorIs(t, err, statsErr)
}

func TestRedisStreamManagementOptionRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	_, err := NewWorkerE(WithManagementStatus(management.StatusMetadata{}))
	assert.ErrorIs(t, err, ErrInvalidManagementStatus)
}

func TestRedisStreamAdvertisesReplayOnlyWithAnAllowlist(t *testing.T) {
	t.Parallel()

	worker := &Worker{opts: options{
		streamName: "jobs", replayDestinations: map[string]struct{}{"archive": {}},
		management: &management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		},
	}, startedAt: time.Now().Add(-time.Minute), now: time.Now}
	status, err := worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.Contains(t, status.Capabilities, management.CapabilityReplay)

	worker.opts.replayDestinations = nil
	status, err = worker.ObserveWorker(context.Background())
	require.NoError(t, err)
	assert.NotContains(t, status.Capabilities, management.CapabilityReplay)
}

func TestRedisStreamReportsConfiguredRetentionCapabilities(t *testing.T) {
	t.Parallel()

	worker := &Worker{opts: options{
		streamName: "jobs", recordMaxLength: 100,
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
