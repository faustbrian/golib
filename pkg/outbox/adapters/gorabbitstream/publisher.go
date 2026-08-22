// Package gorabbitstream adapts transactional outbox envelopes to confirmed
// RabbitMQ Streams publications.
package gorabbitstream

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/faustbrian/golib/pkg/rabbitstream"
)

var (
	// ErrClientRequired reports construction without a publishing client.
	ErrClientRequired = errors.New("outbox/gorabbitstream: publishing client is required")
	// ErrInvalidConfig reports a target or message-bound policy that cannot be enforced.
	ErrInvalidConfig = errors.New("outbox/gorabbitstream: configuration is invalid")
	// ErrInvalidEnvelope reports an envelope that cannot be represented by the configured stream contract.
	ErrInvalidEnvelope = errors.New("outbox/gorabbitstream: envelope is invalid")
	// ErrReservedMetadata reports application metadata that would forge an adapter-owned wire field.
	ErrReservedMetadata = errors.New("outbox/gorabbitstream: metadata uses a reserved field")
	// ErrUnconfirmed reports a client return that did not prove broker confirmation.
	ErrUnconfirmed = errors.New("outbox/gorabbitstream: publication was not confirmed")
	// ErrContextRequired reports a nil operation context.
	ErrContextRequired = errors.New("outbox/gorabbitstream: context is required")
	// ErrClientPanic reports a contained client panic without disclosing its value.
	ErrClientPanic = errors.New("outbox/gorabbitstream: publishing client panicked")
)

const defaultContentType = "application/json"

// Client is the narrow confirmed-publisher surface implemented by
// rabbitstream.Producer.
type Client interface {
	// Publish must return a definitive broker confirmation for relay success.
	Publish(context.Context, rabbitstream.Message) (rabbitstream.DeliveryResult, error)
}

// Config binds one publisher to one ordinary Stream or Super Stream and to a
// complete bounded message policy.
type Config struct {
	// Stream names the one ordinary stream accepted from Envelope.Topic.
	Stream string
	// SuperStream names the one Super Stream accepted from Envelope.Topic.
	SuperStream string
	// Limits bounds every retained message field before client admission.
	Limits rabbitstream.Limits
}

// Publisher maps one persisted envelope to one confirmed stream message. It
// owns no producer, retry loop, outbox transition, or topology lifecycle.
type Publisher struct {
	client Client
	config Config
}

// New constructs a bounded outbox publisher without opening a broker connection.
func New(client Client, config Config) (*Publisher, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	if config.Limits == (rabbitstream.Limits{}) {
		config.Limits = rabbitstream.DefaultLimits()
	}
	probe := rabbitstream.Message{Stream: config.Stream, SuperStream: config.SuperStream}
	if err := probe.Validate(config.Limits); err != nil {
		return nil, errors.Join(ErrInvalidConfig, err)
	}

	return &Publisher{client: client, config: config}, nil
}

// Publish maps, validates, owns, and synchronously publishes one envelope.
// Success means rabbitstream observed a broker confirmation; the caller still
// owns the separate durable outbox transition and its duplicate window.
func (publisher *Publisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	if ctx == nil {
		return ErrContextRequired
	}
	message, err := publisher.message(envelope)
	if err != nil {
		return err
	}
	result, err := publish(publisher.client, ctx, message)
	if err != nil {
		return publishError{cause: err}
	}
	switch result.State {
	case rabbitstream.DeliveryConfirmed:
		return nil
	case rabbitstream.DeliveryRejected:
		return publishError{cause: rabbitstream.ErrBrokerRejected}
	case rabbitstream.DeliveryAmbiguous:
		return publishError{cause: rabbitstream.ErrPublishAmbiguous}
	default:
		return publishError{cause: ErrUnconfirmed}
	}
}

func publish(
	client Client,
	ctx context.Context,
	message rabbitstream.Message,
) (result rabbitstream.DeliveryResult, err error) {
	defer func() {
		if recover() != nil {
			result = rabbitstream.DeliveryResult{State: rabbitstream.DeliveryAmbiguous}
			err = &rabbitstream.OperationError{
				Operation: rabbitstream.OperationPublish,
				Category:  rabbitstream.CategoryPublishAmbiguous,
				Cause:     ErrClientPanic,
			}
		}
	}()

	return client.Publish(ctx, message)
}

