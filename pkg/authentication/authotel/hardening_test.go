package authotel_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	"github.com/faustbrian/golib/pkg/authentication/authotel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestCompletedCallbackReleasesRequestState(t *testing.T) {
	t.Parallel()

	requestReleased := make(chan struct{})
	instrumenterReleased := make(chan struct{})
	counterReleased := make(chan struct{})
	histogramReleased := make(chan struct{})
	finish, err := completedCallbackWithRequestState(
		requestReleased,
		instrumenterReleased,
		counterReleased,
		histogramReleased,
	)
	if err != nil {
		t.Fatalf("completedCallbackWithRequestState() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	requestDone := false
	instrumenterDone := false
	counterDone := false
	histogramDone := false
	for time.Now().Before(deadline) {
		runtime.GC()
		runtime.Gosched()
		select {
		case <-requestReleased:
			requestDone = true
		default:
		}
		select {
		case <-instrumenterReleased:
			instrumenterDone = true
		default:
		}
		select {
		case <-counterReleased:
			counterDone = true
		default:
		}
		select {
		case <-histogramReleased:
			histogramDone = true
		default:
		}
		runtime.KeepAlive(finish)
		if requestDone && instrumenterDone && counterDone && histogramDone {
			return
		}
	}
	t.Fatalf(
		"completed callback retained state while reachable: request=%t instrumenter=%t counter=%t histogram=%t",
		!requestDone,
		!instrumenterDone,
		!counterDone,
		!histogramDone,
	)
}

func completedCallbackWithRequestState(
	requestReleased chan<- struct{},
	instrumenterReleased chan<- struct{},
	counterReleased chan<- struct{},
	histogramReleased chan<- struct{},
) (func(authentication.Event), error) {
	baseMeter := metricnoop.NewMeterProvider().Meter("retention-test")
	counter, err := baseMeter.Int64Counter("retention.counter")
	if err != nil {
		return nil, err
	}
	histogram, err := baseMeter.Float64Histogram("retention.histogram")
	if err != nil {
		return nil, err
	}
	retainedCounter := &retentionCounter{Int64Counter: counter}
	retainedHistogram := &retentionHistogram{Float64Histogram: histogram}
	runtime.AddCleanup(retainedCounter, func(done chan<- struct{}) { close(done) }, counterReleased)
	runtime.AddCleanup(retainedHistogram, func(done chan<- struct{}) { close(done) }, histogramReleased)
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracenoop.NewTracerProvider(),
		MeterProvider: retentionMeterProvider{
			MeterProvider: metricnoop.NewMeterProvider(),
			meter: retentionMeter{
				Meter:     baseMeter,
				counter:   retainedCounter,
				histogram: retainedHistogram,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	runtime.AddCleanup(
		instrumenter,
		func(done chan<- struct{}) { close(done) },
		instrumenterReleased,
	)
	type contextKey struct{}
	type requestState struct {
		_ [1024]byte
	}
	state := &requestState{}
	runtime.AddCleanup(state, func(done chan<- struct{}) { close(done) }, requestReleased)
	ctx := context.WithValue(context.Background(), contextKey{}, state)
	_, finish := instrumenter.Start(ctx, authentication.CredentialBearer)
	finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
	runtime.KeepAlive(state)
	runtime.KeepAlive(instrumenter)
	return finish, nil
}

func TestCapturedTelemetryExcludesAuthenticationMaterialAndFailureText(t *testing.T) {
	t.Parallel()

	canaries := []string{
		"credential-token-canary",
		"claim-value-canary",
		"identity-subject-canary",
		"issuer-canary",
		"endpoint-canary",
		"arbitrary-error-canary",
		"secret-token-span-status",
	}
	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	want := errors.New(strings.Join(canaries[1:6], "|"))
	authenticator, err := authentication.NewInstrumented(
		authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			return authentication.AnonymousResult(), want
		}),
		instrumenter,
		fixedClock{},
	)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	gotResult, gotErr := authenticator.Authenticate(
		context.Background(),
		authentication.NewBearerCredential(canaries[0]),
	)
	if gotErr != want || gotResult.State() != authentication.ResultAnonymous {
		t.Fatalf("Authenticate() = result %#v, error %v; want original result and error", gotResult, gotErr)
	}

	var panicAttempts atomic.Int64
	panicInstrumenter, err := authotel.New(authotel.Config{
		TracerProvider: spanPanicTracerProvider{
			TracerProvider: tracerProvider,
			operation:      "status",
			attempts:       &panicAttempts,
		},
		MeterProvider: metricnoop.NewMeterProvider(),
	})
	if err != nil {
		t.Fatalf("New(panic provider) error = %v", err)
	}
	_, finish := panicInstrumenter.Start(context.Background(), authentication.CredentialBearer)
	finish(authentication.Event{Outcome: authentication.OutcomeFailed})

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	observed := strings.Join(capturedTelemetryStrings(recorder.Ended(), data), "\n")
	for _, canary := range canaries {
		if strings.Contains(observed, canary) {
			t.Fatalf("captured telemetry disclosed %q in %q", canary, observed)
		}
	}
}

func TestHighConcurrencyKeepsMetricCardinalityAndCompletionBounded(t *testing.T) {
	t.Parallel()

	const attempts = 1024
	recorder := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)),
		MeterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var group sync.WaitGroup
	for index := range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			kind := authentication.CredentialKind(fmt.Sprintf("hostile-kind-%d", index))
			outcome := authentication.Outcome(fmt.Sprintf("hostile-outcome-%d", index))
			failure := authentication.FailureKind(fmt.Sprintf("hostile-failure-%d", index))
			if index%4 == 0 {
				kind = authentication.CredentialBearer
				outcome = authentication.OutcomeFailed
				failure = authentication.FailureRejected
			}
			_, finish := instrumenter.Start(context.Background(), kind)
			finish(authentication.Event{Outcome: outcome, Failure: failure, Duration: time.Duration(index)})
			finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
		}()
	}
	group.Wait()

	if got := len(recorder.Ended()); got != attempts {
		t.Fatalf("ended spans = %d, want %d", got, attempts)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			pointCount, observationCount := metricPointCounts(measurement)
			if pointCount > 112 {
				t.Fatalf("%s point cardinality = %d, want at most 112", measurement.Name, pointCount)
			}
			if observationCount != attempts {
				t.Fatalf("%s observations = %d, want %d", measurement.Name, observationCount, attempts)
			}
		}
	}
}

