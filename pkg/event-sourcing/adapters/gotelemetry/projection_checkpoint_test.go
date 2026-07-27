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

func TestProjectionCheckpointInstrumentationMeasuresStatusAndSave(
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
	status, err := projection.NewStatus(projection.StatusInput{
		State:         projection.StateRunning,
		Checkpoint:    7,
		HasCheckpoint: true,
	})
	if err != nil {
		t.Fatalf("NewStatus() error = %v", err)
	}
	next := &telemetryCheckpointStore{status: status}
	store, err := instrumentation.WrapProjectionCheckpointStore(next)
	if err != nil {
		t.Fatalf("WrapProjectionCheckpointStore() error = %v", err)
	}
	parentCtx, parent := instrumentation.tracer.Start(
		context.Background(),
		"parent",
	)
	got, err := store.Status(parentCtx, "account-summary")
	if err != nil || !got.Valid() {
		t.Fatalf("Status() = %#v, %v", got, err)
	}
	if err := store.Save(parentCtx, "account-summary", 7, 9); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	parent.End()

	if len(next.contexts) != 2 {
		t.Fatalf("downstream context count = %d", len(next.contexts))
	}
	for _, spanContext := range next.contexts {
		if spanContext.TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("checkpoint call did not receive the operation context")
		}
	}
	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("span count = %d", len(spans))
	}
	assertProjectionCheckpointSpan(
		t,
		spans[0],
		"event_sourcing.projection.checkpoint.status",
		"account-summary",
		"running",
		"7",
	)
	assertProjectionCheckpointSpan(
		t,
		spans[1],
		"event_sourcing.projection.checkpoint.save",
		"account-summary",
		"",
		"9",
	)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(
		t,
		metrics,
		"projection_checkpoint_status",
		"success",
		1,
	)
	assertOperationMetric(
		t,
		metrics,
		"projection_checkpoint_save",
		"success",
		1,
	)
}

func TestProjectionCheckpointInstrumentationPreservesFailuresAndPanics(
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
	secret := errors.New("secret checkpoint failure")
	status, err := projection.NewStatus(projection.StatusInput{
		State:         projection.StateRunning,
		Checkpoint:    7,
		HasCheckpoint: true,
	})
	if err != nil {
		t.Fatalf("NewStatus() error = %v", err)
	}
	next := &telemetryCheckpointStore{
		status:    status,
		statusErr: secret,
		saveErr:   secret,
	}
	store, err := instrumentation.WrapProjectionCheckpointStore(next)
	if err != nil {
		t.Fatalf("WrapProjectionCheckpointStore() error = %v", err)
	}
	if _, err := store.Status(
		context.Background(),
		"account-summary",
	); !errors.Is(err, secret) {
		t.Fatalf("Status() error = %v", err)
	}
	if err := store.Save(
		context.Background(),
		"account-summary",
		1,
		2,
	); !errors.Is(err, secret) {
		t.Fatalf("Save() error = %v", err)
	}
	if span := recorder.Ended()[0]; projectionSpanValue(
		span,
		"event_sourcing.projection.state",
	) != "" || projectionSpanValue(
		span,
		"event_sourcing.projection.checkpoint",
	) != "" {
		t.Fatalf("failed status span recorded returned state = %#v", span)
	}
	next.statusErr = nil
	next.saveErr = nil
	next.panicValue = secret
	for _, operation := range []string{"status", "save"} {
		next.panicOperation = operation
		assertPanicPreserved(t, secret, func() {
			if operation == "status" {
				_, _ = store.Status(context.Background(), "account-summary")
			} else {
				_ = store.Save(
					context.Background(),
					"account-summary",
					1,
					2,
				)
			}
		})
	}
	if strings.Contains(fmt.Sprint(recorder.Ended()), secret.Error()) {
		t.Fatal("checkpoint telemetry disclosed failure diagnostics")
	}
}

