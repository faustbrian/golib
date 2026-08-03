package rabbitmq

import (
	"context"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/stretchr/testify/assert"
)

func TestValidateDeadLetterRejectsEachInvalidBoundary(t *testing.T) {
	valid := newOptions(WithDeadLetter(DeadLetterConfig{
		Exchange: "events-dead", Queue: "jobs-dead",
		RoutingKey: "jobs.dead", MaxDeliveryAttempts: 5,
	}))
	assert.NoError(t, valid.validateDeadLetter())

	for name, mutate := range map[string]func(*options){
		"minimum attempts": func(candidate *options) {
			candidate.deadLetter.MaxDeliveryAttempts = 2
		},
		"maximum attempts": func(candidate *options) {
			candidate.deadLetter.MaxDeliveryAttempts = 101
		},
		"shared source exchange": func(candidate *options) {
			candidate.deadLetter.Exchange = candidate.exchangeName
		},
		"shared source routing key": func(candidate *options) {
			candidate.deadLetter.RoutingKey = candidate.routingKey
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)

			assert.NoError(t, candidate.validateDeadLetter())
		})
	}

	for _, test := range []struct {
		name   string
		mutate func(*options)
	}{
		{name: "blank exchange", mutate: func(opts *options) { opts.deadLetter.Exchange = " " }},
		{name: "blank queue", mutate: func(opts *options) { opts.deadLetter.Queue = " " }},
		{name: "blank routing key", mutate: func(opts *options) { opts.deadLetter.RoutingKey = " " }},
		{name: "control character", mutate: func(opts *options) { opts.deadLetter.Exchange = "events\n" }},
		{name: "source queue", mutate: func(opts *options) { opts.deadLetter.Queue = opts.queue }},
		{name: "source route", mutate: func(opts *options) {
			opts.deadLetter.Exchange = opts.exchangeName
			opts.deadLetter.RoutingKey = opts.routingKey
		}},
		{name: "one attempt", mutate: func(opts *options) { opts.deadLetter.MaxDeliveryAttempts = 1 }},
		{name: "too many attempts", mutate: func(opts *options) { opts.deadLetter.MaxDeliveryAttempts = 102 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)

			assert.ErrorIs(t, candidate.validateDeadLetter(), queue.ErrInvalidConfiguration)
		})
	}
}

func TestDefaultOptionsPreserveOperationalBounds(t *testing.T) {
	defaults := newOptions()

	assert.Equal(t, "amqp://guest:guest@localhost:5672/", defaults.addr)
	assert.Equal(t, "golang-queue", defaults.queue)
	assert.Equal(t, "golang-queue", defaults.tag)
	assert.Equal(t, "test-exchange", defaults.exchangeName)
	assert.Equal(t, ExchangeDirect, defaults.exchangeType)
	assert.Equal(t, "test-key", defaults.routingKey)
	assert.False(t, defaults.autoAck)
	assert.NotNil(t, defaults.logger)
	assert.NoError(t, defaults.runFunc(context.Background(), nil))
	assert.Equal(t, ReconnectConfig{
		MaxRetries: 5, InitialDelay: 500 * time.Millisecond, MaxDelay: 5 * time.Second,
	}, defaults.reconnect)
	assert.Equal(t, 6*time.Second, defaults.requestTimeout)
	assert.Equal(t, 5*time.Second, defaults.publishTimeout)
	assert.Equal(t, DeadLetterConfig{
		Exchange: "test-exchange-dead", Queue: "golang-queue-dead",
		RoutingKey: "test-key.dead", MaxDeliveryAttempts: 5,
	}, defaults.deadLetter)
}

func TestDialWithRetryMakesExactlyTheConfiguredAttempts(t *testing.T) {
	originalDial := dialAMQP
	t.Cleanup(func() {
		dialAMQP = originalDial
	})

	attempts := 0
	dialAMQP = func(string) (amqpConnection, error) {
		attempts++
		if attempts > 3 {
			return &fakeAMQPConnection{}, nil
		}
		return nil, errors.New("unavailable")
	}
	config := ReconnectConfig{
		MaxRetries: 3, InitialDelay: 2 * time.Millisecond, MaxDelay: 3 * time.Millisecond,
	}
	assert.Equal(t, 3*time.Millisecond, nextRabbitMQRetryDelay(config.InitialDelay, config.MaxDelay))
	assert.Equal(t, 3*time.Millisecond, nextRabbitMQRetryDelay(config.MaxDelay, config.MaxDelay))

	connection, err := dialWithRetry("amqp://rabbit", ReconnectConfig{MaxRetries: 3})
	assert.Nil(t, connection)
	assert.Error(t, err)
	assert.Equal(t, 3, attempts)

	attempts = 0
	connection, err = dialWithRetry("amqp://rabbit", ReconnectConfig{MaxRetries: 1})
	assert.Nil(t, connection)
	assert.Error(t, err)
	assert.Equal(t, 1, attempts)
}

func TestWorkerPublishTimeoutBoundaryPrecedesBrokerSetup(t *testing.T) {
	original := connectRabbitMQ
	t.Cleanup(func() { connectRabbitMQ = original })

	connections := 0
	connectRabbitMQ = func(string, ReconnectConfig) (amqpConnection, amqpChannel, error) {
		connections++
		return &fakeAMQPConnection{}, &fakeAMQPChannel{}, nil
	}

	worker, err := NewWorkerE(WithPublishTimeout(0))
	assert.Nil(t, worker)
	assert.ErrorContains(t, err, "publish timeout")
	assert.Zero(t, connections)

	worker, err = NewWorkerE(WithPublishTimeout(time.Nanosecond))
	assert.NoError(t, err)
	assert.NotNil(t, worker)
	assert.Equal(t, 1, connections)
	assert.NoError(t, worker.Shutdown())
}
