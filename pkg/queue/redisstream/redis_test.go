//go:build integration

package redisdb

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func startPortBoundRedisContainer(
	ctx context.Context,
	t *testing.T,
	request func() testcontainers.ContainerRequest,
) testcontainers.Container {
	t.Helper()

	var lastErr error
	for range 5 {
		redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: request(),
			Started:          true,
		})
		if err == nil {
			return redisC
		}
		if redisC != nil {
			_ = redisC.Terminate(ctx)
		}
		if !strings.Contains(err.Error(), "port is already allocated") &&
			!strings.Contains(err.Error(), "address already in use") {
			require.NoError(t, err)
		}
		lastErr = err
	}

	require.NoError(t, lastErr)

	return nil
}

func setupRedisClusterContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	redisC := startPortBoundRedisContainer(ctx, t, func() testcontainers.ContainerRequest {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		hostPort := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
		require.NoError(t, listener.Close())

		return testcontainers.ContainerRequest{
			Image:        "redis:6.2.22@sha256:3b477db2f54035771360d023c9aff4c6255ba833834511b8eedc5ba8c10d0bce",
			ExposedPorts: []string{"6379/tcp"},
			Cmd: []string{
				"redis-server", "--cluster-enabled", "yes",
				"--cluster-config-file", "nodes.conf",
				"--cluster-node-timeout", "1000", "--appendonly", "no",
				"--cluster-announce-ip", "127.0.0.1",
				"--cluster-announce-port", hostPort,
			},
			HostConfigModifier: func(config *container.HostConfig) {
				config.PortBindings = network.PortMap{
					network.MustParsePort("6379/tcp"): {{
						HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: hostPort,
					}},
				}
			},
			WaitingFor: wait.NewExecStrategy(
				[]string{"redis-cli", "-h", "localhost", "-p", "6379", "ping"},
			),
		}
	})
	for _, command := range [][]string{
		{"sh", "-c", "redis-cli CLUSTER ADDSLOTS $(seq 0 16383)"},
	} {
		exitCode, _, execErr := redisC.Exec(ctx, command)
		require.NoError(t, execErr)
		require.Zero(t, exitCode)
	}
	require.Eventually(t, func() bool {
		exitCode, output, execErr := redisC.Exec(ctx, []string{"redis-cli", "CLUSTER", "INFO"})
		if execErr != nil || exitCode != 0 {
			return false
		}
		body, readErr := io.ReadAll(output)
		return readErr == nil && strings.Contains(string(body), "cluster_state:ok")
	}, 5*time.Second, 100*time.Millisecond)

	endpoint, err := redisC.Endpoint(ctx, "")
	require.NoError(t, err)

	return redisC, endpoint
}

func setupRedisContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image:        "redis:6.2.22@sha256:3b477db2f54035771360d023c9aff4c6255ba833834511b8eedc5ba8c10d0bce",
		ExposedPorts: []string{"6379/tcp"},
		Cmd: []string{
			"sh", "-c",
			"redis-server --daemonize yes && while :; do sleep 3600; done",
		},
		WaitingFor: wait.NewExecStrategy(
			[]string{"redis-cli", "-h", "localhost", "-p", "6379", "ping"},
		),
	}
	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	endpoint, err := redisC.PortEndpoint(ctx, "6379/tcp", "")
	require.NoError(t, err)

	return redisC, endpoint
}

func stopRedisServer(ctx context.Context, t *testing.T, redisC testcontainers.Container) {
	t.Helper()

	exitCode, _, err := redisC.Exec(ctx, []string{"redis-cli", "shutdown", "save"})
	require.NoError(t, err)
	require.Zero(t, exitCode)
	require.Eventually(t, func() bool {
		exitCode, _, execErr := redisC.Exec(ctx, []string{"redis-cli", "ping"})
		return execErr == nil && exitCode != 0
	}, 5*time.Second, 10*time.Millisecond)
}

func startRedisServer(ctx context.Context, t *testing.T, redisC testcontainers.Container) {
	t.Helper()

	exitCode, _, err := redisC.Exec(ctx, []string{"redis-server", "--daemonize", "yes"})
	require.NoError(t, err)
	require.Zero(t, exitCode)
	require.Eventually(t, func() bool {
		exitCode, _, execErr := redisC.Exec(ctx, []string{"redis-cli", "ping"})
		return execErr == nil && exitCode == 0
	}, 5*time.Second, 10*time.Millisecond)
}

func TestWithRedis(t *testing.T) {
	ctx := context.Background()
	redisC, _ := setupRedisContainer(ctx, t)
	testcontainers.CleanupContainer(t, redisC)
}

