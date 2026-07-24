package rabbitmq

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsConfigureRabbitMQ(t *testing.T) {
	logger := queue.NewEmptyLogger()
	runErr := errors.New("run")
	reconnect := ReconnectConfig{MaxRetries: 3, InitialDelay: time.Millisecond, MaxDelay: time.Second}
	opts := newOptions(
		WithAddr("amqp://rabbit"),
		WithExchangeName("events"),
		WithExchangeType(ExchangeTopic),
		WithRoutingKey("jobs.created"),
		WithTag("worker-1"),
		WithAutoAck(true),
		WithQueue("jobs"),
		WithRunFunc(func(context.Context, core.TaskMessage) error { return runErr }),
		WithLogger(logger),
		WithReconnectConfig(reconnect),
		WithRequestTimeout(25*time.Millisecond),
		WithPublishTimeout(50*time.Millisecond),
		WithDeadLetter(DeadLetterConfig{
			Exchange: "events-dead", Queue: "jobs-dead",
			RoutingKey: "jobs.dead", MaxDeliveryAttempts: 7,
		}),
	)

	assert.Equal(t, "amqp://rabbit", opts.addr)
	assert.Equal(t, "events", opts.exchangeName)
	assert.Equal(t, ExchangeTopic, opts.exchangeType)
	assert.Equal(t, "jobs.created", opts.routingKey)
	assert.Equal(t, "worker-1", opts.tag)
	assert.True(t, opts.autoAck)
	assert.Equal(t, "jobs", opts.queue)
	assert.Equal(t, logger, opts.logger)
	assert.Equal(t, reconnect, opts.reconnect)
	assert.Equal(t, 25*time.Millisecond, opts.requestTimeout)
	assert.Equal(t, 50*time.Millisecond, opts.publishTimeout)
	assert.Equal(t, DeadLetterConfig{
		Exchange: "events-dead", Queue: "jobs-dead",
		RoutingKey: "jobs.dead", MaxDeliveryAttempts: 7,
	}, opts.deadLetter)
	assert.ErrorIs(t, opts.runFunc(context.Background(), nil), runErr)
	assert.NoError(t, newOptions().runFunc(context.Background(), nil))
	newOptions(WithLogger(logger), WithExchangeType("invalid"))

	for _, exchange := range []string{ExchangeDirect, ExchangeFanout, ExchangeTopic, ExchangeHeaders} {
		assert.True(t, isVaildExchange(exchange))
	}
	worker := &Worker{opts: opts}
	assert.Equal(t, "rabbitmq", worker.BackendName())
	assert.Equal(t, "jobs", worker.QueueName())
}

func TestDialWithRetryValidatesRetriesAndBacksOff(t *testing.T) {
	_, err := dialWithRetry("amqp://rabbit", ReconnectConfig{})
	assert.ErrorContains(t, err, "at least one")

	original := dialAMQP
	t.Cleanup(func() { dialAMQP = original })
	attempts := 0
	dialAMQP = func(string) (amqpConnection, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("unavailable")
		}
		return &fakeAMQPConnection{}, nil
	}
	connection, err := dialWithRetry("amqp://rabbit", ReconnectConfig{
		MaxRetries: 3, InitialDelay: time.Nanosecond, MaxDelay: time.Nanosecond,
	})
	require.NoError(t, err)
	assert.NotNil(t, connection)
	assert.Equal(t, 3, attempts)

	dialAMQP = func(string) (amqpConnection, error) {
		return nil, errors.New("unavailable")
	}
	connection, err = dialWithRetry("amqp://rabbit", ReconnectConfig{
		MaxRetries: 2, InitialDelay: 0, MaxDelay: 0,
	})
	assert.Nil(t, connection)
	assert.ErrorContains(t, err, "after retries")
}

