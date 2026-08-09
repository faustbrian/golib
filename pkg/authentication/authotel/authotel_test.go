package authotel_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authotel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestInstrumenterEmitsBoundedTraceAndMetrics(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
	finish(authentication.Event{
		Outcome:  authentication.OutcomeFailed,
		Failure:  authentication.FailureRejected,
		Duration: 25 * time.Millisecond,
	})

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Name() != "authentication.authenticate" {
		t.Fatalf("spans = %#v", spans)
	}
	if spans[0].InstrumentationScope().Version != "" || spans[0].InstrumentationScope().SchemaURL != "" {
		t.Fatalf("span scope mislabels adapter convention: %#v", spans[0].InstrumentationScope())
	}
	if !hasAttribute(spans[0].Attributes(), "authentication.credential.kind", "bearer") ||
		!hasAttribute(spans[0].Attributes(), "authentication.outcome", "failed") ||
		!hasAttribute(spans[0].Attributes(), "authentication.failure.kind", "rejected") {
		t.Fatalf("span attributes = %#v", spans[0].Attributes())
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("span status = %#v", spans[0].Status())
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("metric data = %#v", data.ScopeMetrics)
	}
	if data.ScopeMetrics[0].Scope.Version != "" || data.ScopeMetrics[0].Scope.SchemaURL != "" {
		t.Fatalf("metric scope mislabels adapter convention: %#v", data.ScopeMetrics[0].Scope)
	}
	for _, metric := range data.ScopeMetrics[0].Metrics {
		switch metric.Name {
		case "authentication.attempts":
			points := metric.Data.(metricdata.Sum[int64]).DataPoints
			if len(points) != 1 || points[0].Value != 1 {
				t.Fatalf("attempt points = %#v", points)
			}
		case "authentication.duration":
			points := metric.Data.(metricdata.Histogram[float64]).DataPoints
			if len(points) != 1 || points[0].Count != 1 || points[0].Sum != 0.025 {
				t.Fatalf("duration points = %#v", points)
			}
		default:
			t.Fatalf("unexpected metric %q", metric.Name)
		}
	}
}

func TestInstrumenterNormalizesInvalidEventsToClosedValues(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	secret := "secret-token-subject-issuer-endpoint"
	ctx, finish := instrumenter.Start(context.Background(), authentication.CredentialKind(secret))
	finish(authentication.Event{
		Outcome:  authentication.Outcome(secret),
		Failure:  authentication.FailureKind(secret),
		Duration: -time.Second,
	})

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("span count = %d, want 1", len(spans))
	}
	for key, want := range map[string]string{
		"authentication.credential.kind": "unknown",
		"authentication.outcome":         "unknown",
		"authentication.failure.kind":    "unknown",
	} {
		if !hasAttribute(spans[0].Attributes(), key, want) {
			t.Errorf("span attribute %q = %#v, want %q", key, spans[0].Attributes(), want)
		}
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if strings.Contains(measurement.Name, secret) || metricContainsAttribute(measurement, secret) {
				t.Fatalf("metric contains caller-controlled value: %#v", measurement)
			}
			if measurement.Name == "authentication.duration" {
				points := measurement.Data.(metricdata.Histogram[float64]).DataPoints
				if len(points) != 1 || points[0].Sum != 0 {
					t.Fatalf("duration points = %#v, want one zero observation", points)
				}
			}
		}
	}
}

func TestInstrumenterCompletesExactlyOnceUnderConcurrentUse(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated, Duration: time.Millisecond})
		}()
	}
	group.Wait()

	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("span count = %d, want 1", got)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			switch measurement.Name {
			case "authentication.attempts":
				points := measurement.Data.(metricdata.Sum[int64]).DataPoints
				if len(points) != 1 || points[0].Value != 1 {
					t.Fatalf("attempt points = %#v, want one completion", points)
				}
			case "authentication.duration":
				points := measurement.Data.(metricdata.Histogram[float64]).DataPoints
				if len(points) != 1 || points[0].Count != 1 {
					t.Fatalf("duration points = %#v, want one completion", points)
				}
			}
		}
	}
}

