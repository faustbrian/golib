package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestProjectionControlInstrumentationMeasuresOperations(t *testing.T) {
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
	running := telemetryProjectionStatus(t, projection.StateRunning, 7)
	paused := telemetryProjectionStatus(t, projection.StatePaused, 7)
	reset := telemetryProjectionStatus(t, projection.StatePaused, 0)
	next := &telemetryProjectionController{
		status: running,
		pause:  paused,
		resume: running,
		reset:  reset,
	}
	controller, err := instrumentation.WrapProjectionController(
		"account-summary",
		next,
	)
	if err != nil {
		t.Fatalf("WrapProjectionController() error = %v", err)
	}
	parentCtx, parent := instrumentation.tracer.Start(
		context.Background(),
		"parent",
	)
	if got, err := controller.Status(parentCtx); err != nil ||
		got.State() != projection.StateRunning {
		t.Fatalf("Status() = %#v, %v", got, err)
	}
	if got, err := controller.Pause(parentCtx); err != nil ||
		got.State() != projection.StatePaused {
		t.Fatalf("Pause() = %#v, %v", got, err)
	}
	if got, err := controller.Resume(parentCtx); err != nil ||
		got.State() != projection.StateRunning {
		t.Fatalf("Resume() = %#v, %v", got, err)
	}
	maxPosition := eventsourcing.GlobalPosition(^uint64(0))
	if got, err := controller.ResetCheckpoint(
		parentCtx,
		maxPosition,
	); err != nil ||
		got.State() != projection.StatePaused {
		t.Fatalf("ResetCheckpoint() = %#v, %v", got, err)
	}
	parent.End()

	if len(next.contexts) != 4 || next.expected != maxPosition {
		t.Fatalf(
			"downstream calls = contexts %d, expected %d",
			len(next.contexts),
			next.expected,
		)
	}
	for _, spanContext := range next.contexts {
		if spanContext.TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("control call did not receive the operation context")
		}
	}
	spans := recorder.Ended()
	if len(spans) != 5 {
		t.Fatalf("span count = %d", len(spans))
	}
	assertProjectionCheckpointSpan(
		t,
		spans[0],
		"event_sourcing.projection.control.status",
		"account-summary",
		"running",
		"7",
	)
	assertProjectionCheckpointSpan(
		t,
		spans[1],
		"event_sourcing.projection.control.pause",
		"account-summary",
		"paused",
		"7",
	)
	assertProjectionCheckpointSpan(
		t,
		spans[2],
		"event_sourcing.projection.control.resume",
		"account-summary",
		"running",
		"7",
	)
	assertProjectionCheckpointSpan(
		t,
		spans[3],
		"event_sourcing.projection.control.reset",
		"account-summary",
		"paused",
		"",
	)
	if projectionSpanValue(
		spans[3],
		"event_sourcing.projection.expected_checkpoint",
	) != "18446744073709551615" {
		t.Fatalf("reset span = %#v", spans[3])
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, operation := range []string{
		"projection_control_status",
		"projection_control_pause",
		"projection_control_resume",
		"projection_control_reset",
	} {
		assertOperationMetric(t, metrics, operation, "success", 1)
	}
}

func TestProjectionControlInstrumentationPreservesFailuresAndPanics(
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
	secret := errors.New("secret control failure")
	status := telemetryProjectionStatus(t, projection.StateRunning, 7)
	next := &telemetryProjectionController{
		status: status,
		pause:  status,
		resume: status,
		reset:  status,
		err:    secret,
	}
	controller, err := instrumentation.WrapProjectionController(
		"account-summary",
		next,
	)
	if err != nil {
		t.Fatalf("WrapProjectionController() error = %v", err)
	}
	for _, operation := range []string{"status", "pause", "resume", "reset"} {
		next.failureOperation = operation
		got, callErr := callTelemetryProjectionControl(
			controller,
			operation,
			7,
		)
		if !errors.Is(callErr, secret) || got.State() != status.State() {
			t.Fatalf("%s() = %#v, %v", operation, got, callErr)
		}
		span := recorder.Ended()[len(recorder.Ended())-1]
		if projectionSpanValue(
			span,
			"event_sourcing.projection.state",
		) != "" || projectionSpanValue(
			span,
			"event_sourcing.projection.checkpoint",
		) != "" {
			t.Fatalf("%s failure recorded returned status = %#v", operation, span)
		}
	}
	next.failureOperation = ""
	next.panicValue = secret
	for _, operation := range []string{"status", "pause", "resume", "reset"} {
		next.panicOperation = operation
		assertPanicPreserved(t, secret, func() {
			_, _ = callTelemetryProjectionControl(controller, operation, 7)
		})
	}
	if strings.Contains(fmt.Sprint(recorder.Ended()), secret.Error()) {
		t.Fatal("projection control telemetry disclosed diagnostics")
	}
	assertAllSpansError(t, recorder.Ended())
}

func TestProjectionControlInstrumentationRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(
		t,
		propagation.TraceContext{},
	)
	next := &telemetryProjectionController{}
	if _, err := instrumentation.WrapProjectionController(
		"",
		next,
	); !errors.Is(err, ErrProjectionNameInvalid) {
		t.Fatalf("invalid name error = %v", err)
	}
	if _, err := instrumentation.WrapProjectionController(
		"account-summary",
		nil,
	); !errors.Is(err, ErrProjectionControllerRequired) {
		t.Fatalf("nil controller error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if _, err := nilInstrumentation.WrapProjectionController(
		"account-summary",
		next,
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	controller, err := instrumentation.WrapProjectionController(
		"account-summary",
		next,
	)
	if err != nil {
		t.Fatalf("WrapProjectionController() error = %v", err)
	}
	for _, operation := range []string{"status", "pause", "resume", "reset"} {
		if _, err := callTelemetryProjectionControl(
			controller,
			operation,
			1,
		); err != nil {
			t.Fatalf("%s(background) error = %v", operation, err)
		}
		before := len(next.contexts)
		var callErr error
		switch operation {
		case "status":
			_, callErr = controller.Status(nilContext())
		case "pause":
			_, callErr = controller.Pause(nilContext())
		case "resume":
			_, callErr = controller.Resume(nilContext())
		case "reset":
			_, callErr = controller.ResetCheckpoint(nilContext(), 1)
		}
		if !errors.Is(callErr, ErrContextRequired) {
			t.Fatalf("%s(nil) error = %v", operation, callErr)
		}
		if len(next.contexts) != before {
			t.Fatalf("%s(nil) reached downstream", operation)
		}
	}
}

type telemetryProjectionController struct {
	status           projection.Status
	pause            projection.Status
	resume           projection.Status
	reset            projection.Status
	expected         eventsourcing.GlobalPosition
	contexts         []trace.SpanContext
	failureOperation string
	panicOperation   string
	panicValue       any
	err              error
}

func (controller *telemetryProjectionController) Status(
	ctx context.Context,
) (projection.Status, error) {
	controller.contexts = append(
		controller.contexts,
		trace.SpanContextFromContext(ctx),
	)
	if controller.panicOperation == "status" {
		panic(controller.panicValue)
	}
	if controller.failureOperation == "status" {
		return controller.status, controller.err
	}

	return controller.status, nil
}

func (controller *telemetryProjectionController) Pause(
	ctx context.Context,
) (projection.Status, error) {
	controller.contexts = append(
		controller.contexts,
		trace.SpanContextFromContext(ctx),
	)
	if controller.panicOperation == "pause" {
		panic(controller.panicValue)
	}
	if controller.failureOperation == "pause" {
		return controller.pause, controller.err
	}

	return controller.pause, nil
}

func (controller *telemetryProjectionController) Resume(
	ctx context.Context,
) (projection.Status, error) {
	controller.contexts = append(
		controller.contexts,
		trace.SpanContextFromContext(ctx),
	)
	if controller.panicOperation == "resume" {
		panic(controller.panicValue)
	}
	if controller.failureOperation == "resume" {
		return controller.resume, controller.err
	}

	return controller.resume, nil
}

func (controller *telemetryProjectionController) ResetCheckpoint(
	ctx context.Context,
	expected eventsourcing.GlobalPosition,
) (projection.Status, error) {
	controller.contexts = append(
		controller.contexts,
		trace.SpanContextFromContext(ctx),
	)
	controller.expected = expected
	if controller.panicOperation == "reset" {
		panic(controller.panicValue)
	}
	if controller.failureOperation == "reset" {
		return controller.reset, controller.err
	}

	return controller.reset, nil
}

func callTelemetryProjectionControl(
	controller ProjectionController,
	operation string,
	expected eventsourcing.GlobalPosition,
) (projection.Status, error) {
	switch operation {
	case "status":
		return controller.Status(context.Background())
	case "pause":
		return controller.Pause(context.Background())
	case "resume":
		return controller.Resume(context.Background())
	default:
		return controller.ResetCheckpoint(context.Background(), expected)
	}
}

func telemetryProjectionStatus(
	t testing.TB,
	state projection.RunState,
	checkpoint eventsourcing.GlobalPosition,
) projection.Status {
	t.Helper()

	status, err := projection.NewStatus(projection.StatusInput{
		State:         state,
		Checkpoint:    checkpoint,
		HasCheckpoint: checkpoint != 0,
	})
	if err != nil {
		t.Fatalf("NewStatus() error = %v", err)
	}

	return status
}
