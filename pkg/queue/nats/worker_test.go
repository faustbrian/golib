package nats

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	server "github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsConfigureNATS(t *testing.T) {
	logger := queue.NewEmptyLogger()
	runErr := errors.New("run")
	opts := newOptions(
		WithAddr("nats://one:4222", "nats://two:4222"),
		WithSubj("jobs"),
		WithQueue("workers"),
		WithRunFunc(func(context.Context, core.TaskMessage) error { return runErr }),
		WithLogger(logger),
		WithRequestTimeout(25*time.Millisecond),
		WithConnectTimeout(30*time.Millisecond),
	)

	assert.Equal(t, "nats://one:4222,nats://two:4222", opts.addr)
	assert.Equal(t, "jobs", opts.subj)
	assert.Equal(t, "workers", opts.queue)
	assert.Equal(t, logger, opts.logger)
	assert.Equal(t, 25*time.Millisecond, opts.requestTimeout)
	assert.Equal(t, 30*time.Millisecond, opts.connectTimeout)
	assert.ErrorIs(t, opts.runFunc(context.Background(), nil), runErr)
	assert.Equal(t, natsgo.DefaultURL, newOptions(WithAddr()).addr)
	assert.NoError(t, newOptions().runFunc(context.Background(), nil))
	worker := &Worker{opts: opts}
	assert.Equal(t, "nats", worker.BackendName())
	assert.Equal(t, "jobs", worker.QueueName())
}

func TestWorkerQueuesRequestsRunsAndShutsDown(t *testing.T) {
	url := runNATSServer(t)
	var handled []byte
	worker, err := NewWorkerE(
		WithAddr(url),
		WithSubj("jobs"),
		WithQueue("workers"),
		WithRequestTimeout(time.Second),
		WithRunFunc(func(_ context.Context, task core.TaskMessage) error {
			handled = append([]byte(nil), task.Payload()...)
			return nil
		}),
	)
	require.NoError(t, err)
	message := job.NewMessage(rawMessage("payload"))

	require.NoError(t, worker.Queue(&message))
	require.NoError(t, worker.client.Flush())
	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), received.Payload())
	require.NoError(t, worker.Run(context.Background(), received))
	assert.Equal(t, []byte("payload"), handled)
	require.NoError(t, worker.Shutdown())
	assert.ErrorIs(t, worker.Shutdown(), queue.ErrQueueShutdown)
	assert.ErrorIs(t, worker.Queue(&message), queue.ErrQueueShutdown)
}

func TestLegacyConstructorReturnsConnectedWorker(t *testing.T) {
	worker := NewWorker(WithAddr(runNATSServer(t)))
	require.NoError(t, worker.Shutdown())
}

func TestLegacyConstructorPanicsOnConnectionError(t *testing.T) {
	assert.Panics(t, func() {
		NewWorker(WithAddr("nats://127.0.0.1:1"), WithConnectTimeout(20*time.Millisecond))
	})
}

func TestWorkerReturnsSubscriptionError(t *testing.T) {
	worker, err := NewWorkerE(
		WithAddr(runNATSServer(t)),
		WithSubj("invalid..subject"),
	)

	assert.Nil(t, worker)
	assert.ErrorContains(t, err, "subscribe")
}

func TestRequestReturnsDecodeClosedAndTimeoutErrors(t *testing.T) {
	t.Run("decode", func(t *testing.T) {
		tasks := make(chan *natsgo.Msg, 1)
		tasks <- &natsgo.Msg{Data: []byte("not-json")}
		worker := &Worker{tasks: tasks, opts: newOptions()}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.Error(t, err)
	})

	t.Run("oversized", func(t *testing.T) {
		tasks := make(chan *natsgo.Msg, 1)
		tasks <- &natsgo.Msg{Data: bytes.Repeat([]byte("x"), job.DefaultMaxMessageBytes+1)}
		worker := &Worker{tasks: tasks, opts: newOptions()}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, job.ErrMessageTooLarge)
	})

	t.Run("closed", func(t *testing.T) {
		tasks := make(chan *natsgo.Msg)
		close(tasks)
		worker := &Worker{tasks: tasks, opts: newOptions()}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)
	})

	t.Run("timeout", func(t *testing.T) {
		worker := &Worker{
			tasks: make(chan *natsgo.Msg),
			opts:  newOptions(WithRequestTimeout(time.Millisecond)),
		}
		worker.startOnce.Do(func() {})

		started := time.Now()
		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, queue.ErrNoTaskInQueue)
		assert.Less(t, time.Since(started), 100*time.Millisecond)
	})
}

