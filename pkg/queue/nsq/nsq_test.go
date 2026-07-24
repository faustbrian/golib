//go:build integration

package nsq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	nsqgo "github.com/nsqio/go-nsq"
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

func setupNSQContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image: "nsqio/nsq:v1.3.0@sha256:1a369c146af71bc95c25d54b375a2b98452478c1eaf4e85f8fcb01da20f2c78a",
		ExposedPorts: []string{
			"4150/tcp", // nsqd port
			"4151/tcp", // http port
		},
		WaitingFor: wait.ForLog("TCP: listening on"),
		Cmd: []string{
			"sh", "-c",
			"nsqd & echo $! > /tmp/nsqd.pid; while :; do sleep 3600; done",
		},
	}
	nsqC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	endpoint, err := nsqC.PortEndpoint(ctx, "4150/tcp", "")
	require.NoError(t, err)

	return nsqC, endpoint
}

func stopNSQServer(ctx context.Context, t *testing.T, nsqC testcontainers.Container) {
	t.Helper()

	exitCode, _, err := nsqC.Exec(ctx, []string{
		"sh", "-c", "kill -TERM \"$(cat /tmp/nsqd.pid)\"",
	})
	require.NoError(t, err)
	require.Zero(t, exitCode)
}

func startNSQServer(ctx context.Context, t *testing.T, nsqC testcontainers.Container) {
	t.Helper()

	exitCode, _, err := nsqC.Exec(ctx, []string{
		"sh", "-c",
		"nsqd > /tmp/nsqd-restart.log 2>&1 & echo $! > /tmp/nsqd.pid",
	})
	require.NoError(t, err)
	require.Zero(t, exitCode)
}

func TestNSQDefaultFlow(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := &mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("test1"),
		WithChannel("test1"),
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

func TestNSQDeadLettersExhaustedDeliveryBeforeFinishingSource(t *testing.T) {
	ctx := t.Context()
	nsqC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, nsqC)

	terminal := make(chan []byte, 1)
	consumer, err := nsqgo.NewConsumer("terminal-jobs-dead", "inspect", nsqgo.NewConfig())
	require.NoError(t, err)
	consumer.SetLoggerLevel(nsqgo.LogLevelError)
	consumer.AddHandler(nsqgo.HandlerFunc(func(message *nsqgo.Message) error {
		terminal <- append([]byte(nil), message.Body...)
		return nil
	}))
	require.NoError(t, consumer.ConnectToNSQD(endpoint))
	defer func() {
		consumer.Stop()
		<-consumer.StopChan
	}()

	worker, err := NewWorkerE(
		WithAddr(endpoint), WithTopic("terminal-jobs"), WithChannel("workers"),
		WithDeadLetter("terminal-jobs-dead", 2),
		WithRunFunc(func(context.Context, core.TaskMessage) error {
			return errors.New("retryable")
		}),
	)
	require.NoError(t, err)
	worker.cfg.DefaultRequeueDelay = 10 * time.Millisecond
	worker.cfg.MaxRequeueDelay = 20 * time.Millisecond
	q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(1))
	require.NoError(t, err)
	q.Start()
	defer q.Release()
	require.NoError(t, q.Queue(mockMessage{Message: "terminal-payload"}))

	select {
	case encoded := <-terminal:
		record, decodeErr := decodeNSQDeadLetter(encoded)
		require.NoError(t, decodeErr)
		assert.Equal(t, uint16(2), record.Attempts)
		assert.Equal(t, management.ClassificationRetryable, record.Classification)
		assert.Equal(t, "attempts_exhausted", record.FailureCode)
		decoded, decodeErr := job.DecodeE(record.Payload, job.DefaultMaxMessageBytes)
		require.NoError(t, decodeErr)
		assert.Equal(t, []byte("terminal-payload"), decoded.Payload())
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for NSQ dead letter")
	}
}

