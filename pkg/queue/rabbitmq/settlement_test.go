package rabbitmq

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingAcknowledger struct {
	acks        int
	nacks       int
	lastRequeue bool
	ackErr      error
	nackErr     error
}

func TestRequestRoutesClassifiedFailuresAfterDurablePublish(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		failure          error
		attempt          int64
		exchange         string
		routingKey       string
		publishedAttempt int64
		nacked           bool
	}{
		{
			name: "retryable", failure: errors.New("temporary"), attempt: 1,
			exchange: "events", routingKey: "jobs.run", publishedAttempt: 2,
		},
		{
			name: "exhausted", failure: errors.New("temporary"), attempt: 5,
			exchange: "events-dead", routingKey: "jobs.dead", publishedAttempt: 5,
		},
		{
			name: "permanent", failure: management.NewFailure(
				management.ClassificationPermanent, "invalid_order", errors.New("invalid"),
			), attempt: 1, exchange: "events-dead", routingKey: "jobs.dead",
			publishedAttempt: 1,
		},
		{
			name: "canceled", failure: context.Canceled, attempt: 1, nacked: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			acknowledger := &recordingAcknowledger{}
			message := job.NewTask(nil)
			deliveries := make(chan amqp.Delivery, 1)
			deliveries <- amqp.Delivery{
				Acknowledger: acknowledger, DeliveryTag: 1, Body: message.Bytes(),
				Headers: amqp.Table{deliveryAttemptHeader: test.attempt},
			}
			confirmations := make(chan amqp.Confirmation, 1)
			confirmations <- amqp.Confirmation{Ack: true}
			channel := &fakeAMQPChannel{confirmations: confirmations}
			worker := &Worker{
				channel: channel, confirmations: confirmations, tasks: deliveries,
				opts: newOptions(
					WithExchangeName("events"), WithRoutingKey("jobs.run"),
					WithPublishTimeout(time.Second), WithDeadLetter(DeadLetterConfig{
						Exchange: "events-dead", Queue: "jobs-dead",
						RoutingKey: "jobs.dead", MaxDeliveryAttempts: 5,
					}),
				),
			}
			worker.startOnce.Do(func() {})
			task, err := worker.Request()
			require.NoError(t, err)
			require.NoError(t, task.(*job.Message).NackFailure(test.failure))
			if test.nacked {
				assert.Equal(t, 1, acknowledger.nacks)
				assert.True(t, acknowledger.lastRequeue)
				assert.Empty(t, channel.publishExchange)
				return
			}
			assert.Equal(t, 1, acknowledger.acks)
			assert.Equal(t, test.exchange, channel.publishExchange)
			assert.Equal(t, test.routingKey, channel.publishRoutingKey)
			assert.Equal(t, test.publishedAttempt, channel.published.Headers[deliveryAttemptHeader])
		})
	}
}

func (a *recordingAcknowledger) Ack(uint64, bool) error {
	a.acks++
	return a.ackErr
}

func TestRabbitSettlementKeepsSourceRecoverableOnPublishFailure(t *testing.T) {
	t.Parallel()

	acknowledger := &recordingAcknowledger{}
	channel := &fakeAMQPChannel{publishErr: errors.New("destination unavailable")}
	worker := &Worker{
		channel: channel, confirmations: make(chan amqp.Confirmation),
		opts: newOptions(WithPublishTimeout(time.Second)),
	}
	task := amqp.Delivery{
		Acknowledger: acknowledger, DeliveryTag: 1, Body: []byte("payload"),
		Headers: amqp.Table{deliveryAttemptHeader: int64(1)},
	}
	err := worker.settleRabbitFailure(task, management.NewFailure(
		management.ClassificationPermanent, "invalid", errors.New("invalid"),
	))
	require.Error(t, err)
	resolution := management.ResolveFailure(err)
	assert.Equal(t, management.ClassificationInfrastructure, resolution.Classification)
	assert.Equal(t, management.FailureCodeDeadLetterDestinationUnavailable, resolution.Code)
	assert.Zero(t, acknowledger.acks)
	assert.Equal(t, 1, acknowledger.nacks)
	assert.True(t, acknowledger.lastRequeue)
}

func TestRabbitSettlementReportsAckFailureAfterDurablePublish(t *testing.T) {
	t.Parallel()

	acknowledger := &recordingAcknowledger{ackErr: errors.New("ack unknown")}
	confirmations := make(chan amqp.Confirmation, 1)
	confirmations <- amqp.Confirmation{Ack: true}
	worker := &Worker{
		channel:       &fakeAMQPChannel{confirmations: confirmations},
		confirmations: confirmations, opts: newOptions(WithPublishTimeout(time.Second)),
	}
	task := amqp.Delivery{
		Acknowledger: acknowledger, DeliveryTag: 1, Body: []byte("payload"),
		Headers: amqp.Table{deliveryAttemptHeader: int64(5)},
	}
	err := worker.settleRabbitFailure(task, errors.New("exhausted"))
	require.ErrorContains(t, err, "acknowledge")
	assert.Equal(t, 1, acknowledger.acks)
	assert.Zero(t, acknowledger.nacks)
}

