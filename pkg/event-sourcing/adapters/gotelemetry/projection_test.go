package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestProjectionRunnerInstrumentationMeasuresProgressAndTermination(
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
	message := telemetryMessageAtPosition(t, 1)
	global := &telemetryGlobalReader{
		read: func(
			_ context.Context,
			options eventsourcing.ReadGlobalOptions,
		) (eventsourcing.MessageIterator, error) {
			messages := []eventsourcing.Message(nil)
			if options.FromPosition() == 1 {
				messages = []eventsourcing.Message{message}
			}

			return &telemetryIterator{messages: messages}, nil
		},
	}
	checkpoints := memory.NewProjectionStore()
	var handlerSpan trace.SpanContext
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "account-summary",
		Reader:      global,
		Checkpoints: checkpoints,
		Handler: func(
			ctx context.Context,
			_ eventsourcing.Delivery,
		) error {
			handlerSpan = trace.SpanContextFromContext(ctx)

			return nil
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	wrapped, err := instrumentation.WrapProjectionRunner(
		"account-summary",
		runner,
	)
	if err != nil {
		t.Fatalf("WrapProjectionRunner() error = %v", err)
	}
	parentCtx, parent := instrumentation.tracer.Start(
		context.Background(),
		"parent",
	)
	progress, err := wrapped.RunBatch(parentCtx)
	if err != nil ||
		progress.Scanned() != 1 ||
		progress.Handled() != 1 ||
		progress.Checkpointed() != 1 ||
		progress.Checkpoint() != 1 {
		t.Fatalf("RunBatch(progress) = %#v, %v", progress, err)
	}
	terminated, err := wrapped.RunBatch(parentCtx)
	if err != nil ||
		terminated.Scanned() != 0 ||
		terminated.Checkpoint() != 1 {
		t.Fatalf("RunBatch(terminated) = %#v, %v", terminated, err)
	}
	if err := instrumentation.RecordProjectionLag(
		parentCtx,
		"account-summary",
		1,
		5,
	); err != nil {
		t.Fatalf("RecordProjectionLag() error = %v", err)
	}
	parent.End()
	if handlerSpan.TraceID() != parent.SpanContext().TraceID() {
		t.Fatal("projection handler did not receive the operation context")
	}

	spans := recorder.Ended()
	if len(spans) != 3 {
		t.Fatalf("span count = %d", len(spans))
	}
	assertProjectionSpan(t, spans[0], "account-summary", "progress", 1, 1, 0, 0, 1, "1")
	assertProjectionSpan(t, spans[1], "account-summary", "terminated", 0, 0, 0, 0, 0, "1")
	for _, span := range spans[:2] {
		if span.Parent().TraceID() != parent.SpanContext().TraceID() {
			t.Fatal("projection span did not preserve its parent")
		}
	}
	if projectionSpanValue(
		spans[2],
		"event_sourcing.projection.name",
	) != "account-summary" ||
		projectionSpanInt64(
			spans[2],
			"event_sourcing.projection.lag",
		) != 4 {
		t.Fatalf("parent lag span = %#v", spans[2])
	}
	if telemetry := fmt.Sprint(spans); strings.Contains(
		telemetry,
		message.ID().String(),
	) || strings.Contains(telemetry, "secret") {
		t.Fatal("projection telemetry disclosed event data")
	}

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertOperationMetric(t, metrics, "projection_run_batch", "success", 2)
	assertProjectionMessageMetric(t, metrics, "account-summary", "scanned", 1)
	assertProjectionMessageMetric(t, metrics, "account-summary", "handled", 1)
	assertProjectionMessageMetric(
		t,
		metrics,
		"account-summary",
		"checkpointed",
		1,
	)
	assertProjectionLagMetric(t, metrics, "account-summary", 4)
}

func TestProjectionRunnerInstrumentationMeasuresSkippedPoison(t *testing.T) {
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
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name: "poison-summary",
		Reader: &telemetryGlobalReader{
			iterator: &telemetryIterator{
				messages: []eventsourcing.Message{
					telemetryMessageAtPosition(t, 1),
				},
			},
		},
		Checkpoints: memory.NewProjectionStore(),
		Handler: func(context.Context, eventsourcing.Delivery) error {
			return errors.New("secret poison cause")
		},
		PoisonPolicy: func(
			context.Context,
			projection.PoisonedDelivery,
		) (projection.PoisonDecision, error) {
			return projection.SkipPoison, nil
		},
		BatchSize: 1,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	wrapped, err := instrumentation.WrapProjectionRunner(
		"poison-summary",
		runner,
	)
	if err != nil {
		t.Fatalf("WrapProjectionRunner() error = %v", err)
	}
	result, err := wrapped.RunBatch(context.Background())
	if err != nil || result.Skipped() != 1 {
		t.Fatalf("RunBatch() = %#v, %v", result, err)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("span count = %d", len(spans))
	}
	assertProjectionSpan(
		t,
		spans[0],
		"poison-summary",
		"progress",
		1,
		0,
		0,
		1,
		1,
		"1",
	)
	if strings.Contains(fmt.Sprint(spans), "secret") {
		t.Fatal("projection poison telemetry disclosed failure data")
	}
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	assertProjectionMessageMetric(
		t,
		metrics,
		"poison-summary",
		"skipped",
		1,
	)
}

func TestProjectionRunnerInstrumentationPreservesErrorsAndPanics(
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
	secret := errors.New("projection secret")
	next := &telemetryProjectionRunner{err: secret}
	runner, err := instrumentation.WrapProjectionRunner("summary", next)
	if err != nil {
		t.Fatalf("WrapProjectionRunner() error = %v", err)
	}
	if _, err := runner.RunBatch(context.Background()); !errors.Is(
		err,
		secret,
	) {
		t.Fatalf("RunBatch() error = %v", err)
	}
	next.err = nil
	next.panicValue = secret
	assertPanicPreserved(t, secret, func() {
		_, _ = runner.RunBatch(context.Background())
	})

	spans := recorder.Ended()
	if len(spans) != 2 ||
		spans[0].Status().Code != codes.Error ||
		spans[1].Status().Code != codes.Error ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.replay.termination",
		) != "error" ||
		projectionSpanValue(
			spans[1],
			"event_sourcing.replay.termination",
		) != "panic" ||
		projectionSpanValue(
			spans[0],
			"event_sourcing.outcome",
		) != "error" ||
		projectionSpanValue(
			spans[1],
			"event_sourcing.outcome",
		) != "panic" ||
		strings.Contains(fmt.Sprint(spans), secret.Error()) {
		t.Fatalf("failure spans = %#v", spans)
	}
}