func TestOpenRabbitMQReturnsDialAndChannelErrors(t *testing.T) {
	originalDial := dialAMQP
	t.Cleanup(func() {
		dialAMQP = originalDial
	})

	dialAMQP = func(string) (amqpConnection, error) { return nil, errors.New("dial") }
	connection, channel, err := openRabbitMQ("amqp://rabbit", ReconnectConfig{MaxRetries: 1})
	assert.Nil(t, connection)
	assert.Nil(t, channel)
	assert.Error(t, err)

	dialAMQP = func(string) (amqpConnection, error) {
		return &fakeAMQPConnection{openErr: errors.New("channel")}, nil
	}
	connection, channel, err = openRabbitMQ("amqp://rabbit", ReconnectConfig{MaxRetries: 1})
	assert.Nil(t, connection)
	assert.Nil(t, channel)
	assert.ErrorContains(t, err, "channel")

	expectedChannel := &amqp.Channel{}
	dialAMQP = func(string) (amqpConnection, error) {
		return &fakeAMQPConnection{rawChannel: expectedChannel}, nil
	}
	connection, channel, err = openRabbitMQ("amqp://rabbit", ReconnectConfig{MaxRetries: 1})
	require.NoError(t, err)
	assert.NotNil(t, connection)
	assert.Same(t, expectedChannel, channel)
}

func TestWorkerConstructors(t *testing.T) {
	t.Run("invalid dead letter", func(t *testing.T) {
		worker, err := NewWorkerE(WithDeadLetter(DeadLetterConfig{}))
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "dead-letter")
	})
	t.Run("invalid exchange", func(t *testing.T) {
		worker, err := NewWorkerE(
			WithLogger(queue.NewEmptyLogger()),
			WithExchangeType("invalid"),
		)
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "exchange type")
	})

	t.Run("invalid publish timeout", func(t *testing.T) {
		worker, err := NewWorkerE(WithPublishTimeout(0))
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "publish timeout")
	})

	t.Run("connection", func(t *testing.T) {
		withRabbitConnector(t, nil, nil, errors.New("connect"))
		worker, err := NewWorkerE()
		assert.Nil(t, worker)
		assert.ErrorIs(t, err, errConnect)
	})

	t.Run("exchange declaration", func(t *testing.T) {
		connection := &fakeAMQPConnection{}
		channel := &fakeAMQPChannel{exchangeErr: errors.New("exchange")}
		withRabbitConnector(t, connection, channel, nil)
		worker, err := NewWorkerE()
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "declare")
		assert.Equal(t, 1, connection.closes)
		assert.Equal(t, 1, channel.closes)
	})

	t.Run("publisher confirm setup", func(t *testing.T) {
		connection := &fakeAMQPConnection{}
		channel := &fakeAMQPChannel{confirmErr: errors.New("confirm")}
		withRabbitConnector(t, connection, channel, nil)
		worker, err := NewWorkerE()
		assert.Nil(t, worker)
		assert.ErrorContains(t, err, "publisher confirms")
		assert.Equal(t, 1, connection.closes)
		assert.Equal(t, 1, channel.closes)
	})

	t.Run("success", func(t *testing.T) {
		connection := &fakeAMQPConnection{}
		channel := &fakeAMQPChannel{}
		withRabbitConnector(t, connection, channel, nil)
		worker := NewWorker()
		require.NoError(t, worker.Shutdown())
	})

	t.Run("legacy panic", func(t *testing.T) {
		withRabbitConnector(t, nil, nil, errors.New("connect"))
		assert.Panics(t, func() { NewWorker() })
	})
}

var errConnect = errors.New("connect")

func TestStartConsumerCoversSetupStages(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*fakeAMQPChannel)
	}{
		{name: "success"},
		{name: "declare error", configure: func(c *fakeAMQPChannel) { c.queueErr = errors.New("declare") }},
		{name: "bind error", configure: func(c *fakeAMQPChannel) { c.bindErr = errors.New("bind") }},
		{name: "consume error", configure: func(c *fakeAMQPChannel) { c.consumeErr = errors.New("consume") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := &fakeAMQPChannel{deliveries: make(chan amqp.Delivery)}
			if test.configure != nil {
				test.configure(channel)
			}
			worker := &Worker{
				channel: channel,
				opts:    newOptions(WithLogger(queue.NewEmptyLogger())),
			}

			err := worker.startConsumer()
			if test.configure == nil {
				assert.NoError(t, err)
				assert.Equal(t, channel.deliveries, worker.tasks)
				assert.NoError(t, worker.startConsumer())
			} else {
				assert.Error(t, err)
			}
		})
	}
}