func TestProjectionCheckpointInstrumentationPreservesInputsAndRedactsInvalidName(
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
	validStatus, err := projection.NewStatus(projection.StatusInput{
		State: projection.StateRunning,
	})
	if err != nil {
		t.Fatalf("NewStatus() error = %v", err)
	}
	next := &telemetryCheckpointStore{status: validStatus}
	store, err := instrumentation.WrapProjectionCheckpointStore(next)
	if err != nil {
		t.Fatalf("WrapProjectionCheckpointStore() error = %v", err)
	}
	invalidName := "secret-" + strings.Repeat(
		"x",
		eventsourcing.MaxAggregateIDBytes,
	)
	if _, err := store.Status(
		context.Background(),
		invalidName,
	); err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	next.status = projection.Status{}
	if _, err := store.Status(
		context.Background(),
		"account-summary",
	); err != nil {
		t.Fatalf("Status(invalid result) error = %v", err)
	}
	if err := store.Save(
		context.Background(),
		"account-summary",
		7,
		9,
	); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if len(next.names) != 3 ||
		next.names[0] != invalidName ||
		next.expected != 7 ||
		next.next != 9 {
		t.Fatalf(
			"downstream inputs = names %v, positions %d/%d",
			next.names,
			next.expected,
			next.next,
		)
	}
	spans := recorder.Ended()
	if projectionSpanValue(
		spans[0],
		"event_sourcing.projection.name",
	) != "invalid" ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.projection.state",
		) != "running" ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.projection.checkpoint",
		) != "" ||
		projectionSpanValue(
			spans[1],
			"event_sourcing.projection.state",
		) != "unknown" ||
		strings.Contains(fmt.Sprint(spans), invalidName) {
		t.Fatalf("checkpoint spans = %#v", spans)
	}
}

func TestProjectionCheckpointInstrumentationRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(
		t,
		propagation.TraceContext{},
	)
	if _, err := instrumentation.WrapProjectionCheckpointStore(nil); !errors.Is(
		err,
		ErrProjectionCheckpointStoreRequired,
	) {
		t.Fatalf("nil store error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if _, err := nilInstrumentation.WrapProjectionCheckpointStore(
		&telemetryCheckpointStore{},
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	next := &telemetryCheckpointStore{}
	store, err := instrumentation.WrapProjectionCheckpointStore(next)
	if err != nil {
		t.Fatalf("WrapProjectionCheckpointStore() error = %v", err)
	}
	if _, err := store.Status(
		nilContext(),
		"account-summary",
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Status(nil) error = %v", err)
	}
	if err := store.Save(
		nilContext(),
		"account-summary",
		0,
		1,
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("Save(nil) error = %v", err)
	}
	if len(next.contexts) != 0 {
		t.Fatal("nil contexts reached the downstream checkpoint store")
	}
}

type telemetryCheckpointStore struct {
	status         projection.Status
	statusErr      error
	saveErr        error
	panicOperation string
	panicValue     any
	contexts       []trace.SpanContext
	names          []string
	expected       eventsourcing.GlobalPosition
	next           eventsourcing.GlobalPosition
}

func (store *telemetryCheckpointStore) Status(
	ctx context.Context,
	name string,
) (projection.Status, error) {
	store.contexts = append(store.contexts, trace.SpanContextFromContext(ctx))
	store.names = append(store.names, name)
	if store.panicOperation == "status" {
		panic(store.panicValue)
	}

	return store.status, store.statusErr
}

func (store *telemetryCheckpointStore) Save(
	ctx context.Context,
	name string,
	expected eventsourcing.GlobalPosition,
	next eventsourcing.GlobalPosition,
) error {
	store.contexts = append(store.contexts, trace.SpanContextFromContext(ctx))
	store.names = append(store.names, name)
	store.expected = expected
	store.next = next
	if store.panicOperation == "save" {
		panic(store.panicValue)
	}

	return store.saveErr
}

func assertProjectionCheckpointSpan(
	t testing.TB,
	span sdktrace.ReadOnlySpan,
	name string,
	projectionName string,
	state string,
	checkpoint string,
) {
	t.Helper()

	if span.Name() != name ||
		projectionSpanValue(
			span,
			"event_sourcing.projection.name",
		) != projectionName ||
		projectionSpanValue(
			span,
			"event_sourcing.projection.state",
		) != state ||
		projectionSpanValue(
			span,
			"event_sourcing.projection.checkpoint",
		) != checkpoint {
		t.Fatalf("checkpoint span = %#v", span)
	}
}
