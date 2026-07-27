package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/processmanager"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type telemetryCommand struct {
	action string
}

type processManagerFunc[Command any] func(
	context.Context,
	eventsourcing.Delivery,
) (processmanager.PlanResult[Command], error)

func (function processManagerFunc[Command]) Plan(
	ctx context.Context,
	delivery eventsourcing.Delivery,
) (processmanager.PlanResult[Command], error) {
	return function(ctx, delivery)
}

func TestProcessManagerInstrumentationMeasuresSuccessfulPlan(t *testing.T) {
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
	manager, err := processmanager.New(
		processmanager.Config[telemetryCommand]{
			Name:        "welcome-email",
			Replay:      processmanager.AllowReplay,
			MaxCommands: 2,
			Planner: func(
				ctx context.Context,
				delivery eventsourcing.Delivery,
			) ([]telemetryCommand, error) {
				downstream = trace.SpanContextFromContext(ctx)
				handled = delivery

				return []telemetryCommand{
					{action: "secret-command-one"},
					{action: "secret-command-two"},
				}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("processmanager.New() error = %v", err)
	}
	wrapped, err := WrapProcessManager(
		instrumentation,
		"welcome-email",
		manager,
	)
	if err != nil {
		t.Fatalf("WrapProcessManager() error = %v", err)
	}
	delivery := telemetryDelivery(
		t,
		"secret-process-manager-message",
		eventsourcing.DeliveryReplay,
	)
	parentCtx, parent := instrumentation.tracer.Start(
		context.Background(),
		"parent",
	)
	result, err := wrapped.Plan(parentCtx, delivery)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	parent.End()
	if downstream.TraceID() != parent.SpanContext().TraceID() ||
		handled.Message().ID() != delivery.Message().ID() ||
		result.MessageID() != delivery.Message().ID() ||
		result.Mode() != eventsourcing.DeliveryReplay ||
		len(result.Commands()) != 2 {
		t.Fatalf("Plan() did not preserve context, delivery, or result: %#v", result)
	}
	spans := recorder.Ended()
	if len(spans) != 2 ||
		spans[0].Name() != "event_sourcing.process_manager.plan" ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.process_manager.name",
		) != "welcome-email" ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.delivery.mode",
		) != "replay" ||
		projectionSpanInt64(
			spans[0],
			"event_sourcing.process_manager.command_count",
		) != 2 {
		t.Fatalf("process-manager spans = %#v", spans)
	}
	diagnostics := fmt.Sprint(spans)
	if strings.Contains(diagnostics, delivery.Message().ID().String()) ||
		strings.Contains(diagnostics, "secret-command") {
		t.Fatal("process-manager telemetry disclosed event or command data")
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "process_manager_plan", "success", 1)
	assertDeliveryMetric(t, metrics, "replay", "success", 1)
}

func TestProcessManagerInstrumentationPreservesFailuresAndPanics(
	t *testing.T,
) {
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
	secret := errors.New("secret process-manager failure")
	delivery := telemetryDelivery(
		t,
		"process-manager-failure",
		eventsourcing.DeliveryLive,
	)
	failing, err := WrapProcessManager(
		instrumentation,
		"welcome-email",
		processManagerFunc[telemetryCommand](func(
			context.Context,
			eventsourcing.Delivery,
		) (processmanager.PlanResult[telemetryCommand], error) {
			return processmanager.PlanResult[telemetryCommand]{}, secret
		}),
	)
	if err != nil {
		t.Fatalf("WrapProcessManager(failing) error = %v", err)
	}
	if _, err := failing.Plan(context.Background(), delivery); !errors.Is(
		err,
		secret,
	) {
		t.Fatalf("failing Plan() error = %v", err)
	}
	panicking, err := WrapProcessManager(
		instrumentation,
		"welcome-email",
		processManagerFunc[telemetryCommand](func(
			context.Context,
			eventsourcing.Delivery,
		) (processmanager.PlanResult[telemetryCommand], error) {
			panic(secret)
		}),
	)
	if err != nil {
		t.Fatalf("WrapProcessManager(panicking) error = %v", err)
	}
	assertPanicPreserved(t, secret, func() {
		_, _ = panicking.Plan(context.Background(), delivery)
	})
	spans := recorder.Ended()
	if strings.Contains(fmt.Sprint(spans), secret.Error()) {
		t.Fatal("process-manager telemetry disclosed diagnostics")
	}
	for _, span := range spans {
		if projectionSpanInt64(
			span,
			"event_sourcing.process_manager.command_count",
		) != -1 {
			t.Fatal("failed process-manager plan reported command count")
		}
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "process_manager_plan", "error", 1)
	assertOperationMetric(t, metrics, "process_manager_plan", "panic", 1)
	assertDeliveryMetric(t, metrics, "live", "error", 1)
	assertDeliveryMetric(t, metrics, "live", "panic", 1)
}

func TestProcessManagerInstrumentationRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(
		t,
		propagation.TraceContext{},
	)
	next := processManagerFunc[telemetryCommand](func(
		context.Context,
		eventsourcing.Delivery,
	) (processmanager.PlanResult[telemetryCommand], error) {
		return processmanager.PlanResult[telemetryCommand]{}, nil
	})
	if _, err := WrapProcessManager(
		instrumentation,
		"tenant/acme",
		next,
	); !errors.Is(err, ErrProcessManagerNameInvalid) {
		t.Fatalf("invalid name error = %v", err)
	}
	if _, err := WrapProcessManager[telemetryCommand](
		instrumentation,
		"welcome-email",
		nil,
	); !errors.Is(err, ErrProcessManagerRequired) {
		t.Fatalf("nil manager error = %v", err)
	}
	if _, err := WrapProcessManager(
		(*Instrumentation)(nil),
		"welcome-email",
		next,
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	calls := 0
	wrapped, err := WrapProcessManager(
		instrumentation,
		"welcome-email",
		processManagerFunc[telemetryCommand](func(
			context.Context,
			eventsourcing.Delivery,
		) (processmanager.PlanResult[telemetryCommand], error) {
			calls++

			return processmanager.PlanResult[telemetryCommand]{}, nil
		}),
	)
	if err != nil {
		t.Fatalf("WrapProcessManager() error = %v", err)
	}
	if _, err := wrapped.Plan(
		nilContext(),
		eventsourcing.Delivery{},
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}
	if calls != 0 {
		t.Fatal("nil context reached downstream process manager")
	}
}
