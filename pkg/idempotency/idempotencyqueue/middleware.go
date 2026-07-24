// Package idempotencyqueue provides durable consumer ownership and
// redelivery deduplication for messages exposing a Payload method.
package idempotencyqueue

import (
	"context"
	"errors"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

var (
	// ErrInProgress tells a broker to retry after another owner's lease.
	ErrInProgress = errors.New("idempotencyqueue: delivery in progress")
	// ErrConflict identifies reuse of a delivery key for a different payload.
	ErrConflict = errors.New("idempotencyqueue: delivery fingerprint conflict")
	// ErrTerminalFailure identifies a previously recorded permanent failure.
	ErrTerminalFailure = errors.New("idempotencyqueue: delivery terminally failed")
)

// Message is satisfied by queue core.TaskMessage and similar deliveries.
type Message interface {
	Payload() []byte
}

// Handler processes one elected delivery owner.
type Handler func(context.Context, Message) error

// KeyFunc creates the consumer and delivery-scoped semantic key.
type KeyFunc func(context.Context, Message) (idempotency.Key, error)

// FingerprintFunc computes canonical payload identity.
type FingerprintFunc func(Message) (idempotency.Fingerprint, error)

// Options configures durable consumer ownership and cleanup.
type Options struct {
	Service           *idempotency.Service
	Lease             time.Duration
	TransitionTimeout time.Duration
	Key               KeyFunc
	Fingerprint       FingerprintFunc
}

// Middleware deduplicates completed deliveries and owns retry transitions.
type Middleware struct {
	service           *idempotency.Service
	lease             time.Duration
	transitionTimeout time.Duration
	key               KeyFunc
	fingerprint       FingerprintFunc
}

// New validates options and constructs queue middleware.
func New(options Options) (*Middleware, error) {
	if options.Service == nil {
		return nil, configurationError("service")
	}
	if options.Lease <= 0 || options.Lease > idempotency.MaxLease {
		return nil, configurationError("lease")
	}
	if options.Key == nil {
		return nil, configurationError("key")
	}
	if options.Fingerprint == nil {
		return nil, configurationError("fingerprint")
	}
	if options.TransitionTimeout == 0 {
		options.TransitionTimeout = 5 * time.Second
	}
	if options.TransitionTimeout < 0 {
		return nil, configurationError("transition_timeout")
	}
	return &Middleware{
		service: options.Service, lease: options.Lease,
		transitionTimeout: options.TransitionTimeout,
		key:               options.Key, fingerprint: options.Fingerprint,
	}, nil
}

// Handle executes handler for an acquired delivery and completes it on success.
func (m *Middleware) Handle(ctx context.Context, message Message, handler Handler) error {
	if message == nil {
		return configurationError("message")
	}
	if handler == nil {
		return configurationError("handler")
	}
	key, err := m.key(ctx, message)
	if err != nil {
		return err
	}
	fingerprint, err := m.fingerprint(message)
	if err != nil {
		return err
	}
	begin, err := m.service.Begin(ctx, idempotency.BeginRequest{
		Acquire: idempotency.AcquireRequest{Key: key, Fingerprint: fingerprint, Lease: m.lease},
	})
	if err != nil {
		return err
	}
	switch begin.Outcome {
	case idempotency.OutcomeReplayed:
		return nil
	case idempotency.OutcomeInProgress:
		return ErrInProgress
	case idempotency.OutcomeConflict:
		return ErrConflict
	case idempotency.OutcomeTerminalFailure:
		return ErrTerminalFailure
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			_ = m.release(ctx, begin.Record.Ownership())
			panic(recovered)
		}
	}()
	handlerCtx := idempotency.WithOwnership(ctx, begin.Record.Ownership())
	if err := handler(handlerCtx, message); err != nil {
		return errors.Join(err, m.release(ctx, begin.Record.Ownership()))
	}
	_, err = m.service.Complete(ctx, idempotency.CompleteRequest{
		Ownership: begin.Record.Ownership(),
	})
	return err
}

func (m *Middleware) release(ctx context.Context, ownership idempotency.Ownership) error {
	transitionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.transitionTimeout)
	defer cancel()
	_, err := m.service.Release(transitionCtx, ownership)
	return err
}

// Wrap preserves the concrete message type expected by queue WithFn.
func Wrap[M Message](
	middleware *Middleware,
	next func(context.Context, M) error,
) func(context.Context, M) error {
	return func(ctx context.Context, message M) error {
		return middleware.Handle(ctx, message, func(ctx context.Context, _ Message) error {
			return next(ctx, message)
		})
	}
}

func configurationError(field string) error {
	return &idempotency.Error{Reason: idempotency.ReasonInvalidConfiguration, Field: field}
}
