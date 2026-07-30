package gokafka

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
)

const (
	// HeaderDeadLetterSourceTopic identifies the original Kafka topic.
	HeaderDeadLetterSourceTopic = "esdlq.source_topic"
	// HeaderDeadLetterSourcePartition identifies the original Kafka partition.
	HeaderDeadLetterSourcePartition = "esdlq.source_partition"
	// HeaderDeadLetterSourceOffset identifies the original Kafka offset.
	HeaderDeadLetterSourceOffset = "esdlq.source_offset"
	// HeaderDeadLetterSourceTime identifies the original Kafka record time.
	HeaderDeadLetterSourceTime = "esdlq.source_time"
)

var (
	// ErrInvalidDeadLetterTopic reports an invalid dead-letter destination.
	ErrInvalidDeadLetterTopic = errors.New(
		"event-sourcing/gokafka: dead-letter topic is invalid",
	)
	// ErrInvalidDeadLetterConfig reports limits that cannot contain the
	// required source-position headers.
	ErrInvalidDeadLetterConfig = errors.New(
		"event-sourcing/gokafka: dead-letter configuration is invalid",
	)
	// ErrFailureCauseRequired reports a missing original handling failure.
	ErrFailureCauseRequired = errors.New(
		"event-sourcing/gokafka: failure cause is required",
	)
	// ErrDeadLetterLoop reports a record already at or marked for the
	// configured dead-letter destination.
	ErrDeadLetterLoop = errors.New(
		"event-sourcing/gokafka: dead-letter loop is denied",
	)
	// ErrDeadLetterPublisherPanic reports a contained publisher panic.
	ErrDeadLetterPublisherPanic = errors.New(
		"event-sourcing/gokafka: dead-letter publisher panicked",
	)
	// ErrDeadLetterPublishFailed categorizes a failed publication attempt.
	ErrDeadLetterPublishFailed = errors.New(
		"event-sourcing/gokafka: dead-letter publication failed",
	)
)

// DeadLetterError reports a failed source position without exposing record
// data, application diagnostics, broker diagnostics, or panic values.
type DeadLetterError struct {
	cause     error
	topic     string
	partition int32
	offset    int64
}

// Error implements error with a stable redacted diagnostic.
func (*DeadLetterError) Error() string {
	return ErrDeadLetterPublishFailed.Error()
}

// Unwrap preserves the stable category and publisher failure.
func (err *DeadLetterError) Unwrap() []error {
	return []error{ErrDeadLetterPublishFailed, err.cause}
}

// SourceTopic returns the Kafka topic containing the failed source record.
func (err *DeadLetterError) SourceTopic() string {
	return err.topic
}

// Partition returns the Kafka partition containing the failed source record.
func (err *DeadLetterError) Partition() int32 {
	return err.partition
}

// Offset returns the Kafka offset containing the failed source record.
func (err *DeadLetterError) Offset() int64 {
	return err.offset
}

// DeadLetterPolicy synchronously republishes one failed source record to a
// fixed dead-letter topic. It is safe for concurrent use when its publisher is.
type DeadLetterPolicy struct {
	publisher Publisher
	topic     string
	limits    kafka.MessageLimits
}

var _ FailurePolicy = (*DeadLetterPolicy)(nil)

// DeadLetterPolicyConfig defines one fixed destination and its outbound record
// bounds. Zero Limits use DefaultRecordLimits.
type DeadLetterPolicyConfig struct {
	Topic  string
	Limits kafka.MessageLimits
}

// NewDeadLetterPolicy validates a synchronous publisher and fixed dead-letter
// destination.
func NewDeadLetterPolicy(
	publisher Publisher,
	config DeadLetterPolicyConfig,
) (*DeadLetterPolicy, error) {
	if publisher == nil {
		return nil, ErrPublisherRequired
	}
	if config.Limits == (kafka.MessageLimits{}) {
		config.Limits = DefaultRecordLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return nil, ErrInvalidDeadLetterConfig
	}
	if !validTopic(config.Topic, config.Limits.MaxTopicBytes) {
		return nil, ErrInvalidDeadLetterTopic
	}
	if !validDeadLetterLimits(config.Limits) {
		return nil, ErrInvalidDeadLetterConfig
	}

	return &DeadLetterPolicy{
		publisher: publisher,
		topic:     config.Topic,
		limits:    config.Limits,
	}, nil
}