func TestRabbitSettlementExhaustionOverridesGenericHandlerCode(t *testing.T) {
	t.Parallel()

	confirmations := make(chan amqp.Confirmation, 1)
	confirmations <- amqp.Confirmation{Ack: true}
	channel := &fakeAMQPChannel{confirmations: confirmations}
	worker := &Worker{
		channel: channel, confirmations: confirmations,
		opts: newOptions(WithPublishTimeout(time.Second)),
	}
	task := amqp.Delivery{
		Acknowledger: &recordingAcknowledger{}, DeliveryTag: 1, Body: []byte("payload"),
		Headers: amqp.Table{deliveryAttemptHeader: int64(5)},
	}
	err := worker.settleRabbitFailure(task, management.NewFailure(
		management.ClassificationRetryable, "handler_failed", errors.New("temporary"),
	))
	require.NoError(t, err)
	assert.Equal(t, "attempts_exhausted", channel.published.Headers[failureCodeHeader])
}

func TestRabbitSettlementFallbackAndMalformedAttempts(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		failure error
		headers amqp.Table
		requeue bool
	}{
		"nil failure": {headers: nil, requeue: true},
		"retry without publisher": {
			failure: errors.New("temporary"), headers: amqp.Table{}, requeue: true,
		},
		"terminal without publisher": {
			failure: management.NewFailure(
				management.ClassificationPermanent, "invalid", errors.New("invalid"),
			),
			headers: amqp.Table{deliveryAttemptHeader: int64(1)}, requeue: false,
		},
		"malformed attempt": {
			failure: errors.New("temporary"),
			headers: amqp.Table{deliveryAttemptHeader: "invalid"}, requeue: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			acknowledger := &recordingAcknowledger{}
			worker := &Worker{opts: newOptions()}
			err := worker.settleRabbitFailure(amqp.Delivery{
				Acknowledger: acknowledger, DeliveryTag: 1, Headers: test.headers,
			}, test.failure)
			require.NoError(t, err)
			assert.Equal(t, 1, acknowledger.nacks)
			assert.Equal(t, test.requeue, acknowledger.lastRequeue)
		})
	}
}

func TestRabbitPublishSettlementReportsConfirmationOutcomes(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		confirmations chan amqp.Confirmation
		timeout       time.Duration
	}{
		"closed": {
			confirmations: closedConfirmations(), timeout: time.Second,
		},
		"rejected": {
			confirmations: confirmationChannel(amqp.Confirmation{Ack: false}),
			timeout:       time.Second,
		},
		"timeout": {
			confirmations: make(chan amqp.Confirmation), timeout: time.Millisecond,
		},
	} {
		t.Run(name, func(t *testing.T) {
			worker := &Worker{
				channel: &fakeAMQPChannel{}, confirmations: test.confirmations,
				opts: newOptions(WithPublishTimeout(test.timeout)),
			}
			err := worker.publishRabbitSettlement(
				amqp.Delivery{}, "events", "jobs", 1,
				management.ClassificationRetryable, "handler_failed", false,
			)
			assert.Error(t, err)
		})
	}
}

func TestRabbitDeliveryAttemptAcceptsOnlyBoundedIntegers(t *testing.T) {
	t.Parallel()

	for _, headers := range []amqp.Table{
		nil,
		{},
		{deliveryAttemptHeader: int64(1)},
		{deliveryAttemptHeader: int32(2)},
		{deliveryAttemptHeader: int(3)},
	} {
		attempt, ok := rabbitDeliveryAttempt(headers)
		assert.True(t, ok)
		assert.GreaterOrEqual(t, attempt, int64(1))
	}
	for _, value := range []any{"1", int64(0), int64(102)} {
		_, ok := rabbitDeliveryAttempt(amqp.Table{deliveryAttemptHeader: value})
		assert.False(t, ok)
	}
}

func confirmationChannel(value amqp.Confirmation) chan amqp.Confirmation {
	confirmations := make(chan amqp.Confirmation, 1)
	confirmations <- value
	return confirmations
}

func closedConfirmations() chan amqp.Confirmation {
	confirmations := make(chan amqp.Confirmation)
	close(confirmations)
	return confirmations
}

func (a *recordingAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacks++
	a.lastRequeue = requeue
	return a.nackErr
}

func (a *recordingAcknowledger) Reject(uint64, bool) error { return nil }

func TestRequestDefersRabbitMQSettlement(t *testing.T) {
	acknowledger := &recordingAcknowledger{}
	message := job.NewTask(nil)
	deliveries := make(chan amqp.Delivery, 1)
	deliveries <- amqp.Delivery{
		Acknowledger: acknowledger,
		DeliveryTag:  1,
		Body:         message.Bytes(),
	}
	worker := &Worker{opts: newOptions(), tasks: deliveries}
	worker.startOnce.Do(func() {})

	task, err := worker.Request()
	require.NoError(t, err)
	require.Zero(t, acknowledger.acks)
	require.Zero(t, acknowledger.nacks)

	delivery := task.(*job.Message)
	require.NoError(t, delivery.Ack())
	require.NoError(t, delivery.Nack())
	require.Equal(t, 1, acknowledger.acks)
	require.Equal(t, 1, acknowledger.nacks)
	require.True(t, acknowledger.lastRequeue)
}
