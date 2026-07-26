package gotelemetry

import (
	"context"
	"errors"
	"strings"

	"github.com/faustbrian/golib/pkg/kafka"
)

var (
	// ErrKafkaPublisherRequired reports a missing downstream Kafka publisher.
	ErrKafkaPublisherRequired = errors.New(
		"event-sourcing/gotelemetry: Kafka publisher is required",
	)
	// ErrKafkaHandlerRequired reports a missing downstream Kafka handler.
	ErrKafkaHandlerRequired = errors.New(
		"event-sourcing/gotelemetry: Kafka handler is required",
	)
	// ErrInvalidKafkaPropagation reports propagation configuration that cannot
	// be applied within the configured record limits.
	ErrInvalidKafkaPropagation = errors.New(
		"event-sourcing/gotelemetry: invalid Kafka propagation configuration",
	)
	// ErrKafkaPropagationRejected categorizes an outbound record that cannot be
	// copied and injected within the configured limits.
	ErrKafkaPropagationRejected = errors.New(
		"event-sourcing/gotelemetry: Kafka propagation rejected",
	)
)

// KafkaPublisher is the synchronous publication contract implemented by the
// Kafka producer and the event-sourcing Kafka dispatcher publisher.
type KafkaPublisher interface {
	Publish(context.Context, kafka.Message) error
}

// KafkaPropagationConfig bounds records before propagation allocates or copies
// their caller-controlled fields. A zero Limits value selects the Kafka
// package defaults.
type KafkaPropagationConfig struct {
	Limits kafka.MessageLimits
}

// KafkaPropagationError preserves a stable rejection category without
// exposing record data.
type KafkaPropagationError struct {
	cause error
}

// Error implements error with a redacted diagnostic.
func (*KafkaPropagationError) Error() string {
	return ErrKafkaPropagationRejected.Error()
}

// Unwrap preserves the rejection category and underlying stable cause.
func (err *KafkaPropagationError) Unwrap() []error {
	return []error{ErrKafkaPropagationRejected, err.cause}
}

// WrapKafkaPublisher copies a record, replaces the configured propagator's
// declared headers, injects the current context, and synchronously invokes
// next. Existing propagation headers never pass through when no valid context
// is available.
func (instrumentation *Instrumentation) WrapKafkaPublisher(
	next KafkaPublisher,
	config KafkaPropagationConfig,
) (KafkaPublisher, error) {
	fields, limits, err := instrumentation.kafkaPropagation(config)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, ErrKafkaPublisherRequired
	}

	return kafkaPublisher{
		instrumentation: instrumentation,
		next:            next,
		fields:          fields,
		limits:          limits,
	}, nil
}

// WrapKafkaHandler extracts a remote parent from bounded, unambiguous headers
// before invoking next. Oversized, duplicated, or malformed propagation is
// ignored, while the record remains available to the downstream handler.
func (instrumentation *Instrumentation) WrapKafkaHandler(
	next kafka.Handler,
	config KafkaPropagationConfig,
) (kafka.Handler, error) {
	fields, limits, err := instrumentation.kafkaPropagation(config)
	if err != nil {
		return nil, err
	}
	if next == nil {
		return nil, ErrKafkaHandlerRequired
	}

	return kafkaHandler{
		instrumentation: instrumentation,
		next:            next,
		fields:          fields,
		limits:          limits,
	}, nil
}

func (instrumentation *Instrumentation) kafkaPropagation(
	config KafkaPropagationConfig,
) (map[string]struct{}, kafka.MessageLimits, error) {
	if instrumentation == nil || !instrumentation.valid() {
		return nil, kafka.MessageLimits{}, ErrRuntimeRequired
	}
	limits := config.Limits
	if limits == (kafka.MessageLimits{}) {
		limits = kafka.DefaultMessageLimits()
	}
	if err := limits.Validate(); err != nil {
		return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
	}
	propagationFields := instrumentation.propagator.Fields()
	if len(propagationFields) > limits.MaxHeaders {
		return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
	}
	fields := make(map[string]struct{}, len(propagationFields))
	for _, field := range propagationFields {
		if !validPropagationField(field, limits.MaxHeaderKeyBytes) {
			return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
		}
		if _, exists := fields[field]; exists {
			return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
		}
		fields[field] = struct{}{}
	}

	return fields, limits, nil
}

type kafkaPublisher struct {
	instrumentation *Instrumentation
	next            KafkaPublisher
	fields          map[string]struct{}
	limits          kafka.MessageLimits
}

func (publisher kafkaPublisher) Publish(
	ctx context.Context,
	message kafka.Message,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if !validKafkaMessage(message, publisher.limits) {
		return kafkaPropagationFailure(kafka.ErrInvalidMessageLimits)
	}
	owned := cloneKafkaMessage(message, publisher.fields)
	carrier := &kafkaInjectCarrier{
		headers: owned.Headers,
		fields:  publisher.fields,
		limits:  publisher.limits,
	}
	publisher.instrumentation.propagator.Inject(ctx, carrier)
	owned.Headers = carrier.headers
	if carrier.rejected || !validKafkaMessage(owned, publisher.limits) {
		return kafkaPropagationFailure(kafka.ErrInvalidMessageLimits)
	}

	return publisher.next.Publish(ctx, owned)
}

type kafkaHandler struct {
	instrumentation *Instrumentation
	next            kafka.Handler
	fields          map[string]struct{}
	limits          kafka.MessageLimits
}

