package gotelemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestObserverDefinesNoOpSampledOutAndShutdownOutcomes(t *testing.T) {
	t.Parallel()

	observation := validLifecycleObservation()
	t.Run("no-op providers", func(t *testing.T) {
		t.Parallel()

		instrumentation, err := New(Config{Runtime: completeTestRuntime()})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("Observer() changed Kafka outcome: %v", err)
		}
	})

	t.Run("sampled-out traces retain metrics", func(t *testing.T) {
		t.Parallel()

		spans := tracetest.NewSpanRecorder()
		reader := sdkmetric.NewManualReader()
		instrumentation, err := New(Config{Runtime: testRuntime{
			tracerProvider: sdktrace.NewTracerProvider(
				sdktrace.WithSampler(sdktrace.NeverSample()),
				sdktrace.WithSpanProcessor(spans),
			),
			meterProvider: sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		}})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("Observer() changed Kafka outcome: %v", err)
		}
		if ended := spans.Ended(); len(ended) != 0 {
			t.Fatalf("sampled-out spans = %d, want 0", len(ended))
		}
		var metrics metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &metrics); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		assertIntCounter(t, metrics, "kafka.client.operations", 1, map[string]any{
			"kafka.operation": "producer.shutdown",
		})
	})

	t.Run("shutdown providers", func(t *testing.T) {
		t.Parallel()

		tracerProvider := sdktrace.NewTracerProvider()
		meterProvider := sdkmetric.NewMeterProvider()
		instrumentation, err := New(Config{Runtime: testRuntime{
			tracerProvider: tracerProvider,
			meterProvider:  meterProvider,
		}})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Fatalf("TracerProvider.Shutdown() error = %v", err)
		}
		if err := meterProvider.Shutdown(context.Background()); err != nil {
			t.Fatalf("MeterProvider.Shutdown() error = %v", err)
		}
		if err := instrumentation.Observer()(context.Background(), observation); err != nil {
			t.Fatalf("Observer() changed Kafka outcome after SDK shutdown: %v", err)
		}
	})
}

func TestObserverCooperatesWithProviderBackpressureCancellation(t *testing.T) {
	t.Parallel()

	processor := &cancelableStartProcessor{entered: make(chan struct{})}
	instrumentation, err := New(Config{Runtime: testRuntime{
		tracerProvider: sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(processor),
		),
		meterProvider: metricnoop.NewMeterProvider(),
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- instrumentation.Observer()(ctx, validLifecycleObservation())
	}()
	select {
	case <-processor.entered:
	case err := <-result:
		t.Fatalf("Observer() returned before provider callback: %v", err)
	case <-time.After(time.Second):
		t.Fatal("provider callback was not entered")
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Observer() changed Kafka outcome: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cooperative provider did not release the observer deadline")
	}
}

func TestObserverAndConstructionRemainRaceSafeDuringProviderLifecycle(t *testing.T) {
	t.Parallel()

	tracerProvider := sdktrace.NewTracerProvider()
	meterProvider := sdkmetric.NewMeterProvider()
	instrumentation, err := New(Config{Runtime: testRuntime{
		tracerProvider: tracerProvider,
		meterProvider:  meterProvider,
	}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var group sync.WaitGroup
	failures := make(chan error, 65)
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			failures <- instrumentation.Observer()(
				context.Background(),
				validLifecycleObservation(),
			)
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			failures <- err
			return
		}
		failures <- meterProvider.Shutdown(context.Background())
	}()
	group.Wait()
	close(failures)
	for failure := range failures {
		if failure != nil {
			t.Fatalf("provider lifecycle changed Kafka outcome: %v", failure)
		}
	}

	cause := errors.New("provider rejected instrument")
	base := metricnoop.NewMeterProvider().Meter("test")
	failing := Config{Runtime: failingRuntime{meter: failingMeterProvider{
		MeterProvider: metricnoop.NewMeterProvider(),
		meter: failingMeter{
			Meter: base, failName: "kafka.client.operations", cause: cause,
		},
	}}}
	group = sync.WaitGroup{}
	failures = make(chan error, 64)
	for range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, constructErr := New(failing)
			if !errors.Is(constructErr, ErrInstrumentCreation) ||
				!errors.Is(constructErr, cause) {
				failures <- constructErr
				return
			}
			failures <- nil
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		if failure != nil {
			t.Fatalf("concurrent New() error = %v", failure)
		}
	}
}

func validLifecycleObservation() kafka.Observation {
	return kafka.Observation{
		Kind:      kafka.ObservationProducerShutdown,
		StartedAt: time.Unix(1, 0),
		Duration:  time.Millisecond,
		Succeeded: true,
	}
}

type cancelableStartProcessor struct {
	entered chan struct{}
	once    sync.Once
}

func (processor *cancelableStartProcessor) OnStart(
	ctx context.Context,
	_ sdktrace.ReadWriteSpan,
) {
	processor.once.Do(func() { close(processor.entered) })
	<-ctx.Done()
}

func (*cancelableStartProcessor) OnEnd(sdktrace.ReadOnlySpan) {}

func (*cancelableStartProcessor) Shutdown(context.Context) error { return nil }

func (*cancelableStartProcessor) ForceFlush(context.Context) error { return nil }

var _ sdktrace.SpanProcessor = (*cancelableStartProcessor)(nil)
