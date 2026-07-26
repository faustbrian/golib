package metric

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestOptionsEnforceAttributeAndCardinalityBudgets(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	options, err := Options(Config{
		CardinalityLimit: 3,
		Views: []ViewConfig{{
			Name:              "http.server.requests",
			AllowedAttributes: []attribute.Key{"http.request.method"},
		}},
	})
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}
	providerOptions := append([]sdkmetric.Option{sdkmetric.WithReader(reader)}, options...)
	provider := sdkmetric.NewMeterProvider(providerOptions...)
	counter, err := provider.Meter("test").Int64Counter("http.server.requests")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}
	for index := range 10 {
		counter.Add(
			context.Background(),
			1,
			metric.WithAttributes(
				attribute.String("http.request.method", fmt.Sprintf("METHOD-%d", index)),
				attribute.String("user.id", fmt.Sprintf("secret-%d", index)),
			),
		)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	points := data.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64]).DataPoints
	if len(points) != 3 {
		t.Fatalf("data points = %d, want bounded cardinality of 3", len(points))
	}
	for _, point := range points {
		if _, present := point.Attributes.Value(attribute.Key("user.id")); present {
			t.Fatal("disallowed user.id attribute was recorded")
		}
	}
}

func TestOptionsRejectInvalidContracts(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{CardinalityLimit: 0},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: ""}}},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: "latency", Boundaries: []float64{1, 1}}}},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: "latency", Boundaries: []float64{2, 1}}}},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: "latency", Boundaries: []float64{math.NaN()}}}},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: "latency", Boundaries: []float64{math.Inf(1)}}}},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: "latency", Unit: strings.Repeat("u", 64)}}},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: "latency", AllowedAttributes: []attribute.Key{""}}}},
		{CardinalityLimit: 10, Views: []ViewConfig{{Name: "latency", AllowedAttributes: []attribute.Key{"route", "route"}}}},
	}
	for _, config := range tests {
		if _, err := Options(config); err == nil {
			t.Fatalf("Options(%+v) error = nil, want validation error", config)
		}
	}
}

func TestOptionsConstructExplicitHistogramView(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	options, err := Options(Config{
		CardinalityLimit: 10,
		Views: []ViewConfig{{
			Name:       "request.duration",
			Unit:       "s",
			Boundaries: []float64{0.1, 0.5, 1},
			NoMinMax:   true,
		}},
	})
	if err != nil {
		t.Fatalf("Options() error = %v", err)
	}
	providerOptions := append([]sdkmetric.Option{sdkmetric.WithReader(reader)}, options...)
	provider := sdkmetric.NewMeterProvider(providerOptions...)
	histogram, err := provider.Meter("test").Float64Histogram("request.duration", metric.WithUnit("s"))
	if err != nil {
		t.Fatalf("Float64Histogram() error = %v", err)
	}
	histogram.Record(context.Background(), 0.3)
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	histogramData := data.ScopeMetrics[0].Metrics[0].Data.(metricdata.Histogram[float64])
	if got := histogramData.DataPoints[0].Bounds; len(got) != 3 || got[1] != 0.5 {
		t.Fatalf("histogram bounds = %v, want configured boundaries", got)
	}
}