func TestStartConsumerRejectsShutdownWorker(t *testing.T) {
	worker := &Worker{stopFlag: 1}

	assert.ErrorIs(t, worker.startConsumer(), queue.ErrQueueShutdown)
}

func TestStartConsumerDeclaresDurableDeadLetterTopology(t *testing.T) {
	t.Parallel()

	deliveries := make(chan amqp.Delivery)
	channel := &fakeAMQPChannel{deliveries: deliveries}
	worker := &Worker{
		channel: channel,
		opts: newOptions(
			WithExchangeName("events"), WithQueue("jobs"), WithRoutingKey("jobs.run"),
			WithDeadLetter(DeadLetterConfig{
				Exchange: "events-dead", Queue: "jobs-dead",
				RoutingKey: "jobs.dead", MaxDeliveryAttempts: 5,
			}),
		),
	}
	require.NoError(t, worker.startConsumer())
	require.Len(t, channel.declaredExchanges, 1)
	assert.Equal(t, exchangeDeclaration{
		name: "events-dead", kind: ExchangeDirect, durable: true,
	}, channel.declaredExchanges[0])
	require.Len(t, channel.declaredQueues, 2)
	assert.Equal(t, "jobs-dead", channel.declaredQueues[0].name)
	assert.True(t, channel.declaredQueues[0].durable)
	assert.Equal(t, "jobs", channel.declaredQueues[1].name)
	assert.Equal(t, amqp.Table{
		"x-dead-letter-exchange":    "events-dead",
		"x-dead-letter-routing-key": "jobs.dead",
	}, channel.declaredQueues[1].arguments)
	assert.Contains(t, channel.bindings, queueBinding{
		queue: "jobs-dead", routingKey: "jobs.dead", exchange: "events-dead",
	})
}

func TestStartConsumerReportsEveryTopologyFailure(t *testing.T) {
	t.Parallel()

	for name, channel := range map[string]*fakeAMQPChannel{
		"dead-letter exchange": {exchangeErr: errors.New("exchange")},
		"source queue": {
			queueErrors: []error{nil, errors.New("source queue")},
		},
		"source binding": {
			bindErrors: []error{nil, errors.New("source binding")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			worker := &Worker{
				channel: channel, opts: newOptions(WithLogger(queue.NewEmptyLogger())),
			}
			assert.Error(t, worker.startConsumer())
		})
	}
}

func TestWorkerRunQueueRequestAndShutdown(t *testing.T) {
	expectedRun := errors.New("run")
	connection := &fakeAMQPConnection{}
	deliveries := make(chan amqp.Delivery, 1)
	confirmations := make(chan amqp.Confirmation, 1)
	channel := &fakeAMQPChannel{deliveries: deliveries}
	worker := &Worker{
		conn:          connection,
		channel:       channel,
		confirmations: confirmations,
		stop:          make(chan struct{}),
		opts: newOptions(
			WithLogger(queue.NewEmptyLogger()),
			WithAutoAck(true),
			WithRunFunc(func(context.Context, core.TaskMessage) error { return expectedRun }),
		),
		tasks: deliveries,
	}
	worker.startOnce.Do(func() {})
	confirmations <- amqp.Confirmation{DeliveryTag: 1, Ack: true}
	message := job.NewMessage(rawMessage("payload"))

	assert.ErrorIs(t, worker.Run(context.Background(), &message), expectedRun)
	require.NoError(t, worker.Queue(&message))
	assert.Equal(t, message.Bytes(), channel.published.Body)
	assert.Equal(t, amqp.Persistent, channel.published.DeliveryMode)
	deadline, ok := channel.publishContext.Deadline()
	assert.True(t, ok)
	assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, time.Second)

	deliveries <- amqp.Delivery{Body: message.Bytes()}
	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), received.Payload())
	assert.False(t, received.(*job.Message).AcknowledgementRequired())

	require.NoError(t, worker.Shutdown())
	assert.Equal(t, 1, channel.cancels)
	assert.Equal(t, 1, channel.closes)
	assert.Equal(t, 1, connection.closes)
	assert.ErrorIs(t, worker.Shutdown(), queue.ErrQueueShutdown)
	assert.ErrorIs(t, worker.Queue(&message), queue.ErrQueueShutdown)
}

