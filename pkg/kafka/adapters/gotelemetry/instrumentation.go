// Package gotelemetry translates bounded kafka observations into
// OpenTelemetry spans and metrics without recording record data or arbitrary
// application errors.
package gotelemetry

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	// MessagingSemanticConventionVersion is the exact development-status
	// OpenTelemetry messaging convention emitted by this adapter.
	MessagingSemanticConventionVersion = "1.43.0"

	instrumentationName = "github.com/faustbrian/golib/pkg/kafka/adapters/gotelemetry"
	maxAllowedValues    = 128
	maxTopicLength      = 249
	maxIdentityLength   = 255
)

var (
	// ErrRuntimeRequired reports a missing or incomplete telemetry runtime.
	ErrRuntimeRequired = errors.New("kafka/gotelemetry: runtime is required")
	// ErrInvalidAttributePolicy reports an unbounded, duplicated, or invalid
	// telemetry identity allowlist.
	ErrInvalidAttributePolicy = errors.New(
		"kafka/gotelemetry: attribute policy is invalid",
	)
	// ErrContextRequired reports a nil observer context.
	ErrContextRequired = errors.New("kafka/gotelemetry: context is required")
	// ErrInvalidObservation reports metadata outside the root observation
	// contract.
	ErrInvalidObservation = errors.New(
		"kafka/gotelemetry: observation is invalid",
	)
	// ErrInstrumentCreation categorizes OpenTelemetry instrument construction
	// failures without rendering provider diagnostics.
	ErrInstrumentCreation = errors.New(
		"kafka/gotelemetry: instrument creation failed",
	)
)

// Runtime is the standard-provider surface needed by this adapter. Keeping the
// interface here prevents OpenTelemetry from entering the root Kafka module.
type Runtime interface {
	TracerProvider() trace.TracerProvider
	MeterProvider() metric.MeterProvider
}

// AttributePolicy explicitly bounds Kafka identities admitted to telemetry.
// Empty allowlists omit the corresponding identity. Values are matched exactly
// and copied during construction.
type AttributePolicy struct {
	AllowedClientIDs      []string
	AllowedTopics         []string
	AllowedConsumerGroups []string
}

// Validate reports whether every identity allowlist is bounded, unique, and
// safe to copy into telemetry.
func (policy AttributePolicy) Validate() error {
	_, err := normalizeAttributePolicy(policy)

	return err
}

// Config owns immutable adapter dependencies and cardinality policy.
type Config struct {
	Runtime    Runtime
	Attributes AttributePolicy
}

// Validate checks dependencies and cardinality policy without creating
// instruments.
func (config Config) Validate() error {
	_, _, _, err := validateConfig(config)

	return err
}

// InstrumentError preserves a provider failure for intentional local
// classification while returning a stable redacted diagnostic.
type InstrumentError struct {
	cause error
}

// Error implements error without exposing provider diagnostics.
func (*InstrumentError) Error() string {
	return ErrInstrumentCreation.Error()
}

// Unwrap preserves both the stable category and provider cause.
func (err *InstrumentError) Unwrap() []error {
	return []error{ErrInstrumentCreation, err.cause}
}

// Instrumentation owns immutable tracing and metric instruments. It starts no
// goroutines and is safe for concurrent observer calls.
type Instrumentation struct {
	tracer            trace.Tracer
	clientDuration    metric.Float64Histogram
	processDuration   metric.Float64Histogram
	sentMessages      metric.Int64Counter
	consumedMessages  metric.Int64Counter
	operations        metric.Int64Counter
	operationDuration metric.Float64Histogram
	requestSize       metric.Int64Histogram
	requestQueue      metric.Float64Histogram
	throttleDuration  metric.Float64Histogram
	attributes        normalizedAttributePolicy
}

type normalizedAttributePolicy struct {
	clientIDs      map[string]struct{}
	topics         map[string]struct{}
	consumerGroups map[string]struct{}
}

