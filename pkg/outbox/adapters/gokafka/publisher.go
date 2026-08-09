// Package gokafka adapts transactional outbox envelopes to Kafka messages.
package gokafka

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
)

var (
	ErrClientRequired   = errors.New("outbox/gokafka: Kafka client is required")
	ErrInvalidEnvelope  = errors.New("outbox/gokafka: envelope routing identity is invalid")
	ErrInvalidConfig    = errors.New("outbox/gokafka: configuration is invalid")
	ErrPublishPanic     = errors.New("outbox/gokafka: Kafka client panicked during publish")
	ErrReservedMetadata = errors.New(
		"outbox/gokafka: metadata uses a reserved Kafka header",
	)
)

// Client is the narrow Kafka producer surface used by Publisher.
type Client interface {
	Publish(context.Context, kafka.Message) error
	Health(context.Context) error
}

// Publisher synchronously publishes canonical outbox payloads to Kafka.
type Publisher struct {
	client Client
	limits Limits
}

// Limits combines the persisted outbox bounds with the Kafka record bounds
// enforced before the producer is called.
type Limits struct {
	Envelope outbox.Limits
	Kafka    kafka.MessageLimits
}

// DefaultLimits returns the outbox defaults and the first-party producer's
// conservative Kafka record defaults.
func DefaultLimits() Limits {
	return Limits{
		Envelope: outbox.DefaultLimits(),
		Kafka:    kafka.DefaultMessageLimits(),
	}
}

// Option configures a Publisher during construction. Options are created by
// this package so construction does not execute caller callbacks.
type Option interface {
	apply(*Limits)
}

type limitsOption struct {
	limits Limits
}

func (option limitsOption) apply(target *Limits) {
	*target = option.limits
}

// WithLimits replaces the bounds enforced before publication.
func WithLimits(limits Limits) Option {
	return limitsOption{limits: limits}
}

// New creates a Kafka publisher adapter.
func New(client Client, options ...Option) (*Publisher, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	limits := DefaultLimits()
	for _, option := range options {
		if option == nil {
			return nil, ErrInvalidConfig
		}
		option.apply(&limits)
	}
	if err := limits.Envelope.Validate(); err != nil {
		return nil, errors.Join(ErrInvalidConfig, err)
	}
	if err := limits.Kafka.Validate(); err != nil {
		return nil, errors.Join(ErrInvalidConfig, err)
	}

	return &Publisher{client: client, limits: limits}, nil
}

