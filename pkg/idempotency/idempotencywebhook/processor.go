// Package idempotencywebhook provides provider-delivery deduplication for
// webhook messages with bounded payload fingerprints and durable ownership.
package idempotencywebhook

import (
	"context"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyqueue"
)

var (
	// ErrInProgress reports another active delivery owner.
	ErrInProgress = idempotencyqueue.ErrInProgress
	// ErrConflict reports reuse of a provider delivery ID for another payload.
	ErrConflict = idempotencyqueue.ErrConflict
	// ErrTerminalFailure reports a deliberately persisted permanent failure.
	ErrTerminalFailure = idempotencyqueue.ErrTerminalFailure
)

// Delivery is the structural payload contract expected from webhook messages.
type Delivery interface {
	Payload() []byte
}

// Handler processes one elected provider delivery.
type Handler func(context.Context, Delivery) error

// KeyFunc constructs a provider, endpoint, tenant, and delivery-scoped key.
type KeyFunc func(context.Context, Delivery) (idempotency.Key, error)

// FingerprintFunc computes canonical event identity.
type FingerprintFunc func(Delivery) (idempotency.Fingerprint, error)

// Options configures durable webhook delivery ownership.
type Options struct {
	Service           *idempotency.Service
	Lease             time.Duration
	TransitionTimeout time.Duration
	Key               KeyFunc
	Fingerprint       FingerprintFunc
}

// Processor deduplicates completed provider deliveries.
type Processor struct {
	middleware *idempotencyqueue.Middleware
}

// New validates options and constructs a webhook processor.
func New(options Options) (*Processor, error) {
	var key idempotencyqueue.KeyFunc
	if options.Key != nil {
		key = func(ctx context.Context, message idempotencyqueue.Message) (idempotency.Key, error) {
			return options.Key(ctx, message.(Delivery))
		}
	}
	var fingerprint idempotencyqueue.FingerprintFunc
	if options.Fingerprint != nil {
		fingerprint = func(message idempotencyqueue.Message) (idempotency.Fingerprint, error) {
			return options.Fingerprint(message.(Delivery))
		}
	}
	middleware, err := idempotencyqueue.New(idempotencyqueue.Options{
		Service: options.Service, Lease: options.Lease,
		TransitionTimeout: options.TransitionTimeout,
		Key:               key, Fingerprint: fingerprint,
	})
	if err != nil {
		return nil, err
	}
	return &Processor{middleware: middleware}, nil
}

// Handle executes handler once and deduplicates completed redelivery.
func (p *Processor) Handle(ctx context.Context, delivery Delivery, handler Handler) error {
	if handler == nil {
		return configurationError("handler")
	}
	return p.middleware.Handle(ctx, delivery, func(ctx context.Context, _ idempotencyqueue.Message) error {
		return handler(ctx, delivery)
	})
}

// Wrap preserves the provider-specific delivery type used by a webhook router.
func Wrap[D Delivery](
	processor *Processor,
	next func(context.Context, D) error,
) func(context.Context, D) error {
	if next == nil {
		return func(context.Context, D) error { return configurationError("handler") }
	}
	return func(ctx context.Context, delivery D) error {
		return processor.Handle(ctx, delivery, func(ctx context.Context, _ Delivery) error {
			return next(ctx, delivery)
		})
	}
}

func configurationError(field string) error {
	return &idempotency.Error{Reason: idempotency.ReasonInvalidConfiguration, Field: field}
}