// New validates and copies configuration before constructing telemetry
// instruments.
func New(config Config) (*Instrumentation, error) {
	tracerProvider, meterProvider, attributes, err := validateConfig(config)
	if err != nil {
		return nil, err
	}

	meter := meterProvider.Meter(instrumentationName)
	clientDuration, err := meter.Float64Histogram(
		"messaging.client.operation.duration",
		metric.WithDescription(
			"Duration of messaging operation initiated by a producer or consumer client.",
		),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.005,
			0.01,
			0.025,
			0.05,
			0.075,
			0.1,
			0.25,
			0.5,
			0.75,
			1,
			2.5,
			5,
			7.5,
			10,
		),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	processDuration, err := meter.Float64Histogram(
		"messaging.process.duration",
		metric.WithDescription(
			"Duration of processing operation performed by a consumer.",
		),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.005,
			0.01,
			0.025,
			0.05,
			0.075,
			0.1,
			0.25,
			0.5,
			0.75,
			1,
			2.5,
			5,
			7.5,
			10,
		),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	sentMessages, err := meter.Int64Counter(
		"messaging.client.sent.messages",
		metric.WithDescription(
			"Number of messages producer attempted to send to the broker.",
		),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	consumedMessages, err := meter.Int64Counter(
		"messaging.client.consumed.messages",
		metric.WithDescription(
			"Number of messages delivered to the application.",
		),
		metric.WithUnit("{message}"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	operations, err := meter.Int64Counter(
		"kafka.client.operations",
		metric.WithDescription(
			"Completed Kafka policy operations by bounded operation and outcome.",
		),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	operationDuration, err := meter.Float64Histogram(
		"kafka.client.operation.duration",
		metric.WithDescription(
			"Duration of completed Kafka policy operations.",
		),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.001,
			0.005,
			0.01,
			0.025,
			0.05,
			0.1,
			0.25,
			0.5,
			1,
			2.5,
			5,
			10,
			30,
		),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	requestSize, err := meter.Int64Histogram(
		"kafka.client.request.size",
		metric.WithDescription(
			"Kafka protocol request or response size below TLS framing.",
		),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(
			1024,
			4096,
			16384,
			65536,
			262144,
			1048576,
			4194304,
			16777216,
			67108864,
		),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	requestQueue, err := meter.Float64Histogram(
		"kafka.client.request.queue.duration",
		metric.WithDescription(
			"Time a Kafka request waited in the client before network write.",
		),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.001,
			0.005,
			0.01,
			0.025,
			0.05,
			0.1,
			0.25,
			0.5,
			1,
			2.5,
			5,
		),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}
	throttleDuration, err := meter.Float64Histogram(
		"kafka.client.throttle.duration",
		metric.WithDescription("Kafka broker-imposed throttle duration."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.001,
			0.005,
			0.01,
			0.025,
			0.05,
			0.1,
			0.25,
			0.5,
			1,
			2.5,
			5,
			10,
		),
	)
	if err != nil {
		return nil, instrumentFailure(err)
	}

	return &Instrumentation{
		tracer:            tracerProvider.Tracer(instrumentationName),
		clientDuration:    clientDuration,
		processDuration:   processDuration,
		sentMessages:      sentMessages,
		consumedMessages:  consumedMessages,
		operations:        operations,
		operationDuration: operationDuration,
		requestSize:       requestSize,
		requestQueue:      requestQueue,
		throttleDuration:  throttleDuration,
		attributes:        attributes,
	}, nil
}

func validateConfig(
	config Config,
) (
	trace.TracerProvider,
	metric.MeterProvider,
	normalizedAttributePolicy,
	error,
) {
	if nilInterface(config.Runtime) {
		return nil, nil, normalizedAttributePolicy{}, ErrRuntimeRequired
	}
	tracerProvider := config.Runtime.TracerProvider()
	meterProvider := config.Runtime.MeterProvider()
	if nilInterface(tracerProvider) || nilInterface(meterProvider) {
		return nil, nil, normalizedAttributePolicy{}, ErrRuntimeRequired
	}
	attributes, err := normalizeAttributePolicy(config.Attributes)
	if err != nil {
		return nil, nil, normalizedAttributePolicy{}, err
	}

	return tracerProvider, meterProvider, attributes, nil
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

// Observer returns the synchronous Kafka observer owned by this
// instrumentation. The returned function does not retain callback contexts or
// observations.
func (instrumentation *Instrumentation) Observer() kafka.ObserverFunc {
	return instrumentation.observe
}

func (instrumentation *Instrumentation) observe(
	ctx context.Context,
	observation kafka.Observation,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if instrumentation == nil ||
		instrumentation.tracer == nil ||
		instrumentation.clientDuration == nil ||
		instrumentation.processDuration == nil ||
		instrumentation.sentMessages == nil ||
		instrumentation.consumedMessages == nil ||
		instrumentation.operations == nil ||
		instrumentation.operationDuration == nil ||
		instrumentation.requestSize == nil ||
		instrumentation.requestQueue == nil ||
		instrumentation.throttleDuration == nil {
		return ErrRuntimeRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := observation.Validate(); err != nil {
		return ErrInvalidObservation
	}

	operation := messagingOperation(observation)
	metricAttributes := instrumentation.messagingAttributes(
		observation,
		operation.name,
		operation.operationType,
	)
	policyAttributes := instrumentation.policyAttributes(observation)
	spanAttributes := make(
		[]attribute.KeyValue,
		0,
		len(metricAttributes)+len(policyAttributes)+8,
	)
	spanAttributes = append(spanAttributes, metricAttributes...)
	spanAttributes = append(spanAttributes, policyAttributes...)
	if observation.PartitionKnown {
		spanAttributes = append(
			spanAttributes,
			attribute.String(
				"messaging.destination.partition.id",
				strconv.FormatInt(int64(observation.Partition), 10),
			),
		)
	}
	if observation.OffsetKnown {
		spanAttributes = append(
			spanAttributes,
			attribute.Int64("messaging.kafka.offset", observation.Offset),
		)
	}
	if operation.batch {
		spanAttributes = append(
			spanAttributes,
			attribute.Int64(
				"messaging.batch.message_count",
				int64(observation.RecordCount),
			),
		)
	}
	spanAttributes = appendObservationDiagnostics(spanAttributes, observation)

	_, span := instrumentation.tracer.Start(
		ctx,
		instrumentation.spanName(operation, observation.Topic),
		trace.WithSpanKind(operation.spanKind),
		trace.WithTimestamp(observation.StartedAt),
		trace.WithAttributes(spanAttributes...),
	)
	if !observation.Succeeded {
		span.SetStatus(codes.Error, "Kafka operation failed")
	}
	if operation.clientDuration {
		instrumentation.clientDuration.Record(
			ctx,
			observation.Duration.Seconds(),
			metric.WithAttributes(metricAttributes...),
		)
	}
	if operation.processDuration {
		instrumentation.processDuration.Record(
			ctx,
			observation.Duration.Seconds(),
			metric.WithAttributes(metricAttributes...),
		)
	}
	if operation.sentMessages {
		instrumentation.sentMessages.Add(
			ctx,
			int64(observation.RecordCount),
			metric.WithAttributes(metricAttributes...),
		)
	}
	if operation.consumedMessages {
		instrumentation.consumedMessages.Add(
			ctx,
			int64(observation.RecordCount),
			metric.WithAttributes(metricAttributes...),
		)
	}
	instrumentation.operations.Add(
		ctx,
		1,
		metric.WithAttributes(policyAttributes...),
	)
	instrumentation.operationDuration.Record(
		ctx,
		observation.Duration.Seconds(),
		metric.WithAttributes(policyAttributes...),
	)
	instrumentation.recordBrokerMetrics(ctx, observation, policyAttributes)
	span.End(trace.WithTimestamp(observation.StartedAt.Add(observation.Duration)))

	return nil
}

func (instrumentation *Instrumentation) messagingAttributes(
	observation kafka.Observation,
	operationName string,
	operationType string,
) []attribute.KeyValue {
	if operationName == "" {
		return nil
	}
	attributes := []attribute.KeyValue{
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.operation.name", operationName),
		attribute.String("messaging.operation.type", operationType),
	}
	if instrumentation.attributes.allowsClientID(observation.ClientID) {
		attributes = append(
			attributes,
			attribute.String("messaging.client.id", observation.ClientID),
		)
	}
	if instrumentation.attributes.allowsTopic(observation.Topic) {
		attributes = append(
			attributes,
			attribute.String("messaging.destination.name", observation.Topic),
		)
	}
	if instrumentation.attributes.allowsConsumerGroup(observation.GroupID) {
		attributes = append(
			attributes,
			attribute.String(
				"messaging.consumer.group.name",
				observation.GroupID,
			),
		)
	}
	if !observation.Succeeded {
		attributes = append(
			attributes,
			attribute.String("error.type", observation.Category.String()),
		)
	}

	return attributes
}

func (instrumentation *Instrumentation) policyAttributes(
	observation kafka.Observation,
) []attribute.KeyValue {
	outcome := "success"
	if !observation.Succeeded {
		outcome = "error"
	}
	attributes := []attribute.KeyValue{
		attribute.String("kafka.operation", observation.Kind.String()),
		attribute.String("kafka.outcome", outcome),
	}
	if instrumentation.attributes.allowsClientID(observation.ClientID) {
		attributes = append(
			attributes,
			attribute.String("kafka.client.id", observation.ClientID),
		)
	}
	if instrumentation.attributes.allowsTopic(observation.Topic) {
		attributes = append(
			attributes,
			attribute.String("kafka.topic", observation.Topic),
		)
	}
	if instrumentation.attributes.allowsConsumerGroup(observation.GroupID) {
		attributes = append(
			attributes,
			attribute.String("kafka.consumer.group", observation.GroupID),
		)
	}
	if !observation.Succeeded {
		attributes = append(
			attributes,
			attribute.String("error.type", observation.Category.String()),
		)
	}

	return attributes
}

func appendObservationDiagnostics(
	attributes []attribute.KeyValue,
	observation kafka.Observation,
) []attribute.KeyValue {
	if observation.BrokerKnown {
		attributes = append(
			attributes,
			attribute.Int64("kafka.broker.id", int64(observation.BrokerID)),
		)
	}
	if observation.APIKeyKnown {
		attributes = append(
			attributes,
			attribute.Int64(
				"kafka.protocol.api_key",
				int64(observation.APIKey),
			),
		)
	}
	if observation.Kind == kafka.ObservationBrokerRequest {
		attributes = append(
			attributes,
			attribute.Int64("kafka.request.bytes", observation.RequestBytes),
			attribute.Int64("kafka.response.bytes", observation.ResponseBytes),
			attribute.Float64(
				"kafka.request.queue.duration",
				observation.QueueDuration.Seconds(),
			),
		)
	}
	if observation.Kind == kafka.ObservationBrokerThrottle {
		attributes = append(
			attributes,
			attribute.Float64(
				"kafka.throttle.duration",
				observation.ThrottleDuration.Seconds(),
			),
			attribute.Bool(
				"kafka.throttled_after_response",
				observation.ThrottledAfterResponse,
			),
		)
	}
	if observation.RecordCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64("kafka.record.count", int64(observation.RecordCount)),
		)
	}
	if observation.PartitionCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64(
				"kafka.partition.count",
				int64(observation.PartitionCount),
			),
		)
	}
	if observation.BrokerCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64("kafka.broker.count", int64(observation.BrokerCount)),
		)
	}
	if observation.TopicCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64("kafka.topic.count", int64(observation.TopicCount)),
		)
	}
	if observation.GroupCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64(
				"kafka.consumer_group.count",
				int64(observation.GroupCount),
			),
		)
	}
	if observation.GroupMemberCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64(
				"kafka.consumer_group.member.count",
				int64(observation.GroupMemberCount),
			),
		)
	}
	if observation.ProcessedCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64(
				"kafka.record.processed_count",
				int64(observation.ProcessedCount),
			),
		)
	}
	if observation.CommittedCount > 0 {
		attributes = append(
			attributes,
			attribute.Int64(
				"kafka.record.committed_count",
				int64(observation.CommittedCount),
			),
		)
	}
	if observation.RecordBytes > 0 {
		attributes = append(
			attributes,
			attribute.Int64("kafka.record.size", observation.RecordBytes),
		)
	}
	switch observation.Kind {
	case kafka.ObservationReplayPlan,
		kafka.ObservationReplayRecord,
		kafka.ObservationReplayRun:
		attributes = append(
			attributes,
			attribute.Int64(
				"kafka.replay.processed",
				observation.ReplayProcessed,
			),
			attribute.Int64("kafka.replay.skipped", observation.ReplaySkipped),
			attribute.Int64("kafka.replay.failed", observation.ReplayFailed),
			attribute.Int64(
				"kafka.replay.remaining",
				observation.ReplayRemaining,
			),
		)
	case kafka.ObservationDependencyHealth:
		attributes = append(
			attributes,
			attribute.Bool(
				"kafka.dependency.healthy",
				observation.DependencyHealthy,
			),
		)
	case kafka.ObservationReadiness:
		attributes = append(
			attributes,
			attribute.Bool(
				"kafka.dependency.healthy",
				observation.DependencyHealthy,
			),
			attribute.Bool("kafka.readiness.ready", observation.Ready),
			attribute.Int64(
				"kafka.readiness.consecutive_failures",
				int64(observation.ConsecutiveFailures),
			),
			attribute.Int64(
				"kafka.readiness.consecutive_successes",
				int64(observation.ConsecutiveSuccesses),
			),
		)
	}
	if observation.Truncated {
		attributes = append(
			attributes,
			attribute.Bool("kafka.observation.truncated", true),
		)
	}

	return attributes
}

