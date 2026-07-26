//go:build integration

package nats

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"

	natsgo "github.com/nats-io/nats.go"
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

func setupNatsContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image: "nats:2.10.29@sha256:5498ba57b9471840be3d15b033a4eec554d1c02fa6c2cc0ca2d888637f6c6e2f",
		ExposedPorts: []string{
			"4222/tcp", // client port
			"6222/tcp", // cluster port
			"8222/tcp", // monitoring port
		},
		WaitingFor: wait.ForLog("Server is ready"),
	}
	natsC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	endpoint, err := natsC.Endpoint(ctx, "")
	require.NoError(t, err)

	return natsC, endpoint
}

func TestDefaultFlow(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)

	m := &mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithSubj("test"),
		WithQueue("test"),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(1),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	q.Start()
	waitForCompleted(t, q, 2)
	q.Release()
}

func TestCoreNATSDropsMessagesPublishedWithoutSubscriber(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)

	client, err := natsgo.Connect(endpoint)
	require.NoError(t, err)
	t.Cleanup(client.Close)
	message := job.NewMessage(mockMessage{Message: "published-before-subscribe"})
	require.NoError(t, client.Publish("lossy", message.Bytes()))
	require.NoError(t, client.Flush())

	worker := NewWorker(
		WithAddr(endpoint),
		WithSubj("lossy"),
		WithQueue("lossy"),
		WithRequestTimeout(20*time.Millisecond),
	)
	t.Cleanup(func() { _ = worker.Shutdown() })
	received, err := worker.Request()
	assert.Nil(t, received)
	assert.ErrorIs(t, err, queue.ErrNoTaskInQueue)
}

func TestCoreNATSReconnectsToAvailableBroker(t *testing.T) {
	ctx := context.Background()
	firstContainer, firstEndpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, firstContainer)
	secondContainer, secondEndpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, secondContainer)

	worker := NewWorker(
		WithAddr(firstEndpoint, secondEndpoint),
		WithSubj("restart"),
		WithQueue("restart"),
	)
	q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(1))
	require.NoError(t, err)
	q.Start()
	require.NoError(t, q.Queue(mockMessage{Message: "before-restart"}))
	waitForCompleted(t, q, 1)

	var activeContainer testcontainers.Container
	var survivingEndpoint string
	switch connectedURL := worker.client.ConnectedUrl(); {
	case strings.Contains(connectedURL, endpointHost(firstEndpoint)):
		activeContainer = firstContainer
		survivingEndpoint = secondEndpoint
	case strings.Contains(connectedURL, endpointHost(secondEndpoint)):
		activeContainer = secondContainer
		survivingEndpoint = firstEndpoint
	default:
		t.Fatalf("connected to unexpected NATS endpoint: %s", connectedURL)
	}
	stopTimeout := time.Second
	require.NoError(t, activeContainer.Stop(ctx, &stopTimeout))
	require.Eventually(t, func() bool {
		return worker.client.Status() == natsgo.CONNECTED &&
			strings.Contains(worker.client.ConnectedUrl(), endpointHost(survivingEndpoint))
	}, 30*time.Second, 10*time.Millisecond, "status=%s last_error=%v", worker.client.Status(), worker.client.LastError())

	require.NoError(t, q.Queue(mockMessage{Message: "after-restart"}))
	waitForCompleted(t, q, 2)
	q.Release()
}

func endpointHost(endpoint string) string {
	return strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
}

func TestClusteredHost(t *testing.T) {
	m := &mockMessage{
		Message: "foo",
	}
	url := runNATSServer(t)
	w := NewWorker(
		WithAddr("nats://127.0.0.1:1", url),
		WithSubj("test"),
		WithQueue("cluster"),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(1),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	q.Start()
	waitForCompleted(t, q, 2)
	q.Release()
}

func TestShutdown(t *testing.T) {
	w := NewWorker(
		WithAddr(runNATSServer(t)),
		WithSubj("test"),
		WithQueue("test"),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	q.Shutdown()
	// check shutdown once
	q.Shutdown()
	q.Wait()
}

func TestCustomFuncAndWait(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := &mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	w := NewWorker(
		WithAddr(endpoint),
		WithSubj("test"),
		WithQueue("test"),
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
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(2),
	)
	assert.NoError(t, err)
	q.Start()
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	waitForSignal(t, started)
	waitForSignal(t, started)
	close(release)
	waitForCompleted(t, q, 4)
	q.Shutdown()
	q.Wait()
}

func TestEnqueueJobAfterShutdown(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
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
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 1)
	deadline := make(chan error, 1)
	w := NewWorker(
		WithAddr(endpoint),
		WithSubj("JobReachTimeout"),
		WithQueue("test"),
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
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "test",
	}
	started := make(chan struct{}, 1)
	canceled := make(chan error, 1)
	w := NewWorker(
		WithAddr(endpoint),
		WithSubj("CancelJob"),
		WithQueue("test"),
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
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithSubj("GoroutineLeak"),
		WithQueue("test"),
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
	for i := 0; i < 500; i++ {
		assert.NoError(t, q.Queue(m))
	}
	waitForCompleted(t, q, 500)
	q.Release()
}

func TestGoroutinePanic(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithSubj("GoroutinePanic"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
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
	waitForCompleted(t, q, 2)
	assert.Equal(t, uint64(2), q.FailureTasks())
	q.Shutdown()
	assert.Error(t, q.Queue(m))
	q.Wait()
}

func TestReQueueTaskInWorkerBeforeShutdown(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNatsContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	job := &job.Message{
		Body: []byte("foo"),
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithSubj("test02"),
		WithQueue("test02"),
	)

	assert.NoError(t, w.Queue(job))
	require.Eventually(t, func() bool {
		delivered, err := w.subscription.Delivered()
		return err == nil && delivered == 1
	}, 5*time.Second, time.Millisecond)
	assert.NoError(t, w.Shutdown())
}
