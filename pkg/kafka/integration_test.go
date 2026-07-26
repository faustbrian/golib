//go:build integration

package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const integrationKafkaImage = "confluentinc/confluent-local:7.5.0@" +
	"sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd"

func TestKafkaProducerConsumerCompatibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tckafka.Run(
		ctx,
		integrationKafkaImage,
	)
	if err != nil {
		t.Fatalf("start Kafka: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate Kafka: %v", err)
		}
	})

	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve Kafka brokers: %v", err)
	}
	topic := fmt.Sprintf("golib-compatibility-%d", time.Now().UnixNano())
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                brokers,
		ClientID:               "golib-compatibility-producer",
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionZstd},
		Security:               kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct producer: %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := producer.Health(ctx); err != nil {
		t.Fatalf("check Kafka health: %v", err)
	}
	createIntegrationTopic(t, ctx, brokers, topic, 1)
	explicitTopic := topic + "-explicit"
	createIntegrationTopic(t, ctx, brokers, explicitTopic, 4)
	explicitResult := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic:     explicitTopic,
		Partition: kafka.ExplicitPartition(3),
		Key:       []byte("aggregate-explicit"),
		Value:     []byte("explicit"),
	})
	if explicitResult.Err != nil || explicitResult.Partition != 3 {
		t.Fatalf("explicit partition delivery = %#v", explicitResult)
	}

	for index, value := range []string{"first", "second", "third"} {
		err := producer.Publish(ctx, kafka.Message{
			Topic: topic,
			Key:   []byte("aggregate-1"),
			Value: []byte(value),
			Headers: []kafka.Header{{
				Key:   "event-index",
				Value: []byte(fmt.Sprintf("%d", index)),
			}},
		})
		if err != nil {
			t.Fatalf("publish message %d: %v", index, err)
		}
	}

	values := consumeValues(
		t,
		ctx,
		brokers,
		topic,
		"golib-compatibility-success",
		3,
	)
	if !slices.Equal(values, []string{"first", "second", "third"}) {
		t.Fatalf("consumed values = %q", values)
	}
	assertGroupCommitted(
		t,
		ctx,
		brokers,
		topic,
		"golib-compatibility-success",
	)

	failedConsumer := newIntegrationConsumer(
		t,
		brokers,
		topic,
		"golib-compatibility-retry",
	)
	processingFailure := errors.New("injected processing failure")
	func() {
		defer failedConsumer.Close()
		for {
			result, err := failedConsumer.RunOnce(
				ctx,
				kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					return processingFailure
				}),
			)
			if result.Polled == 0 && err == nil {
				continue
			}
			if !errors.Is(err, processingFailure) ||
				result.Committed != 0 {
				t.Fatalf(
					"failed delivery result = %#v, error = %v",
					result,
					err,
				)
			}
			break
		}
	}()

	retried := consumeValues(
		t,
		ctx,
		brokers,
		topic,
		"golib-compatibility-retry",
		3,
	)
	if !slices.Equal(retried, values) {
		t.Fatalf("retried values = %q, want %q", retried, values)
	}
}

func consumeValues(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	count int,
) []string {
	t.Helper()

	consumer := newIntegrationConsumer(t, brokers, topic, groupID)
	defer consumer.Close()

	values := make([]string, 0, count)
	for len(values) < count {
		result, err := consumer.RunOnce(
			ctx,
			kafka.HandlerFunc(func(
				_ context.Context,
				message kafka.ConsumedMessage,
			) error {
				if string(message.Key) != "aggregate-1" {
					return fmt.Errorf("unexpected key %q", message.Key)
				}
				if len(message.Headers) != 1 ||
					message.Headers[0].Key != "event-index" ||
					string(message.Headers[0].Value) !=
						fmt.Sprintf("%d", len(values)) {
					return fmt.Errorf("unexpected headers %#v", message.Headers)
				}
				values = append(values, string(message.Value))

				return nil
			}),
		)
		if err != nil {
			t.Fatalf("consume messages: %v", err)
		}
		if result.Processed != result.Polled ||
			result.Committed != result.Processed {
			t.Fatalf("consume result = %#v", result)
		}
		if result.Polled == 0 && ctx.Err() != nil {
			t.Fatalf("consume messages: %v", ctx.Err())
		}
	}

	return values
}

func newIntegrationConsumer(
	t *testing.T,
	brokers []string,
	topic string,
	groupID string,
) *kafka.Consumer {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:           brokers,
		ClientID:          groupID,
		GroupID:           groupID,
		Topics:            []string{topic},
		ResetOffset:       kafka.OffsetEarliest,
		MaxPollRecords:    10,
		FetchMaxWait:      100 * time.Millisecond,
		SessionTimeout:    10 * time.Second,
		RebalanceTimeout:  10 * time.Second,
		HeartbeatInterval: time.Second,
		HandlerTimeout:    10 * time.Second,
		CommitTimeout:     10 * time.Second,
		DialTimeout:       10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}

	return consumer
}

func assertGroupCommitted(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct inspector: %v", err)
	}
	defer inspector.Close()
	groups, err := inspector.ConsumerGroupLag(ctx, groupID)
	if err != nil {
		t.Fatalf("inspect committed group offset: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Partitions) != 1 {
		t.Fatalf("committed group state = %#v", groups)
	}
	partition := groups[0].Partitions[0]
	if groups[0].Group != groupID ||
		partition.Topic != topic ||
		partition.CommittedOffset != 3 ||
		partition.EndOffset != 3 ||
		partition.Lag != 0 {
		t.Fatalf("committed group state = %#v", groups[0])
	}
}

func createIntegrationTopic(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct Kafka administrator: %v", err)
	}
	defer client.Close()

	responses, err := kadm.NewClient(client).CreateTopics(
		ctx,
		partitions,
		1,
		nil,
		topic,
	)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists {
		t.Fatalf("create topic response omitted %q", topic)
	}
	if response.Err != nil {
		t.Fatalf("create topic %q: %v", topic, response.Err)
	}
}