// HandleFailure republishes owned source bytes and returns FailureHandled only
// after the publisher acknowledges the dead-letter record.
func (policy *DeadLetterPolicy) HandleFailure(
	ctx context.Context,
	record kafka.ConsumedMessage,
	cause error,
) (FailureDisposition, error) {
	if ctx == nil {
		return FailureRetry, ErrContextRequired
	}
	if policy == nil || policy.publisher == nil {
		return FailureRetry, ErrPublisherRequired
	}
	if err := ctx.Err(); err != nil {
		return FailureRetry, err
	}
	if cause == nil {
		return FailureRetry, ErrFailureCauseRequired
	}
	if record.Partition < 0 || record.Offset < 0 {
		return FailureRetry, ErrInvalidKafkaPosition
	}
	if err := validateDeadLetterRecord(record, policy.limits); err != nil {
		return FailureRetry, ErrRecordCorrupt
	}
	if record.Topic == policy.topic || hasDeadLetterHeader(record.Headers) {
		return FailureRetry, ErrDeadLetterLoop
	}

	message := deadLetterMessage(policy.topic, record)
	if err := callDeadLetterPublisher(ctx, policy.publisher, message); err != nil {
		return FailureRetry, &DeadLetterError{
			cause:     err,
			topic:     record.Topic,
			partition: record.Partition,
			offset:    record.Offset,
		}
	}
	if err := ctx.Err(); err != nil {
		return FailureRetry, err
	}

	return FailureHandled, nil
}

func deadLetterMessage(
	topic string,
	record kafka.ConsumedMessage,
) kafka.Message {
	headers := make([]kafka.Header, 0, len(record.Headers)+4)
	for _, item := range record.Headers {
		headers = append(headers, kafka.Header{
			Key:   item.Key,
			Value: slices.Clone(item.Value),
		})
	}
	positionHeaders := deadLetterPositionHeaders(record)
	headers = append(headers, positionHeaders[:]...)

	return kafka.Message{
		Topic:   topic,
		Key:     slices.Clone(record.Key),
		Value:   slices.Clone(record.Value),
		Headers: headers,
	}
}

func validDeadLetterLimits(limits kafka.MessageLimits) bool {
	longestKey := len(HeaderDeadLetterSourcePartition)
	minimumHeaderBytes := len(HeaderDeadLetterSourceTopic) +
		len(HeaderDeadLetterSourcePartition) +
		len(HeaderDeadLetterSourceOffset) +
		len(HeaderDeadLetterSourceTime) +
		1 +
		1 +
		1 +
		len("1970-01-01T00:00:00Z")

	return limits.MaxHeaders >= 4 &&
		limits.MaxHeaderKeyBytes >= longestKey &&
		limits.MaxHeaderValueBytes >= len("1970-01-01T00:00:00Z") &&
		limits.MaxHeaderBytes >= minimumHeaderBytes
}

func validateDeadLetterRecord(
	record kafka.ConsumedMessage,
	limits kafka.MessageLimits,
) error {
	if !validTopic(record.Topic, limits.MaxTopicBytes) ||
		len(record.Key) > limits.MaxKeyBytes ||
		len(record.Value) > limits.MaxValueBytes ||
		len(record.Headers) > limits.MaxHeaders-4 ||
		record.Timestamp.IsZero() {
		return ErrRecordCorrupt
	}
	headerBytes := 0
	for _, item := range record.Headers {
		next, valid := nextHeaderByteTotal(headerBytes, item, limits)
		if !valid {
			return ErrRecordCorrupt
		}
		headerBytes = next
	}
	for _, item := range deadLetterPositionHeaders(record) {
		next, valid := nextHeaderByteTotal(headerBytes, item, limits)
		if !valid {
			return ErrRecordCorrupt
		}
		headerBytes = next
	}

	return nil
}

func hasDeadLetterHeader(headers []kafka.Header) bool {
	for _, item := range headers {
		if strings.HasPrefix(item.Key, "esdlq.") {
			return true
		}
	}

	return false
}

func deadLetterPositionHeaders(
	record kafka.ConsumedMessage,
) [4]kafka.Header {
	return [4]kafka.Header{
		{
			Key:   HeaderDeadLetterSourceTopic,
			Value: []byte(record.Topic),
		},
		{
			Key: HeaderDeadLetterSourcePartition,
			Value: []byte(strconv.FormatInt(
				int64(record.Partition),
				10,
			)),
		},
		{
			Key:   HeaderDeadLetterSourceOffset,
			Value: []byte(strconv.FormatInt(record.Offset, 10)),
		},
		{
			Key: HeaderDeadLetterSourceTime,
			Value: []byte(record.Timestamp.UTC().Format(
				time.RFC3339Nano,
			)),
		},
	}
}

func callDeadLetterPublisher(
	ctx context.Context,
	publisher Publisher,
	message kafka.Message,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrDeadLetterPublisherPanic
		}
	}()

	return publisher.Publish(ctx, message)
}