func TestDuplicateCompletionDoesNotWaitForBlockingTelemetry(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	baseProvider := tracenoop.NewTracerProvider()
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: blockingEndTracerProvider{
			TracerProvider: baseProvider,
			entered:        entered,
			release:        release,
		},
		MeterProvider: metricnoop.NewMeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)

	winnerDone := make(chan struct{})
	go func() {
		finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
		close(winnerDone)
	}()
	select {
	case <-entered:
	case <-time.After(250 * time.Millisecond):
		<-winnerDone
		t.Fatal("winning completion did not reach telemetry")
	}

	duplicateDone := make(chan struct{})
	go func() {
		finish(authentication.Event{Outcome: authentication.OutcomeFailed})
		close(duplicateDone)
	}()
	select {
	case <-duplicateDone:
		close(release)
		<-winnerDone
	case <-time.After(250 * time.Millisecond):
		close(release)
		<-winnerDone
		<-duplicateDone
		t.Fatal("duplicate completion waited for blocking telemetry")
	}
}

func TestInstrumenterUsesDocumentedClosedAttributeValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		kind        authentication.CredentialKind
		event       authentication.Event
		wantKind    string
		wantOutcome string
		wantFailure string
	}{
		{name: "basic authenticated", kind: authentication.CredentialBasic, event: authentication.Event{Outcome: authentication.OutcomeAuthenticated, Failure: authentication.FailureRejected}, wantKind: "basic", wantOutcome: "authenticated", wantFailure: "none"},
		{name: "bearer anonymous", kind: authentication.CredentialBearer, event: authentication.Event{Outcome: authentication.OutcomeAnonymous}, wantKind: "bearer", wantOutcome: "anonymous", wantFailure: "none"},
		{name: "api key absent", kind: authentication.CredentialAPIKey, event: authentication.Event{Outcome: authentication.OutcomeFailed, Failure: authentication.FailureAbsent}, wantKind: "api_key", wantOutcome: "failed", wantFailure: "absent"},
		{name: "invalid", event: authentication.Event{Outcome: authentication.OutcomeFailed, Failure: authentication.FailureInvalid}, wantKind: "unknown", wantOutcome: "failed", wantFailure: "invalid"},
		{name: "rejected", event: authentication.Event{Outcome: authentication.OutcomeFailed, Failure: authentication.FailureRejected}, wantKind: "unknown", wantOutcome: "failed", wantFailure: "rejected"},
		{name: "unavailable", event: authentication.Event{Outcome: authentication.OutcomeFailed, Failure: authentication.FailureUnavailable}, wantKind: "unknown", wantOutcome: "failed", wantFailure: "unavailable"},
		{name: "ambiguous", event: authentication.Event{Outcome: authentication.OutcomeFailed, Failure: authentication.FailureAmbiguous}, wantKind: "unknown", wantOutcome: "failed", wantFailure: "ambiguous"},
		{name: "unclassified", event: authentication.Event{Outcome: authentication.OutcomeFailed}, wantKind: "unknown", wantOutcome: "failed", wantFailure: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			instrumenter, err := authotel.New(authotel.Config{
				TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
				MeterProvider:  metricnoop.NewMeterProvider(),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, finish := instrumenter.Start(context.Background(), tt.kind)
			finish(tt.event)
			spans := recorder.Ended()
			if len(spans) != 1 ||
				!hasAttribute(spans[0].Attributes(), "authentication.credential.kind", tt.wantKind) ||
				!hasAttribute(spans[0].Attributes(), "authentication.outcome", tt.wantOutcome) ||
				!hasAttribute(spans[0].Attributes(), "authentication.failure.kind", tt.wantFailure) {
				t.Fatalf("span attributes = %#v", spans)
			}
		})
	}
}

func TestInstrumenterCompletesCanceledAttempts(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		MeterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	next, finish := instrumenter.Start(ctx, authentication.CredentialBearer)
	if !errors.Is(next.Err(), context.Canceled) {
		t.Fatalf("Start() context error = %v, want canceled", next.Err())
	}
	finish(authentication.Event{Outcome: authentication.OutcomeFailed, Failure: authentication.FailureUnavailable})

	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("span count = %d, want 1", got)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("metric data = %#v", data.ScopeMetrics)
	}
}

func TestInstrumenterIsolatesRuntimeObserverPanics(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	realMeterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		MeterProvider: &errorMeterProvider{meter: panicCounterMeter{
			Meter: realMeterProvider.Meter("test"),
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
	finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated, Duration: time.Millisecond})

	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("span count = %d, want 1 after metric panic", got)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 1 ||
		data.ScopeMetrics[0].Metrics[0].Name != "authentication.duration" {
		t.Fatalf("metric data = %#v, want duration after counter panic", data.ScopeMetrics)
	}
}

