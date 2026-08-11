package gotelemetry

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestEveryObservationHasAnExactPublicSpanAndDurationContract(t *testing.T) {
	t.Parallel()

	type contract struct {
		kind      kafka.ObservationKind
		name      string
		spanKind  trace.SpanKind
		messaging bool
	}
	contracts := []contract{
		{kafka.ObservationProduceRecord, "kafka producer.publish_completion", trace.SpanKindInternal, false},
		{kafka.ObservationProduceBatch, "kafka producer.publish_batch_completion", trace.SpanKindInternal, false},
		{kafka.ObservationProduceAsync, "kafka producer.publish_async_completion", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeRecord, "kafka consumer.record_completion", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeBatch, "kafka consumer.batch_completion", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeCommit, "commit", trace.SpanKindClient, true},
		{kafka.ObservationConsumePoll, "kafka consumer.poll_cycle", trace.SpanKindInternal, false},
		{kafka.ObservationBrokerConnect, "kafka broker.connect", trace.SpanKindClient, false},
		{kafka.ObservationBrokerRequest, "kafka broker.request", trace.SpanKindClient, false},
		{kafka.ObservationBrokerThrottle, "kafka broker.throttle", trace.SpanKindInternal, false},
		{kafka.ObservationBrokerDisconnect, "kafka broker.disconnect", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeAssigned, "kafka consumer.assigned", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeRevoked, "kafka consumer.revoked", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeLost, "kafka consumer.lost", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeBlocked, "kafka consumer.rebalance_blocked", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeGroupError, "kafka consumer.group_error", trace.SpanKindInternal, false},
		{kafka.ObservationTransactionBegin, "kafka transaction.begin", trace.SpanKindInternal, false},
		{kafka.ObservationTransactionCommit, "kafka transaction.commit", trace.SpanKindClient, false},
		{kafka.ObservationTransactionAbort, "kafka transaction.abort", trace.SpanKindClient, false},
		{kafka.ObservationReplayPlan, "kafka replay.plan", trace.SpanKindClient, false},
		{kafka.ObservationReplayRecord, "process", trace.SpanKindConsumer, true},
		{kafka.ObservationReplayRun, "kafka replay.run", trace.SpanKindInternal, false},
		{kafka.ObservationReplayShutdown, "kafka replay.shutdown", trace.SpanKindInternal, false},
		{kafka.ObservationInspectorCluster, "kafka inspector.cluster", trace.SpanKindClient, false},
		{kafka.ObservationInspectorTopics, "kafka inspector.topics", trace.SpanKindClient, false},
		{kafka.ObservationInspectorConsumerGroups, "kafka inspector.consumer_groups", trace.SpanKindClient, false},
		{kafka.ObservationDependencyHealth, "kafka inspector.dependency_health", trace.SpanKindClient, false},
		{kafka.ObservationReadiness, "kafka inspector.readiness", trace.SpanKindInternal, false},
		{kafka.ObservationInspectorShutdown, "kafka inspector.shutdown", trace.SpanKindInternal, false},
		{kafka.ObservationProducerShutdown, "kafka producer.shutdown", trace.SpanKindInternal, false},
		{kafka.ObservationConsumerShutdown, "kafka consumer.shutdown", trace.SpanKindInternal, false},
		{kafka.ObservationTransactionProcessorShutdown, "kafka transaction_processor.shutdown", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeRetryScheduled, "kafka consumer.retry_scheduled", trace.SpanKindInternal, false},
		{kafka.ObservationConsumeRebalanceWait, "kafka consumer.rebalance_wait", trace.SpanKindInternal, false},
	}

	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{Runtime: testRuntime{
		tracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)),
		meterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for index, item := range contracts {
		observation := validContractObservation(item.kind, index)
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("Observer(%s) error = %v", item.kind, err)
		}
	}

	ended := spans.Ended()
	if len(ended) != len(contracts) {
		t.Fatalf("ended spans = %d, want %d", len(ended), len(contracts))
	}
	for index, item := range contracts {
		span := ended[index]
		observation := validContractObservation(item.kind, index)
		if span.Name() != item.name || span.SpanKind() != item.spanKind ||
			!span.StartTime().Equal(observation.StartedAt) ||
			!span.EndTime().Equal(observation.StartedAt.Add(observation.Duration)) {
			t.Fatalf(
				"%s span = %q/%s/%s-%s",
				item.kind,
				span.Name(),
				span.SpanKind(),
				span.StartTime(),
				span.EndTime(),
			)
		}
		attributes := attributeMap(span.Attributes())
		_, hasMessagingSystem := attributes["messaging.system"]
		if hasMessagingSystem != item.messaging {
			t.Fatalf("%s messaging attributes = %t, want %t", item.kind, hasMessagingSystem, item.messaging)
		}
		if attributes["kafka.operation"] != item.kind.String() {
			t.Fatalf("%s kafka.operation = %#v", item.kind, attributes["kafka.operation"])
		}
		wantStatus := codes.Unset
		if !observation.Succeeded {
			wantStatus = codes.Error
		}
		if span.Status().Code != wantStatus {
			t.Fatalf("%s status = %s, want %s", item.kind, span.Status().Code, wantStatus)
		}
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for index, item := range contracts {
		assertFloatHistogram(
			t,
			metrics,
			"kafka.client.operation.duration",
			validContractObservation(item.kind, index).Duration.Seconds(),
			map[string]any{"kafka.operation": item.kind.String()},
		)
	}
}

