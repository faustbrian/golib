package gotelemetry

import (
	"context"
	"errors"
	"strings"

	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
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
	if instrumentation == nil {
		return nil, kafka.MessageLimits{}, ErrRuntimeRequired
	}
	if !instrumentation.valid() {
		return nil, kafka.MessageLimits{}, ErrRuntimeRequired
	}
	limits := config.Limits
	if limits == (kafka.MessageLimits{}) {
		limits = kafka.DefaultMessageLimits()
	}
	if err := limits.Validate(); err != nil {
		return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
	}
	propagationFields, fieldsAvailable := kafkaPropagationFields(
		instrumentation.propagator,
	)
	if !fieldsAvailable {
		return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
	}
	if len(propagationFields) > limits.MaxHeaders {
		return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
	}
	fields := make(map[string]struct{}, len(propagationFields))
	for _, field := range propagationFields {
		if !supportedPropagationField(field) ||
			len(field) > limits.MaxHeaderKeyBytes {
			return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
		}
		if _, exists := fields[field]; exists {
			return nil, kafka.MessageLimits{}, ErrInvalidKafkaPropagation
		}
		fields[field] = struct{}{}
	}

	return fields, limits, nil
}

func kafkaPropagationFields(
	propagator interface{ Fields() []string },
) (fields []string, available bool) {
	defer func() {
		if recover() != nil {
			fields = nil
			available = false
		}
	}()
	return propagator.Fields(), true
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
	owned := cloneKafkaMessage(message)
	carrier := &kafkaInjectCarrier{
		headers: owned.Headers,
		fields:  publisher.fields,
		limits:  publisher.limits,
	}
	injected := injectKafkaContext(
		publisher.instrumentation.propagator,
		ctx,
		carrier,
	)
	owned.Headers = carrier.headers
	if !injected || carrier.rejected || !validKafkaMessage(owned, publisher.limits) {
		owned = cloneKafkaMessage(message)
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
		ctx = extractKafkaContext(
			handler.instrumentation.propagator,
			ctx,
			kafkaExtractCarrier{headers: owned.Headers, fields: handler.fields},
		)
	}

	return handler.next.Handle(ctx, owned)
}

func injectKafkaContext(
	propagator interface {
		Inject(context.Context, propagation.TextMapCarrier)
	},
	ctx context.Context,
	carrier propagation.TextMapCarrier,
) (injected bool) {
	defer func() {
		if recover() != nil {
			injected = false
		}
	}()
	propagator.Inject(ctx, carrier)
	return true
}

func extractKafkaContext(
	propagator interface {
		Extract(context.Context, propagation.TextMapCarrier) context.Context
	},
	ctx context.Context,
	carrier propagation.TextMapCarrier,
) (extracted context.Context) {
	extracted = ctx
	defer func() {
		if recover() != nil {
			extracted = ctx
		}
	}()
	extracted = propagator.Extract(ctx, carrier)
	if extracted == nil {
		return ctx
	}
	spanContext := trace.SpanContextFromContext(extracted)
	if !spanContext.IsValid() {
		return ctx
	}
	return trace.ContextWithRemoteSpanContext(ctx, spanContext)
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
		if kafkaHeaderKeyMatches(header.Key, key) {
			return string(header.Value)
		}
	}
	return ""
}

func (kafkaExtractCarrier) Set(key, value string) {
	_, _ = key, value
}

func (carrier kafkaExtractCarrier) Keys() []string {
	return propagationKeys(carrier.headers, carrier.fields)
}

func propagationKeys(
	headers []kafka.Header,
	fields map[string]struct{},
) []string {
	keys := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, header := range headers {
		key, canonical := canonicalKafkaHeaderKey(header.Key)
		if !canonical {
			continue
		}
		if _, allowed := fields[key]; allowed {
			if _, duplicate := seen[key]; !duplicate {
				keys = append(keys, key)
				seen[key] = struct{}{}
			}
		}
	}
	return keys
}

func cloneKafkaMessage(message kafka.Message) kafka.Message {
	headers := make([]kafka.Header, 0, len(message.Headers))
	for _, header := range message.Headers {
		key, canonical := canonicalKafkaHeaderKey(header.Key)
		if canonical && supportedPropagationField(key) {
			continue
		}
		headers = append(headers, kafka.Header{
			Key:   header.Key,
			Value: append([]byte(nil), header.Value...),
		})
	}
	return kafka.Message{
		Topic:     message.Topic,
		Partition: message.Partition,
		Key:       append([]byte(nil), message.Key...),
		Value:     append([]byte(nil), message.Value...),
		Headers:   headers,
		Timestamp: message.Timestamp,
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
		if !kafkaHeaderKeyMatches(header.Key, key) {
			filtered = append(filtered, header)
		}
	}
	return filtered
}

func supportedPropagationField(field string) bool {
	return field == "traceparent" || field == "tracestate"
}

func canonicalKafkaHeaderKey(key string) (string, bool) {
	bytes := []byte(key)
	for index, character := range bytes {
		if character >= 0x80 {
			return "", false
		}
		if character >= 'A' && character <= 'Z' {
			bytes[index] = character + ('a' - 'A')
		}
	}
	return string(bytes), true
}

func kafkaHeaderKeyMatches(header, canonical string) bool {
	key, valid := canonicalKafkaHeaderKey(header)
	return valid && key == canonical
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
			len(header.Value) > limits.MaxHeaderValueBytes {
			return false
		}
		total += len(header.Key)
		if total > limits.MaxHeaderBytes {
			return false
		}
		total += len(header.Value)
		if total > limits.MaxHeaderBytes {
			return false
		}
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
		key, canonical := canonicalKafkaHeaderKey(header.Key)
		if !canonical {
			continue
		}
		if _, propagationField := fields[key]; propagationField {
			if _, duplicate := seen[key]; duplicate {
				return false
			}
			seen[key] = struct{}{}
		}
	}
	return true
}

func kafkaPropagationFailure(cause error) error {
	return &KafkaPropagationError{cause: cause}
}
