package gotelemetry_test

import (
	"context"
	"fmt"
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

func ExampleTraceContextPropagation() {
	policy, err := gotelemetry.NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		panic(err)
	}
	producerContext := trace.ContextWithSpanContext(
		context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SpanID:  trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24},
		}),
	)
	produced, err := policy.Inject(
		producerContext,
		kafka.ProducerRecord{Topic: "orders.v1", Key: []byte("order-1")},
	)
	if err != nil {
		panic(err)
	}
	consumedContext, err := policy.Extract(
		context.Background(),
		kafka.ConsumedRecord{Topic: produced.Topic, Headers: produced.Headers},
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(trace.SpanContextFromContext(consumedContext).IsRemote())
	// Output: true
}

type exampleRuntime struct{}

func (exampleRuntime) TracerProvider() trace.TracerProvider {
	return tracenoop.NewTracerProvider()
}

func (exampleRuntime) MeterProvider() metric.MeterProvider {
	return metricnoop.NewMeterProvider()
}
