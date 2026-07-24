package nsq

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	nsqgo "github.com/nsqio/go-nsq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsConfigureNSQ(t *testing.T) {
	logger := queue.NewEmptyLogger()
	runErr := errors.New("run")
	opts := newOptions(
		WithAddr("nsq:4150"),
		WithTopic("jobs"),
		WithChannel("workers"),
		WithMaxInFlight(42),
		WithLogLevel(nsqgo.LogLevelWarning),
		WithRunFunc(func(context.Context, core.TaskMessage) error { return runErr }),
		WithLogger(logger),
		WithRequestTimeout(25*time.Millisecond),
		WithTouchInterval(30*time.Millisecond),
		WithConnectTimeout(35*time.Millisecond),
		WithDeadLetter("jobs-dead", 7),
	)

	assert.Equal(t, "nsq:4150", opts.addr)
	assert.Equal(t, "jobs", opts.topic)
	assert.Equal(t, "workers", opts.channel)
	assert.Equal(t, 42, opts.maxInFlight)
	assert.Equal(t, nsqgo.LogLevelWarning, opts.logLevel)
	assert.Equal(t, logger, opts.logger)
	assert.Equal(t, 25*time.Millisecond, opts.requestTimeout)
	assert.Equal(t, 30*time.Millisecond, opts.touchInterval)
	assert.Equal(t, 35*time.Millisecond, opts.connectTimeout)
	assert.Equal(t, "jobs-dead", opts.deadLetterTopic)
	assert.Equal(t, uint16(7), opts.maxDeliveryAttempts)
	assert.ErrorIs(t, opts.runFunc(context.Background(), nil), runErr)
	assert.NoError(t, newOptions().runFunc(context.Background(), nil))
	worker := &Worker{opts: opts}
	assert.Equal(t, "nsq", worker.BackendName())
	assert.Equal(t, "jobs", worker.QueueName())
}

func TestWorkerConstructorAndShutdown(t *testing.T) {
	worker := NewWorker(WithAddr("127.0.0.1:1"))
	require.NoError(t, worker.Shutdown())
	assert.ErrorIs(t, worker.Shutdown(), queue.ErrQueueShutdown)
}

func TestLegacyConstructorPanicsOnInvalidConfig(t *testing.T) {
	assert.Panics(t, func() {
		NewWorker(WithAddr("127.0.0.1:1"), WithMaxInFlight(-1))
	})
}

func TestNewWorkerEReturnsInvalidConfig(t *testing.T) {
	worker, err := NewWorkerE(WithAddr("127.0.0.1:1"), WithMaxInFlight(-1))
	assert.Nil(t, worker)
	assert.Error(t, err)
}

func TestNewWorkerERejectsUnsafeDeadLetterPolicy(t *testing.T) {
	t.Parallel()

	for name, option := range map[string]Option{
		"blank topic": WithDeadLetter(" ", 5),
		"same topic":  WithDeadLetter("gorush", 5),
		"attempts":    WithDeadLetter("gorush-dead", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			worker, err := NewWorkerE(WithAddr("127.0.0.1:1"), option)
			assert.Nil(t, worker)
			assert.ErrorIs(t, err, queue.ErrInvalidConfiguration)
		})
	}
}

func TestStartConsumerReturnsConfigurationAndConnectionErrors(t *testing.T) {
	t.Run("invalid topic", func(t *testing.T) {
		worker, err := NewWorkerE(WithAddr("127.0.0.1:1"), WithTopic("invalid topic"))
		require.NoError(t, err)
		assert.Error(t, worker.startConsumer())
		worker.p.Stop()
	})

	t.Run("connection", func(t *testing.T) {
		worker, err := NewWorkerE(
			WithAddr("127.0.0.1:1"),
			WithConnectTimeout(20*time.Millisecond),
		)
		require.NoError(t, err)
		assert.Error(t, worker.startConsumer())
		worker.q.Stop()
		<-worker.q.StopChan
		worker.p.Stop()
	})
}