func (instrumentation *Instrumentation) recordBrokerMetrics(
	ctx context.Context,
	observation kafka.Observation,
	baseAttributes []attribute.KeyValue,
) {
	switch observation.Kind {
	case kafka.ObservationBrokerRequest:
		attributes := append(
			[]attribute.KeyValue(nil),
			baseAttributes...,
		)
		if observation.APIKeyKnown {
			attributes = append(
				attributes,
				attribute.Int64(
					"kafka.protocol.api_key",
					int64(observation.APIKey),
				),
			)
		}
		requestAttributes := append(
			append([]attribute.KeyValue(nil), attributes...),
			attribute.String("kafka.request.direction", "request"),
		)
		responseAttributes := append(
			append([]attribute.KeyValue(nil), attributes...),
			attribute.String("kafka.request.direction", "response"),
		)
		instrumentation.requestSize.Record(
			ctx,
			observation.RequestBytes,
			metric.WithAttributes(requestAttributes...),
		)
		instrumentation.requestSize.Record(
			ctx,
			observation.ResponseBytes,
			metric.WithAttributes(responseAttributes...),
		)
		instrumentation.requestQueue.Record(
			ctx,
			observation.QueueDuration.Seconds(),
			metric.WithAttributes(attributes...),
		)
	case kafka.ObservationBrokerThrottle:
		attributes := append(
			append([]attribute.KeyValue(nil), baseAttributes...),
			attribute.Bool(
				"kafka.throttled_after_response",
				observation.ThrottledAfterResponse,
			),
		)
		instrumentation.throttleDuration.Record(
			ctx,
			observation.ThrottleDuration.Seconds(),
			metric.WithAttributes(attributes...),
		)
	}
}

