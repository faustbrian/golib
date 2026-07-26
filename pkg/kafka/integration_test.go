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
	explicitTopic := topic + "-explicit"
	settlementTopic := topic + "-settlement"
	membershipTopic := topic + "-membership"
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:  brokers,
		ClientID: "golib-compatibility-producer",
		AllowedTopics: []string{
			topic, explicitTopic, settlementTopic, membershipTopic,
		},
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
	createIntegrationTopic(t, ctx, brokers, explicitTopic, 4)
	createIntegrationTopic(t, ctx, brokers, settlementTopic, 2)
	createIntegrationTopic(t, ctx, brokers, membershipTopic, 2)
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

	provePartitionSettlement(t, ctx, brokers, producer, settlementTopic)
	proveMembershipPolicy(t, ctx, brokers, producer, membershipTopic)
}

func proveMembershipPolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	publish := func(partition int32, value string) {
		t.Helper()
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic: topic, Partition: kafka.ExplicitPartition(partition),
			Key: []byte(value), Value: []byte(value),
		})
		if result.Err != nil {
			t.Fatalf("publish membership fixture: %v", result.Err)
		}
	}
	publish(0, "first")
	publish(1, "second")

	const groupID = "golib-compatibility-static-member"
	first := consumeMembershipValues(t, ctx, brokers, topic, groupID, 2)
	slices.Sort(first)
	if !slices.Equal(first, []string{"first", "second"}) {
		t.Fatalf("first static membership values = %q", first)
	}

	publish(0, "third")
	second := consumeMembershipValues(t, ctx, brokers, topic, groupID, 1)
	if !slices.Equal(second, []string{"third"}) {
		t.Fatalf("restarted static membership values = %q", second)
	}
}

func consumeMembershipValues(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	count int,
) []string {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:           brokers,
		ClientID:          groupID,
		GroupID:           groupID,
		InstanceID:        "static-member-01",
		Rack:              "integration-rack",
		Topics:            []string{topic},
		ResetOffset:       kafka.OffsetEarliest,
		BalancePolicy:     kafka.BalanceEagerSticky,
		MaxPollRecords:    10,
		SessionTimeout:    10 * time.Second,
		RebalanceTimeout:  10 * time.Second,
		HeartbeatInterval: time.Second,
		HandlerTimeout:    10 * time.Second,
		CommitTimeout:     10 * time.Second,
		DialTimeout:       10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct membership consumer: %v", err)
	}
	defer consumer.Close()

	values := make([]string, 0, count)
	for len(values) < count {
		result, runErr := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			message kafka.ConsumedMessage,
		) error {
			values = append(values, string(message.Value))

			return nil
		}))
		if runErr != nil {
			t.Fatalf("consume membership fixture: %v", runErr)
		}
		if result.Processed != result.Committed {
			t.Fatalf("membership consume result = %#v", result)
		}
		if result.Polled == 0 && ctx.Err() != nil {
			t.Fatalf("consume membership fixture: %v", ctx.Err())
		}
	}

	return values
}

func provePartitionSettlement(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
) {
	t.Helper()

	for _, record := range []kafka.ProducerRecord{
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-ok")},
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-fail")},
		{Topic: topic, Partition: kafka.ExplicitPartition(0), Key: []byte("p0"), Value: []byte("p0-skipped")},
		{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-first")},
		{Topic: topic, Partition: kafka.ExplicitPartition(1), Key: []byte("p1"), Value: []byte("p1-second")},
	} {
		if result := producer.PublishRecord(ctx, record); result.Err != nil {
			t.Fatalf("publish settlement fixture: %v", result.Err)
		}
	}

	const groupID = "golib-compatibility-partition-settlement"
	consumer := newIntegrationConsumer(t, brokers, topic, groupID)
	defer consumer.Close()
	var handled []string
	for {
		result, err := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			message kafka.ConsumedMessage,
		) error {
			handled = append(handled, string(message.Value))
			if string(message.Value) == "p0-fail" {
				return errors.New("partition zero failed")
			}

			return nil
		}))
		if result.Polled == 0 && err == nil {
			continue
		}
		if err == nil || result != (kafka.PollResult{Polled: 5, Processed: 3, Committed: 3}) {
			t.Fatalf("partition settlement result/error = %#v/%v", result, err)
		}
		break
	}
	slices.Sort(handled)
	if !slices.Equal(handled, []string{"p0-fail", "p0-ok", "p1-first", "p1-second"}) {
		t.Fatalf("partition settlement handled values = %q", handled)
	}
	assertPartitionCommits(t, ctx, brokers, topic, groupID, map[int32]int64{0: 1, 1: 2})
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

func assertPartitionCommits(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	want map[int32]int64,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-partition-settlement-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct partition settlement inspector: %v", err)
	}
	defer inspector.Close()
	groups, err := inspector.ConsumerGroupLag(ctx, groupID)
	if err != nil {
		t.Fatalf("inspect partition settlement offsets: %v", err)
	}
	if len(groups) != 1 || len(groups[0].Partitions) != len(want) {
		t.Fatalf("partition settlement group state = %#v", groups)
	}
	for _, partition := range groups[0].Partitions {
		if partition.Topic != topic ||
			partition.CommittedOffset != want[partition.Partition] {
			t.Fatalf("partition settlement group state = %#v", groups[0])
		}
		delete(want, partition.Partition)
	}
	if len(want) != 0 {
		t.Fatalf("partition settlement missing offsets = %#v", want)
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