func TestBoundedBatchExporterBackpressureDoesNotBlockCompletion(t *testing.T) {
	t.Parallel()

	exporter := &blockingSpanExporter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	processor := sdktrace.NewBatchSpanProcessor(
		exporter,
		sdktrace.WithMaxQueueSize(8),
		sdktrace.WithMaxExportBatchSize(1),
		sdktrace.WithBatchTimeout(time.Hour),
	)
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	defer func() {
		close(exporter.release)
		shutdownContext, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tracerProvider.Shutdown(shutdownContext)
	}()
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  metricnoop.NewMeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, firstFinish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
	firstFinish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
	select {
	case <-exporter.entered:
	case <-time.After(time.Second):
		t.Fatal("batch exporter did not enter blocked export")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1024 {
			_, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
			finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bounded non-blocking batch exporter backpressure blocked completion")
	}
}

func TestAdapterRemainsBoundedAfterCallerShutsDownSDKs(t *testing.T) {
	t.Parallel()

	tracerProvider := sdktrace.NewTracerProvider()
	meterProvider := sdkmetric.NewMeterProvider()
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  meterProvider,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := tracerProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("tracer Shutdown() error = %v", err)
	}
	if err := meterProvider.Shutdown(context.Background()); err != nil {
		t.Fatalf("meter Shutdown() error = %v", err)
	}

	started := time.Now()
	for range 1024 {
		next, finish := instrumenter.Start(context.Background(), authentication.CredentialBearer)
		if next == nil || finish == nil {
			t.Fatal("Start() returned nil after caller-owned shutdown")
		}
		finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("post-shutdown adapter use took %s, want at most 1s", elapsed)
	}
}