func TestRedisStreamNativeManagementStatusIntegration(t *testing.T) {
	ctx := t.Context()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	worker, err := NewWorkerE(
		WithAddr(endpoint), WithStreamName("management-status"),
		WithGroup("management-workers"), WithConsumer("worker-1"),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1.0.0", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		}),
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, worker.Shutdown()) }()
	require.NoError(t, worker.rdb.XGroupCreateMkStream(
		ctx, "management-status", "management-workers", "0",
	).Err())
	message := job.NewMessage(mockMessage{Message: "status-payload"})
	require.NoError(t, worker.Queue(&message))
	delivery, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("status-payload"), delivery.Payload())

	reader, err := management.NewStatusReader(management.StatusReaderConfig{
		Workers: []management.WorkerStatusProvider{worker},
		Queues:  []management.QueueStatusProvider{worker},
	})
	require.NoError(t, err)
	workers, err := reader.ListWorkers(ctx, management.StatusPageRequest{Limit: 1})
	require.NoError(t, err)
	require.Len(t, workers.Items, 1)
	assert.Equal(t, "worker-1", workers.Items[0].ID)
	assert.Equal(t, "redis-streams", workers.Items[0].Backend)
	queues, err := reader.ListQueues(ctx, management.StatusPageRequest{Limit: 1})
	require.NoError(t, err)
	require.Len(t, queues.Items, 1)
	assert.Equal(t, int64(1), queues.Items[0].Metrics.Pending.Value)
	assert.True(t, queues.Items[0].Metrics.Pending.Supported)
	assert.False(t, queues.Items[0].Metrics.Depth.Supported)
	assert.False(t, queues.Items[0].Metrics.Succeeded.Supported)
}

