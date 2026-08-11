package gotelemetry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func FuzzKafkaRecordPassThrough(f *testing.F) {
	f.Add(byte(0), int32(0), int64(0), "traceparent", "value")
	f.Add(byte(1), int32(7), int64(1_786_448_394), "traceſtate", "application")
	f.Fuzz(func(t *testing.T, mode byte, partition int32, unixNano int64, key, value string) {
		if len(key) > 256 || len(value) > 256 {
			t.Skip()
		}
		selection := kafka.PartitionSelection{}
		if mode&1 == 1 {
			selection = kafka.ExplicitPartition(partition)
		}
		message := kafka.Message{
			Topic:     "events",
			Partition: selection,
			Headers:   []kafka.Header{{Key: key, Value: []byte(value)}},
			Timestamp: time.Unix(0, unixNano).UTC(),
		}
		instrumentation := newKafkaTestInstrumentationForFuzz(t, propagation.TraceContext{})
		downstream := &recordingKafkaPublisher{}
		publisher, err := instrumentation.WrapKafkaPublisher(
			downstream,
			KafkaPropagationConfig{},
		)
		if err != nil {
			t.Fatalf("WrapKafkaPublisher() error = %v", err)
		}
		if err := publisher.Publish(context.Background(), message); err != nil {
			if errors.Is(err, ErrKafkaPropagationRejected) {
				return
			}
			t.Fatalf("Publish() error = %v", err)
		}
		if downstream.message.Partition != message.Partition {
			t.Fatalf("partition = %#v, want %#v", downstream.message.Partition, message.Partition)
		}
		if !downstream.message.Timestamp.Equal(message.Timestamp) {
			t.Fatalf("timestamp = %v, want %v", downstream.message.Timestamp, message.Timestamp)
		}
	})
}

func FuzzProjectionNamesAndPositions(f *testing.F) {
	f.Add("projection", uint64(0), uint64(1))
	f.Add("tenant-derived-name", ^uint64(0), uint64(0))
	instrumentation := newKafkaTestInstrumentationForFuzz(f, propagation.TraceContext{})
	f.Fuzz(func(t *testing.T, name string, current, high uint64) {
		if len(name) > 1024 {
			t.Skip()
		}
		err := instrumentation.RecordProjectionLag(
			context.Background(),
			name,
			eventsourcing.GlobalPosition(current),
			eventsourcing.GlobalPosition(high),
		)
		if err != nil &&
			!errors.Is(err, ErrProjectionNameInvalid) &&
			!errors.Is(err, ErrProjectionLagInvalid) {
			t.Fatalf("RecordProjectionLag() error = %v", err)
		}
	})
}

func FuzzSerializationMetadataPassThrough(f *testing.F) {
	f.Add("key", "value")
	f.Add("authorization", "credential")
	instrumentation := newKafkaTestInstrumentationForFuzz(f, propagation.TraceContext{})
	encoded, err := eventsourcing.NewEncodedEvent(eventsourcing.EncodedEventInput{
		Name:        "fuzz.event",
		Version:     1,
		ContentType: "application/octet-stream",
		Payload:     []byte("payload"),
	})
	if err != nil {
		f.Fatalf("NewEncodedEvent() error = %v", err)
	}
	f.Fuzz(func(t *testing.T, key, value string) {
		if len(key) > 256 || len(value) > 1024 {
			t.Skip()
		}
		input, err := eventsourcing.NewUpcastEvent(
			encoded,
			map[string]string{key: value},
		)
		if err != nil {
			return
		}
		upcaster, err := instrumentation.WrapUpcaster(legacyTelemetryUpcaster(func(
			event eventsourcing.UpcastEvent,
		) ([]eventsourcing.UpcastEvent, error) {
			return []eventsourcing.UpcastEvent{event}, nil
		}))
		if err != nil {
			t.Fatalf("WrapUpcaster() error = %v", err)
		}
		output, err := upcaster.UpcastContext(context.Background(), input)
		if err != nil || len(output) != 1 {
			t.Fatalf("UpcastContext() = %#v, %v", output, err)
		}
		if got := output[0].Metadata()[key]; got != value {
			t.Fatalf("metadata[%q] = %q, want exact value", key, got)
		}
	})
}

