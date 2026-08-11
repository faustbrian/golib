package outboxotel_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/otel"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func ExampleTelemetry_WrapPublisher() {
	instrumentation, err := outboxotel.New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagation.TraceContext{},
	})
	if err != nil {
		panic(err)
	}
	publisher, err := instrumentation.WrapPublisher(examplePublisher{})
	if err != nil {
		panic(err)
	}

	err = publisher.Publish(context.Background(), outbox.Envelope{})
	fmt.Println(err)
	// Output: <nil>
}

type examplePublisher struct{}

func (examplePublisher) Publish(context.Context, outbox.Envelope) error { return nil }
