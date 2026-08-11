package gotelemetry

import (
	"context"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

var (
	benchmarkPropagatedRecord kafka.ProducerRecord
	benchmarkExtractedContext context.Context
)

func BenchmarkObserver(b *testing.B) {
	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	observation := kafka.Observation{
		Kind:           kafka.ObservationProduceRecord,
		StartedAt:      time.Unix(1, 0),
		Duration:       time.Millisecond,
		Partition:      1,
		PartitionKnown: true,
		Offset:         42,
		OffsetKnown:    true,
		RecordCount:    1,
		RecordBytes:    128,
		Succeeded:      true,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := observer(ctx, observation); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkObserverModes(b *testing.B) {
	observation := kafka.Observation{
		Kind:        kafka.ObservationProduceRecord,
		StartedAt:   time.Unix(1, 0),
		Duration:    time.Millisecond,
		ClientID:    "checkout-producer",
		Topic:       "orders",
		RecordCount: 1,
		RecordBytes: 128,
		Succeeded:   true,
	}
	type mode struct {
		name       string
		runtime    Runtime
		attributes AttributePolicy
	}
	sampledOut := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)
	recording := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	b.Cleanup(func() {
		_ = sampledOut.Shutdown(context.Background())
		_ = recording.Shutdown(context.Background())
	})
	modes := []mode{
		{
			name: "no-op",
			runtime: testRuntime{
				tracerProvider: tracenoop.NewTracerProvider(),
				meterProvider:  metricnoop.NewMeterProvider(),
			},
		},
		{
			name: "sampled-out",
			runtime: testRuntime{
				tracerProvider: sampledOut,
				meterProvider:  metricnoop.NewMeterProvider(),
			},
		},
		{
			name: "allowlist-hit",
			runtime: testRuntime{
				tracerProvider: tracenoop.NewTracerProvider(),
				meterProvider:  metricnoop.NewMeterProvider(),
			},
			attributes: AttributePolicy{
				AllowedClientIDs: []string{"checkout-producer"},
				AllowedTopics:    []string{"orders"},
			},
		},
		{
			name: "recording",
			runtime: testRuntime{
				tracerProvider: recording,
				meterProvider:  metricnoop.NewMeterProvider(),
			},
		},
	}
	for _, current := range modes {
		b.Run(current.name, func(b *testing.B) {
			instrumentation, err := New(Config{
				Runtime: current.runtime, Attributes: current.attributes,
			})
			if err != nil {
				b.Fatalf("New() error = %v", err)
			}
			observer := instrumentation.Observer()
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := observer(ctx, observation); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkTraceContextPropagation(b *testing.B) {
	policy, err := NewTraceContextPropagation(kafka.DefaultMessageLimits())
	if err != nil {
		b.Fatalf("NewTraceContextPropagation() error = %v", err)
	}
	ctx := trace.ContextWithSpanContext(
		context.Background(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
			SpanID:  trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24},
		}),
	)
	record := kafka.ProducerRecord{
		Topic: "orders.v1",
		Key:   []byte("order-1"),
		Value: []byte("payload"),
		Headers: []kafka.Header{
			{Key: "content-type", Value: []byte("application/octet-stream")},
		},
	}
	propagated, err := policy.Inject(ctx, record)
	if err != nil {
		b.Fatalf("Inject() setup error = %v", err)
	}
	consumed := kafka.ConsumedRecord{
		Topic: propagated.Topic, Key: propagated.Key, Value: propagated.Value,
		Headers: propagated.Headers,
	}

	b.Run("inject", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchmarkPropagatedRecord, err = policy.Inject(ctx, record)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("extract", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchmarkExtractedContext, err = policy.Extract(context.Background(), consumed)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