func FuzzHostileProviderIsolation(f *testing.F) {
	f.Add(byte(0), "provider")
	f.Add(byte(31), "panic")
	f.Fuzz(func(t *testing.T, mode byte, privateValue string) {
		if len(privateValue) > 256 {
			t.Skip()
		}
		baseTracer := tracenoop.NewTracerProvider().Tracer("fuzz")
		var tracer trace.Tracer
		switch mode & 3 {
		case 0:
			tracer = baseTracer
		case 1:
			tracer = panickingTracer{Tracer: baseTracer, panicStart: true}
		case 2:
			tracer = panickingTracer{
				Tracer: baseTracer,
				span: panickingSpan{
					Span: trace.SpanFromContext(context.Background()),
				},
			}
		default:
			tracer = replacingContextTracer{Tracer: baseTracer}
		}
		var meterProvider metric.MeterProvider = metricnoop.NewMeterProvider()
		if mode&4 != 0 {
			meterProvider = panickingMeterProvider{
				MeterProvider: metricnoop.NewMeterProvider(),
				meter: panickingMeter{
					Meter: metricnoop.NewMeterProvider().Meter("fuzz"),
				},
			}
		}
		propagator := panickingPropagator{
			fields:       []string{"traceparent"},
			panicInject:  mode&8 != 0,
			panicExtract: mode&16 != 0,
		}
		instrumentation, err := New(testRuntime{
			tracer: panickingTracerProvider{
				TracerProvider: tracenoop.NewTracerProvider(),
				tracer:         tracer,
			},
			meter:      meterProvider,
			propagator: propagator,
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		type contextKey struct{}
		key := contextKey{}
		ctx, cancel := context.WithCancelCause(
			context.WithValue(context.Background(), key, privateValue),
		)
		cause := errors.New("caller canceled")
		cancel(cause)
		downstream := fmt.Errorf("downstream-%s", privateValue)
		dispatcher, err := instrumentation.WrapDispatcher(
			dispatcherFunc(func(got context.Context, _ []eventsourcing.Delivery) error {
				if got.Value(key) != privateValue || !errors.Is(context.Cause(got), cause) {
					t.Fatal("provider replaced caller context state")
				}
				return downstream
			}),
		)
		if err != nil {
			t.Fatalf("WrapDispatcher() error = %v", err)
		}
		if got := dispatcher.Dispatch(ctx, nil); got != downstream {
			t.Fatalf("Dispatch() error = %v, want exact %v", got, downstream)
		}

		publisher, err := instrumentation.WrapKafkaPublisher(
			&recordingKafkaPublisher{err: downstream},
			KafkaPropagationConfig{},
		)
		if err != nil {
			t.Fatalf("WrapKafkaPublisher() error = %v", err)
		}
		if got := publisher.Publish(ctx, kafka.Message{Topic: "events"}); got != downstream {
			t.Fatalf("Publish() error = %v, want exact %v", got, downstream)
		}

		handler, err := instrumentation.WrapKafkaHandler(
			kafka.HandlerFunc(func(got context.Context, _ kafka.ConsumedMessage) error {
				if got.Value(key) != privateValue || !errors.Is(context.Cause(got), cause) {
					t.Fatal("propagator replaced caller context state")
				}
				return downstream
			}),
			KafkaPropagationConfig{},
		)
		if err != nil {
			t.Fatalf("WrapKafkaHandler() error = %v", err)
		}
		if got := handler.Handle(ctx, kafka.ConsumedMessage{}); got != downstream {
			t.Fatalf("Handle() error = %v, want exact %v", got, downstream)
		}
	})
}

func newKafkaTestInstrumentationForFuzz(
	t testing.TB,
	propagator propagation.TextMapPropagator,
) *Instrumentation {
	t.Helper()
	instrumentation, err := New(testRuntime{
		tracer:     tracenoop.NewTracerProvider(),
		meter:      metricnoop.NewMeterProvider(),
		propagator: propagator,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return instrumentation
}