func TestRedisStreamDeadLetterLifecycleIntegration(t *testing.T) {
	ctx := t.Context()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	options := []Option{
		WithAddr(endpoint), WithStreamName("dead-letter-jobs"),
		WithGroup("dead-letter-workers"), WithBlockTime(10 * time.Millisecond),
		WithRequestTimeout(5 * time.Second), WithReclaim(time.Hour, time.Second, 1),
		WithDeadLetter("dead-letter-records", 2),
		WithFailureStream("failure-records"), WithLogger(queue.NewEmptyLogger()),
		WithRecordRetention(2),
		WithReplayDestinations("replay-archive"),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		}),
	}
	first, err := NewWorkerE(append(options, WithConsumer("worker-1"))...)
	require.NoError(t, err)
	message := job.NewMessage(mockMessage{Message: "retryable"})
	require.NoError(t, first.Queue(&message))
	delivery, err := first.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).NackFailure(errors.New("retry")))
	// Age the pending lease without a wall-clock wait. The second worker can
	// reclaim it once, while its realistic lease prevents duplicate reclaims.
	pendingLease, err := first.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: "dead-letter-jobs", Group: "dead-letter-workers",
		Start: "-", End: "+", Count: 1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, pendingLease, 1)
	redisClient, ok := first.rdb.(*redis.Client)
	require.True(t, ok)
	require.NoError(t, redisClient.Do(
		ctx, "XCLAIM", "dead-letter-jobs", "dead-letter-workers", "worker-1", 0,
		pendingLease[0].ID, "IDLE", int64(time.Hour/time.Millisecond),
		"RETRYCOUNT", 1, "JUSTID",
	).Err())
	require.NoError(t, first.Shutdown())

	second, err := NewWorkerE(append(
		options, WithConsumer("worker-2"),
		WithReclaim(time.Minute, time.Second, 1),
	)...)
	require.NoError(t, err)
	defer func() { require.NoError(t, second.Shutdown()) }()
	pendingBefore, err := second.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: "dead-letter-jobs", Group: "dead-letter-workers",
		Start: "-", End: "+", Count: 1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, pendingBefore, 1)
	require.Equal(t, int64(1), pendingBefore[0].RetryCount)
	require.GreaterOrEqual(t, pendingBefore[0].Idle, 59*time.Minute)
	reclaimed, err := second.Request()
	require.NoError(t, err)
	require.Equal(t, []byte("retryable"), reclaimed.Payload())
	require.NoError(t, reclaimed.(*job.Message).NackFailure(errors.New("retry again")))
	pending, err := second.rdb.XPending(ctx, "dead-letter-jobs", "dead-letter-workers").Result()
	require.NoError(t, err)
	require.Zero(t, pending.Count)
	failures, err := second.rdb.XLen(ctx, "failure-records").Result()
	require.NoError(t, err)
	require.Equal(t, int64(2), failures)
	deadLetters, err := second.rdb.XRange(ctx, "dead-letter-records", "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, deadLetters, 1)
	require.Equal(t, "2", deadLetters[0].Values[deliveryAttemptsField])
	require.Equal(t, "attempts_exhausted", deadLetters[0].Values[failureCodeField])
	page, err := second.ListDeadLetters(ctx, management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, management.CurrentEnvelopeVersion, page.Items[0].EnvelopeVersion)
	require.Equal(t, management.ClassificationRetryable, page.Items[0].Classification)
	revealed, err := second.Inspect(ctx, management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: page.Items[0].ID,
		Visibility: management.PayloadRevealed,
	})
	require.NoError(t, err)
	require.Equal(t, []byte(message.Bytes()), revealed.Payload.Data)

	now := time.Now().UTC()
	command := func(id string, action management.CommandAction, name string) management.Command {
		return management.Command{
			ID: id, IdempotencyKey: id, Actor: "integration-test", Reason: "verify Redis mutation",
			Protocol: management.ProtocolVersion{Major: 1}, Action: action,
			Target:      management.Target{Kind: management.TargetDeadLetter, Name: name},
			RequestedAt: now, Deadline: now.Add(time.Minute),
		}
	}
	replay := command("replay-1", management.CommandReplay, page.Items[0].ID)
	replay.Confirmed = true
	replay.Replay = &management.ReplayOptions{
		Destination: "replay-archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	result, err := second.Execute(ctx, replay)
	require.NoError(t, err)
	require.Equal(t, management.CommandAcknowledged, result.Status)
	replay.ID, replay.IdempotencyKey = "replay-duplicate", "replay-duplicate"
	result, err = second.Execute(ctx, replay)
	require.NoError(t, err)
	require.Equal(t, management.CommandRejected, result.Status)
	replay.ID, replay.IdempotencyKey = "replay-replace", "replay-replace"
	replay.Replay.IdempotencyPolicy = management.ReplayReplaceDuplicate
	result, err = second.Execute(ctx, replay)
	require.NoError(t, err)
	require.Equal(t, management.CommandAcknowledged, result.Status)
	replayed, err := second.rdb.XRange(ctx, "replay-archive", "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, replayed, 1)
	require.Equal(t, page.Items[0].ID, replayed[0].Values[replayPriorDeadLetterField])

	retry := command("retry-1", management.CommandRetry, page.Items[0].ID)
	result, err = second.Execute(ctx, retry)
	require.NoError(t, err)
	require.Equal(t, management.CommandAcknowledged, result.Status)

	for index := range 2 {
		require.NoError(t, second.appendRecord(
			ctx, "dead-letter-records", strconv.Itoa(index+10)+"-0",
			message.Bytes(), 1, streamqueue.FailureMetadata{
				Classification: management.ClassificationPermanent, Code: "invalid",
			},
		))
	}
	bulk := command("bulk-1", management.CommandBulkRetry, "dead-letter-records")
	bulk.Confirmed = true
	bulk.Selection = &management.Selection{Limit: 2}
	result, err = second.Execute(ctx, bulk)
	require.NoError(t, err)
	require.Equal(t, management.CommandAcknowledged, result.Status)

	require.NoError(t, second.appendRecord(
		ctx, "dead-letter-records", "20-0", message.Bytes(), 1,
		streamqueue.FailureMetadata{
			Classification: management.ClassificationPermanent, Code: "invalid",
		},
	))
	deleteRecords, err := second.rdb.XRevRangeN(ctx, "dead-letter-records", "+", "-", 1).Result()
	require.NoError(t, err)
	require.Len(t, deleteRecords, 1)
	remove := command("delete-1", management.CommandDelete, deleteRecords[0].ID)
	result, err = second.Execute(ctx, remove)
	require.NoError(t, err)
	require.Equal(t, management.CommandAcknowledged, result.Status)

	require.NoError(t, second.appendRecord(
		ctx, "dead-letter-records", "30-0", message.Bytes(), 1,
		streamqueue.FailureMetadata{
			Classification: management.ClassificationPermanent, Code: "invalid",
		},
	))
	purge := command("purge-1", management.CommandPurge, "dead-letter-records")
	purge.Confirmed = true
	result, err = second.Execute(ctx, purge)
	require.NoError(t, err)
	require.Equal(t, management.CommandAcknowledged, result.Status)
	deadLength, err := second.rdb.XLen(ctx, "dead-letter-records").Result()
	require.NoError(t, err)
	require.Zero(t, deadLength)
	for index := range 3 {
		require.NoError(t, second.appendRecord(
			ctx, "dead-letter-records", strconv.Itoa(index+40)+"-0",
			message.Bytes(), 1, streamqueue.FailureMetadata{
				Classification: management.ClassificationPermanent, Code: "invalid",
			},
		))
	}
	deadLength, err = second.rdb.XLen(ctx, "dead-letter-records").Result()
	require.NoError(t, err)
	require.Equal(t, int64(2), deadLength)
}

type mockMessage struct {
	Message string
}

func (m mockMessage) Bytes() []byte {
	return []byte(m.Message)
}

func waitForCompleted(t *testing.T, q *queue.Queue, count uint64) {
	t.Helper()
	require.Eventually(t, func() bool {
		return q.CompletedTasks() == count
	}, 5*time.Second, time.Millisecond)
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func TestRedisDefaultFlow(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	m := &mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("test"),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m))
	q.Start()
	waitForCompleted(t, q, 1)
	q.Release()
}