func TestMetricNamesUnitsMonotonicityAndBucketsAreStable(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instrumentation, err := New(Config{Runtime: testRuntime{
		tracerProvider: sdktrace.NewTracerProvider(),
		meterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	observations := []kafka.Observation{
		validContractObservation(kafka.ObservationProduceRecord, 0),
		validContractObservation(kafka.ObservationConsumePoll, 1),
		validContractObservation(kafka.ObservationReplayRecord, 2),
		validContractObservation(kafka.ObservationConsumeCommit, 3),
		validContractObservation(kafka.ObservationBrokerRequest, 4),
		validContractObservation(kafka.ObservationBrokerThrottle, 5),
	}
	observations[4].RequestBytes = 128
	observations[4].ResponseBytes = 256
	observations[4].QueueDuration = 2 * time.Millisecond
	observations[5].ThrottleDuration = 3 * time.Millisecond
	for _, observation := range observations {
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("Observer(%s) error = %v", observation.Kind, err)
		}
	}

	var resource metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resource); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	type descriptor struct {
		description string
		unit        string
		monotonic   bool
		bounds      []float64
		intBounds   []float64
	}
	want := map[string]descriptor{
		"messaging.client.operation.duration": {description: "Duration of messaging operation initiated by a producer or consumer client.", unit: "s", bounds: []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}},
		"messaging.process.duration":          {description: "Duration of processing operation.", unit: "s", bounds: []float64{0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}},
		"messaging.client.consumed.messages":  {description: "Number of messages that were delivered to the application.", unit: "{message}", monotonic: true},
		"kafka.client.operations":             {description: "Completed Kafka policy operations by bounded operation and outcome.", unit: "{operation}", monotonic: true},
		"kafka.client.operation.duration":     {description: "Duration of completed Kafka policy operations.", unit: "s", bounds: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}},
		"kafka.client.request.size":           {description: "Kafka protocol request or response size below TLS framing.", unit: "By", intBounds: []float64{1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216, 67108864}},
		"kafka.client.request.queue.duration": {description: "Time a Kafka request waited in the client before network write.", unit: "s", bounds: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}},
		"kafka.client.throttle.duration":      {description: "Kafka broker-imposed throttle duration.", unit: "s", bounds: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}},
	}
	seen := make(map[string]struct{}, len(want))
	for _, scope := range resource.ScopeMetrics {
		for _, current := range scope.Metrics {
			descriptor, exists := want[current.Name]
			if !exists {
				t.Fatalf("unexpected metric %q", current.Name)
			}
			seen[current.Name] = struct{}{}
			if current.Description != descriptor.description {
				t.Fatalf("%s description = %q, want %q", current.Name, current.Description, descriptor.description)
			}
			if current.Unit != descriptor.unit {
				t.Fatalf("%s unit = %q, want %q", current.Name, current.Unit, descriptor.unit)
			}
			switch data := current.Data.(type) {
			case metricdata.Sum[int64]:
				if data.IsMonotonic != descriptor.monotonic {
					t.Fatalf("%s monotonic = %t, want %t", current.Name, data.IsMonotonic, descriptor.monotonic)
				}
			case metricdata.Histogram[float64]:
				if len(data.DataPoints) == 0 || !reflect.DeepEqual(data.DataPoints[0].Bounds, descriptor.bounds) {
					t.Fatalf("%s bounds = %#v, want %#v", current.Name, data.DataPoints, descriptor.bounds)
				}
			case metricdata.Histogram[int64]:
				if len(data.DataPoints) == 0 ||
					!reflect.DeepEqual(data.DataPoints[0].Bounds, descriptor.intBounds) {
					t.Fatalf("%s bounds = %#v, want %#v", current.Name, data.DataPoints, descriptor.intBounds)
				}
			default:
				t.Fatalf("%s aggregation = %T", current.Name, current.Data)
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("metric descriptors seen = %d, want %d", len(seen), len(want))
	}
}

func validContractObservation(kind kafka.ObservationKind, index int) kafka.Observation {
	observation := kafka.Observation{
		Kind:      kind,
		StartedAt: time.Unix(int64(index+1), 0),
		Duration:  time.Duration(index+1) * time.Millisecond,
		Succeeded: true,
	}
	switch kind {
	case kafka.ObservationProduceRecord,
		kafka.ObservationProduceAsync,
		kafka.ObservationConsumeRecord:
		observation.RecordCount = 1
	case kafka.ObservationProduceBatch,
		kafka.ObservationConsumeBatch,
		kafka.ObservationConsumeCommit:
		observation.RecordCount = 1
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
	case kafka.ObservationConsumeRetryScheduled:
		observation.Succeeded = false
		observation.Category = kafka.ErrorRetryable
		observation.Topic = "events"
		observation.Partition = 1
		observation.PartitionKnown = true
		observation.Offset = 4
		observation.OffsetKnown = true
		observation.RecordCount = 1
		observation.PartitionCount = 1
		observation.RecordBytes = 64
	}

	return observation
}