func (instrumentation *Instrumentation) spanName(
	operation operationDescriptor,
	topic string,
) string {
	if operation.messaging && instrumentation.attributes.allowsTopic(topic) {
		return operation.spanName + " " + topic
	}

	return operation.spanName
}

func (policy normalizedAttributePolicy) allowsClientID(value string) bool {
	_, ok := policy.clientIDs[value]

	return ok
}

func (policy normalizedAttributePolicy) allowsTopic(value string) bool {
	_, ok := policy.topics[value]

	return ok
}

func (policy normalizedAttributePolicy) allowsConsumerGroup(value string) bool {
	_, ok := policy.consumerGroups[value]

	return ok
}

func normalizeAttributePolicy(
	policy AttributePolicy,
) (normalizedAttributePolicy, error) {
	clientIDs, err := normalizeAllowlist(
		policy.AllowedClientIDs,
		maxIdentityLength,
		validIdentity,
	)
	if err != nil {
		return normalizedAttributePolicy{}, err
	}
	topics, err := normalizeAllowlist(
		policy.AllowedTopics,
		maxTopicLength,
		validTopic,
	)
	if err != nil {
		return normalizedAttributePolicy{}, err
	}
	consumerGroups, err := normalizeAllowlist(
		policy.AllowedConsumerGroups,
		maxIdentityLength,
		validIdentity,
	)
	if err != nil {
		return normalizedAttributePolicy{}, err
	}

	return normalizedAttributePolicy{
		clientIDs:      clientIDs,
		topics:         topics,
		consumerGroups: consumerGroups,
	}, nil
}