func TestQueueReturnsPublishError(t *testing.T) {
	expected := errors.New("publish")
	worker := &Worker{
		channel: &fakeAMQPChannel{publishErr: expected},
		opts:    newOptions(),
	}
	message := job.NewMessage(rawMessage("payload"))

	assert.ErrorIs(t, worker.Queue(&message), expected)
}

func TestQueueReturnsConsumerSetupError(t *testing.T) {
	expected := errors.New("declare")
	worker := &Worker{
		channel: &fakeAMQPChannel{queueErr: expected},
		opts:    newOptions(WithLogger(queue.NewEmptyLogger())),
	}
	message := job.NewMessage(rawMessage("payload"))

	assert.ErrorIs(t, worker.Queue(&message), expected)
}

func TestQueueEstablishesBindingBeforePublishing(t *testing.T) {
	deliveries := make(chan amqp.Delivery)
	confirmations := make(chan amqp.Confirmation, 1)
	confirmations <- amqp.Confirmation{DeliveryTag: 1, Ack: true}
	channel := &fakeAMQPChannel{deliveries: deliveries}
	worker := &Worker{
		channel:       channel,
		confirmations: confirmations,
		opts:          newOptions(WithLogger(queue.NewEmptyLogger())),
	}
	message := job.NewMessage(rawMessage("payload"))

	require.NoError(t, worker.Queue(&message))
	assert.Equal(t, (<-chan amqp.Delivery)(deliveries), worker.tasks)
}

func TestQueueRequiresPositivePublisherConfirmation(t *testing.T) {
	message := job.NewMessage(rawMessage("payload"))

	for name, confirmation := range map[string]amqp.Confirmation{
		"negative acknowledgement": {DeliveryTag: 1, Ack: false},
		"positive acknowledgement": {DeliveryTag: 1, Ack: true},
	} {
		t.Run(name, func(t *testing.T) {
			confirmations := make(chan amqp.Confirmation, 1)
			confirmations <- confirmation
			worker := &Worker{
				channel:       &fakeAMQPChannel{},
				confirmations: confirmations,
				opts:          newOptions(),
			}

			err := worker.Queue(&message)
			if confirmation.Ack {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, "not acknowledged")
			}
		})
	}

	t.Run("timeout", func(t *testing.T) {
		worker := &Worker{
			channel:       &fakeAMQPChannel{},
			confirmations: make(chan amqp.Confirmation),
			opts:          newOptions(WithPublishTimeout(time.Millisecond)),
		}

		assert.ErrorIs(t, worker.Queue(&message), context.DeadlineExceeded)
	})

	t.Run("closed confirmation channel", func(t *testing.T) {
		confirmations := make(chan amqp.Confirmation)
		close(confirmations)
		worker := &Worker{
			channel:       &fakeAMQPChannel{},
			confirmations: confirmations,
			opts:          newOptions(),
		}

		assert.ErrorContains(t, worker.Queue(&message), "channel closed")
	})
}