func (publisher *Publisher) message(envelope outbox.Envelope) (rabbitstream.Message, error) {
	if envelope.ID == "" || envelope.PayloadVersion == 0 ||
		envelope.Topic == "" || envelope.Topic != publisher.target() {
		return rabbitstream.Message{}, ErrInvalidEnvelope
	}
	for key := range envelope.Metadata {
		if reservedMetadata(key) {
			return rabbitstream.Message{}, errors.Join(ErrInvalidEnvelope, ErrReservedMetadata)
		}
	}

	contentType := defaultContentType
	if value := envelope.Metadata["es.content_type"]; value != "" {
		contentType = value
	}
	message := rabbitstream.Message{
		Stream: publisher.config.Stream, SuperStream: publisher.config.SuperStream,
		RoutingKey: routingKey(envelope), Timestamp: envelope.CreatedAt,
		ContentType: contentType, MessageID: strings.Clone(envelope.ID),
		CorrelationID: strings.Clone(envelope.Metadata["correlation-id"]),
		Payload:       bytes.Clone(envelope.Payload),
		Properties: []rabbitstream.MetadataEntry{{
			Key: "schema-version", Value: []byte(strconv.FormatUint(uint64(envelope.PayloadVersion), 10)),
		}},
	}
	if envelope.IdempotencyKey != "" {
		message.Properties = append(message.Properties, rabbitstream.MetadataEntry{
			Key: "idempotency-key", Value: []byte(envelope.IdempotencyKey),
		})
	}
	for _, key := range []string{"traceparent", "tracestate"} {
		if value, ok := envelope.Metadata[key]; ok {
			message.Headers = append(message.Headers, rabbitstream.MetadataEntry{
				Key: key, Value: []byte(value),
			})
		}
	}
	keys := make([]string, 0, len(envelope.Metadata))
	for key := range envelope.Metadata {
		if key == "correlation-id" || key == "traceparent" || key == "tracestate" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		message.Properties = append(message.Properties, rabbitstream.MetadataEntry{
			Key: strings.Clone(key), Value: []byte(envelope.Metadata[key]),
		})
	}
	if err := message.Validate(publisher.config.Limits); err != nil {
		return rabbitstream.Message{}, errors.Join(ErrInvalidEnvelope, err)
	}

	return message, nil
}

func (publisher *Publisher) target() string {
	if publisher.config.Stream != "" {
		return publisher.config.Stream
	}

	return publisher.config.SuperStream
}

func routingKey(envelope outbox.Envelope) string {
	if envelope.OrderingKey != "" {
		return envelope.OrderingKey
	}
	if envelope.IdempotencyKey != "" {
		return envelope.IdempotencyKey
	}

	return envelope.ID
}

func reservedMetadata(key string) bool {
	switch key {
	case "schema-version", "idempotency-key", "content-type":
		return true
	default:
		return false
	}
}

type publishError struct{ cause error }

// Error returns a stable message without broker or envelope contents.
func (publishError) Error() string { return "outbox/gorabbitstream: publish failed" }

// Unwrap preserves the programmatic client failure for classification.
func (err publishError) Unwrap() error { return err.cause }

// ClassifyError maps definite local or broker input rejection to permanent
// relay failure. Every ambiguous or recoverable transport outcome remains
// transient because retry may duplicate an already accepted event.
func ClassifyError(err error) relay.ErrorClass {
	if errors.Is(err, ErrInvalidEnvelope) || errors.Is(err, rabbitstream.ErrInvalidConfiguration) ||
		errors.Is(err, rabbitstream.ErrValidation) || errors.Is(err, rabbitstream.ErrMessageTooLarge) ||
		errors.Is(err, rabbitstream.ErrBrokerRejected) {
		return relay.ErrorPermanent
	}

	return relay.ErrorTransient
}
