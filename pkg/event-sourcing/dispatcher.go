package eventsourcing

import (
	"context"
	"errors"
	"fmt"
)

// DeliveryMode distinguishes ordinary live handling from controlled replay.
type DeliveryMode uint8

const (
	// DeliveryLive is post-persistence application delivery.
	DeliveryLive DeliveryMode = iota + 1
	// DeliveryReplay is explicit historical delivery.
	DeliveryReplay
)

// String returns the stable diagnostic delivery mode.
func (mode DeliveryMode) String() string {
	switch mode {
	case DeliveryLive:
		return "live"
	case DeliveryReplay:
		return "replay"
	default:
		return "unknown"
	}
}

// Delivery is an immutable persisted message plus its delivery intent.
type Delivery struct {
	message Message
	mode    DeliveryMode
}

// NewDelivery validates a persisted message and explicit delivery mode.
func NewDelivery(message Message, mode DeliveryMode) (Delivery, error) {
	if message.ID().IsZero() {
		return Delivery{}, invalid("message", "must be assigned")
	}
	if mode != DeliveryLive && mode != DeliveryReplay {
		return Delivery{}, invalid("delivery_mode", "must be live or replay")
	}

	return Delivery{message: message, mode: mode}, nil
}

// Message returns the immutable persisted message.
func (delivery Delivery) Message() Message {
	return delivery.message
}

// Mode returns the explicit live or replay intent.
func (delivery Delivery) Mode() DeliveryMode {
	return delivery.mode
}

// IsZero reports whether the delivery has not been assigned.
func (delivery Delivery) IsZero() bool {
	return delivery.message.ID().IsZero() || delivery.mode == 0
}

// ConsumerFunc handles one persisted delivery synchronously.
type ConsumerFunc func(context.Context, Delivery) error

// DeliveryFilter selects deliveries before a consumer is called.
type DeliveryFilter func(Delivery) bool

// ConsumerOption configures one immutable consumer registration.
type ConsumerOption interface {
	configureConsumer(*Consumer) error
}

type filterConsumerOption struct {
	filter DeliveryFilter
}

// FilterDelivery adds one ordered filter to a consumer.
func FilterDelivery(filter DeliveryFilter) ConsumerOption {
	return filterConsumerOption{filter: filter}
}

// Consumer is an immutable named synchronous handler registration.
type Consumer struct {
	id      string
	handler ConsumerFunc
	filters []DeliveryFilter
}

// NewConsumer validates one stable registration identity and handler.
func NewConsumer(
	id string,
	handler ConsumerFunc,
	options ...ConsumerOption,
) (Consumer, error) {
	if !validToken(id, MaxMessageIDBytes) {
		return Consumer{}, invalid("consumer_id", "must be a non-empty canonical token")
	}
	if handler == nil {
		return Consumer{}, invalid("handler", "must be assigned")
	}

	consumer := Consumer{id: id, handler: handler}
	for _, option := range options {
		if option == nil {
			return Consumer{}, invalid("option", "must be assigned")
		}
		if err := option.configureConsumer(&consumer); err != nil {
			return Consumer{}, fmt.Errorf("configure consumer: %w", err)
		}
	}

	return consumer, nil
}

// ID returns the stable registration identity.
func (consumer Consumer) ID() string {
	return consumer.id
}

func (option filterConsumerOption) configureConsumer(consumer *Consumer) error {
	if option.filter == nil {
		return invalid("filter", "must be assigned")
	}
	consumer.filters = append(consumer.filters, option.filter)

	return nil
}

// Dispatcher synchronously delivers persisted messages to consumers.
type Dispatcher interface {
	Dispatch(context.Context, []Delivery) error
}

// SyncDispatcher performs ordered in-process delivery.
//
// It is immutable after construction, safe for concurrent use, and permits
// reentrant dispatch without holding locks across application callbacks.
type SyncDispatcher struct {
	consumers       []Consumer
	continueOnError bool
}

// SyncDispatcherOption is one validated consumer or dispatch-policy option.
type SyncDispatcherOption interface {
	configureSyncDispatcher(*syncDispatcherBuilder) error
}

type syncDispatcherBuilder struct {
	consumers       []Consumer
	identities      map[string]struct{}
	continueOnError bool
}

type continueOnConsumerError struct{}