func TestRequestReturnsSetupDecodeClosedAndTimeoutErrors(t *testing.T) {
	t.Run("setup", func(t *testing.T) {
		worker := &Worker{
			channel: &fakeAMQPChannel{queueErr: errors.New("declare")},
			opts:    newOptions(WithLogger(queue.NewEmptyLogger())),
		}
		message, err := worker.Request()
		assert.Nil(t, message)
		assert.Error(t, err)
	})

	t.Run("decode", func(t *testing.T) {
		acknowledger := &recordingAcknowledger{}
		deliveries := make(chan amqp.Delivery, 1)
		deliveries <- amqp.Delivery{
			Acknowledger: acknowledger,
			DeliveryTag:  1,
			Body:         []byte("not-json"),
		}
		worker := &Worker{tasks: deliveries, opts: newOptions()}
		worker.startOnce.Do(func() {})
		message, err := worker.Request()
		assert.Nil(t, message)
		assert.Error(t, err)
		assert.Equal(t, 1, acknowledger.nacks)
		assert.False(t, acknowledger.lastRequeue)
	})

	t.Run("decode settlement failure", func(t *testing.T) {
		nackErr := errors.New("nack")
		acknowledger := &recordingAcknowledger{nackErr: nackErr}
		deliveries := make(chan amqp.Delivery, 1)
		deliveries <- amqp.Delivery{
			Acknowledger: acknowledger,
			DeliveryTag:  1,
			Body:         []byte("not-json"),
		}
		worker := &Worker{tasks: deliveries, opts: newOptions()}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, nackErr)
		assert.Equal(t, 1, acknowledger.nacks)
	})

	t.Run("decode after automatic acknowledgement", func(t *testing.T) {
		acknowledger := &recordingAcknowledger{}
		deliveries := make(chan amqp.Delivery, 1)
		deliveries <- amqp.Delivery{
			Acknowledger: acknowledger,
			DeliveryTag:  1,
			Body:         []byte("not-json"),
		}
		worker := &Worker{tasks: deliveries, opts: newOptions(WithAutoAck(true))}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorContains(t, err, "decode RabbitMQ delivery")
		assert.Zero(t, acknowledger.nacks)
	})

	t.Run("oversized", func(t *testing.T) {
		acknowledger := &recordingAcknowledger{}
		deliveries := make(chan amqp.Delivery, 1)
		deliveries <- amqp.Delivery{
			Acknowledger: acknowledger,
			DeliveryTag:  1,
			Body:         bytes.Repeat([]byte("x"), job.DefaultMaxMessageBytes+1),
		}
		worker := &Worker{tasks: deliveries, opts: newOptions()}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, job.ErrMessageTooLarge)
		assert.Equal(t, 1, acknowledger.nacks)
		assert.False(t, acknowledger.lastRequeue)
	})

	t.Run("closed", func(t *testing.T) {
		deliveries := make(chan amqp.Delivery)
		close(deliveries)
		worker := &Worker{tasks: deliveries, opts: newOptions()}
		worker.startOnce.Do(func() {})
		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)
	})

	t.Run("timeout", func(t *testing.T) {
		worker := &Worker{
			tasks: make(chan amqp.Delivery),
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

func TestShutdownReturnsFirstResourceError(t *testing.T) {
	for _, test := range []struct {
		name       string
		cancelErr  error
		channelErr error
		connectErr error
		expected   error
	}{
		{name: "cancel", cancelErr: errors.New("cancel"), expected: errors.New("cancel")},
		{name: "channel", channelErr: errors.New("channel"), expected: errors.New("channel")},
		{name: "connection", connectErr: errors.New("connection"), expected: errors.New("connection")},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := &fakeAMQPConnection{closeErr: test.connectErr}
			channel := &fakeAMQPChannel{cancelErr: test.cancelErr, closeErr: test.channelErr}
			worker := &Worker{
				conn: connection, channel: channel, stop: make(chan struct{}),
				opts: newOptions(WithLogger(queue.NewEmptyLogger())),
			}

			assert.EqualError(t, worker.Shutdown(), test.expected.Error())
		})
	}
}

func TestShutdownWithoutResources(t *testing.T) {
	worker := &Worker{stop: make(chan struct{}), opts: newOptions()}
	require.NoError(t, worker.Shutdown())
}

func withRabbitConnector(
	t *testing.T,
	connection amqpConnection,
	channel amqpChannel,
	err error,
) {
	t.Helper()
	original := connectRabbitMQ
	connectRabbitMQ = func(string, ReconnectConfig) (amqpConnection, amqpChannel, error) {
		if err != nil {
			return nil, nil, errConnect
		}
		return connection, channel, nil
	}
	t.Cleanup(func() { connectRabbitMQ = original })
}

type fakeAMQPConnection struct {
	closes     int
	closeErr   error
	rawChannel *amqp.Channel
	openErr    error
}

func (c *fakeAMQPConnection) Close() error {
	c.closes++
	return c.closeErr
}

func (c *fakeAMQPConnection) Channel() (*amqp.Channel, error) {
	return c.rawChannel, c.openErr
}

type fakeAMQPChannel struct {
	exchangeErr       error
	queueErr          error
	queueErrors       []error
	queueCalls        int
	bindErr           error
	bindErrors        []error
	bindCalls         int
	consumeErr        error
	cancelErr         error
	closeErr          error
	publishErr        error
	confirmErr        error
	deliveries        <-chan amqp.Delivery
	published         amqp.Publishing
	publishContext    context.Context
	publishExchange   string
	publishRoutingKey string
	confirmations     chan amqp.Confirmation
	confirms          int
	cancels           int
	closes            int
	declaredExchanges []exchangeDeclaration
	declaredQueues    []queueDeclaration
	bindings          []queueBinding
}

type exchangeDeclaration struct {
	name    string
	kind    string
	durable bool
}

type queueDeclaration struct {
	name      string
	durable   bool
	arguments amqp.Table
}

type queueBinding struct {
	queue      string
	routingKey string
	exchange   string
}

func (c *fakeAMQPChannel) ExchangeDeclare(
	name, kind string, durable, _ bool, _ bool, _ bool, _ amqp.Table,
) error {
	c.declaredExchanges = append(c.declaredExchanges, exchangeDeclaration{
		name: name, kind: kind, durable: durable,
	})
	return c.exchangeErr
}

func (c *fakeAMQPChannel) QueueDeclare(
	name string, durable, _ bool, _ bool, _ bool, arguments amqp.Table,
) (amqp.Queue, error) {
	c.declaredQueues = append(c.declaredQueues, queueDeclaration{
		name: name, durable: durable, arguments: arguments,
	})
	if c.queueCalls < len(c.queueErrors) {
		err := c.queueErrors[c.queueCalls]
		c.queueCalls++
		return amqp.Queue{Name: name}, err
	}
	c.queueCalls++
	return amqp.Queue{Name: name}, c.queueErr
}

func (c *fakeAMQPChannel) QueueBind(
	queue, routingKey, exchange string, _ bool, _ amqp.Table,
) error {
	c.bindings = append(c.bindings, queueBinding{
		queue: queue, routingKey: routingKey, exchange: exchange,
	})
	if c.bindCalls < len(c.bindErrors) {
		err := c.bindErrors[c.bindCalls]
		c.bindCalls++
		return err
	}
	c.bindCalls++
	return c.bindErr
}

func (c *fakeAMQPChannel) Consume(string, string, bool, bool, bool, bool, amqp.Table) (<-chan amqp.Delivery, error) {
	return c.deliveries, c.consumeErr
}

func (c *fakeAMQPChannel) Cancel(string, bool) error {
	c.cancels++
	return c.cancelErr
}

func (c *fakeAMQPChannel) Close() error {
	c.closes++
	return c.closeErr
}

func (c *fakeAMQPChannel) Confirm(bool) error {
	c.confirms++
	return c.confirmErr
}

func (c *fakeAMQPChannel) NotifyPublish(receiver chan amqp.Confirmation) chan amqp.Confirmation {
	if c.confirmations != nil {
		return c.confirmations
	}
	return receiver
}

func (c *fakeAMQPChannel) PublishWithContext(
	ctx context.Context,
	exchange, routingKey string,
	_, _ bool,
	message amqp.Publishing,
) error {
	c.publishContext = ctx
	c.publishExchange = exchange
	c.publishRoutingKey = routingKey
	c.published = message
	return c.publishErr
}

type rawMessage string

func (m rawMessage) Bytes() []byte { return []byte(m) }