func (handler kafkaHandler) Handle(
	ctx context.Context,
	message kafka.ConsumedMessage,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	owned := message
	if validKafkaPropagationHeaders(
		message.Headers,
		handler.fields,
		handler.limits,
	) {
		owned = cloneConsumedHeaders(message)
		ctx = handler.instrumentation.propagator.Extract(
			ctx,
			kafkaExtractCarrier{headers: owned.Headers, fields: handler.fields},
		)
	}

	return handler.next.Handle(ctx, owned)
}

type kafkaInjectCarrier struct {
	headers  []kafka.Header
	fields   map[string]struct{}
	limits   kafka.MessageLimits
	rejected bool
}

func (carrier *kafkaInjectCarrier) Get(key string) string {
	return kafkaExtractCarrier{
		headers: carrier.headers,
		fields:  carrier.fields,
	}.Get(key)
}

func (carrier *kafkaInjectCarrier) Set(key, value string) {
	if carrier.rejected {
		return
	}
	key = strings.ToLower(key)
	if _, allowed := carrier.fields[key]; !allowed ||
		len(value) > carrier.limits.MaxHeaderValueBytes {
		carrier.rejected = true
		return
	}
	carrier.headers = removeKafkaHeaders(carrier.headers, key)
	carrier.headers = append(carrier.headers, kafka.Header{
		Key:   key,
		Value: []byte(value),
	})
}

func (carrier *kafkaInjectCarrier) Keys() []string {
	return propagationKeys(carrier.headers, carrier.fields)
}

type kafkaExtractCarrier struct {
	headers []kafka.Header
	fields  map[string]struct{}
}

func (carrier kafkaExtractCarrier) Get(key string) string {
	key = strings.ToLower(key)
	if _, allowed := carrier.fields[key]; !allowed {
		return ""
	}
	for index := len(carrier.headers) - 1; index >= 0; index-- {
		header := carrier.headers[index]
		if strings.EqualFold(header.Key, key) {
			return string(header.Value)
		}
	}
	return ""
}

func (kafkaExtractCarrier) Set(string, string) {}

func (carrier kafkaExtractCarrier) Keys() []string {
	return propagationKeys(carrier.headers, carrier.fields)
}

func propagationKeys(
	headers []kafka.Header,
	fields map[string]struct{},
) []string {
	keys := make([]string, 0, len(fields))
	for _, header := range headers {
		key := strings.ToLower(header.Key)
		if _, allowed := fields[key]; !allowed {
			continue
		}
		duplicate := false
		for _, existing := range keys {
			if existing == key {
				duplicate = true
				break
			}
		}
		if !duplicate {
			keys = append(keys, key)
		}
	}
	return keys
}

func cloneKafkaMessage(
	message kafka.Message,
	propagationFields map[string]struct{},
) kafka.Message {
	headers := make([]kafka.Header, 0, len(message.Headers))
	for _, header := range message.Headers {
		if _, replaced := propagationFields[strings.ToLower(header.Key)]; replaced {
			continue
		}
		headers = append(headers, kafka.Header{
			Key:   header.Key,
			Value: append([]byte(nil), header.Value...),
		})
	}
	return kafka.Message{
		Topic:   message.Topic,
		Key:     append([]byte(nil), message.Key...),
		Value:   append([]byte(nil), message.Value...),
		Headers: headers,
	}
}

func cloneConsumedHeaders(message kafka.ConsumedMessage) kafka.ConsumedMessage {
	headers := make([]kafka.Header, len(message.Headers))
	for index, header := range message.Headers {
		headers[index] = kafka.Header{
			Key:   header.Key,
			Value: append([]byte(nil), header.Value...),
		}
	}
	message.Headers = headers
	return message
}

func removeKafkaHeaders(headers []kafka.Header, key string) []kafka.Header {
	filtered := headers[:0]
	for _, header := range headers {
		if !strings.EqualFold(header.Key, key) {
			filtered = append(filtered, header)
		}
	}
	return filtered
}

func validPropagationField(field string, maxBytes int) bool {
	if field == "" ||
		field != strings.ToLower(field) ||
		len(field) > maxBytes ||
		strings.HasPrefix(field, "es.") {
		return false
	}
	for _, character := range field {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' ||
			character == '_' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

func validKafkaMessage(
	message kafka.Message,
	limits kafka.MessageLimits,
) bool {
	return len(message.Topic) <= limits.MaxTopicBytes &&
		len(message.Key) <= limits.MaxKeyBytes &&
		len(message.Value) <= limits.MaxValueBytes &&
		validKafkaHeaders(message.Headers, limits)
}

func validKafkaHeaders(
	headers []kafka.Header,
	limits kafka.MessageLimits,
) bool {
	if len(headers) > limits.MaxHeaders {
		return false
	}
	total := 0
	for _, header := range headers {
		if header.Key == "" ||
			len(header.Key) > limits.MaxHeaderKeyBytes ||
			len(header.Value) > limits.MaxHeaderValueBytes ||
			len(header.Key) > limits.MaxHeaderBytes-total {
			return false
		}
		total += len(header.Key)
		if len(header.Value) > limits.MaxHeaderBytes-total {
			return false
		}
		total += len(header.Value)
	}
	return true
}

func validKafkaPropagationHeaders(
	headers []kafka.Header,
	fields map[string]struct{},
	limits kafka.MessageLimits,
) bool {
	if !validKafkaHeaders(headers, limits) {
		return false
	}
	seen := make(map[string]struct{}, len(fields))
	for _, header := range headers {
		key := strings.ToLower(header.Key)
		if _, propagationField := fields[key]; !propagationField {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func kafkaPropagationFailure(cause error) error {
	return &KafkaPropagationError{cause: cause}
}