func TestStartConsumerConfiguresConsumer(t *testing.T) {
	worker, err := NewWorkerE(WithAddr("127.0.0.1:1"))
	require.NoError(t, err)
	worker.connectConsumer = func(*nsqgo.Consumer, string) error { return nil }

	require.NoError(t, worker.startConsumer())
	assert.NotNil(t, worker.Stats())
	require.NoError(t, worker.Shutdown())
}

func TestStartConsumerRejectsShutdownWorker(t *testing.T) {
	worker, err := NewWorkerE(WithAddr("127.0.0.1:1"))
	require.NoError(t, err)
	worker.connectConsumer = func(*nsqgo.Consumer, string) error { return nil }

	require.NoError(t, worker.Shutdown())
	assert.ErrorIs(t, worker.startConsumer(), queue.ErrQueueShutdown)
	assert.Nil(t, worker.Stats())
}

func TestHandleMessageCoversDeliveryLifecycle(t *testing.T) {
	t.Run("empty body", func(t *testing.T) {
		worker := &Worker{opts: newOptions(), tasks: make(chan *nsqgo.Message, 1)}
		assert.NoError(t, worker.handleMessage(nsqgo.NewMessage(nsqgo.MessageID{}, nil)))
	})

	t.Run("delivered", func(t *testing.T) {
		message := nsqgo.NewMessage(nsqgo.MessageID{}, []byte("payload"))
		worker := &Worker{
			stop:  make(chan struct{}),
			tasks: make(chan *nsqgo.Message, 1),
			opts:  newOptions(),
		}

		require.NoError(t, worker.handleMessage(message))
		assert.Same(t, message, <-worker.tasks)
	})

	t.Run("requeued on shutdown", func(t *testing.T) {
		delegate := &messageDelegate{}
		message := nsqgo.NewMessage(nsqgo.MessageID{}, []byte("payload"))
		message.Delegate = delegate
		stop := make(chan struct{})
		close(stop)
		worker := &Worker{stop: stop, tasks: make(chan *nsqgo.Message), opts: newOptions()}

		require.NoError(t, worker.handleMessage(message))
		assert.Equal(t, 1, delegate.requeued)
	})

	t.Run("touches while waiting", func(t *testing.T) {
		touched := make(chan struct{}, 1)
		delegate := &touchDelegate{touched: touched}
		message := nsqgo.NewMessage(nsqgo.MessageID{}, []byte("payload"))
		message.Delegate = delegate
		worker := &Worker{
			stop:  make(chan struct{}),
			tasks: make(chan *nsqgo.Message),
			opts:  newOptions(WithTouchInterval(time.Millisecond)),
		}
		done := make(chan error, 1)
		go func() { done <- worker.handleMessage(message) }()

		select {
		case <-touched:
		case <-time.After(time.Second):
			t.Fatal("message was not touched")
		}
		assert.Same(t, message, <-worker.tasks)
		require.NoError(t, <-done)
	})
}

func TestQueueUsesProducerAndHonorsShutdown(t *testing.T) {
	message := job.NewMessage(rawMessage("payload"))
	worker := &Worker{
		opts: newOptions(WithTopic("jobs")),
		publish: func(topic string, body []byte) error {
			assert.Equal(t, "jobs", topic)
			assert.NotEmpty(t, body)
			return nil
		},
	}
	assert.NoError(t, worker.Queue(&message))

	expected := errors.New("publish")
	worker.publish = func(string, []byte) error { return expected }
	assert.ErrorIs(t, worker.Queue(&message), expected)
	atomicStoreStopped(worker)
	assert.ErrorIs(t, worker.Queue(&message), queue.ErrQueueShutdown)
}

func TestRunUsesConfiguredHandler(t *testing.T) {
	expected := errors.New("run")
	worker := &Worker{opts: newOptions(WithRunFunc(
		func(context.Context, core.TaskMessage) error { return expected },
	))}

	assert.ErrorIs(t, worker.Run(context.Background(), nil), expected)
}