func TestQueueReturnsPublishError(t *testing.T) {
	worker, err := NewWorkerE(WithAddr(runNATSServer(t)))
	require.NoError(t, err)
	worker.client.Close()
	message := job.NewMessage(rawMessage("payload"))

	assert.Error(t, worker.Queue(&message))
	require.NoError(t, worker.Shutdown())
}

func TestShutdownWithoutSubscription(t *testing.T) {
	client, err := natsgo.Connect(runNATSServer(t))
	require.NoError(t, err)
	worker := &Worker{
		client: client,
		stop:   make(chan struct{}),
		exit:   make(chan struct{}),
		tasks:  make(chan *natsgo.Msg),
	}

	require.NoError(t, worker.Shutdown())
}

func TestShutdownClosesReconnectInProgress(t *testing.T) {
	instance, err := server.NewServer(&server.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	require.NoError(t, err)
	go instance.Start()
	require.True(t, instance.ReadyForConnections(5*time.Second))

	worker, err := NewWorkerE(WithAddr(instance.ClientURL()))
	require.NoError(t, err)
	instance.Shutdown()
	instance.WaitForShutdown()
	require.Eventually(t, func() bool {
		return worker.client.Status() == natsgo.RECONNECTING
	}, 5*time.Second, time.Millisecond)

	require.NoError(t, worker.Shutdown())
	assert.True(t, worker.client.IsClosed())
}

func TestHandleMessageRequeuesDuringShutdown(t *testing.T) {
	for _, test := range []struct {
		name        string
		message     *natsgo.Msg
		closeClient bool
	}{
		{name: "nil message"},
		{name: "publish succeeds", message: &natsgo.Msg{Data: []byte("payload")}},
		{name: "publish fails", message: &natsgo.Msg{Data: []byte("payload")}, closeClient: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := natsgo.Connect(runNATSServer(t))
			require.NoError(t, err)
			if test.closeClient {
				client.Close()
			}
			stop := make(chan struct{})
			close(stop)
			worker := &Worker{
				client: client,
				stop:   stop,
				exit:   make(chan struct{}),
				tasks:  make(chan *natsgo.Msg),
				opts: newOptions(
					WithSubj("jobs"),
					WithLogger(queue.NewEmptyLogger()),
				),
			}

			worker.handleMessage(test.message)
			select {
			case <-worker.exit:
			default:
				t.Fatal("handleMessage did not signal shutdown completion")
			}
			client.Close()
		})
	}
}

func TestConcurrentMessagesSignalShutdownOnce(t *testing.T) {
	client, err := natsgo.Connect(runNATSServer(t))
	require.NoError(t, err)
	stop := make(chan struct{})
	close(stop)
	worker := &Worker{
		client: client,
		stop:   stop,
		exit:   make(chan struct{}),
		tasks:  make(chan *natsgo.Msg),
		opts: newOptions(
			WithSubj("jobs"),
			WithLogger(queue.NewEmptyLogger()),
		),
	}
	var group sync.WaitGroup
	group.Add(10)
	for range 10 {
		go func() {
			defer group.Done()
			worker.handleMessage(&natsgo.Msg{Data: []byte("payload")})
		}()
	}

	assert.NotPanics(t, group.Wait)
	client.Close()
}

func runNATSServer(t *testing.T) string {
	t.Helper()
	instance, err := server.NewServer(&server.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	require.NoError(t, err)
	go instance.Start()
	require.True(t, instance.ReadyForConnections(5*time.Second))
	t.Cleanup(func() {
		instance.Shutdown()
		instance.WaitForShutdown()
	})
	return instance.ClientURL()
}

type rawMessage string

func (m rawMessage) Bytes() []byte { return []byte(m) }