func TestProjectionRunnerInstrumentationRejectsInvalidCalls(t *testing.T) {
	t.Parallel()

	instrumentation := newKafkaTestInstrumentation(t, propagation.TraceContext{})
	next := &telemetryProjectionRunner{}
	if _, err := instrumentation.WrapProjectionRunner("", next); !errors.Is(
		err,
		ErrProjectionNameInvalid,
	) {
		t.Fatalf("empty name error = %v", err)
	}
	if _, err := instrumentation.WrapProjectionRunner(
		"summary",
		nil,
	); !errors.Is(err, ErrProjectionRunnerRequired) {
		t.Fatalf("nil runner error = %v", err)
	}
	var nilInstrumentation *Instrumentation
	if _, err := nilInstrumentation.WrapProjectionRunner(
		"summary",
		next,
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation error = %v", err)
	}
	runner, err := instrumentation.WrapProjectionRunner("summary", next)
	if err != nil {
		t.Fatalf("WrapProjectionRunner() error = %v", err)
	}
	if _, err := runner.RunBatch(nilContext()); !errors.Is(
		err,
		ErrContextRequired,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if err := instrumentation.RecordProjectionLag(
		nilContext(),
		"summary",
		0,
		1,
	); !errors.Is(err, ErrContextRequired) {
		t.Fatalf("nil lag context error = %v", err)
	}
	if err := instrumentation.RecordProjectionLag(
		context.Background(),
		"",
		0,
		1,
	); !errors.Is(err, ErrProjectionNameInvalid) {
		t.Fatalf("invalid lag name error = %v", err)
	}
	if err := instrumentation.RecordProjectionLag(
		context.Background(),
		"summary",
		2,
		1,
	); !errors.Is(err, ErrProjectionLagInvalid) {
		t.Fatalf("reversed lag error = %v", err)
	}
	if err := instrumentation.RecordProjectionLag(
		context.Background(),
		"summary",
		0,
		eventsourcing.GlobalPosition(^uint64(0)),
	); !errors.Is(err, ErrProjectionLagInvalid) {
		t.Fatalf("overflow lag error = %v", err)
	}
	if err := nilInstrumentation.RecordProjectionLag(
		context.Background(),
		"summary",
		0,
		1,
	); !errors.Is(err, ErrRuntimeRequired) {
		t.Fatalf("nil instrumentation lag error = %v", err)
	}
}

type telemetryProjectionRunner struct {
	err        error
	panicValue any
	span       trace.SpanContext
}

func (runner *telemetryProjectionRunner) RunBatch(
	ctx context.Context,
) (projection.BatchResult, error) {
	runner.span = trace.SpanContextFromContext(ctx)
	if runner.panicValue != nil {
		panic(runner.panicValue)
	}

	return projection.BatchResult{}, runner.err
}

func telemetryMessageAtPosition(
	t testing.TB,
	position eventsourcing.GlobalPosition,
) eventsourcing.Message {
	t.Helper()

	base := telemetryDelivery(
		t,
		"secret-projection-message",
		eventsourcing.DeliveryReplay,
	).Message()
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         base.ID().String(),
			Stream:     base.Stream(),
			Event:      base.Event(),
			Metadata:   base.Metadata(),
			RecordedAt: base.RecordedAt(),
		},
	)
	if err != nil {
		t.Fatalf("NewPendingMessage() error = %v", err)
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:        pending,
		StreamVersion:  base.StreamVersion(),
		GlobalPosition: position,
	})
	if err != nil {
		t.Fatalf("NewMessage() error = %v", err)
	}

	return message
}