// ContinueOnConsumerError selects ordered best-effort delivery and joins all
// consumer failures. Cancellation still stops new calls.
func ContinueOnConsumerError() SyncDispatcherOption {
	return continueOnConsumerError{}
}

// NewSyncDispatcher validates ordered consumers and explicit policy options.
func NewSyncDispatcher(options ...SyncDispatcherOption) (*SyncDispatcher, error) {
	builder := syncDispatcherBuilder{
		identities: make(map[string]struct{}, len(options)),
	}
	for _, option := range options {
		if option == nil {
			return nil, invalid("option", "must be assigned")
		}
		if err := option.configureSyncDispatcher(&builder); err != nil {
			return nil, fmt.Errorf("configure synchronous dispatcher: %w", err)
		}
	}

	return &SyncDispatcher{
		consumers:       builder.consumers,
		continueOnError: builder.continueOnError,
	}, nil
}

func (consumer Consumer) configureSyncDispatcher(
	builder *syncDispatcherBuilder,
) error {
	if consumer.id == "" || consumer.handler == nil {
		return invalid("consumer", "must be constructed")
	}
	if _, duplicate := builder.identities[consumer.id]; duplicate {
		return fmt.Errorf("%w: %s", ErrDuplicateConsumer, consumer.id)
	}
	builder.identities[consumer.id] = struct{}{}
	consumer.filters = append([]DeliveryFilter(nil), consumer.filters...)
	builder.consumers = append(builder.consumers, consumer)

	return nil
}

func (continueOnConsumerError) configureSyncDispatcher(
	builder *syncDispatcherBuilder,
) error {
	if builder.continueOnError {
		return invalid("continue_on_error", "must not be configured more than once")
	}
	builder.continueOnError = true

	return nil
}

// Dispatch invokes consumers in message-major, registration order.
//
// It stops at the first filter or consumer error. Empty delivery succeeds.
func (dispatcher *SyncDispatcher) Dispatch(
	ctx context.Context,
	deliveries []Delivery,
) error {
	if ctx == nil {
		return ErrInvalidArgument
	}
	for _, delivery := range deliveries {
		if delivery.IsZero() {
			return ErrInvalidArgument
		}
	}

	var failures []error
	for _, delivery := range deliveries {
		for _, consumer := range dispatcher.consumers {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(failures, err)...)
			}
			selected, err := selectsDelivery(consumer, delivery)
			if err != nil {
				if !dispatcher.continueOnError {
					return err
				}
				failures = append(failures, err)

				continue
			}
			if !selected {
				continue
			}
			if err := callConsumer(ctx, consumer, delivery); err != nil {
				if !dispatcher.continueOnError {
					return err
				}
				failures = append(failures, err)
			}
		}
	}

	return errors.Join(failures...)
}

func selectsDelivery(
	consumer Consumer,
	delivery Delivery,
) (selected bool, err error) {
	defer func() {
		if recover() != nil {
			selected = false
			err = &ConsumerError{
				ConsumerID: consumer.id,
				MessageID:  delivery.message.ID(),
				Cause:      ErrConsumerPanic,
			}
		}
	}()

	for _, filter := range consumer.filters {
		if !filter(delivery) {
			return false, nil
		}
	}

	return true, nil
}

func callConsumer(
	ctx context.Context,
	consumer Consumer,
	delivery Delivery,
) (err error) {
	defer func() {
		if recover() != nil {
			err = &ConsumerError{
				ConsumerID: consumer.id,
				MessageID:  delivery.message.ID(),
				Cause:      ErrConsumerPanic,
			}
		}
	}()

	if err := consumer.handler(ctx, delivery); err != nil {
		return &ConsumerError{
			ConsumerID: consumer.id,
			MessageID:  delivery.message.ID(),
			Cause:      err,
		}
	}

	return nil
}

// ConsumerError identifies a failed consumer without printing message data or
// the wrapped application diagnostic.
type ConsumerError struct {
	ConsumerID string
	MessageID  MessageID
	Cause      error
}

// Error implements error with redacted diagnostics.
func (*ConsumerError) Error() string {
	return "event consumer failed"
}

// Unwrap preserves the application cause for errors.Is and errors.As.
func (err *ConsumerError) Unwrap() error {
	return err.Cause
}

var _ Dispatcher = (*SyncDispatcher)(nil)
