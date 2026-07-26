package gokafka

import (
	"context"
	"errors"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

var (
	// ErrConsumerRequired reports a missing delivery consumer.
	ErrConsumerRequired = errors.New(
		"event-sourcing/gokafka: delivery consumer is required",
	)
	// ErrReplayHandlingDenied reports replay consumption without explicit
	// opt-in.
	ErrReplayHandlingDenied = errors.New(
		"event-sourcing/gokafka: replay handling is denied",
	)
	// ErrInvalidHandlerOption reports a nil or duplicated handler option.
	ErrInvalidHandlerOption = errors.New(
		"event-sourcing/gokafka: record handler option is invalid",
	)
	// ErrConsumerPanic reports a contained delivery consumer panic.
	ErrConsumerPanic = errors.New(
		"event-sourcing/gokafka: delivery consumer panicked",
	)
	// ErrRecordHandlingFailed categorizes a record that must not be settled.
	ErrRecordHandlingFailed = errors.New(
		"event-sourcing/gokafka: record handling failed",
	)
	// ErrInvalidKafkaPosition reports a negative partition or offset.
	ErrInvalidKafkaPosition = errors.New(
		"event-sourcing/gokafka: Kafka position is invalid",
	)
	// ErrFailurePolicyRequired reports a missing failure policy.
	ErrFailurePolicyRequired = errors.New(
		"event-sourcing/gokafka: failure policy is required",
	)
	// ErrFailurePolicyPanic reports a contained failure-policy panic.
	ErrFailurePolicyPanic = errors.New(
		"event-sourcing/gokafka: failure policy panicked",
	)
	// ErrInvalidFailureDisposition reports an unknown policy result.
	ErrInvalidFailureDisposition = errors.New(
		"event-sourcing/gokafka: failure disposition is invalid",
	)
)

// DeliveryConsumer handles one decoded event delivery before its Kafka offset
// may be settled.
type DeliveryConsumer interface {
	Consume(context.Context, eventsourcing.Delivery) error
}

// DeliveryConsumerFunc adapts a function to DeliveryConsumer.
type DeliveryConsumerFunc func(context.Context, eventsourcing.Delivery) error

// Consume implements DeliveryConsumer.
func (consumer DeliveryConsumerFunc) Consume(
	ctx context.Context,
	delivery eventsourcing.Delivery,
) error {
	if consumer == nil {
		return ErrConsumerRequired
	}

	return consumer(ctx, delivery)
}

// FailureDisposition controls whether a failed record remains unsettled or
// has been durably handled and may be settled.
type FailureDisposition uint8

const (
	// FailureRetry leaves the record unsettled for at-least-once retry.
	FailureRetry FailureDisposition = iota + 1
	// FailureHandled permits settlement after a policy durably quarantines,
	// dead-letters, or otherwise handles the record.
	FailureHandled
)

// FailurePolicy synchronously decides how to handle one failed record.
//
// Record bytes are borrowed for the call and must be copied before retention.
type FailurePolicy interface {
	HandleFailure(
		context.Context,
		kafka.ConsumedMessage,
		error,
	) (FailureDisposition, error)
}

// FailurePolicyFunc adapts a function to FailurePolicy.
type FailurePolicyFunc func(
	context.Context,
	kafka.ConsumedMessage,
	error,
) (FailureDisposition, error)

// HandleFailure implements FailurePolicy.
func (policy FailurePolicyFunc) HandleFailure(
	ctx context.Context,
	record kafka.ConsumedMessage,
	cause error,
) (FailureDisposition, error) {
	if policy == nil {
		return 0, ErrFailurePolicyRequired
	}

	return policy(ctx, record, cause)
}

// RecordHandler decodes one Kafka record and invokes an event delivery
// consumer synchronously. It implements kafka.Handler and is safe for
// concurrent use when its codec resolver and consumer are.
type RecordHandler struct {
	codec         *RecordCodec
	consumer      DeliveryConsumer
	failurePolicy FailurePolicy
	allowReplay   bool
}

var _ kafka.Handler = (*RecordHandler)(nil)

// HandlerError reports the failed Kafka position without exposing record data,
// consumer diagnostics, or panic values.
type HandlerError struct {
	cause     error
	topic     string
	partition int32
	offset    int64
}

// Error implements error with a stable redacted diagnostic.
func (*HandlerError) Error() string {
	return ErrRecordHandlingFailed.Error()
}

// Unwrap preserves the stable category and underlying cause.
func (err *HandlerError) Unwrap() []error {
	return []error{ErrRecordHandlingFailed, err.cause}
}

// Topic returns the Kafka topic containing the failed record.
func (err *HandlerError) Topic() string {
	return err.topic
}

// Partition returns the Kafka partition containing the failed record.
func (err *HandlerError) Partition() int32 {
	return err.partition
}

// Offset returns the Kafka offset of the failed record.
func (err *HandlerError) Offset() int64 {
	return err.offset
}

// RecordHandlerOption configures one immutable record-handling policy.
type RecordHandlerOption interface {
	configureRecordHandler(*recordHandlerConfig) error
}

type recordHandlerConfig struct {
	allowReplay   bool
	failurePolicy FailurePolicy
}

type allowReplayHandlingOption struct{}
type failurePolicyOption struct {
	policy FailurePolicy
}

// AllowReplayHandling explicitly permits replay delivery handling.
func AllowReplayHandling() RecordHandlerOption {
	return allowReplayHandlingOption{}
}

// WithFailurePolicy installs an explicit synchronous retry or handled
// decision for decode and consumer failures.
func WithFailurePolicy(policy FailurePolicy) RecordHandlerOption {
	return failurePolicyOption{policy: policy}
}

// NewRecordHandler validates the record codec and delivery consumer.
func NewRecordHandler(
	codec *RecordCodec,
	consumer DeliveryConsumer,
	options ...RecordHandlerOption,
) (*RecordHandler, error) {
	if codec == nil {
		return nil, ErrCodecRequired
	}
	if consumer == nil {
		return nil, ErrConsumerRequired
	}

	config := recordHandlerConfig{}
	for _, option := range options {
		if option == nil {
			return nil, ErrInvalidHandlerOption
		}
		if err := option.configureRecordHandler(&config); err != nil {
			return nil, err
		}
	}

	return &RecordHandler{
		codec:         codec,
		consumer:      consumer,
		failurePolicy: config.failurePolicy,
		allowReplay:   config.allowReplay,
	}, nil
}

func (allowReplayHandlingOption) configureRecordHandler(
	config *recordHandlerConfig,
) error {
	if config.allowReplay {
		return ErrInvalidHandlerOption
	}
	config.allowReplay = true

	return nil
}

func (option failurePolicyOption) configureRecordHandler(
	config *recordHandlerConfig,
) error {
	if option.policy == nil || config.failurePolicy != nil {
		return ErrInvalidHandlerOption
	}
	config.failurePolicy = option.policy

	return nil
}

// Handle decodes and handles one record. Returning nil permits the owning
// group consumer to settle the offset.
func (handler *RecordHandler) Handle(
	ctx context.Context,
	record kafka.ConsumedMessage,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if handler == nil || handler.codec == nil || handler.consumer == nil {
		return ErrConsumerRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Partition < 0 || record.Offset < 0 {
		return recordHandlingFailure(record, ErrInvalidKafkaPosition)
	}

	delivery, err := handler.codec.Decode(record)
	if err != nil {
		return handler.handleFailure(ctx, record, err)
	}
	if delivery.Mode() == eventsourcing.DeliveryReplay &&
		!handler.allowReplay {
		return recordHandlingFailure(record, ErrReplayHandlingDenied)
	}

	if err := callDeliveryConsumer(
		ctx,
		handler.consumer,
		delivery,
	); err != nil {
		return handler.handleFailure(ctx, record, err)
	}
	if err := ctx.Err(); err != nil {
		return recordHandlingFailure(record, err)
	}

	return nil
}

func (handler *RecordHandler) handleFailure(
	ctx context.Context,
	record kafka.ConsumedMessage,
	cause error,
) error {
	if handler.failurePolicy == nil {
		return recordHandlingFailure(record, cause)
	}
	if err := ctx.Err(); err != nil {
		return recordHandlingFailure(record, errors.Join(cause, err))
	}
	disposition, err := callFailurePolicy(
		ctx,
		handler.failurePolicy,
		record,
		cause,
	)
	if err != nil {
		return recordHandlingFailure(record, errors.Join(cause, err))
	}
	switch disposition {
	case FailureRetry:
		return recordHandlingFailure(record, cause)
	case FailureHandled:
		if err := ctx.Err(); err != nil {
			return recordHandlingFailure(
				record,
				errors.Join(cause, err),
			)
		}

		return nil
	default:
		return recordHandlingFailure(
			record,
			errors.Join(cause, ErrInvalidFailureDisposition),
		)
	}
}

func callDeliveryConsumer(
	ctx context.Context,
	consumer DeliveryConsumer,
	delivery eventsourcing.Delivery,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrConsumerPanic
		}
	}()

	return consumer.Consume(ctx, delivery)
}

func callFailurePolicy(
	ctx context.Context,
	policy FailurePolicy,
	record kafka.ConsumedMessage,
	cause error,
) (disposition FailureDisposition, err error) {
	defer func() {
		if recover() != nil {
			disposition = 0
			err = ErrFailurePolicyPanic
		}
	}()

	return policy.HandleFailure(ctx, record, cause)
}

func recordHandlingFailure(
	record kafka.ConsumedMessage,
	cause error,
) error {
	return &HandlerError{
		cause:     cause,
		topic:     record.Topic,
		partition: record.Partition,
		offset:    record.Offset,
	}
}
