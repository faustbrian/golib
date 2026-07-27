package gotelemetry_test

import (
	"context"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	gotelemetry "github.com/faustbrian/golib/pkg/kafka/adapters/gotelemetry"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func Example() {
	instrumentation, err := gotelemetry.New(gotelemetry.Config{
		Runtime: exampleRuntime{},
		Attributes: gotelemetry.AttributePolicy{
			AllowedClientIDs: []string{"orders-producer"},
			AllowedTopics:    []string{"orders"},
		},
	})
	if err != nil {
		panic(err)
	}

	_ = instrumentation.Observer()(context.Background(), kafka.Observation{
		Kind:        kafka.ObservationProduceRecord,
		StartedAt:   time.Now(),
		Duration:    time.Millisecond,
		ClientID:    "orders-producer",
		Topic:       "orders",
		RecordCount: 1,
		Succeeded:   true,
	})
}

type exampleRuntime struct{}

func (exampleRuntime) TracerProvider() trace.TracerProvider {
	return tracenoop.NewTracerProvider()
}

func (exampleRuntime) MeterProvider() metric.MeterProvider {
	return metricnoop.NewMeterProvider()
}
