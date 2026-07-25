// Package gokafka adapts transactional outbox envelopes to Kafka messages.
package gokafka

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
)

var (
	ErrClientRequired  = errors.New("outbox/gokafka: Kafka client is required")
	ErrInvalidEnvelope = errors.New("outbox/gokafka: envelope routing identity is invalid")
)

// Client is the narrow Kafka producer surface used by Publisher.
type Client interface {
	Publish(context.Context, kafka.Message) error
	Health(context.Context) error
}

// Publisher synchronously publishes canonical outbox payloads to Kafka.
type Publisher struct {
	client Client
}

// New creates a Kafka publisher adapter.
func New(client Client) (*Publisher, error) {
	if client == nil {
		return nil, ErrClientRequired
	}

	return &Publisher{client: client}, nil
}

// Publish sends an envelope payload with a stable partition key and schema
// identity headers. Kafka acknowledgement does not make delivery exactly once.
func (publisher *Publisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	if envelope.ID == "" || envelope.Topic == "" || envelope.PayloadVersion == 0 {
		return ErrInvalidEnvelope
	}

	headers := []kafka.Header{
		{Key: "content-type", Value: []byte("application/json")},
		{Key: "event-id", Value: []byte(envelope.ID)},
		{
			Key:   "schema-version",
			Value: []byte(strconv.FormatUint(uint64(envelope.PayloadVersion), 10)),
		},
	}
	if envelope.IdempotencyKey != "" {
		headers = append(headers, kafka.Header{
			Key:   "idempotency-key",
			Value: []byte(envelope.IdempotencyKey),
		})
	}

	if err := publisher.client.Publish(ctx, kafka.Message{
		Topic:   envelope.Topic,
		Key:     []byte(partitionKey(envelope)),
		Value:   envelope.Payload,
		Headers: headers,
	}); err != nil {
		return fmt.Errorf("outbox/gokafka: publish envelope %q: %w", envelope.ID, err)
	}

	return nil
}

// Health verifies Kafka broker connectivity through the producer.
func (publisher *Publisher) Health(ctx context.Context) error {
	return publisher.client.Health(ctx)
}

func partitionKey(envelope outbox.Envelope) string {
	if envelope.OrderingKey != "" {
		return envelope.OrderingKey
	}
	if envelope.IdempotencyKey != "" {
		return envelope.IdempotencyKey
	}

	return envelope.ID
}