func TestNSQReconnectsAfterBrokerRestart(t *testing.T) {
	ctx := context.Background()
	nsqC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, nsqC)

	worker := NewWorker(WithAddr(endpoint), WithTopic("restart"), WithChannel("restart"))
	worker.cfg.LookupdPollInterval = 100 * time.Millisecond
	q, err := queue.NewQueue(queue.WithWorker(worker), queue.WithWorkerCount(1))
	require.NoError(t, err)
	q.Start()
	require.NoError(t, q.Queue(mockMessage{Message: "before-restart"}))
	waitForCompleted(t, q, 1)

	stopNSQServer(ctx, t, nsqC)
	require.Eventually(t, func() bool {
		stats := worker.Stats()
		return stats != nil && stats.Connections == 0
	}, 5*time.Second, time.Millisecond)
	startNSQServer(ctx, t, nsqC)
	require.Eventually(t, func() bool {
		stats := worker.Stats()
		return stats != nil && stats.Connections == 1
	}, 10*time.Second, 10*time.Millisecond)

	require.NoError(t, q.Queue(mockMessage{Message: "after-restart"}))
	waitForCompleted(t, q, 2)
	q.Release()
}

func TestNSQShutdown(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("test2"),
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

func TestNSQCustomFuncAndWait(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := &mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("test3"),
		WithMaxInFlight(10),
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
		queue.WithWorkerCount(10),
	)
	assert.NoError(t, err)
	q.Start()
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
	natsC, endpoint := setupNSQContainer(ctx, t)
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
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	started := make(chan struct{}, 1)
	deadline := make(chan error, 1)
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("timeout"),
		WithMaxInFlight(2),
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
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "test",
	}
	started := make(chan struct{}, 1)
	canceled := make(chan error, 1)
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("cancel"),
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
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("GoroutineLeak"),
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
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("GoroutinePanic"),
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

func TestNSQStatsinQueue(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("nsq_stats"),
		WithRunFunc(func(ctx context.Context, m core.TaskMessage) error {
			return nil
		}),
	)
	q, err := queue.NewQueue(
		queue.WithWorker(w),
		queue.WithWorkerCount(1),
	)
	assert.NoError(t, err)
	assert.NoError(t, q.Queue(m))
	assert.NoError(t, q.Queue(m))
	q.Start()
	require.Eventually(t, func() bool {
		stats := w.Stats()
		return stats != nil && stats.Connections == 1 &&
			stats.MessagesReceived == 2 && stats.MessagesFinished == 2
	}, 5*time.Second, time.Millisecond)
	q.Release()
	assert.Equal(t, int(0), w.Stats().Connections)
}

func TestNSQStatsInWorker(t *testing.T) {
	ctx := context.Background()
	natsC, endpoint := setupNSQContainer(ctx, t)
	defer testcontainers.CleanupContainer(t, natsC)
	m := mockMessage{
		Message: "foo",
	}
	w := NewWorker(
		WithAddr(endpoint),
		WithTopic("nsq_stats_queue"),
	)
	message := job.NewMessage(m)

	assert.Equal(t, int(0), len(w.tasks))
	assert.NoError(t, w.Queue(&message))
	assert.NoError(t, w.Queue(&message))
	assert.NoError(t, w.Queue(&message))
	assert.Nil(t, w.Stats())

	task, err := w.Request()
	assert.Equal(t, int(1), w.Stats().Connections)
	assert.NotNil(t, task)
	assert.NoError(t, err)

	assert.Equal(t, uint64(1), w.Stats().MessagesReceived)
	assert.Equal(t, uint64(0), w.Stats().MessagesFinished)
	assert.Equal(t, uint64(0), w.Stats().MessagesRequeued)
	assert.NoError(t, task.(*job.Message).Ack())
	assert.Eventually(t, func() bool {
		stats := w.Stats()
		return stats.MessagesReceived == 2 && stats.MessagesFinished == 1
	}, time.Second, time.Millisecond)
	_ = w.Shutdown()
	assert.Eventually(t, func() bool {
		return w.Stats().MessagesRequeued == 1
	}, time.Second, time.Millisecond)
}
