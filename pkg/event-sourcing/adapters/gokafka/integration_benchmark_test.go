//go:build integration

package gokafka_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	gokafka "github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
)

func BenchmarkDispatchBoundary(b *testing.B) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tckafka.Run(ctx, integrationKafkaImage)
	if container != nil {
		cleanupKafkaContainer(b, container)
	}
	if err != nil {
		b.Fatalf("start Kafka: %v", err)
	}
	brokers, err := container.Brokers(ctx)
	if err != nil {
		b.Fatalf("resolve Kafka brokers: %v", err)
	}
	topic := fmt.Sprintf("event-sourcing-benchmark-%d", time.Now().UnixNano())
	createIntegrationTopic(b, ctx, brokers, topic)
	codec := integrationCodec(b, topic)
	delivery := integrationDeliveries(b)[:1]
	producer := integrationProducer(b, brokers, topic)
	record, err := codec.Encode(delivery[0])
	if err != nil {
		b.Fatalf("encode benchmark delivery: %v", err)
	}

	b.Run("raw_kafka_all_in_sync_replicas", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := producer.Publish(ctx, record); err != nil {
				b.Fatalf("publish directly through Kafka: %v", err)
			}
		}
	})

	b.Run("adapter_kafka_all_in_sync_replicas", func(b *testing.B) {
		dispatcher, err := gokafka.NewDispatcher(producer, codec)
		if err != nil {
			b.Fatalf("construct Kafka dispatcher: %v", err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if err := dispatcher.Dispatch(ctx, delivery); err != nil {
				b.Fatalf("dispatch through Kafka: %v", err)
			}
		}
	})
}
