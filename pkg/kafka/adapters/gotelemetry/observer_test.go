package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestObserverEmitsKafkaProducerSemanticConventions(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spans),
	)
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: tracerProvider,
			meterProvider:  meterProvider,
		},
		Attributes: AttributePolicy{
			AllowedClientIDs: []string{"checkout-producer"},
			AllowedTopics:    []string{"orders"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	startedAt := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	observer := instrumentation.Observer()
	parentCtx, parent := tracerProvider.Tracer("test").Start(
		context.Background(),
		"parent",
	)
	defer parent.End()
	if err := observer(parentCtx, kafka.Observation{
		Kind:           kafka.ObservationProduceRecord,
		StartedAt:      startedAt,
		Duration:       25 * time.Millisecond,
		ClientID:       "checkout-producer",
		Topic:          "orders",
		Partition:      3,
		PartitionKnown: true,
		Offset:         42,
		OffsetKnown:    true,
		RecordCount:    1,
		RecordBytes:    128,
		Succeeded:      true,
	}); err != nil {
		t.Fatalf("Observer() error = %v", err)
	}

	ended := spans.Ended()
	if len(ended) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(ended))
	}
	span := ended[0]
	if span.Name() != "send orders" ||
		span.SpanKind() != trace.SpanKindProducer ||
		span.Parent().TraceID() != parent.SpanContext().TraceID() ||
		!span.StartTime().Equal(startedAt) ||
		!span.EndTime().Equal(startedAt.Add(25*time.Millisecond)) {
		t.Fatalf(
			"span = name %q kind %s start %s end %s",
			span.Name(),
			span.SpanKind(),
			span.StartTime(),
			span.EndTime(),
		)
	}
	assertSpanAttributes(t, span.Attributes(), map[string]any{
		"messaging.system":                   "kafka",
		"messaging.operation.name":           "send",
		"messaging.operation.type":           "send",
		"messaging.client.id":                "checkout-producer",
		"messaging.destination.name":         "orders",
		"messaging.destination.partition.id": "3",
		"messaging.kafka.offset":             int64(42),
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertFloatHistogram(
		t,
		metrics,
		"messaging.client.operation.duration",
		0.025,
		map[string]any{
			"messaging.system":           "kafka",
			"messaging.operation.name":   "send",
			"messaging.client.id":        "checkout-producer",
			"messaging.destination.name": "orders",
		},
	)
	assertIntCounter(
		t,
		metrics,
		"messaging.client.sent.messages",
		1,
		map[string]any{
			"messaging.system":           "kafka",
			"messaging.operation.name":   "send",
			"messaging.client.id":        "checkout-producer",
			"messaging.destination.name": "orders",
		},
	)
}

func TestObserverEmitsKafkaConsumerSemanticConventions(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spans),
	)
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: tracerProvider,
			meterProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
			),
		},
		Attributes: AttributePolicy{
			AllowedClientIDs:      []string{"orders-consumer"},
			AllowedTopics:         []string{"orders"},
			AllowedConsumerGroups: []string{"fulfillment"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	startedAt := time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC)
	observer := instrumentation.Observer()
	if err := observer(context.Background(), kafka.Observation{
		Kind:           kafka.ObservationConsumeBatch,
		StartedAt:      startedAt,
		Duration:       40 * time.Millisecond,
		ClientID:       "orders-consumer",
		GroupID:        "fulfillment",
		Topic:          "orders",
		Partition:      2,
		PartitionKnown: true,
		Offset:         44,
		OffsetKnown:    true,
		RecordCount:    3,
		ProcessedCount: 3,
		Succeeded:      true,
	}); err != nil {
		t.Fatalf("observe batch: %v", err)
	}
	if err := observer(context.Background(), kafka.Observation{
		Kind:           kafka.ObservationConsumePoll,
		StartedAt:      startedAt.Add(time.Second),
		Duration:       50 * time.Millisecond,
		ClientID:       "orders-consumer",
		GroupID:        "fulfillment",
		Topic:          "orders",
		RecordCount:    2,
		PartitionCount: 1,
		ProcessedCount: 1,
		CommittedCount: 1,
		Succeeded:      false,
		Truncated:      true,
		Category:       kafka.ErrorRetryable,
	}); err != nil {
		t.Fatalf("observe poll: %v", err)
	}

	ended := spans.Ended()
	if len(ended) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(ended))
	}
	if ended[0].Name() != "process orders" ||
		ended[0].SpanKind() != trace.SpanKindConsumer {
		t.Fatalf(
			"process span = %q/%s",
			ended[0].Name(),
			ended[0].SpanKind(),
		)
	}
	assertSpanAttributes(t, ended[0].Attributes(), map[string]any{
		"messaging.system":                   "kafka",
		"messaging.operation.name":           "process",
		"messaging.operation.type":           "process",
		"messaging.consumer.group.name":      "fulfillment",
		"messaging.batch.message_count":      int64(3),
		"messaging.destination.partition.id": "2",
		"messaging.kafka.offset":             int64(44),
	})
	if ended[1].Name() != "poll orders" ||
		ended[1].SpanKind() != trace.SpanKindClient ||
		ended[1].Status().Code != codes.Error {
		t.Fatalf(
			"poll span = %q/%s/%s",
			ended[1].Name(),
			ended[1].SpanKind(),
			ended[1].Status().Code,
		)
	}
	assertSpanAttributes(t, ended[1].Attributes(), map[string]any{
		"messaging.system":              "kafka",
		"messaging.operation.name":      "poll",
		"messaging.operation.type":      "receive",
		"messaging.batch.message_count": int64(2),
		"kafka.record.committed_count":  int64(1),
		"kafka.observation.truncated":   true,
		"error.type":                    "retryable",
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertFloatHistogram(
		t,
		metrics,
		"messaging.process.duration",
		0.04,
		map[string]any{
			"messaging.system":              "kafka",
			"messaging.operation.name":      "process",
			"messaging.consumer.group.name": "fulfillment",
		},
	)
	assertFloatHistogram(
		t,
		metrics,
		"messaging.client.operation.duration",
		0.05,
		map[string]any{
			"messaging.system":         "kafka",
			"messaging.operation.name": "poll",
			"error.type":               "retryable",
		},
	)
	assertIntCounter(
		t,
		metrics,
		"messaging.client.consumed.messages",
		2,
		map[string]any{
			"messaging.system":         "kafka",
			"messaging.operation.name": "poll",
			"error.type":               "retryable",
		},
	)
}

func TestObserverCoversEveryStableKafkaObservation(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
			),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	kinds := []kafka.ObservationKind{
		kafka.ObservationProduceRecord,
		kafka.ObservationProduceBatch,
		kafka.ObservationProduceAsync,
		kafka.ObservationConsumeRecord,
		kafka.ObservationConsumeBatch,
		kafka.ObservationConsumeCommit,
		kafka.ObservationConsumePoll,
		kafka.ObservationBrokerConnect,
		kafka.ObservationBrokerRequest,
		kafka.ObservationBrokerThrottle,
		kafka.ObservationBrokerDisconnect,
		kafka.ObservationConsumeAssigned,
		kafka.ObservationConsumeRevoked,
		kafka.ObservationConsumeLost,
		kafka.ObservationConsumeBlocked,
		kafka.ObservationConsumeGroupError,
		kafka.ObservationTransactionBegin,
		kafka.ObservationTransactionCommit,
		kafka.ObservationTransactionAbort,
		kafka.ObservationReplayPlan,
		kafka.ObservationReplayRecord,
		kafka.ObservationReplayRun,
		kafka.ObservationReplayShutdown,
		kafka.ObservationInspectorCluster,
		kafka.ObservationInspectorTopics,
		kafka.ObservationInspectorConsumerGroups,
		kafka.ObservationDependencyHealth,
		kafka.ObservationReadiness,
		kafka.ObservationInspectorShutdown,
	}
	observer := instrumentation.Observer()
	for index, kind := range kinds {
		recordCount := 0
		if kind >= kafka.ObservationProduceRecord &&
			kind <= kafka.ObservationConsumePoll {
			recordCount = 1
		}
		observation := kafka.Observation{
			Kind:        kind,
			StartedAt:   time.Unix(int64(index+1), 0),
			Duration:    time.Duration(index+1) * time.Millisecond,
			RecordCount: recordCount,
			Succeeded:   true,
		}
		switch kind {
		case kafka.ObservationReplayPlan,
			kafka.ObservationReplayRun:
			observation.PartitionCount = 1
		case kafka.ObservationReplayRecord:
			observation.RecordCount = 1
			observation.ProcessedCount = 1
			observation.ReplayProcessed = 1
		case kafka.ObservationInspectorCluster:
			observation.BrokerCount = 1
		case kafka.ObservationInspectorTopics:
			observation.TopicCount = 1
			observation.PartitionCount = 1
		case kafka.ObservationInspectorConsumerGroups:
			observation.GroupCount = 1
		case kafka.ObservationDependencyHealth:
			observation.DependencyHealthy = true
		case kafka.ObservationReadiness:
			observation.DependencyHealthy = true
			observation.Ready = true
			observation.ConsecutiveSuccesses = 1
		}
		if err := observer(context.Background(), observation); err != nil {
			t.Fatalf("observe %s: %v", kind, err)
		}
	}

	ended := spans.Ended()
	if len(ended) != len(kinds) {
		t.Fatalf("ended spans = %d, want %d", len(ended), len(kinds))
	}
	for index, span := range ended {
		if span.Name() == "" {
			t.Fatalf("span for %s has no name", kinds[index])
		}
		assertSpanAttributes(t, span.Attributes(), map[string]any{
			"kafka.operation": kinds[index].String(),
		})
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, kind := range kinds {
		assertIntCounter(
			t,
			metrics,
			"kafka.client.operations",
			1,
			map[string]any{"kafka.operation": kind.String()},
		)
	}
}

func TestObserverEmitsInspectorSpansAndBoundedDiagnostics(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
			),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	observations := []kafka.Observation{
		{
			Kind:        kafka.ObservationInspectorCluster,
			StartedAt:   time.Unix(1, 0),
			Duration:    time.Millisecond,
			BrokerCount: 3,
			Succeeded:   true,
		},
		{
			Kind:           kafka.ObservationInspectorTopics,
			StartedAt:      time.Unix(2, 0),
			Duration:       2 * time.Millisecond,
			TopicCount:     2,
			PartitionCount: 6,
			Succeeded:      true,
		},
		{
			Kind:             kafka.ObservationInspectorConsumerGroups,
			StartedAt:        time.Unix(3, 0),
			Duration:         3 * time.Millisecond,
			GroupCount:       2,
			GroupMemberCount: 4,
			PartitionCount:   8,
			Succeeded:        true,
		},
		{
			Kind:                 kafka.ObservationReadiness,
			StartedAt:            time.Unix(4, 0),
			Duration:             4 * time.Millisecond,
			DependencyHealthy:    false,
			Ready:                true,
			ConsecutiveFailures:  2,
			ConsecutiveSuccesses: 0,
			Succeeded:            false,
			Category:             kafka.ErrorRetryable,
		},
	}
	for _, observation := range observations {
		if err := observer(context.Background(), observation); err != nil {
			t.Fatalf("observe %s: %v", observation.Kind, err)
		}
	}

	ended := spans.Ended()
	wantNames := []string{
		"kafka inspector.cluster",
		"kafka inspector.topics",
		"kafka inspector.consumer_groups",
		"kafka inspector.readiness",
	}
	if len(ended) != len(wantNames) {
		t.Fatalf("ended spans = %d, want %d", len(ended), len(wantNames))
	}
	for index, span := range ended {
		if span.Name() != wantNames[index] ||
			span.SpanKind() != trace.SpanKindClient {
			t.Fatalf(
				"inspector span %d = %q/%s",
				index,
				span.Name(),
				span.SpanKind(),
			)
		}
	}
	assertSpanAttributes(t, ended[0].Attributes(), map[string]any{
		"kafka.broker.count": int64(3),
	})
	assertSpanAttributes(t, ended[1].Attributes(), map[string]any{
		"kafka.topic.count":     int64(2),
		"kafka.partition.count": int64(6),
	})
	assertSpanAttributes(t, ended[2].Attributes(), map[string]any{
		"kafka.consumer_group.count":        int64(2),
		"kafka.consumer_group.member.count": int64(4),
		"kafka.partition.count":             int64(8),
	})
	assertSpanAttributes(t, ended[3].Attributes(), map[string]any{
		"kafka.dependency.healthy":              false,
		"kafka.readiness.ready":                 true,
		"kafka.readiness.consecutive_failures":  int64(2),
		"kafka.readiness.consecutive_successes": int64(0),
		"error.type":                            "retryable",
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, observation := range observations {
		assertIntCounter(
			t,
			metrics,
			"kafka.client.operations",
			1,
			map[string]any{"kafka.operation": observation.Kind.String()},
		)
	}
	assertMetricAbsent(t, metrics, "messaging.client.operation.duration")
	assertMetricAbsent(t, metrics, "messaging.process.duration")
}

func TestObserverEmitsReplayMessagingAndExactProgress(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
			),
		},
		Attributes: AttributePolicy{AllowedTopics: []string{"events"}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	if err := observer(context.Background(), kafka.Observation{
		Kind:            kafka.ObservationReplayRecord,
		StartedAt:       time.Unix(1, 0),
		Duration:        time.Millisecond,
		Topic:           "events",
		Partition:       2,
		PartitionKnown:  true,
		Offset:          9,
		OffsetKnown:     true,
		RecordCount:     1,
		ProcessedCount:  1,
		ReplayProcessed: 1,
		Succeeded:       true,
	}); err != nil {
		t.Fatalf("observe replay record: %v", err)
	}
	if err := observer(context.Background(), kafka.Observation{
		Kind:            kafka.ObservationReplayRun,
		StartedAt:       time.Unix(2, 0),
		Duration:        2 * time.Millisecond,
		PartitionCount:  2,
		ReplayProcessed: 5,
		ReplaySkipped:   3,
		ReplayFailed:    1,
		ReplayRemaining: 8,
		Succeeded:       false,
		Category:        kafka.ErrorPermanent,
	}); err != nil {
		t.Fatalf("observe replay run: %v", err)
	}

	ended := spans.Ended()
	if len(ended) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(ended))
	}
	if ended[0].Name() != "process events" ||
		ended[0].SpanKind() != trace.SpanKindConsumer {
		t.Fatalf(
			"replay record span = %q/%s",
			ended[0].Name(),
			ended[0].SpanKind(),
		)
	}
	assertSpanAttributes(t, ended[0].Attributes(), map[string]any{
		"messaging.system":         "kafka",
		"messaging.operation.name": "process",
		"messaging.operation.type": "process",
		"kafka.replay.processed":   int64(1),
		"kafka.replay.skipped":     int64(0),
		"kafka.replay.failed":      int64(0),
		"kafka.replay.remaining":   int64(0),
	})
	assertSpanAttributes(t, ended[1].Attributes(), map[string]any{
		"kafka.operation":        "replay.run",
		"kafka.replay.processed": int64(5),
		"kafka.replay.skipped":   int64(3),
		"kafka.replay.failed":    int64(1),
		"kafka.replay.remaining": int64(8),
		"error.type":             "permanent",
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertFloatHistogram(
		t,
		metrics,
		"messaging.process.duration",
		0.001,
		map[string]any{
			"messaging.system":         "kafka",
			"messaging.operation.name": "process",
		},
	)
	assertMetricAbsent(t, metrics, "messaging.client.consumed.messages")
}

func TestObserverKeepsUnprocessedReplayOutcomesOutsideMessagingSemantics(
	t *testing.T,
) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
			),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	observations := []kafka.Observation{
		{
			Kind:          kafka.ObservationReplayRecord,
			StartedAt:     time.Unix(1, 0),
			Duration:      time.Millisecond,
			RecordCount:   1,
			ReplaySkipped: 1,
			Succeeded:     true,
		},
		{
			Kind:         kafka.ObservationReplayRecord,
			StartedAt:    time.Unix(2, 0),
			Duration:     time.Millisecond,
			RecordCount:  1,
			ReplayFailed: 1,
			Succeeded:    false,
			Category:     kafka.ErrorPermanent,
		},
	}
	for index, observation := range observations {
		if err := observer(context.Background(), observation); err != nil {
			t.Fatalf("observe replay outcome %d: %v", index, err)
		}
	}

	ended := spans.Ended()
	if len(ended) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(ended))
	}
	for index, span := range ended {
		if span.Name() != "kafka replay.record" ||
			span.SpanKind() != trace.SpanKindClient {
			t.Fatalf(
				"replay outcome %d span = %q/%s",
				index,
				span.Name(),
				span.SpanKind(),
			)
		}
		attributes := attributeMap(span.Attributes())
		if _, exists := attributes["messaging.system"]; exists {
			t.Fatalf(
				"replay outcome %d has messaging attributes: %#v",
				index,
				attributes,
			)
		}
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertMetricAbsent(t, metrics, "messaging.process.duration")
	assertMetricAbsent(t, metrics, "messaging.client.consumed.messages")
}

