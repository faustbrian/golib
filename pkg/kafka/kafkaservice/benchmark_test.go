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