func TestInstrumenterIsolatesDurationObserverPanics(t *testing.T) {
	t.Parallel()

	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	realMeterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		MeterProvider: &errorMeterProvider{meter: panicHistogramMeter{
			Meter: realMeterProvider.Meter("test"),
		}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
	finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated, Duration: time.Millisecond})

	if got := len(recorder.Ended()); got != 1 {
		t.Fatalf("span count = %d, want 1 after duration panic", got)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 1 ||
		data.ScopeMetrics[0].Metrics[0].Name != "authentication.attempts" {
		t.Fatalf("metric data = %#v, want attempts after duration panic", data.ScopeMetrics)
	}
}

func TestInstrumenterIsolatesEverySpanObserverPanic(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"attributes", "status", "end"} {
		t.Run(operation, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			reader := sdkmetric.NewManualReader()
			var attempts atomic.Int64
			baseProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			instrumenter, err := authotel.New(authotel.Config{
				TracerProvider: spanPanicTracerProvider{
					TracerProvider: baseProvider,
					operation:      operation,
					attempts:       &attempts,
				},
				MeterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
			finish(authentication.Event{
				Outcome: authentication.OutcomeFailed,
				Failure: authentication.FailureRejected,
			})

			if attempts.Load() != 1 {
				t.Fatalf("%s attempts = %d, want 1", operation, attempts.Load())
			}
			var data metricdata.ResourceMetrics
			if err := reader.Collect(context.Background(), &data); err != nil {
				t.Fatalf("Collect() error = %v", err)
			}
			if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
				t.Fatalf("metric data after %s panic = %#v", operation, data.ScopeMetrics)
			}
			wantEnded := 1
			if operation == "end" {
				wantEnded = 0
			}
			if got := len(recorder.Ended()); got != wantEnded {
				t.Fatalf("ended spans after %s panic = %d, want %d", operation, got, wantEnded)
			}
		})
	}
}

func TestNewRejectsMissingProviders(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	tests := []authotel.Config{
		{},
		{TracerProvider: sdktrace.NewTracerProvider()},
		{TracerProvider: tracenoop.NewTracerProvider()},
		{MeterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))},
	}
	for _, config := range tests {
		if _, err := authotel.New(config); !errors.Is(err, authentication.ErrInvalidConfiguration) ||
			!strings.Contains(err.Error(), "missing OpenTelemetry provider") {
			t.Fatalf("New(%+v) error = %v", config, err)
		}
	}
	var typedNil *errorMeterProvider
	if _, err := authotel.New(authotel.Config{
		TracerProvider: sdktrace.NewTracerProvider(),
		MeterProvider:  typedNil,
	}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(typed nil provider) error = %v", err)
	}
	var typedNilTracer *panicTracerProvider
	if _, err := authotel.New(authotel.Config{
		TracerProvider: typedNilTracer,
		MeterProvider:  metricnoop.NewMeterProvider(),
	}); !errors.Is(err, authentication.ErrInvalidConfiguration) {
		t.Fatalf("New(typed nil tracer provider) error = %v", err)
	}
}

func TestNewReportsInstrumentConstructionFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("instrument construction failed with secret-token")
	base := metricnoop.NewMeterProvider().Meter("test")
	tests := []struct {
		meter errorMeter
		want  string
	}{
		{meter: errorMeter{Meter: base, counterErr: want}, want: "attempts instrument failure"},
		{meter: errorMeter{Meter: base, histogramErr: want}, want: "duration instrument failure"},
	}
	for _, tt := range tests {
		_, err := authotel.New(authotel.Config{
			TracerProvider: sdktrace.NewTracerProvider(),
			MeterProvider:  &errorMeterProvider{meter: tt.meter},
		})
		if !errors.Is(err, authentication.ErrInvalidConfiguration) ||
			!strings.Contains(err.Error(), tt.want) {
			t.Fatalf("New() error = %v, want sanitized %q", err, tt.want)
		}
		if errors.Is(err, want) || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("New() error disclosed provider failure: %v", err)
		}
	}
}

func TestNewContainsProviderPanicsWithoutDisclosingThem(t *testing.T) {
	t.Parallel()

	secret := "secret-token-provider-panic"
	tests := []authotel.Config{
		{
			TracerProvider: tracenoop.NewTracerProvider(),
			MeterProvider:  panicMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), value: secret},
		},
		{
			TracerProvider: panicTracerProvider{TracerProvider: tracenoop.NewTracerProvider(), value: secret},
			MeterProvider:  metricnoop.NewMeterProvider(),
		},
	}
	for _, config := range tests {
		_, err := authotel.New(config)
		if !errors.Is(err, authentication.ErrInvalidConfiguration) {
			t.Fatalf("New() error = %v, want invalid configuration", err)
		}
		if !strings.Contains(err.Error(), "OpenTelemetry provider failure") {
			t.Fatalf("New() error = %v, want provider failure", err)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("New() error disclosed panic value: %v", err)
		}
	}
}

