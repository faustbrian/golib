package kafkaservice_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

func BenchmarkProducerPublish(b *testing.B) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		b.Fatalf("NewFactory() error = %v", err)
	}
	parent, err := factory.Start()
	if err != nil {
		b.Fatalf("Start() correlation error = %v", err)
	}
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name:        "benchmark-producer",
			Resource:    struct{}{},
			Correlation: factory,
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
		},
	)
	if err != nil {
		b.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		b.Fatalf("Start() error = %v", err)
	}
	ctx := correlation.WithValues(context.Background(), parent)
	record := kafka.ProducerRecord{
		Topic: "orders",
		Key:   []byte("customer-42"),
		Value: make([]byte, 1024),
		Headers: []kafka.Header{{
			Key: "content-type", Value: []byte("application/octet-stream"),
		}},
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(record.Value)))
	b.ResetTimer()
	for b.Loop() {
		if _, _, err = producer.Publish(ctx, record); err != nil {
			b.Fatalf("Publish() error = %v", err)
		}
	}
}

func BenchmarkLifecycleShutdown(b *testing.B) {
	b.Run("producer", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			producer, err := kafkaservice.NewProducer(
				kafkaservice.ProducerOptions[struct{}]{
					Name:        "benchmark-producer",
					Resource:    struct{}{},
					Correlation: benchmarkFactory(b),
					Publish: func(
						context.Context,
						struct{},
						kafka.ProducerRecord,
					) (kafka.DeliveryResult, error) {
						return kafka.DeliveryResult{}, nil
					},
					Shutdown: func(context.Context, struct{}) error { return nil },
				},
			)
			if err != nil {
				b.Fatalf("NewProducer() error = %v", err)
			}
			component := producer.Component()
			if err = component.Start(context.Background()); err != nil {
				b.Fatalf("Start() error = %v", err)
			}
			b.StartTimer()
			if err = component.CloseAdmission(); err != nil {
				b.Fatalf("CloseAdmission() error = %v", err)
			}
			if err = component.Stop(context.Background()); err != nil {
				b.Fatalf("Stop() error = %v", err)
			}
		}
	})

	b.Run("consumer", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			b.StopTimer()
			consumer, err := kafkaservice.NewConsumer(
				kafkaservice.ConsumerOptions[struct{}]{
					Name:        "benchmark-consumer",
					Resource:    struct{}{},
					Correlation: benchmarkFactory(b),
					Handler: kafka.HandlerFunc(func(
						context.Context,
						kafka.ConsumedMessage,
					) error {
						return nil
					}),
					Run: func(context.Context, struct{}, kafka.Handler) error {
						return nil
					},
					Shutdown: func(context.Context, struct{}) error { return nil },
				},
			)
			if err != nil {
				b.Fatalf("NewConsumer() error = %v", err)
			}
			component := consumer.Plan().Components[0]
			if err = component.Start(context.Background()); err != nil {
				b.Fatalf("Start() error = %v", err)
			}
			b.StartTimer()
			if err = component.CloseAdmission(); err != nil {
				b.Fatalf("CloseAdmission() error = %v", err)
			}
			if err = component.Stop(context.Background()); err != nil {
				b.Fatalf("Stop() error = %v", err)
			}
		}
	})
}

func benchmarkFactory(b *testing.B) *correlation.Factory {
	b.Helper()
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		b.Fatalf("NewFactory() error = %v", err)
	}

	return factory
}
