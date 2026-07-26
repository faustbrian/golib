// Package goqueue adapts bounded delivery requests to queue messages.
package goqueue

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	webhook "github.com/faustbrian/golib/pkg/webhook"
)

var ErrInvalidConfig = errors.New("goqueue: invalid configuration")

// Queue is the narrow synchronous producer surface implemented by queue.
type Queue interface {
	Queue(message core.QueuedMessage, options ...job.AllowOption) error
}

// Config bounds messages and optionally assigns queue-level retry policy.
// Delivery handling always performs one HTTP attempt, preventing nested retry
// multiplication.
type Config struct {
	Queue           Queue
	MaxMessageBytes int
	JobOptions      []job.AllowOption
}

// Adapter enqueues versioned delivery requests.
type Adapter struct {
	queue      Queue
	maxBytes   int
	jobOptions []job.AllowOption
}

// New validates the queue and message bound.
func New(config Config) (*Adapter, error) {
	if config.Queue == nil || config.MaxMessageBytes <= 0 {
		return nil, ErrInvalidConfig
	}
	options := append([]job.AllowOption(nil), config.JobOptions...)
	if len(options) == 0 {
		options = []job.AllowOption{{RetryCount: job.Int64(0)}}
	}

	return &Adapter{queue: config.Queue, maxBytes: config.MaxMessageBytes, jobOptions: options}, nil
}

// Enqueue checks cancellation, encodes a bounded request, and synchronously
// hands it to queue. Acceptance is not an exactly-once guarantee.
func (a *Adapter) Enqueue(ctx context.Context, delivery webhook.DeliveryRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := webhook.MarshalDeliveryRequest(delivery, a.maxBytes)
	if err != nil {
		return err
	}
	if err := a.queue.Queue(message(encoded), a.jobOptions...); err != nil {
		return fmt.Errorf("goqueue: enqueue delivery: %w", err)
	}

	return nil
}

// Handle strictly decodes one message and performs one HTTP attempt. The
// surrounding queue owns any retry schedule and settlement behavior.
func Handle(
	ctx context.Context,
	deliverer *webhook.Deliverer,
	encoded []byte,
	maxMessageBytes int,
) (webhook.DeliveryResult, error) {
	if deliverer == nil || maxMessageBytes <= 0 {
		return webhook.DeliveryResult{}, ErrInvalidConfig
	}
	delivery, err := webhook.UnmarshalDeliveryRequest(encoded, maxMessageBytes)
	if err != nil {
		return webhook.DeliveryResult{}, err
	}

	return deliverer.DeliverOnce(ctx, delivery)
}

type message []byte

func (m message) Bytes() []byte {
	return append([]byte(nil), m...)
}
