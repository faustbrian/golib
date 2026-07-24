// Package gooutbox adapts webhook deliveries to outbox envelopes and relay
// publishers.
package gooutbox

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/outbox"
	webhook "github.com/faustbrian/golib/pkg/webhook"
)

var ErrInvalidConfig = errors.New("gooutbox: invalid configuration")

// Build encodes a bounded delivery request into a validated outbox envelope.
func Build(
	builder *outbox.EnvelopeBuilder,
	topic string,
	delivery webhook.DeliveryRequest,
	maxMessageBytes int,
) (outbox.Envelope, error) {
	if builder == nil || topic == "" || maxMessageBytes <= 0 {
		return outbox.Envelope{}, ErrInvalidConfig
	}
	payload, err := webhook.MarshalDeliveryRequest(delivery, maxMessageBytes)
	if err != nil {
		return outbox.Envelope{}, err
	}
	envelope, err := builder.Build(outbox.NewEnvelopeParams{
		Topic:          topic,
		Payload:        payload,
		PayloadVersion: 1,
		Metadata:       delivery.Metadata,
		OrderingKey:    delivery.EventID,
		IdempotencyKey: delivery.IdempotencyKey,
	})
	if err != nil {
		return outbox.Envelope{}, fmt.Errorf("gooutbox: build envelope: %w", err)
	}

	return envelope, nil
}

// Publisher implements relay.Publisher with exactly one HTTP attempt per
// claimed envelope. The relay remains the sole retry and dead-letter owner.
type Publisher struct {
	deliverer *webhook.Deliverer
	maxBytes  int
}

// NewPublisher validates the deliverer and payload bound.
func NewPublisher(deliverer *webhook.Deliverer, maxMessageBytes int) (*Publisher, error) {
	if deliverer == nil || maxMessageBytes <= 0 {
		return nil, ErrInvalidConfig
	}

	return &Publisher{deliverer: deliverer, maxBytes: maxMessageBytes}, nil
}

// Publish decodes one strict envelope payload and performs one HTTP attempt.
func (p *Publisher) Publish(ctx context.Context, envelope outbox.Envelope) error {
	delivery, err := webhook.UnmarshalDeliveryRequest(envelope.Payload, p.maxBytes)
	if err != nil {
		return err
	}
	if _, err := p.deliverer.DeliverOnce(ctx, delivery); err != nil {
		return fmt.Errorf("gooutbox: publish delivery: %w", err)
	}

	return nil
}