func TestObserverReportsBoundedBrokerDiagnosticsWithoutEndpoints(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(reader),
			),
		},
		Attributes: AttributePolicy{
			AllowedClientIDs: []string{"safe-client"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	observer := instrumentation.Observer()
	if err := observer(context.Background(), kafka.Observation{
		Kind:          kafka.ObservationBrokerRequest,
		StartedAt:     time.Unix(100, 0),
		Duration:      20 * time.Millisecond,
		ClientID:      "secret-client",
		BrokerID:      4,
		BrokerKnown:   true,
		APIKey:        1,
		APIKeyKnown:   true,
		RequestBytes:  1024,
		ResponseBytes: 256,
		QueueDuration: 12 * time.Millisecond,
		Succeeded:     false,
		Category:      kafka.ErrorAuthorization,
	}); err != nil {
		t.Fatalf("observe request: %v", err)
	}
	if err := observer(context.Background(), kafka.Observation{
		Kind:                   kafka.ObservationBrokerThrottle,
		StartedAt:              time.Unix(101, 0),
		Duration:               time.Millisecond,
		ClientID:               "secret-client",
		BrokerID:               4,
		BrokerKnown:            true,
		ThrottleDuration:       30 * time.Millisecond,
		ThrottledAfterResponse: true,
		Succeeded:              true,
	}); err != nil {
		t.Fatalf("observe throttle: %v", err)
	}

	ended := spans.Ended()
	if len(ended) != 2 {
		t.Fatalf("ended spans = %d, want 2", len(ended))
	}
	assertSpanAttributes(t, ended[0].Attributes(), map[string]any{
		"kafka.operation":              "broker.request",
		"kafka.broker.id":              int64(4),
		"kafka.protocol.api_key":       int64(1),
		"kafka.request.bytes":          int64(1024),
		"kafka.response.bytes":         int64(256),
		"kafka.request.queue.duration": 0.012,
		"error.type":                   "authorization",
	})
	assertSpanAttributes(t, ended[1].Attributes(), map[string]any{
		"kafka.operation":                "broker.throttle",
		"kafka.broker.id":                int64(4),
		"kafka.throttle.duration":        0.03,
		"kafka.throttled_after_response": true,
	})

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertIntHistogram(
		t,
		metrics,
		"kafka.client.request.size",
		1024,
		map[string]any{
			"kafka.request.direction": "request",
			"kafka.protocol.api_key":  int64(1),
			"error.type":              "authorization",
		},
	)
	assertIntHistogram(
		t,
		metrics,
		"kafka.client.request.size",
		256,
		map[string]any{
			"kafka.request.direction": "response",
			"kafka.protocol.api_key":  int64(1),
			"error.type":              "authorization",
		},
	)
	assertFloatHistogram(
		t,
		metrics,
		"kafka.client.request.queue.duration",
		0.012,
		map[string]any{"kafka.protocol.api_key": int64(1)},
	)
	assertFloatHistogram(
		t,
		metrics,
		"kafka.client.throttle.duration",
		0.03,
		map[string]any{"kafka.throttled_after_response": true},
	)
	if output := fmt.Sprint(ended, metrics); strings.Contains(
		output,
		"secret-client",
	) {
		t.Fatalf("telemetry disclosed disallowed identity: %s", output)
	}
}

func TestObserverExportsOnlyStableKafkaErrorCategories(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewSpanRecorder()
	instrumentation, err := New(Config{
		Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: metricnoop.NewMeterProvider(),
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	observer := instrumentation.Observer()
	for category := kafka.ErrorPermanent; category <= kafka.ErrorFatal; category++ {
		if err := observer(context.Background(), kafka.Observation{
			Kind:      kafka.ObservationBrokerDisconnect,
			StartedAt: time.Unix(int64(category), 0),
			Duration:  time.Millisecond,
			Succeeded: false,
			Category:  category,
		}); err != nil {
			t.Fatalf("observe %s: %v", category, err)
		}
	}

	ended := spans.Ended()
	if len(ended) != int(kafka.ErrorFatal) {
		t.Fatalf("ended spans = %d, want %d", len(ended), kafka.ErrorFatal)
	}
	for index, span := range ended {
		category := kafka.ErrorCategory(index + 1)
		if span.Status().Code != codes.Error ||
			span.Status().Description != "Kafka operation failed" {
			t.Fatalf("status for %s = %#v", category, span.Status())
		}
		assertSpanAttributes(t, span.Attributes(), map[string]any{
			"error.type": category.String(),
		})
	}
}

func TestConfigValidationRejectsTypedNilRuntime(t *testing.T) {
	t.Parallel()

	var runtime *testRuntime
	config := Config{Runtime: runtime}
	if err := config.Validate(); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("Validate() error = %v", err)
	}
	instrumentation, err := New(config)
	if instrumentation != nil || !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("New() = %#v, %v", instrumentation, err)
	}
}

type testRuntime struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

func (runtime testRuntime) TracerProvider() trace.TracerProvider {
	return runtime.tracerProvider
}

func (runtime testRuntime) MeterProvider() metric.MeterProvider {
	return runtime.meterProvider
}

func assertSpanAttributes(
	t *testing.T,
	got []attribute.KeyValue,
	want map[string]any,
) {
	t.Helper()

	attributes := attributeMap(got)
	for key, value := range want {
		if attributes[key] != value {
			t.Fatalf("span attribute %q = %#v, want %#v", key, attributes[key], value)
		}
	}
}

func assertFloatHistogram(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	name string,
	want float64,
	wantAttributes map[string]any,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, current := range scope.Metrics {
			if current.Name != name {
				continue
			}
			histogram, ok := current.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data = %#v", name, current.Data)
			}
			for _, point := range histogram.DataPoints {
				if attributesContain(point.Attributes, wantAttributes) {
					if point.Count != 1 || point.Sum != want {
						t.Fatalf("%s point = %#v", name, point)
					}

					return
				}
			}
			t.Fatalf("%s has no point with attributes %#v", name, wantAttributes)
		}
	}
	t.Fatalf("metric %q not found", name)
}