func TestStartContainsTracerPanicsAndPreservesContext(t *testing.T) {
	t.Parallel()

	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: startPanicTracerProvider{TracerProvider: tracenoop.NewTracerProvider()},
		MeterProvider:  metricnoop.NewMeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "preserved")
	next, finish := instrumenter.Start(ctx, authentication.CredentialBearer)
	if next.Value(contextKey{}) != "preserved" {
		t.Fatal("Start() did not preserve context after tracer panic")
	}
	if finish == nil {
		t.Fatal("Start() returned nil completion after tracer panic")
	}
	finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
}

func TestStartPreservesContextWhenTracerReturnsNil(t *testing.T) {
	t.Parallel()

	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: nilContextTracerProvider{TracerProvider: tracenoop.NewTracerProvider()},
		MeterProvider:  metricnoop.NewMeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "preserved")
	next, finish := instrumenter.Start(ctx, authentication.CredentialBearer)
	if next == nil || next.Value(contextKey{}) != "preserved" {
		t.Fatal("Start() did not preserve context after a hostile tracer returned nil")
	}
	finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
}

func FuzzInstrumenterLifecycle(f *testing.F) {
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracenoop.NewTracerProvider(),
		MeterProvider:  metricnoop.NewMeterProvider(),
	})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	f.Add("bearer", "authenticated", "", int64(0))
	f.Add("unknown", "failed", "rejected", int64(-1))

	type contextKey struct{}
	f.Fuzz(func(
		t *testing.T,
		kind string,
		outcome string,
		failure string,
		durationNanoseconds int64,
	) {
		if len(kind) > 256 || len(outcome) > 256 || len(failure) > 256 {
			t.Skip()
		}

		expectedContextValue := kind + outcome + failure
		ctx := context.WithValue(
			context.Background(),
			contextKey{},
			expectedContextValue,
		)
		next, finish := instrumenter.Start(
			ctx,
			authentication.CredentialKind(kind),
		)
		if next == nil || next.Value(contextKey{}) != expectedContextValue {
			t.Fatal("Start() did not preserve the caller context")
		}
		if finish == nil {
			t.Fatal("Start() returned a nil completion callback")
		}
		finish(authentication.Event{
			Outcome:  authentication.Outcome(outcome),
			Failure:  authentication.FailureKind(failure),
			Duration: time.Duration(durationNanoseconds),
		})
	})
}

type errorMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

type panicMeterProvider struct {
	metric.MeterProvider
	value string
}

func (p panicMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	panic(p.value)
}

type panicTracerProvider struct {
	trace.TracerProvider
	value string
}

func (p panicTracerProvider) Tracer(string, ...trace.TracerOption) trace.Tracer {
	panic(p.value)
}

type startPanicTracerProvider struct{ trace.TracerProvider }

func (startPanicTracerProvider) Tracer(string, ...trace.TracerOption) trace.Tracer {
	return panicStartTracer{Tracer: tracenoop.NewTracerProvider().Tracer("test")}
}

type panicStartTracer struct{ trace.Tracer }

func (panicStartTracer) Start(context.Context, string, ...trace.SpanStartOption) (context.Context, trace.Span) {
	panic("secret-token-tracer-panic")
}

type nilContextTracerProvider struct{ trace.TracerProvider }

func (p nilContextTracerProvider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	return nilContextTracer{Tracer: p.TracerProvider.Tracer(name, options...)}
}

type nilContextTracer struct{ trace.Tracer }

func (t nilContextTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	_, span := t.Tracer.Start(ctx, name, options...)
	return nil, span
}

type spanPanicTracerProvider struct {
	trace.TracerProvider
	operation string
	attempts  *atomic.Int64
}

type blockingEndTracerProvider struct {
	trace.TracerProvider
	entered chan<- struct{}
	release <-chan struct{}
}

func (p blockingEndTracerProvider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	return blockingEndTracer{
		Tracer:  p.TracerProvider.Tracer(name, options...),
		entered: p.entered,
		release: p.release,
	}
}

