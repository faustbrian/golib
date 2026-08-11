package gotelemetry

import (
	"context"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestW3CRemoteSamplingAndTraceStateInteroperateWithoutLinks(t *testing.T) {
	for _, test := range []struct {
		name       string
		traceFlags string
		wantSpans  int
	}{
		{name: "sampled", traceFlags: "01", wantSpans: 1},
		{name: "sampled out", traceFlags: "00", wantSpans: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			reader := sdkmetric.NewManualReader()
			instrumentation, err := New(testRuntime{
				tracer: sdktrace.NewTracerProvider(
					sdktrace.WithSpanProcessor(recorder),
				),
				meter:      sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
				propagator: propagation.TraceContext{},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			consumer, err := instrumentation.WrapConsumer(func(
				context.Context,
				eventsourcing.Delivery,
			) error {
				return nil
			})
			if err != nil {
				t.Fatalf("WrapConsumer() error = %v", err)
			}
			delivery := telemetryDelivery(t, "interoperability", eventsourcing.DeliveryReplay)
			handler, err := instrumentation.WrapKafkaHandler(
				kafka.HandlerFunc(func(ctx context.Context, _ kafka.ConsumedMessage) error {
					return consumer(ctx, delivery)
				}),
				KafkaPropagationConfig{},
			)
			if err != nil {
				t.Fatalf("WrapKafkaHandler() error = %v", err)
			}
			if err := handler.Handle(context.Background(), kafka.ConsumedMessage{
				Headers: []kafka.Header{
					{Key: "traceparent", Value: []byte("00-00000000000000000000000000000001-0000000000000002-" + test.traceFlags)},
					{Key: "tracestate", Value: []byte("vendor=value")},
				},
			}); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			spans := recorder.Ended()
			if len(spans) != test.wantSpans {
				t.Fatalf("ended spans = %d, want %d", len(spans), test.wantSpans)
			}
			if len(spans) == 1 {
				parent := spans[0].Parent()
				if !parent.IsRemote() || parent.TraceState().String() != "vendor=value" {
					t.Fatalf("remote parent = %v", parent)
				}
				if len(spans[0].Links()) != 0 {
					t.Fatalf("links = %#v, want parent-child propagation", spans[0].Links())
				}
			}
			var metrics metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &metrics); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			assertOperationMetric(t, metrics, "consume", "success", 1)
		})
	}
}

func TestInstrumentationScopeDeclaresModuleOwnedSchema(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	instrumentation, err := New(testRuntime{
		tracer: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(recorder),
		),
		meter:      sdkmetric.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	consumer, err := instrumentation.WrapConsumer(func(
		context.Context,
		eventsourcing.Delivery,
	) error {
		return nil
	})
	if err != nil {
		t.Fatalf("WrapConsumer() error = %v", err)
	}
	if err := consumer(
		context.Background(),
		telemetryDelivery(t, "schema", eventsourcing.DeliveryLive),
	); err != nil {
		t.Fatalf("consumer() error = %v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	scope := spans[0].InstrumentationScope()
	if scope.Name != instrumentationName || scope.Version != "" || scope.SchemaURL != "" {
		t.Fatalf("instrumentation scope = %#v", scope)
	}
}