// Publish sends an envelope payload with a stable partition key and schema
// identity headers. Kafka acknowledgement does not make delivery exactly once.
func (publisher *Publisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	if ctx == nil {
		return kafka.ErrContextRequired
	}
	if err := validateEnvelope(envelope, publisher.limits.Envelope); err != nil {
		return errors.Join(ErrInvalidEnvelope, err)
	}
	contentType := "application/json"
	if value := envelope.Metadata["es.content_type"]; value != "" {
		contentType = value
	}
	headers := []kafka.Header{
		{Key: "content-type", Value: []byte(contentType)},
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
	metadataKeys := make([]string, 0, len(envelope.Metadata))
	for key := range envelope.Metadata {
		metadataKeys = append(metadataKeys, key)
	}
	sort.Strings(metadataKeys)
	for _, key := range metadataKeys {
		if reservedHeader(key) {
			return errors.Join(ErrInvalidEnvelope, ErrReservedMetadata)
		}
		headers = append(headers, kafka.Header{
			Key:   strings.Clone(key),
			Value: []byte(envelope.Metadata[key]),
		})
	}
	message := kafka.Message{
		Topic:   strings.Clone(envelope.Topic),
		Key:     []byte(partitionKey(envelope)),
		Value:   bytes.Clone(envelope.Payload),
		Headers: headers,
	}
	if err := validateMessage(message, publisher.limits.Kafka); err != nil {
		return errors.Join(ErrInvalidEnvelope, err)
	}
	if err := publish(publisher.client, ctx, message); err != nil {
		return publishError{cause: err}
	}

	return nil
}

type publishError struct {
	cause error
}

func (err publishError) Error() string { return "outbox/gokafka: publish failed" }

func (err publishError) Unwrap() error { return err.cause }

type healthError struct {
	cause error
}

func (err healthError) Error() string { return "outbox/gokafka: health check failed" }

func (err healthError) Unwrap() error { return err.cause }

type ambiguousPublishError struct{}

func (ambiguousPublishError) Error() string { return ErrPublishPanic.Error() }

func (ambiguousPublishError) Unwrap() error { return ErrPublishPanic }

func (ambiguousPublishError) Category() kafka.ErrorCategory {
	return kafka.ErrorAmbiguous
}

func publish(client Client, ctx context.Context, message kafka.Message) (err error) {
	defer func() {
		if recover() != nil {
			err = ambiguousPublishError{}
		}
	}()

	return client.Publish(ctx, message)
}

func reservedHeader(key string) bool {
	switch key {
	case "content-type", "event-id", "schema-version", "idempotency-key":
		return true
	default:
		return false
	}
}

func validateEnvelope(envelope outbox.Envelope, limits outbox.Limits) error {
	switch {
	case envelope.ID == "":
		return outbox.ErrIDRequired
	case len(envelope.ID) > limits.MaxIDBytes:
		return outbox.ErrIDTooLarge
	case envelope.Topic == "":
		return outbox.ErrTopicRequired
	case len(envelope.Topic) > limits.MaxTopicBytes:
		return outbox.ErrTopicTooLarge
	case len(envelope.Payload) > limits.MaxPayloadBytes:
		return outbox.ErrPayloadTooLarge
	case len(envelope.Metadata) > limits.MaxMetadataEntries:
		return outbox.ErrMetadataEntriesTooLarge
	case metadataTooLarge(envelope.Metadata, limits.MaxMetadataBytes):
		return outbox.ErrMetadataTooLarge
	case len(envelope.OrderingKey) > limits.MaxOrderingKeyBytes:
		return outbox.ErrOrderingKeyTooLarge
	case len(envelope.IdempotencyKey) > limits.MaxIdempotencyKeyBytes:
		return outbox.ErrIdempotencyKeyTooLarge
	case envelope.PayloadVersion == 0:
		return outbox.ErrPayloadVersionRequired
	default:
		return nil
	}
}

func metadataTooLarge(metadata map[string]string, maximum int) bool {
	total := 0
	for key, value := range metadata {
		size := len(key) + len(value)
		if size > maximum-total {
			return true
		}
		total += size
	}

	return false
}

func validateMessage(message kafka.Message, limits kafka.MessageLimits) error {
	switch {
	case len(message.Topic) > limits.MaxTopicBytes:
		return kafka.ErrTopicTooLarge
	case len(message.Key) > limits.MaxKeyBytes:
		return kafka.ErrKeyTooLarge
	case len(message.Value) > limits.MaxValueBytes:
		return kafka.ErrValueTooLarge
	case len(message.Headers) > limits.MaxHeaders:
		return kafka.ErrTooManyHeaders
	}
	total := 0
	for _, header := range message.Headers {
		switch {
		case header.Key == "":
			return kafka.ErrHeaderKeyRequired
		case len(header.Key) > limits.MaxHeaderKeyBytes:
			return kafka.ErrHeaderKeyTooLarge
		case len(header.Value) > limits.MaxHeaderValueBytes:
			return kafka.ErrHeaderValueTooLarge
		case len(header.Key)+len(header.Value) > limits.MaxHeaderBytes-total:
			return kafka.ErrHeadersTooLarge
		}
		total += len(header.Key) + len(header.Value)
	}

	return nil
}

// Health verifies Kafka broker connectivity through the producer. Failures
// retain their cause through errors.Is and errors.As but render a fixed,
// credential-safe diagnostic.
func (publisher *Publisher) Health(ctx context.Context) error {
	if err := publisher.client.Health(ctx); err != nil {
		return healthError{cause: err}
	}

	return nil
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