func TestSDKExporterErrorsDoNotChangeAuthenticationResult(t *testing.T) {
	t.Parallel()

	want := errors.New("authentication result canary")
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(
		sdktrace.NewSimpleSpanProcessor(errorSpanExporter{}),
	))
	defer func() { _ = tracerProvider.Shutdown(context.Background()) }()
	instrumenter, err := authotel.New(authotel.Config{
		TracerProvider: tracerProvider,
		MeterProvider:  metricnoop.NewMeterProvider(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	authenticator, err := authentication.NewInstrumented(
		authenticatorFunc(func(context.Context, authentication.Credential) (authentication.Result, error) {
			return authentication.AnonymousResult(), want
		}),
		instrumenter,
		fixedClock{},
	)
	if err != nil {
		t.Fatalf("NewInstrumented() error = %v", err)
	}
	gotResult, gotErr := authenticator.Authenticate(
		context.Background(),
		authentication.NewBearerCredential("credential canary"),
	)
	if gotErr != want || gotResult.State() != authentication.ResultAnonymous {
		t.Fatalf("Authenticate() = result %#v, error %v; want original result and error", gotResult, gotErr)
	}
}

func FuzzInstrumenterWithHostileProviders(f *testing.F) {
	f.Add(byte(0), "bearer", "authenticated", "", int64(0))
	f.Add(byte(5), "hostile-kind", "failed", "hostile-failure", int64(-1))

	f.Fuzz(func(t *testing.T, mode byte, kind, outcome, failure string, duration int64) {
		if len(kind) > 256 || len(outcome) > 256 || len(failure) > 256 {
			t.Skip()
		}
		config := hostileConfig(mode)
		instrumenter, err := authotel.New(config)
		if err != nil {
			if !errors.Is(err, authentication.ErrInvalidConfiguration) {
				t.Fatalf("New() error = %v", err)
			}
			return
		}
		type contextKey struct{}
		ctx := context.WithValue(context.Background(), contextKey{}, "preserved")
		next, finish := instrumenter.Start(ctx, authentication.CredentialKind(kind))
		if next == nil || next.Value(contextKey{}) != "preserved" || finish == nil {
			t.Fatal("hostile provider changed the caller context or callback contract")
		}
		finish(authentication.Event{
			Outcome:  authentication.Outcome(outcome),
			Failure:  authentication.FailureKind(failure),
			Duration: time.Duration(duration),
		})
		finish(authentication.Event{Outcome: authentication.OutcomeAuthenticated})
	})
}

type authenticatorFunc func(context.Context, authentication.Credential) (authentication.Result, error)

func (function authenticatorFunc) Authenticate(
	ctx context.Context,
	credential authentication.Credential,
) (authentication.Result, error) {
	return function(ctx, credential)
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(0, 0) }

type blockingSpanExporter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type retentionCounter struct {
	metric.Int64Counter
	_ [1024]byte
}

type retentionHistogram struct {
	metric.Float64Histogram
	_ [1024]byte
}

type retentionMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (provider retentionMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return provider.meter
}

type retentionMeter struct {
	metric.Meter
	counter   metric.Int64Counter
	histogram metric.Float64Histogram
}

func (meter retentionMeter) Int64Counter(
	string,
	...metric.Int64CounterOption,
) (metric.Int64Counter, error) {
	return meter.counter, nil
}

func (meter retentionMeter) Float64Histogram(
	string,
	...metric.Float64HistogramOption,
) (metric.Float64Histogram, error) {
	return meter.histogram, nil
}

func (exporter *blockingSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	exporter.once.Do(func() { close(exporter.entered) })
	<-exporter.release
	return nil
}

func (*blockingSpanExporter) Shutdown(context.Context) error { return nil }

type errorSpanExporter struct{}

func (errorSpanExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return errors.New("export failed")
}

func (errorSpanExporter) Shutdown(context.Context) error { return nil }

func hostileConfig(mode byte) authotel.Config {
	baseTracerProvider := tracenoop.NewTracerProvider()
	baseMeterProvider := metricnoop.NewMeterProvider()
	baseMeter := baseMeterProvider.Meter("test")
	switch mode % 8 {
	case 1:
		return authotel.Config{
			TracerProvider: startPanicTracerProvider{TracerProvider: baseTracerProvider},
			MeterProvider:  baseMeterProvider,
		}
	case 2:
		return authotel.Config{
			TracerProvider: nilContextTracerProvider{TracerProvider: baseTracerProvider},
			MeterProvider:  baseMeterProvider,
		}
	case 3:
		return authotel.Config{
			TracerProvider: spanPanicTracerProvider{
				TracerProvider: baseTracerProvider,
				operation:      "attributes",
				attempts:       &atomic.Int64{},
			},
			MeterProvider: baseMeterProvider,
		}
	case 4:
		return authotel.Config{
			TracerProvider: baseTracerProvider,
			MeterProvider: &errorMeterProvider{meter: panicCounterMeter{
				Meter: baseMeter,
			}},
		}
	case 5:
		return authotel.Config{
			TracerProvider: baseTracerProvider,
			MeterProvider: &errorMeterProvider{meter: panicHistogramMeter{
				Meter: baseMeter,
			}},
		}
	case 6:
		return authotel.Config{
			TracerProvider: panicTracerProvider{TracerProvider: baseTracerProvider, value: "panic-canary"},
			MeterProvider:  baseMeterProvider,
		}
	case 7:
		return authotel.Config{
			TracerProvider: baseTracerProvider,
			MeterProvider: panicMeterProvider{
				MeterProvider: baseMeterProvider,
				value:         "panic-canary",
			},
		}
	default:
		return authotel.Config{TracerProvider: baseTracerProvider, MeterProvider: baseMeterProvider}
	}
}

func capturedTelemetryStrings(
	spans []sdktrace.ReadOnlySpan,
	data metricdata.ResourceMetrics,
) []string {
	values := make([]string, 0, len(spans)*8)
	for _, span := range spans {
		scope := span.InstrumentationScope()
		values = append(values, span.Name(), span.Status().Description, scope.Name, scope.Version, scope.SchemaURL)
		values = appendAttributeStrings(values, span.Attributes())
	}
	for _, scopeMetrics := range data.ScopeMetrics {
		scope := scopeMetrics.Scope
		values = append(values, scope.Name, scope.Version, scope.SchemaURL)
		for _, measurement := range scopeMetrics.Metrics {
			values = append(values, measurement.Name, measurement.Description, measurement.Unit)
			switch points := measurement.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range points.DataPoints {
					values = appendAttributeStrings(values, point.Attributes.ToSlice())
				}
			case metricdata.Histogram[float64]:
				for _, point := range points.DataPoints {
					values = appendAttributeStrings(values, point.Attributes.ToSlice())
				}
			}
		}
	}
	return values
}

func appendAttributeStrings(values []string, attributes []attribute.KeyValue) []string {
	for _, candidate := range attributes {
		values = append(values, string(candidate.Key), candidate.Value.String())
	}
	return values
}

func metricPointCounts(measurement metricdata.Metrics) (points int, observations int) {
	switch data := measurement.Data.(type) {
	case metricdata.Sum[int64]:
		for _, point := range data.DataPoints {
			points++
			observations += int(point.Value)
		}
	case metricdata.Histogram[float64]:
		for _, point := range data.DataPoints {
			points++
			observations += int(point.Count)
		}
	}
	return points, observations
}

var (
	_ authentication.Authenticator = authenticatorFunc(nil)
	_ metric.MeterProvider         = (*errorMeterProvider)(nil)
	_ trace.TracerProvider         = nilContextTracerProvider{}
)