func assertIntCounter(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	name string,
	want int64,
	wantAttributes map[string]any,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, current := range scope.Metrics {
			if current.Name != name {
				continue
			}
			sum, ok := current.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %#v", name, current.Data)
			}
			for _, point := range sum.DataPoints {
				if attributesContain(point.Attributes, wantAttributes) {
					if point.Value != want {
						t.Fatalf("%s point = %#v", name, point)
					}

					return
				}
			}
			t.Fatalf("%s has no point with attributes %#v", name, wantAttributes)
		}
	}
	t.Fatalf("metric %q not found", name)
}

func assertIntHistogram(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	name string,
	want int64,
	wantAttributes map[string]any,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, current := range scope.Metrics {
			if current.Name != name {
				continue
			}
			histogram, ok := current.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("%s data = %#v", name, current.Data)
			}
			for _, point := range histogram.DataPoints {
				if attributesContain(point.Attributes, wantAttributes) {
					if point.Count != 1 || point.Sum != want {
						t.Fatalf("%s point = %#v", name, point)
					}

					return
				}
			}
			t.Fatalf("%s has no point with attributes %#v", name, wantAttributes)
		}
	}
	t.Fatalf("metric %q not found", name)
}

func assertMetricAbsent(
	t *testing.T,
	metrics metricdata.ResourceMetrics,
	name string,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, current := range scope.Metrics {
			if current.Name == name {
				t.Fatalf("unexpected metric %q: %#v", name, current.Data)
			}
		}
	}
}

func attributesContain(got attribute.Set, want map[string]any) bool {
	attributes := attributeMap(got.ToSlice())
	for key, value := range want {
		if attributes[key] != value {
			return false
		}
	}

	return true
}

func attributeMap(values []attribute.KeyValue) map[string]any {
	result := make(map[string]any, len(values))
	for _, value := range values {
		result[string(value.Key)] = value.Value.AsInterface()
	}

	return result
}
