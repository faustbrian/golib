package outboxotel_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestCapturedTelemetryExcludesEveryEnvelopeAndFailureField(t *testing.T) {
	t.Parallel()

	const attempts = 8675309
	traceStateMarker := "privacy-credential-" + "9b16"
	forbidden := []string{
		"privacy-envelope-id-7f94",
		"privacy-destination-6b83",
		"privacy-payload-5a72",
		"privacy-metadata-key-4c61",
		"privacy-metadata-value-3d50",
		"privacy-ordering-key-2e49",
		"privacy-idempotency-k" + "ey-1f38",
		"privacy-sql-0a27",
		"sensitive-sensitive",
		"privacy-error-8c05",
		"privacy-panic-7d94",
		traceStateMarker,
	}

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instrumentation, err := outboxotel.New(testRuntime{
		tracer: tracerProvider, meter: meterProvider, propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("new telemetry: %v", err)
	}

	envelope := outbox.Envelope{
		ID:      forbidden[0],
		Topic:   forbidden[1],
		Payload: []byte(forbidden[2]),
		Metadata: map[string]string{
			forbidden[3]:    forbidden[4],
			"authorization": forbidden[8],
			"sql":           forbidden[7],
			"traceparent":   "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"tracestate":    "privacy=" + traceStateMarker,
		},
		OrderingKey:    forbidden[5],
		IdempotencyKey: forbidden[6],
		Attempts:       attempts,
	}
	publishFailure := errors.New(forbidden[9])
	failing, err := instrumentation.WrapPublisher(&recordingPublisher{err: publishFailure})
	if err != nil {
		t.Fatalf("wrap failing publisher: %v", err)
	}
	if got := failing.Publish(context.Background(), envelope); got != publishFailure {
		t.Fatalf("publish error = %v, want exact %v", got, publishFailure)
	}

	panicValue := &privacyPanic{value: forbidden[10]}
	panicking, err := instrumentation.WrapPublisher(&recordingPublisher{panicValue: panicValue})
	if err != nil {
		t.Fatalf("wrap panicking publisher: %v", err)
	}
	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Fatalf("publish panic = %#v, want exact %#v", got, panicValue)
			}
		}()
		_ = panicking.Publish(context.Background(), envelope)
	}()

	instrumentation.Observe(context.Background(), outbox.Event{
		Operation: outbox.Operation(forbidden[3]),
		Outcome:   outbox.Outcome(forbidden[4]),
		Count:     1,
		MessageID: forbidden[0],
		Topic:     forbidden[1],
		Attempts:  attempts,
		Duration:  time.Second,
	})
	oldest := time.Unix(1, 0)
	instrumentation.RecordBacklog(context.Background(), outbox.BacklogStats{
		Pending: 2, Leased: 3, Dead: 4, OldestPendingAt: &oldest,
	}, oldest.Add(time.Second))

	var metrics metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	captured := captureTelemetryText(spanRecorder.Ended(), metrics)
	for _, secret := range forbidden {
		if strings.Contains(captured, secret) {
			t.Errorf("captured telemetry contains forbidden value %q", secret)
		}
	}
	if strings.Contains(captured, fmt.Sprint(attempts)) {
		t.Errorf("captured telemetry contains raw attempt count %d", attempts)
	}
}

type privacyPanic struct {
	value string
}

func captureTelemetryText(spans []sdktrace.ReadOnlySpan, metrics metricdata.ResourceMetrics) string {
	var captured strings.Builder
	appendAttributes := func(attributes []attribute.KeyValue) {
		for _, keyValue := range attributes {
			fmt.Fprintf(&captured, "%s=%s\n", keyValue.Key, keyValue.Value.String())
		}
	}
	appendSet := func(set attribute.Set) {
		appendAttributes(set.ToSlice())
	}

	for _, span := range spans {
		stub := tracetest.SpanStubFromReadOnlySpan(span)
		fmt.Fprintf(&captured, "%s\n%s\n%s\n%s\n%s\n%s\n%s\n",
			stub.Name,
			stub.Status.Description,
			stub.InstrumentationScope.Name,
			stub.InstrumentationScope.Version,
			stub.InstrumentationScope.SchemaURL,
			stub.SpanContext.TraceState().String(),
			stub.Parent.TraceState().String(),
		)
		appendAttributes(stub.Attributes)
		for _, event := range stub.Events {
			fmt.Fprintln(&captured, event.Name)
			appendAttributes(event.Attributes)
		}
		for _, link := range stub.Links {
			fmt.Fprintln(&captured, link.SpanContext.TraceState().String())
			appendAttributes(link.Attributes)
		}
		if stub.Resource != nil {
			fmt.Fprintln(&captured, stub.Resource.SchemaURL())
			appendSet(*stub.Resource.Set())
		}
	}
	if metrics.Resource != nil {
		appendSet(*metrics.Resource.Set())
	}
	for _, scope := range metrics.ScopeMetrics {
		fmt.Fprintf(&captured, "%s\n%s\n%s\n",
			scope.Scope.Name, scope.Scope.Version, scope.Scope.SchemaURL)
		for _, measurement := range scope.Metrics {
			fmt.Fprintf(&captured, "%s\n%s\n%s\n",
				measurement.Name, measurement.Description, measurement.Unit)
			appendMetricData(&captured, measurement.Data, appendSet, appendAttributes)
		}
	}

	return captured.String()
}

func appendMetricData(
	captured *strings.Builder,
	aggregation metricdata.Aggregation,
	appendSet func(attribute.Set),
	appendAttributes func([]attribute.KeyValue),
) {
	switch data := aggregation.(type) {
	case metricdata.Sum[int64]:
		for _, point := range data.DataPoints {
			fmt.Fprintln(captured, point.Value)
			appendSet(point.Attributes)
			for _, exemplar := range point.Exemplars {
				fmt.Fprintln(captured, exemplar.Value)
				appendAttributes(exemplar.FilteredAttributes)
			}
		}
	case metricdata.Histogram[float64]:
		for _, point := range data.DataPoints {
			fmt.Fprintf(captured, "%d\n%g\n%v\n%v\n",
				point.Count, point.Sum, point.Bounds, point.BucketCounts)
			if minimum, defined := point.Min.Value(); defined {
				fmt.Fprintln(captured, minimum)
			}
			if maximum, defined := point.Max.Value(); defined {
				fmt.Fprintln(captured, maximum)
			}
			appendSet(point.Attributes)
			for _, exemplar := range point.Exemplars {
				fmt.Fprintln(captured, exemplar.Value)
				appendAttributes(exemplar.FilteredAttributes)
			}
		}
	case metricdata.Gauge[int64]:
		for _, point := range data.DataPoints {
			fmt.Fprintln(captured, point.Value)
			appendSet(point.Attributes)
			for _, exemplar := range point.Exemplars {
				fmt.Fprintln(captured, exemplar.Value)
				appendAttributes(exemplar.FilteredAttributes)
			}
		}
	case metricdata.Gauge[float64]:
		for _, point := range data.DataPoints {
			fmt.Fprintln(captured, point.Value)
			appendSet(point.Attributes)
			for _, exemplar := range point.Exemplars {
				fmt.Fprintln(captured, exemplar.Value)
				appendAttributes(exemplar.FilteredAttributes)
			}
		}
	}
}