func assertProjectionSpan(
	t testing.TB,
	span sdktrace.ReadOnlySpan,
	name string,
	termination string,
	scanned int64,
	handled int64,
	filtered int64,
	skipped int64,
	checkpointed int64,
	checkpoint string,
) {
	t.Helper()

	attributes := make(map[attribute.Key]attribute.Value)
	for _, item := range span.Attributes() {
		attributes[item.Key] = item.Value
	}
	if span.Name() != "event_sourcing.projection.run_batch" ||
		span.Status().Code != codes.Unset ||
		attributes["event_sourcing.operation"].AsString() !=
			"projection_run_batch" ||
		attributes["event_sourcing.projection.name"].AsString() !=
			name ||
		attributes["event_sourcing.replay.termination"].AsString() !=
			termination ||
		attributes["event_sourcing.projection.scanned"].AsInt64() != scanned ||
		attributes["event_sourcing.projection.handled"].AsInt64() != handled ||
		attributes["event_sourcing.projection.filtered"].AsInt64() != filtered ||
		attributes["event_sourcing.projection.skipped"].AsInt64() != skipped ||
		attributes["event_sourcing.projection.checkpointed"].AsInt64() !=
			checkpointed ||
		attributes["event_sourcing.projection.checkpoint"].AsString() !=
			checkpoint {
		t.Fatalf("projection span = %#v", span)
	}
}

func projectionSpanValue(span sdktrace.ReadOnlySpan, key string) string {
	for _, item := range span.Attributes() {
		if string(item.Key) == key {
			return item.Value.AsString()
		}
	}

	return ""
}

func projectionSpanInt64(span sdktrace.ReadOnlySpan, key string) int64 {
	for _, item := range span.Attributes() {
		if string(item.Key) == key {
			return item.Value.AsInt64()
		}
	}

	return -1
}

func assertProjectionMessageMetric(
	t testing.TB,
	metrics metricdata.ResourceMetrics,
	name string,
	kind string,
	want int64,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "event_sourcing.projection.messages" {
				continue
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("projection messages data = %T", item.Data)
			}
			for _, point := range sum.DataPoints {
				if attributeValue(
					point.Attributes,
					"event_sourcing.projection.name",
				) == name &&
					attributeValue(
						point.Attributes,
						"event_sourcing.projection.result",
					) == kind {
					if point.Value != want {
						t.Fatalf(
							"projection message count = %d, want %d",
							point.Value,
							want,
						)
					}

					return
				}
			}
		}
	}
	t.Fatalf("projection message metric %s/%s is missing", name, kind)
}

func assertProjectionLagMetric(
	t testing.TB,
	metrics metricdata.ResourceMetrics,
	name string,
	want int64,
) {
	t.Helper()

	for _, scope := range metrics.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "event_sourcing.projection.lag" {
				continue
			}
			histogram, ok := item.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("projection lag data = %T", item.Data)
			}
			for _, point := range histogram.DataPoints {
				if attributeValue(
					point.Attributes,
					"event_sourcing.projection.name",
				) == name {
					if point.Count != 1 || point.Sum != want {
						t.Fatalf(
							"projection lag = %d/%d, want 1/%d",
							point.Count,
							point.Sum,
							want,
						)
					}

					return
				}
			}
		}
	}
	t.Fatalf("projection lag metric %s is missing", name)
}
