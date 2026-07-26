package otel_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	cache "github.com/faustbrian/golib/pkg/cache"
	otelobserver "github.com/faustbrian/golib/pkg/cache/observability/otel"
)

func TestObserverRecordsRedactedLowCardinalityMetrics(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	observer, err := otelobserver.New(provider.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := observer.Observe(ctx, cache.Event{
		Operation: cache.OperationGet,
		Outcome:   cache.OutcomeHit,
		Duration:  1500 * time.Microsecond,
		Size:      42,
	}); err != nil {
		t.Fatal(err)
	}
	if err := observer.Observe(ctx, cache.Event{
		Operation: cache.OperationEvict,
		Outcome:   cache.OutcomeEvicted,
		Size:      128,
	}); err != nil {
		t.Fatal(err)
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &collected); err != nil {
		t.Fatal(err)
	}
	metrics := metricMap(collected)
	operations, ok := metrics["cache.operations"].(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("cache.operations missing or wrong type: %#v", metrics["cache.operations"])
	}
	if !hasCounterPoint(operations, cache.OperationGet, cache.OutcomeHit, 1) ||
		!hasCounterPoint(operations, cache.OperationEvict, cache.OutcomeEvicted, 1) {
		t.Fatalf("operation points missing: %#v", operations.DataPoints)
	}
	duration, ok := metrics["cache.operation.duration"].(metricdata.Histogram[float64])
	if !ok || len(duration.DataPoints) == 0 || duration.DataPoints[0].Count == 0 {
		t.Fatalf("duration histogram missing: %#v", metrics["cache.operation.duration"])
	}
	valueSize, ok := metrics["cache.value.size"].(metricdata.Histogram[int64])
	if !ok || len(valueSize.DataPoints) != 1 || valueSize.DataPoints[0].Sum != 42 {
		t.Fatalf("value size histogram mismatch: %#v", metrics["cache.value.size"])
	}
	memorySize, ok := metrics["cache.memory.size"].(metricdata.Gauge[int64])
	if !ok || len(memorySize.DataPoints) != 1 || memorySize.DataPoints[0].Value != 128 {
		t.Fatalf("memory size gauge mismatch: %#v", metrics["cache.memory.size"])
	}
}

func TestObserverRejectsUnboundedAttributes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	observer, err := otelobserver.New(provider.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	if err := observer.Observe(context.Background(), cache.Event{
		Operation: cache.Operation("customer@example.com"),
		Outcome:   cache.OutcomeHit,
	}); err == nil {
		t.Fatal("expected arbitrary operation label to be rejected")
	}
	if err := observer.Observe(context.Background(), cache.Event{
		Operation: cache.OperationGet,
		Outcome:   cache.Outcome("tenant-42"),
	}); err == nil {
		t.Fatal("expected arbitrary outcome label to be rejected")
	}
}

func TestNewRejectsNilMeter(t *testing.T) {
	t.Parallel()

	if _, err := otelobserver.New(nil); err == nil {
		t.Fatal("expected nil meter error")
	}
}

func TestNewPropagatesInstrumentCreationFailures(t *testing.T) {
	t.Parallel()

	provider := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	for _, instrument := range []string{
		"cache.operations",
		"cache.operation.duration",
		"cache.value.size",
		"cache.memory.size",
	} {
		t.Run(instrument, func(t *testing.T) {
			_, err := otelobserver.New(failingMeter{Meter: provider.Meter("test"), instrument: instrument})
			if err == nil || !strings.Contains(err.Error(), "instrument unavailable") {
				t.Fatalf("New returned %v", err)
			}
		})
	}
}

func TestNewAllowsCompatibleDuplicateInstrumentation(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	meter := provider.Meter("test")
	first, err := otelobserver.New(meter)
	if err != nil {
		t.Fatal(err)
	}
	second, err := otelobserver.New(meter)
	if err != nil {
		t.Fatal(err)
	}
	event := cache.Event{Operation: cache.OperationGet, Outcome: cache.OutcomeHit}
	if err := first.Observe(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := second.Observe(t.Context(), event); err != nil {
		t.Fatal(err)
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatal(err)
	}
	operations, ok := metricMap(collected)["cache.operations"].(metricdata.Sum[int64])
	if !ok || !hasCounterPoint(operations, cache.OperationGet, cache.OutcomeHit, 2) {
		t.Fatalf("duplicate observers did not share compatible instruments: %#v", operations)
	}
}

func BenchmarkObserver(b *testing.B) {
	provider := sdkmetric.NewMeterProvider()
	b.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	observer, err := otelobserver.New(provider.Meter("benchmark"))
	if err != nil {
		b.Fatal(err)
	}
	event := cache.Event{
		Operation: cache.OperationGet,
		Outcome:   cache.OutcomeHit,
		Duration:  time.Millisecond,
		Size:      128,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := observer.Observe(context.Background(), event); err != nil {
			b.Fatal(err)
		}
	}
}

type failingMeter struct {
	metric.Meter
	instrument string
}

func (m failingMeter) Int64Counter(name string, options ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if name == m.instrument {
		return nil, errors.New("instrument unavailable")
	}
	return m.Meter.Int64Counter(name, options...)
}

func (m failingMeter) Float64Histogram(name string, options ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	if name == m.instrument {
		return nil, errors.New("instrument unavailable")
	}
	return m.Meter.Float64Histogram(name, options...)
}

func (m failingMeter) Int64Histogram(name string, options ...metric.Int64HistogramOption) (metric.Int64Histogram, error) {
	if name == m.instrument {
		return nil, errors.New("instrument unavailable")
	}
	return m.Meter.Int64Histogram(name, options...)
}

func (m failingMeter) Int64Gauge(name string, options ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	if name == m.instrument {
		return nil, errors.New("instrument unavailable")
	}
	return m.Meter.Int64Gauge(name, options...)
}

func metricMap(resource metricdata.ResourceMetrics) map[string]metricdata.Aggregation {
	metrics := make(map[string]metricdata.Aggregation)
	for _, scope := range resource.ScopeMetrics {
		for _, metric := range scope.Metrics {
			metrics[metric.Name] = metric.Data
		}
	}
	return metrics
}

func hasCounterPoint(sum metricdata.Sum[int64], operation cache.Operation, outcome cache.Outcome, value int64) bool {
	for _, point := range sum.DataPoints {
		if point.Value == value && hasAttribute(point.Attributes.ToSlice(), "cache.operation", string(operation)) &&
			hasAttribute(point.Attributes.ToSlice(), "cache.outcome", string(outcome)) {
			return true
		}
	}
	return false
}

func hasAttribute(attributes []attribute.KeyValue, key string, value string) bool {
	for _, item := range attributes {
		if string(item.Key) == key && item.Value.AsString() == value {
			return true
		}
	}
	return false
}
