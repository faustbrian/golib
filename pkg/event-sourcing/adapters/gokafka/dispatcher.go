package gokafka

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

var (
	// ErrPublisherRequired reports a missing synchronous Kafka publisher.
	ErrPublisherRequired = errors.New(
		"event-sourcing/gokafka: publisher is required",
	)
	// ErrCodecRequired reports a missing record codec.
	ErrCodecRequired = errors.New(
		"event-sourcing/gokafka: record codec is required",
	)
	// ErrContextRequired reports a nil dispatch context.
	ErrContextRequired = errors.New(
		"event-sourcing/gokafka: context is required",
	)
	// ErrReplayDenied reports direct replay publication without explicit opt-in.
	ErrReplayDenied = errors.New(
		"event-sourcing/gokafka: replay publication is denied",
	)
	// ErrInvalidDispatcherOption reports a nil or duplicated option.
	ErrInvalidDispatcherOption = errors.New(
		"event-sourcing/gokafka: dispatcher option is invalid",
	)
	// ErrPublisherPanic reports a contained publisher panic.
	ErrPublisherPanic = errors.New(
		"event-sourcing/gokafka: publisher panicked",
	)
	// ErrDispatchFailed categorizes a partially or wholly failed batch.
	ErrDispatchFailed = errors.New(
		"event-sourcing/gokafka: dispatch failed",
	)
)

// Publisher synchronously publishes one Kafka record and returns only after
// the configured acknowledgement policy succeeds or fails.
type Publisher interface {
	Publish(context.Context, kafka.Message) error
}

// Dispatcher maps and synchronously publishes event deliveries in input
// order. It starts no goroutines and is safe for concurrent and reentrant use
// when its codec resolver and publisher are.
type Dispatcher struct {
	publisher              Publisher
	codec                  *RecordCodec
	allowReplay            bool
	continueOnPublishError bool
}

// DispatchError reports exact batch progress without exposing payloads,
// metadata, broker diagnostics, or panic values.
type DispatchError struct {
	cause     error
	published int
	failed    int
	attempted int
	total     int
}

// Error implements error with a stable redacted diagnostic.
func (*DispatchError) Error() string {
	return ErrDispatchFailed.Error()
}

// Unwrap preserves the stable category and underlying causes.
func (err *DispatchError) Unwrap() []error {
	return []error{ErrDispatchFailed, err.cause}
}

// Published returns the number of acknowledged records.
func (err *DispatchError) Published() int {
	return err.published
}

// Failed returns the number of attempted records that failed.
func (err *DispatchError) Failed() int {
	return err.failed
}

// Attempted returns the number of deliveries encoded or published.
func (err *DispatchError) Attempted() int {
	return err.attempted
}

// Total returns the input delivery count.
func (err *DispatchError) Total() int {
	return err.total
}

// DispatcherOption configures one immutable direct-publication policy.
type DispatcherOption interface {
	configureDispatcher(*dispatcherConfig) error
}

type dispatcherConfig struct {
	allowReplay            bool
	continueOnPublishError bool
}

type allowReplayOption struct{}
type continueOnPublishErrorOption struct{}

// AllowReplay explicitly permits direct publication of replay deliveries.
func AllowReplay() DispatcherOption {
	return allowReplayOption{}
}

// ContinueOnPublishError attempts later deliveries after a publisher error.
// Encoding failures, replay denial, and cancellation always stop the batch.
func ContinueOnPublishError() DispatcherOption {
	return continueOnPublishErrorOption{}
}

// NewDispatcher validates a synchronous publisher and immutable record codec.
func NewDispatcher(
	publisher Publisher,
	codec *RecordCodec,
	options ...DispatcherOption,
) (*Dispatcher, error) {
	if publisher == nil {
		return nil, ErrPublisherRequired
	}
	if codec == nil {
		return nil, ErrCodecRequired
	}

	config := dispatcherConfig{}
	for _, option := range options {
		if option == nil {
			return nil, ErrInvalidDispatcherOption
		}
		if err := option.configureDispatcher(&config); err != nil {
			return nil, err
		}
	}

	return &Dispatcher{
		publisher:              publisher,
		codec:                  codec,
		allowReplay:            config.allowReplay,
		continueOnPublishError: config.continueOnPublishError,
	}, nil
}

func (allowReplayOption) configureDispatcher(config *dispatcherConfig) error {
	if config.allowReplay {
		return ErrInvalidDispatcherOption
	}
	config.allowReplay = true

	return nil
}

func (continueOnPublishErrorOption) configureDispatcher(
	config *dispatcherConfig,
) error {
	if config.continueOnPublishError {
		return ErrInvalidDispatcherOption
	}
	config.continueOnPublishError = true

	return nil
}

// Dispatch encodes and synchronously publishes deliveries in input order.
func (dispatcher *Dispatcher) Dispatch(
	ctx context.Context,
	deliveries []eventsourcing.Delivery,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if dispatcher == nil ||
		dispatcher.publisher == nil ||
		dispatcher.codec == nil {
		return ErrPublisherRequired
	}

	published := 0
	failed := 0
	attempted := 0
	var publishErrors []error
	for _, delivery := range deliveries {
		if err := ctx.Err(); err != nil {
			if attempted == 0 {
				return err
			}

			return dispatchFailure(
				len(deliveries),
				published,
				failed,
				attempted,
				joinDispatchCauses(publishErrors, err),
			)
		}
		if delivery.Mode() == eventsourcing.DeliveryReplay &&
			!dispatcher.allowReplay {
			return dispatchFailure(
				len(deliveries),
				published,
				failed+1,
				attempted+1,
				joinDispatchCauses(publishErrors, ErrReplayDenied),
			)
		}
		record, err := dispatcher.codec.Encode(delivery)
		if err != nil {
			return dispatchFailure(
				len(deliveries),
				published,
				failed+1,
				attempted+1,
				joinDispatchCauses(publishErrors, err),
			)
		}
		attempted++
		if err := callPublisher(ctx, dispatcher.publisher, record); err != nil {
			failed++
			publishErrors = append(publishErrors, err)
			if !dispatcher.continueOnPublishError {
				return dispatchFailure(
					len(deliveries),
					published,
					failed,
					attempted,
					err,
				)
			}

			continue
		}
		published++
	}
	if failed > 0 {
		return dispatchFailure(
			len(deliveries),
			published,
			failed,
			attempted,
			errors.Join(publishErrors...),
		)
	}

	return nil
}

func callPublisher(
	ctx context.Context,
	publisher Publisher,
	message kafka.Message,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrPublisherPanic
		}
	}()

	return publisher.Publish(ctx, message)
}

func dispatchFailure(
	total int,
	published int,
	failed int,
	attempted int,
	cause error,
) error {
	return &DispatchError{
		cause:     cause,
		published: published,
		failed:    failed,
		attempted: attempted,
		total:     total,
	}
}

func joinDispatchCauses(previous []error, current error) error {
	causes := make([]error, 0, len(previous)+1)
	causes = append(causes, previous...)
	causes = append(causes, current)

	return errors.Join(causes...)
}
