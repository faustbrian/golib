package gotelemetry

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/kafka"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func FuzzKafkaPropagationBoundaries(f *testing.F) {
	f.Add(
		"traceparent",
		"00-00000000000000000000000000000001-0000000000000002-01",
		"payload",
	)
	instrumentation := newKafkaTestInstrumentationForFuzz(
		f,
		propagation.TraceContext{},
	)
	publisher, err := instrumentation.WrapKafkaPublisher(
		&recordingKafkaPublisher{},
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if err != nil {
		f.Fatalf("WrapKafkaPublisher() error = %v", err)
	}
	handler, err := instrumentation.WrapKafkaHandler(
		kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
			return nil
		}),
		KafkaPropagationConfig{Limits: tinyKafkaLimits()},
	)
	if err != nil {
		f.Fatalf("WrapKafkaHandler() error = %v", err)
	}

	f.Fuzz(func(t *testing.T, key, value, payload string) {
		if len(key) > 256 || len(value) > 256 || len(payload) > 256 {
			t.Skip()
		}
		headerValue := []byte(value)
		message := kafka.Message{
			Topic:   "events",
			Value:   []byte(payload),
			Headers: []kafka.Header{{Key: key, Value: headerValue}},
		}
		publishErr := publisher.Publish(context.Background(), message)
		if publishErr != nil &&
			!errors.Is(publishErr, ErrKafkaPropagationRejected) {
			t.Fatalf("Publish() error = %v", publishErr)
		}
		if string(message.Headers[0].Value) != value {
			t.Fatal("Publish() mutated caller-owned header")
		}
		if err := handler.Handle(context.Background(), kafka.ConsumedMessage{
			Topic:   "events",
			Value:   []byte(payload),
			Headers: []kafka.Header{{Key: key, Value: headerValue}},
		}); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if string(headerValue) != value {
			t.Fatal("Handle() mutated caller-owned header")
		}
	})
}

func newKafkaTestInstrumentationForFuzz(
	f *testing.F,
	propagator propagation.TextMapPropagator,
) *Instrumentation {
	f.Helper()
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagator,
	})
	if err != nil {
		f.Fatalf("New() error = %v", err)
	}
	return instrumentation
}