func TestRedisStreamBacklogSurvivesBrokerRestart(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	worker := NewWorker(
		WithAddr(endpoint),
		WithStreamName("restart"),
		WithConnectTimeout(250*time.Millisecond),
	)
	q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(1))
	require.NoError(t, err)
	require.NoError(t, q.Queue(mockMessage{Message: "queued-before-restart"}))

	stopRedisServer(ctx, t, redisC)
	startRedisServer(ctx, t, redisC)
	q.Start()
	waitForCompleted(t, q, 1)
	q.Release()
}

func TestRedisShutdown(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("test2"),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	q.Shutdown()
	// check shutdown once
	assert.Error(t, w.Shutdown())
	assert.Equal(t, queue.ErrQueueShutdown, w.Shutdown())
	q.Wait()
}

func TestCustomFuncAndWait(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	m := &mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("test3"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	)
	q := queue.NewPool(
		5,
		queue.WithWorker(w),
	)
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	waitForSignal(t, started)
	waitForSignal(t, started)
	waitForSignal(t, started)
	waitForSignal(t, started)
	close(release)
	waitForCompleted(t, q, 4)
	q.Release()
}

func TestRedisCluster(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisClusterContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	m := &mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})

	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("testCluster"),
		WithCluster(),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}),
	)
	q := queue.NewPool(
		5,
		queue.WithWorker(w),
	)
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	waitForSignal(t, started)
	waitForSignal(t, started)
	waitForSignal(t, started)
	waitForSignal(t, started)
	close(release)
	waitForCompleted(t, q, 4)
	q.Release()
}

func TestEnqueueJobAfterShutdown(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	q.Shutdown()
	// can't queue task after shutdown
	err = q.Queue(m)
	assert.Error(t, err)
	assert.Equal(t, queue.ErrQueueShutdown, err)
	q.Wait()
}

func TestJobReachTimeout(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	m := mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 1)
	deadline := make(chan error, 2)
	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("timeout"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			started <- struct{}{}
			<-ctx.Done()
			deadline <- ctx.Err()
			return ctx.Err()
		}),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	assert.NoError(t, q.Queue(m, job.AllowOption{
		Timeout: job.Time(20 * time.Millisecond),
	}))
	waitForSignal(t, started)
	assert.ErrorIs(t, <-deadline, context.DeadlineExceeded)
	q.Shutdown()
	q.Wait()
	assert.GreaterOrEqual(t, q.CompletedTasks(), uint64(1))
}

func TestCancelJobAfterShutdown(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	m := mockMessage{
		Message: "test",
	}
	started := make(chan struct{}, 1)
	canceled := make(chan error, 1)
	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("cancel"),
		WithLogger(queue.NewLogger()),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			close(started)
			<-ctx.Done()
			canceled <- ctx.Err()
			return ctx.Err()
		}),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	assert.NoError(t, q.Queue(m, job.AllowOption{
		Timeout: job.Time(time.Minute),
	}))
	waitForSignal(t, started)
	q.Shutdown()
	assert.ErrorIs(t, <-canceled, context.Canceled)
	q.Wait()
}

func TestGoroutineLeak(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("GoroutineLeak"),
		WithLogger(queue.NewEmptyLogger()),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			return nil
		}),
	)
	q, err := queue.NewQueue(
		queue.WithLogger(queue.NewEmptyLogger()),
		queue.WithWorker(w),
		queue.WithWorkerCount(10),
	)
	assert.NoError(t, err)
	q.Start()
	for i := 0; i < 50; i++ {
		assert.NoError(t, q.Queue(m))
	}
	waitForCompleted(t, q, 50)
	q.Release()
}

func TestGoroutinePanic(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)
	m := mockMessage{
		Message: "foo",
	}
	panicked := make(chan struct{}, 2)
	w := NewWorker(
		WithAddr(endpoint),
		WithStreamName("GoroutinePanic"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			panicked <- struct{}{}
			panic("missing something")
		}),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	waitForSignal(t, panicked)
	waitForSignal(t, panicked)
	q.Shutdown()
	q.Wait()
	assert.GreaterOrEqual(t, q.FailureTasks(), uint64(2))
	assert.Error(t, q.Queue(m))
}