type blockingEndTracer struct {
	trace.Tracer
	entered chan<- struct{}
	release <-chan struct{}
}

func (t blockingEndTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	next, span := t.Tracer.Start(ctx, name, options...)
	wrapped := blockingEndSpan{Span: span, entered: t.entered, release: t.release}
	return trace.ContextWithSpan(next, wrapped), wrapped
}

type blockingEndSpan struct {
	trace.Span
	entered chan<- struct{}
	release <-chan struct{}
}

func (s blockingEndSpan) End(options ...trace.SpanEndOption) {
	s.entered <- struct{}{}
	<-s.release
	s.Span.End(options...)
}

func (p spanPanicTracerProvider) Tracer(name string, options ...trace.TracerOption) trace.Tracer {
	return spanPanicTracer{
		Tracer:    p.TracerProvider.Tracer(name, options...),
		operation: p.operation,
		attempts:  p.attempts,
	}
}

type spanPanicTracer struct {
	trace.Tracer
	operation string
	attempts  *atomic.Int64
}

func (t spanPanicTracer) Start(
	ctx context.Context,
	name string,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	next, span := t.Tracer.Start(ctx, name, options...)
	wrapped := spanPanic{Span: span, operation: t.operation, attempts: t.attempts}
	return trace.ContextWithSpan(next, wrapped), wrapped
}

type spanPanic struct {
	trace.Span
	operation string
	attempts  *atomic.Int64
}

func (s spanPanic) SetAttributes(values ...attribute.KeyValue) {
	if s.operation == "attributes" {
		panic("secret-token-span-attributes")
	}
	s.Span.SetAttributes(values...)
}

func (s spanPanic) SetStatus(code codes.Code, description string) {
	if s.operation == "status" {
		panic("secret-token-span-status")
	}
	s.Span.SetStatus(code, description)
}

func (s spanPanic) End(options ...trace.SpanEndOption) {
	s.attempts.Add(1)
	if s.operation == "end" {
		panic("secret-token-span-end")
	}
	s.Span.End(options...)
}

func (p *errorMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return p.meter
}

type errorMeter struct {
	metric.Meter
	counterErr   error
	histogramErr error
}

type panicCounterMeter struct{ metric.Meter }

func (m panicCounterMeter) Int64Counter(
	string,
	...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	return panicCounter{Int64Counter: mustCounter(m.Meter)}, nil
}

type panicCounter struct{ metric.Int64Counter }

func (panicCounter) Add(context.Context, int64, ...metric.AddOption) {
	panic("observer exposed secret-token")
}

type panicHistogramMeter struct{ metric.Meter }

func (m panicHistogramMeter) Float64Histogram(
	string,
	...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	return panicHistogram{Float64Histogram: mustHistogram(m.Meter)}, nil
}

type panicHistogram struct{ metric.Float64Histogram }

func (panicHistogram) Record(context.Context, float64, ...metric.RecordOption) {
	panic("observer exposed secret-token")
}

func mustCounter(meter metric.Meter) metric.Int64Counter {
	counter, err := meter.Int64Counter("unused")
	if err != nil {
		panic(err)
	}
	return counter
}

func mustHistogram(meter metric.Meter) metric.Float64Histogram {
	histogram, err := meter.Float64Histogram("unused")
	if err != nil {
		panic(err)
	}
	return histogram
}

func (m errorMeter) Int64Counter(
	string,
	...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}
	return m.Meter.Int64Counter("authentication.attempts")
}

func (m errorMeter) Float64Histogram(
	string,
	...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	if m.histogramErr != nil {
		return nil, m.histogramErr
	}
	return m.Meter.Float64Histogram("authentication.duration")
}

func hasAttribute(attributes []attribute.KeyValue, key, value string) bool {
	for _, candidate := range attributes {
		if string(candidate.Key) == key && candidate.Value.AsString() == value {
			return true
		}
	}
	return false
}

func metricContainsAttribute(measurement metricdata.Metrics, value string) bool {
	var attributes []attribute.KeyValue
	switch points := measurement.Data.(type) {
	case metricdata.Sum[int64]:
		for _, point := range points.DataPoints {
			attributes = append(attributes, point.Attributes.ToSlice()...)
		}
	case metricdata.Histogram[float64]:
		for _, point := range points.DataPoints {
			attributes = append(attributes, point.Attributes.ToSlice()...)
		}
	}
	for _, candidate := range attributes {
		if strings.Contains(candidate.Value.String(), value) {
			return true
		}
	}
	return false
}
