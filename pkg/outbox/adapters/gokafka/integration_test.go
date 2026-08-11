//go:build integration

package gokafka_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
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
			if container == nil {
				return
			}
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

	producerConfig := kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "outbox-publisher-integration",
		AllowedTopics: []string{topic},
		Limits:        gokafka.DefaultLimits().Kafka,
		Security:      kafka.DevelopmentPlaintextSecurity(),
		RecordRetries: 5, RetryBackoffMin: 25 * time.Millisecond,
		RetryBackoffMax: 100 * time.Millisecond, DeliveryTimeout: 10 * time.Second,
		ShutdownTimeout: 11 * time.Second, RequestTimeout: time.Second,
		DialTimeout: time.Second, Linger: time.Millisecond,
	}
	producer, err := kafka.NewProducer(producerConfig)
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

	stopTimeout := 10 * time.Second
	if err := container.Stop(ctx, &stopTimeout); err != nil {
		t.Fatalf("stop Kafka: %v", err)
	}
	failedCtx, cancelFailed := context.WithTimeout(ctx, 1500*time.Millisecond)
	failure := publisher.Publish(failedCtx, outbox.Envelope{
		ID: "event-during-restart", Topic: topic, PayloadVersion: 2,
		OrderingKey: "stream-1", Payload: []byte(`{"sequence":3}`),
	})
	cancelFailed()
	if failure == nil {
		t.Fatal("publish while Kafka was stopped succeeded")
	}
	var categorized interface{ Category() kafka.ErrorCategory }
	if !errors.As(failure, &categorized) {
		t.Fatalf("publish while Kafka was stopped was not categorized: %v", failure)
	}
	if !slices.Contains(
		[]kafka.ErrorCategory{kafka.ErrorRetryable, kafka.ErrorAmbiguous},
		categorized.Category(),
	) {
		t.Fatalf("publish while Kafka was stopped category = %v", categorized.Category())
	}
	for _, broker := range brokers {
		if strings.Contains(failure.Error(), broker) {
			t.Fatalf("restart diagnostic disclosed broker endpoint: %v", failure)
		}
	}
	if err := container.Terminate(ctx); err != nil {
		t.Fatalf("terminate stopped Kafka: %v", err)
	}
	container = nil
	container, err = tckafka.Run(ctx, integrationKafkaImage)
	if err != nil {
		t.Fatalf("start replacement Kafka: %v", err)
	}
	brokers, err = container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve restarted Kafka brokers: %v", err)
	}
	waitForKafkaBroker(t, ctx, brokers, 45*time.Second)
	recoveredTopic := fmt.Sprintf("outbox-publisher-recovered-%d", time.Now().UnixNano())
	createIntegrationTopic(t, ctx, brokers, recoveredTopic)
	producerConfig.Brokers = brokers
	producerConfig.AllowedTopics = []string{recoveredTopic}
	if err := producer.Close(); err != nil {
		t.Fatalf("close producer after broker restart: %v", err)
	}
	producer, err = kafka.NewProducer(producerConfig)
	if err != nil {
		t.Fatalf("reconstruct producer after broker restart: %v", err)
	}
	publisher, err = gokafka.New(producer)
	if err != nil {
		t.Fatalf("reconstruct publisher after broker restart: %v", err)
	}
	waitForPublisherHealth(t, ctx, publisher, 45*time.Second)
	if err := publisher.Publish(ctx, outbox.Envelope{
		ID: "event-after-restart", Topic: recoveredTopic, PayloadVersion: 2,
		OrderingKey: "stream-1", Payload: []byte(`{"sequence":4}`),
	}); err != nil {
		t.Fatalf("publish after Kafka restart: %v", err)
	}
	recovered := consumeIntegrationRecords(t, ctx, brokers, recoveredTopic, 1)
	if string(recovered[0].Value) != `{"sequence":4}` ||
		string(recovered[0].Key) != "stream-1" {
		t.Fatalf("record after restart = %#v", recovered[0])
	}
}

func waitForKafkaBroker(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	timeout time.Duration,
) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct Kafka readiness client: %v", err)
	}
	defer client.Close()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := client.Ping(probeCtx)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Kafka broker: %v", ctx.Err())
		case <-deadline.C:
			t.Fatal("wait for Kafka broker: timeout")
		case <-ticker.C:
		}
	}
}

func waitForPublisherHealth(
	t *testing.T,
	ctx context.Context,
	publisher *gokafka.Publisher,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		probeCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := publisher.Health(probeCtx)
		cancel()
		if err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Kafka health: %v", ctx.Err())
		case <-deadline.C:
			t.Fatal("wait for Kafka health: timeout")
		case <-ticker.C:
		}
	}
}

func createIntegrationTopic(t *testing.T, ctx context.Context, brokers []string, topic string) {
	t.Helper()
	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct Kafka administrator: %v", err)
	}
	defer client.Close()
	minimumInSyncReplicas := "1"
	responses, err := kadm.NewClient(client).CreateTopics(ctx, 1, 1, map[string]*string{
		"min.insync.replicas": &minimumInSyncReplicas,
	}, topic)
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