func TestRequestReturnsConsumerDecodeClosedAndTimeoutErrors(t *testing.T) {
	t.Run("consumer", func(t *testing.T) {
		worker, err := NewWorkerE(
			WithAddr("127.0.0.1:1"),
			WithConnectTimeout(20*time.Millisecond),
		)
		require.NoError(t, err)

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.Error(t, err)
		worker.q.Stop()
		<-worker.q.StopChan
		worker.p.Stop()
	})

	t.Run("decode", func(t *testing.T) {
		delegate := &messageDelegate{}
		tasks := make(chan *nsqgo.Message, 1)
		nsqMessage := nsqgo.NewMessage(nsqgo.MessageID{}, []byte("not-json"))
		nsqMessage.Delegate = delegate
		tasks <- nsqMessage
		worker := &Worker{
			tasks: tasks, opts: newOptions(),
			publish: func(string, []byte) error { return nil },
		}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorContains(t, err, "decode NSQ message")
		assert.Equal(t, 1, delegate.finished)
	})

	t.Run("oversized", func(t *testing.T) {
		delegate := &messageDelegate{}
		tasks := make(chan *nsqgo.Message, 1)
		nsqMessage := nsqgo.NewMessage(
			nsqgo.MessageID{},
			bytes.Repeat([]byte("x"), job.DefaultMaxMessageBytes+1),
		)
		nsqMessage.Delegate = delegate
		tasks <- nsqMessage
		var terminal []byte
		worker := &Worker{
			tasks: tasks, opts: newOptions(),
			publish: func(_ string, body []byte) error {
				terminal = append([]byte(nil), body...)
				return nil
			},
		}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, job.ErrMessageTooLarge)
		assert.Equal(t, 1, delegate.finished)
		record, decodeErr := decodeNSQDeadLetter(terminal)
		require.NoError(t, decodeErr)
		assert.Empty(t, record.Payload)
		assert.Equal(t, int64(job.DefaultMaxMessageBytes+1), record.PayloadSize)
	})

	t.Run("closed", func(t *testing.T) {
		tasks := make(chan *nsqgo.Message)
		close(tasks)
		worker := &Worker{tasks: tasks, opts: newOptions()}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)
	})

	t.Run("timeout", func(t *testing.T) {
		worker := &Worker{
			tasks: make(chan *nsqgo.Message),
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

func TestStatsWithoutConsumer(t *testing.T) {
	assert.Nil(t, (&Worker{}).Stats())
}

func TestStatsWaitsForConsumerInitialization(t *testing.T) {
	worker, err := NewWorkerE(
		WithAddr("127.0.0.1:1"),
		WithRequestTimeout(time.Millisecond),
	)
	require.NoError(t, err)

	connectStarted := make(chan struct{})
	releaseConnect := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(releaseConnect)
		}
	}()
	worker.connectConsumer = func(*nsqgo.Consumer, string) error {
		close(connectStarted)
		<-releaseConnect
		return nil
	}

	requestDone := make(chan error, 1)
	go func() {
		_, requestErr := worker.Request()
		requestDone <- requestErr
	}()
	<-connectStarted

	statsStarted := make(chan struct{})
	statsDone := make(chan *nsqgo.ConsumerStats, 1)
	go func() {
		close(statsStarted)
		statsDone <- worker.Stats()
	}()
	<-statsStarted

	select {
	case <-statsDone:
		t.Fatal("Stats returned before consumer initialization completed")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseConnect)
	released = true
	assert.NotNil(t, <-statsDone)
	assert.ErrorIs(t, <-requestDone, queue.ErrNoTaskInQueue)
	require.NoError(t, worker.Shutdown())
}

type touchDelegate struct {
	touched chan struct{}
}

func (d *touchDelegate) OnFinish(*nsqgo.Message)                       {}
func (d *touchDelegate) OnRequeue(*nsqgo.Message, time.Duration, bool) {}
func (d *touchDelegate) OnTouch(*nsqgo.Message) {
	select {
	case d.touched <- struct{}{}:
	default:
	}
}

type rawMessage string

func (m rawMessage) Bytes() []byte { return []byte(m) }

func atomicStoreStopped(worker *Worker) {
	worker.stopFlag = 1
}
