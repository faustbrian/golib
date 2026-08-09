//go:build integration

package gokafka_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const integrationKafkaImage = "confluentinc/confluent-local:7.5.0@" +
	"sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd"

func TestPublisherPreservesOrderAndMakesRetryDuplicatesObservableInKafka(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tckafka.Run(ctx, integrationKafkaImage)
	if container != nil {
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Minute)
			defer cleanupCancel()
			if err := container.Terminate(cleanupCtx); err != nil {
				t.Errorf("terminate Kafka container: %v", err)
			}
		})
	}
	if err != nil {
		t.Fatalf("start Kafka: %v", err)
	}
	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve Kafka brokers: %v", err)
	}
	topic := fmt.Sprintf("outbox-publisher-%d", time.Now().UnixNano())
	createIntegrationTopic(t, ctx, brokers, topic)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "outbox-publisher-integration",
		AllowedTopics: []string{topic},
		Limits:        gokafka.DefaultLimits().Kafka,
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct producer: %v", err)
	}
	t.Cleanup(func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close producer: %v", err)
		}
	})
	publisher, err := gokafka.New(producer)
	if err != nil {
		t.Fatalf("construct publisher: %v", err)
	}

	envelopes := []outbox.Envelope{
		{
			ID: "event-1", Topic: topic, Payload: []byte(`{"sequence":1}`),
			PayloadVersion: 2, OrderingKey: "stream-1", IdempotencyKey: "command-1",
			Metadata: map[string]string{"z-last": "z", "a-first": "a"},
		},
		{
			ID: "event-2", Topic: topic, Payload: []byte(`{"sequence":2}`),
			PayloadVersion: 2, OrderingKey: "stream-1", IdempotencyKey: "command-2",
		},
	}
	for _, envelope := range envelopes {
		if err := publisher.Publish(ctx, envelope); err != nil {
			t.Fatalf("publish envelope: %v", err)
		}
	}
	// This repeats an already acknowledged envelope, matching relay recovery
	// after a crash before the separate outbox state transition.
	if err := publisher.Publish(ctx, envelopes[0]); err != nil {
		t.Fatalf("republish acknowledged envelope: %v", err)
	}

	records := consumeIntegrationRecords(t, ctx, brokers, topic, 3)
	if got := []string{string(records[0].Value), string(records[1].Value), string(records[2].Value)}; !slices.Equal(got, []string{`{"sequence":1}`, `{"sequence":2}`, `{"sequence":1}`}) {
		t.Fatalf("record order = %v", got)
	}
	for _, record := range records {
		if string(record.Key) != "stream-1" {
			t.Fatalf("record key = %q", record.Key)
		}
	}
	if got := integrationHeaderKeys(records[0].Headers); !slices.Equal(got, []string{
		"content-type", "event-id", "schema-version", "idempotency-key", "a-first", "z-last",
	}) {
		t.Fatalf("record headers = %v", got)
	}
	if string(headerValue(records[0].Headers, "event-id")) != "event-1" ||
		string(headerValue(records[2].Headers, "event-id")) != "event-1" {
		t.Fatalf("duplicate event identity was not stable")
	}
}

func createIntegrationTopic(t *testing.T, ctx context.Context, brokers []string, topic string) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct Kafka administrator: %v", err)
	}
	defer client.Close()
	responses, err := kadm.NewClient(client).CreateTopics(ctx, 1, 1, nil, topic)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists || response.Err != nil {
		t.Fatalf("create topic response = %#v", response)
	}
}

func consumeIntegrationRecords(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	count int,
) []kafka.ConsumedMessage {
	t.Helper()
	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers: brokers, ClientID: "outbox-publisher-consumer",
		GroupID: "outbox-publisher-consumer", Topics: []string{topic},
		ResetOffset: kafka.OffsetEarliest, MaxPollRecords: count,
		FetchMaxWait: 100 * time.Millisecond, SessionTimeout: 10 * time.Second,
		RebalanceTimeout: 10 * time.Second, HeartbeatInterval: time.Second,
		HandlerTimeout: 3 * time.Second, CommitTimeout: 2 * time.Second,
		DialTimeout: 10 * time.Second, Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}
	defer consumer.Close()
	records := make([]kafka.ConsumedMessage, 0, count)
	handler := kafka.HandlerFunc(func(_ context.Context, record kafka.ConsumedMessage) error {
		records = append(records, record.Retain())
		return nil
	})
	for len(records) < count {
		if _, err := consumer.RunOnce(ctx, handler); err != nil {
			t.Fatalf("consume records: %v", err)
		}
		if ctx.Err() != nil {
			t.Fatalf("consume records: %v", ctx.Err())
		}
	}

	return records
}

func integrationHeaderKeys(headers []kafka.Header) []string {
	keys := make([]string, len(headers))
	for index, header := range headers {
		keys[index] = header.Key
	}

	return keys
}