func normalizeAllowlist(
	values []string,
	maxLength int,
	validate func(string, int) bool,
) (map[string]struct{}, error) {
	if len(values) > maxAllowedValues {
		return nil, ErrInvalidAttributePolicy
	}
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validate(value, maxLength) {
			return nil, ErrInvalidAttributePolicy
		}
		if _, duplicate := result[value]; duplicate {
			return nil, ErrInvalidAttributePolicy
		}
		result[value] = struct{}{}
	}

	return result, nil
}

func validIdentity(value string, maxLength int) bool {
	if len(value) == 0 ||
		len(value) > maxLength ||
		!utf8.ValidString(value) ||
		strings.TrimSpace(value) != value {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) {
			return false
		}
	}

	return true
}

func validTopic(value string, maxLength int) bool {
	if value == "." ||
		value == ".." ||
		!validIdentity(value, maxLength) {
		return false
	}
	for _, current := range value {
		if (current >= 'a' && current <= 'z') ||
			(current >= 'A' && current <= 'Z') ||
			(current >= '0' && current <= '9') ||
			current == '.' ||
			current == '_' ||
			current == '-' {
			continue
		}

		return false
	}

	return true
}

type operationDescriptor struct {
	spanName         string
	name             string
	operationType    string
	spanKind         trace.SpanKind
	messaging        bool
	clientDuration   bool
	processDuration  bool
	sentMessages     bool
	consumedMessages bool
	batch            bool
}

