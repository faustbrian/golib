//go:build integration

package redisdb

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type mockMessage struct {
	Message string
}

func (m mockMessage) Bytes() []byte {
	return []byte(m.Message)
}

func (m mockMessage) Payload() []byte {
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

func setupRedisSentinelContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	var sentinelPort string
	redisC := startPortBoundRedisContainer(ctx, t, func() testcontainers.ContainerRequest {
		reservePort := func() string {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
			require.NoError(t, listener.Close())
			return port
		}
		masterPort := reservePort()
		sentinelPort = reservePort()
		masterContainerPort := network.MustParsePort(masterPort + "/tcp")
		sentinelContainerPort := network.MustParsePort(sentinelPort + "/tcp")
		script := fmt.Sprintf(
			"redis-server --port %s --daemonize yes && "+
				"printf 'port %s\\nsentinel monitor mymaster 127.0.0.1 %s 1\\n' > /tmp/sentinel.conf && "+
				"redis-server /tmp/sentinel.conf --sentinel",
			masterPort,
			sentinelPort,
			masterPort,
		)

		return testcontainers.ContainerRequest{
			Image:        "redis:6.2.22@sha256:3b477db2f54035771360d023c9aff4c6255ba833834511b8eedc5ba8c10d0bce",
			ExposedPorts: []string{masterPort + "/tcp", sentinelPort + "/tcp"},
			Cmd:          []string{"sh", "-c", script},
			HostConfigModifier: func(config *container.HostConfig) {
				config.PortBindings = network.PortMap{
					masterContainerPort: {{
						HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: masterPort,
					}},
					sentinelContainerPort: {{
						HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: sentinelPort,
					}},
				}
			},
			WaitingFor: wait.NewExecStrategy([]string{
				"redis-cli", "-p", sentinelPort,
				"SENTINEL", "get-master-addr-by-name", "mymaster",
			}),
		}
	})

	return redisC, "127.0.0.1:" + sentinelPort
}

func TestWithRedis(t *testing.T) {
	ctx := context.Background()
	redisC, _ := setupRedisContainer(ctx, t)
	testcontainers.CleanupContainer(t, redisC)
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
		WithChannel("test"),
		WithDebug(),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	assert.NoError(t, q.Queue(m))
	m.Message = "bar"
	assert.NoError(t, q.Queue(m))
	waitForCompleted(t, q, 2)
	q.Release()
}

func TestRedisDropsMessagesPublishedWithoutSubscriber(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	client := redis.NewClient(&redis.Options{Addr: endpoint})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	message := job.NewMessage(mockMessage{Message: "published-before-subscribe"})

	subscribers, err := client.Publish(ctx, "lossy", message.Bytes()).Result()
	require.NoError(t, err)
	assert.Zero(t, subscribers)

	worker := NewWorker(
		WithAddr(endpoint),
		WithChannel("lossy"),
		WithRequestTimeout(20*time.Millisecond),
	)
	t.Cleanup(func() { _ = worker.Shutdown() })
	received, err := worker.Request()
	assert.Nil(t, received)
	assert.ErrorIs(t, err, queue.ErrNoTaskInQueue)
}

func TestRedisPubSubRecoversAfterBrokerRestart(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	worker := NewWorker(
		WithAddr(endpoint),
		WithChannel("restart"),
		WithConnectTimeout(250*time.Millisecond),
	)
	q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(1))
	require.NoError(t, err)
	q.Start()
	require.NoError(t, q.Queue(mockMessage{Message: "before-restart"}))
	waitForCompleted(t, q, 1)

	stopRedisServer(ctx, t, redisC)
	assert.Error(t, q.Queue(mockMessage{Message: "during-outage"}))
	startRedisServer(ctx, t, redisC)
	require.Eventually(t, func() bool {
		return q.Queue(mockMessage{Message: "after-restart"}) == nil
	}, 10*time.Second, 50*time.Millisecond)
	waitForCompleted(t, q, 2)
	q.Release()
}

func TestRedisShutdown(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	w := NewWorker(
		WithAddr(endpoint),
		WithChannel("test2"),
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
		WithChannel("test3"),
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
		WithChannel("testCluster"),
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

func TestRedisSentinel(t *testing.T) {
	ctx := context.Background()
	redisC, endpoint := setupRedisSentinelContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, redisC)

	m := &mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})

	w := NewWorker(
		WithAddr(endpoint),
		WithMasterName("mymaster"),
		WithChannel("testSentinel"),
		WithSentinel(),
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
	deadline := make(chan error, 1)
	w := NewWorker(
		WithAddr(endpoint),
		WithChannel("timeout"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			close(started)
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
	waitForCompleted(t, q, 1)
	q.Release()
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
		WithChannel("cancel"),
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
		WithChannel("GoroutineLeak"),
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
		WithChannel("GoroutinePanic"),
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
	assert.Equal(t, uint64(2), q.FailureTasks())
	assert.Error(t, q.Queue(m))
}
