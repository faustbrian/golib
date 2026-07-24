//go:build integration

package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	}, 10*time.Second, time.Millisecond)
}

func waitForSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for signal")
	}
}

func setupRabbitMQContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image: "rabbitmq:3.13.7-management@sha256:e582c0bc7766f3342496d8485efb5a1df782b5ce3886ad017e2eaae442311f69",
		ExposedPorts: []string{
			"4369/tcp", // epmd
			"5672/tcp", // amqp
		},
		WaitingFor: wait.ForLog("Server startup complete"),
		Env: map[string]string{
			"RABBITMQ_DEFAULT_USER": "guest",
			"RABBITMQ_DEFAULT_PASS": "guest",
		},
	}
	rabbitMQC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	var endpoint string
	require.Eventually(t, func() bool {
		endpoint, err = rabbitMQC.PortEndpoint(ctx, "5672/tcp", "")
		return err == nil
	}, 5*time.Second, 25*time.Millisecond, "RabbitMQ port mapping did not become visible")

	return rabbitMQC, endpoint
}

func newRabbitWorker(t *testing.T, opts ...Option) *Worker {
	t.Helper()
	ctx := context.Background()
	container, endpoint := setupRabbitMQContainer(ctx, t)
	testcontainers.CleanupContainer(t, container)
	return NewWorker(append(
		[]Option{WithAddr(fmt.Sprintf("amqp://guest:guest@%s/", endpoint))},
		opts...,
	)...)
}

func TestShutdownWorkFlow(t *testing.T) {
	w := newRabbitWorker(t,
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

func TestRabbitMQDeadLettersExhaustedDeliveryAfterConfirmedPublishes(t *testing.T) {
	ctx := t.Context()
	rabbitMQC, endpoint := setupRabbitMQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, rabbitMQC)
	address := fmt.Sprintf("amqp://guest:guest@%s/", endpoint)
	worker, err := NewWorkerE(
		WithAddr(address), WithExchangeName("terminal-events"),
		WithQueue("terminal-jobs"), WithRoutingKey("terminal.run"),
		WithRequestTimeout(time.Second), WithDeadLetter(DeadLetterConfig{
			Exchange: "terminal-events-dead", Queue: "terminal-jobs-dead",
			RoutingKey: "terminal.dead", MaxDeliveryAttempts: 2,
		}),
		WithRunFunc(func(context.Context, core.TaskMessage) error {
			return errors.New("retryable")
		}),
	)
	require.NoError(t, err)
	q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(1))
	require.NoError(t, err)
	q.Start()
	defer q.Release()
	require.NoError(t, q.Queue(mockMessage{Message: "terminal-payload"}))

	connection, err := amqp.Dial(address)
	require.NoError(t, err)
	defer func() { require.NoError(t, connection.Close()) }()
	channel, err := connection.Channel()
	require.NoError(t, err)
	defer func() { require.NoError(t, channel.Close()) }()
	var dead amqp.Delivery
	require.Eventually(t, func() bool {
		message, ok, getErr := channel.Get("terminal-jobs-dead", true)
		if getErr != nil || !ok {
			return false
		}
		dead = message
		return true
	}, 10*time.Second, 10*time.Millisecond)
	decoded, err := job.DecodeE(dead.Body, job.DefaultMaxMessageBytes)
	require.NoError(t, err)
	assert.Equal(t, []byte("terminal-payload"), decoded.Payload())
	assert.Equal(t, int64(2), dead.Headers[deliveryAttemptHeader])
	assert.Equal(t, string(management.ClassificationRetryable), dead.Headers[classificationHeader])
	assert.Equal(t, "attempts_exhausted", dead.Headers[failureCodeHeader])
	assert.Equal(t, int64(management.CurrentEnvelopeVersion), dead.Headers[envelopeVersionHeader])
	source, err := channel.QueueInspect("terminal-jobs")
	require.NoError(t, err)
	assert.Zero(t, source.Messages)
}

func TestBrokerRestartRequiresReplacementWorker(t *testing.T) {
	ctx := context.Background()
	rabbitMQC, endpoint := setupRabbitMQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, rabbitMQC)
	address := fmt.Sprintf("amqp://guest:guest@%s/", endpoint)

	worker := NewWorker(WithAddr(address), WithQueue("restart"))
	q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(1))
	require.NoError(t, err)
	q.Start()
	require.NoError(t, q.Queue(mockMessage{Message: "before-restart"}))
	waitForCompleted(t, q, 1)

	exitCode, _, err := rabbitMQC.Exec(ctx, []string{"rabbitmqctl", "stop_app"})
	require.NoError(t, err)
	require.Zero(t, exitCode)
	assert.Error(t, q.Queue(mockMessage{Message: "old-worker-after-restart"}))
	q.Release()

	exitCode, _, err = rabbitMQC.Exec(ctx, []string{"rabbitmqctl", "start_app"})
	require.NoError(t, err)
	require.Zero(t, exitCode)

	var replacement *Worker
	require.Eventually(t, func() bool {
		candidate, connectErr := NewWorkerE(
			WithAddr(address), WithQueue("restart"),
			WithReconnectConfig(ReconnectConfig{MaxRetries: 1}),
		)
		if connectErr != nil {
			return false
		}
		replacement = candidate
		return true
	}, 30*time.Second, 200*time.Millisecond, "RabbitMQ did not become ready after restart")
	replacementQueue, err := queue.NewQueue(
		queue.WithWorker(replacement),
		queue.WithWorkerCount(1),
	)
	require.NoError(t, err)
	replacementQueue.Start()
	require.NoError(t, replacementQueue.Queue(mockMessage{Message: "replacement-worker"}))
	waitForCompleted(t, replacementQueue, 1)
	replacementQueue.Release()
}

func TestCustomFuncAndWait(t *testing.T) {
	m := &mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	w := newRabbitWorker(t,
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
	q.Release()
}

func TestEnqueueJobAfterShutdown(t *testing.T) {
	m := mockMessage{
		Message: "foo",
	}
	w := newRabbitWorker(t)
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
	m := mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 1)
	deadline := make(chan error, 2)
	w := newRabbitWorker(t,
		WithQueue("JobReachTimeout"),
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
	m := mockMessage{
		Message: "test",
	}
	started := make(chan struct{}, 1)
	canceled := make(chan error, 1)
	w := newRabbitWorker(t,
		WithQueue("CancelJob"),
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
	m := mockMessage{
		Message: "foo",
	}
	w := newRabbitWorker(t,
		WithQueue("GoroutineLeak"),
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
	m := mockMessage{
		Message: "foo",
	}
	panicked := make(chan struct{}, 2)
	w := newRabbitWorker(t,
		WithQueue("GoroutinePanic"),
		WithRoutingKey("GoroutinePanic"),
		WithExchangeName("GoroutinePanic"),
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