func messagingOperation(observation kafka.Observation) operationDescriptor {
	switch observation.Kind {
	case kafka.ObservationProduceRecord, kafka.ObservationProduceAsync:
		return operationDescriptor{
			spanName:       "send",
			name:           "send",
			operationType:  "send",
			spanKind:       trace.SpanKindProducer,
			messaging:      true,
			clientDuration: true,
			sentMessages:   true,
		}
	case kafka.ObservationProduceBatch:
		return operationDescriptor{
			spanName:       "send",
			name:           "send",
			operationType:  "send",
			spanKind:       trace.SpanKindProducer,
			messaging:      true,
			clientDuration: true,
			sentMessages:   true,
			batch:          true,
		}
	case kafka.ObservationConsumeRecord:
		return operationDescriptor{
			spanName:        "process",
			name:            "process",
			operationType:   "process",
			spanKind:        trace.SpanKindConsumer,
			messaging:       true,
			processDuration: true,
		}
	case kafka.ObservationConsumeBatch:
		return operationDescriptor{
			spanName:        "process",
			name:            "process",
			operationType:   "process",
			spanKind:        trace.SpanKindConsumer,
			messaging:       true,
			processDuration: true,
			batch:           true,
		}
	case kafka.ObservationConsumeCommit:
		return operationDescriptor{
			spanName:       "commit",
			name:           "commit",
			operationType:  "settle",
			spanKind:       trace.SpanKindClient,
			messaging:      true,
			clientDuration: true,
			batch:          true,
		}
	case kafka.ObservationConsumePoll:
		return operationDescriptor{
			spanName:         "poll",
			name:             "poll",
			operationType:    "receive",
			spanKind:         trace.SpanKindClient,
			messaging:        true,
			clientDuration:   true,
			consumedMessages: true,
			batch:            true,
		}
	case kafka.ObservationBrokerConnect:
		return operationDescriptor{
			spanName: "kafka broker.connect",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationBrokerRequest:
		return operationDescriptor{
			spanName: "kafka broker.request",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationBrokerThrottle:
		return operationDescriptor{
			spanName: "kafka broker.throttle",
			spanKind: trace.SpanKindInternal,
		}
	case kafka.ObservationBrokerDisconnect:
		return operationDescriptor{
			spanName: "kafka broker.disconnect",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationConsumeAssigned:
		return operationDescriptor{
			spanName: "kafka consumer.assigned",
			spanKind: trace.SpanKindInternal,
		}
	case kafka.ObservationConsumeRevoked:
		return operationDescriptor{
			spanName: "kafka consumer.revoked",
			spanKind: trace.SpanKindInternal,
		}
	case kafka.ObservationConsumeLost:
		return operationDescriptor{
			spanName: "kafka consumer.lost",
			spanKind: trace.SpanKindInternal,
		}
	case kafka.ObservationConsumeBlocked:
		return operationDescriptor{
			spanName: "kafka consumer.rebalance_blocked",
			spanKind: trace.SpanKindInternal,
		}
	case kafka.ObservationConsumeGroupError:
		return operationDescriptor{
			spanName: "kafka consumer.group_error",
			spanKind: trace.SpanKindInternal,
		}
	case kafka.ObservationTransactionBegin:
		return operationDescriptor{
			spanName: "kafka transaction.begin",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationTransactionCommit:
		return operationDescriptor{
			spanName: "kafka transaction.commit",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationTransactionAbort:
		return operationDescriptor{
			spanName: "kafka transaction.abort",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationReplayPlan:
		return operationDescriptor{
			spanName: "kafka replay.plan",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationReplayRecord:
		if observation.ReplayProcessed == 0 {
			return operationDescriptor{
				spanName: "kafka replay.record",
				spanKind: trace.SpanKindClient,
			}
		}

		return operationDescriptor{
			spanName:        "process",
			name:            "process",
			operationType:   "process",
			spanKind:        trace.SpanKindConsumer,
			messaging:       true,
			processDuration: true,
		}
	case kafka.ObservationReplayRun:
		return operationDescriptor{
			spanName: "kafka replay.run",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationReplayShutdown:
		return operationDescriptor{
			spanName: "kafka replay.shutdown",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationInspectorCluster:
		return operationDescriptor{
			spanName: "kafka inspector.cluster",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationInspectorTopics:
		return operationDescriptor{
			spanName: "kafka inspector.topics",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationInspectorConsumerGroups:
		return operationDescriptor{
			spanName: "kafka inspector.consumer_groups",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationDependencyHealth:
		return operationDescriptor{
			spanName: "kafka inspector.dependency_health",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationReadiness:
		return operationDescriptor{
			spanName: "kafka inspector.readiness",
			spanKind: trace.SpanKindClient,
		}
	case kafka.ObservationInspectorShutdown:
		return operationDescriptor{
			spanName: "kafka inspector.shutdown",
			spanKind: trace.SpanKindClient,
		}
	default:
		return operationDescriptor{}
	}
}

func instrumentFailure(cause error) error {
	return &InstrumentError{cause: cause}
}
