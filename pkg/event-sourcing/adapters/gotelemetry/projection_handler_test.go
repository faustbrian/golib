package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestProjectionHandlerInstrumentationMeasuresDelivery(t *testing.T) {
	t.Parallel()

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
	var downstream trace.SpanContext
	var handled eventsourcing.Delivery
	handler, err := instrumentation.WrapProjectionHandler(
		"account-summary",
		func(
			ctx context.Context,
			delivery eventsourcing.Delivery,
		) error {
			downstream = trace.SpanContextFromContext(ctx)
			handled = delivery

			return nil
		},
	)
	if err != nil {
		t.Fatalf("WrapProjectionHandler() error = %v", err)
	}
	delivery := telemetryDelivery(
		t,
		"secret-projection-message",
		eventsourcing.DeliveryReplay,
	)
	parentCtx, parent := instrumentation.tracer.Start(
		context.Background(),
		"parent",
	)
	if err := handler(parentCtx, delivery); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	parent.End()
	if downstream.TraceID() != parent.SpanContext().TraceID() ||
		handled.Message().ID() != delivery.Message().ID() {
		t.Fatal("projection handler did not preserve context or delivery")
	}
	spans := recorder.Ended()
	if len(spans) != 2 ||
		spans[0].Name() != "event_sourcing.projection.handle" ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.projection.name",
		) != "account-summary" ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.delivery.mode",
		) != "replay" {
		t.Fatalf("projection handler spans = %#v", spans)
	}
	if strings.Contains(
		fmt.Sprint(spans),
		delivery.Message().ID().String(),
	) {
		t.Fatal("projection handler telemetry disclosed message identity")
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "projection_handle", "success", 1)
	assertDeliveryMetric(t, metrics, "replay", "success", 1)
}

func TestProjectionHandlerInstrumentationPreservesFailuresAndPanics(
	t *testing.T,
) {
	t.Parallel()

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
	secret := errors.New("secret projection handler failure")
	delivery := telemetryDelivery(
		t,
		"projection-failure",
		eventsourcing.DeliveryReplay,
	)
	failing, err := instrumentation.WrapProjectionHandler(
		"account-summary",
		func(context.Context, eventsourcing.Delivery) error {
			return secret
		},
	)
	if err != nil {
		t.Fatalf("WrapProjectionHandler(failing) error = %v", err)
	}
	if err := failing(context.Background(), delivery); !errors.Is(err, secret) {
		t.Fatalf("failing handler error = %v", err)
	}
	panicking, err := instrumentation.WrapProjectionHandler(
		"account-summary",
		func(context.Context, eventsourcing.Delivery) error {
			panic(secret)
		},
	)
	if err != nil {
		t.Fatalf("WrapProjectionHandler(panicking) error = %v", err)
	}
	assertPanicPreserved(t, secret, func() {
		_ = panicking(context.Background(), delivery)
	})
	if strings.Contains(fmt.Sprint(recorder.Ended()), secret.Error()) {
		t.Fatal("projection handler telemetry disclosed diagnostics")
	}
	assertAllSpansError(t, recorder.Ended())
}

func TestProjectionHandlerInstrumentationRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(
		t,
		propagation.TraceContext{},
	)
	next := func(context.Context, eventsourcing.Delivery) error {
		return nil
	}
	if _, err := instrumentation.WrapProjectionHandler(
		"",
		next,
	); !errors.Is(err, ErrProjectionNameInvalid) {
		t.Fatalf("invalid name error = %v", err)
	}
	if _, err := instrumentation.WrapProjectionHandler(
		"account-summary",
		nil,
	); !errors.Is(err, ErrProjectionHandlerRequired) {
		t.Fatalf("nil handler error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if _, err := nilInstrumentation.WrapProjectionHandler(
		"account-summary",
		next,
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	calls := 0
	handler, err := instrumentation.WrapProjectionHandler(
		"account-summary",
		func(context.Context, eventsourcing.Delivery) error {
			calls++

			return nil
		},
	)
	if err != nil {
		t.Fatalf("WrapProjectionHandler() error = %v", err)
	}
	if err := handler(
		nilContext(),
		eventsourcing.Delivery{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}
	if calls != 0 {
		t.Fatal("nil context reached downstream projection handler")
	}
}
