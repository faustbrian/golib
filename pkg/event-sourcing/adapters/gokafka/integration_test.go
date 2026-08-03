//go:build integration

package gokafka_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	gokafka "github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka"
	"github.com/faustbrian/golib/pkg/kafka"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

const integrationKafkaImage = "confluentinc/confluent-local:7.5.0@" +
	"sha256:8e391de42cfcd3498e7317dcf159790f1f1cc3f3ffce900b30d7da23888687fd"

func TestEventDeliveriesRoundTripThroughKafka(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tckafka.Run(ctx, integrationKafkaImage)
	if container != nil {
		cleanupKafkaContainer(t, container)
	}
	if err != nil {
		t.Fatalf("start Kafka: %v", err)
	}
	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("resolve Kafka brokers: %v", err)
	}
	topic := fmt.Sprintf("event-sourcing-compatibility-%d", time.Now().UnixNano())
	deadLetterTopic := topic + ".dead-letter"
	replayTopic := topic + ".replay"
	createIntegrationTopic(t, ctx, brokers, topic)
	createIntegrationTopic(t, ctx, brokers, deadLetterTopic)
	createIntegrationTopic(t, ctx, brokers, replayTopic)

	codec, err := gokafka.NewRecordCodec(gokafka.RecordCodecConfig{
		Resolver:      gokafka.FixedTopic(topic),
		AllowedTopics: []string{topic, deadLetterTopic, replayTopic},
	})
	if err != nil {
		t.Fatalf("construct record codec: %v", err)
	}
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                brokers,
		ClientID:               "event-sourcing-compatibility-producer",
		AllowedTopics:          []string{topic, deadLetterTopic, replayTopic},
		Limits:                 gokafka.DefaultRecordLimits(),
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
	dispatcher, err := gokafka.NewDispatcher(producer, codec)
	if err != nil {
		t.Fatalf("construct dispatcher: %v", err)
	}

	deliveries := integrationDeliveries(t)
	if err := dispatcher.Dispatch(ctx, deliveries); err != nil {
		t.Fatalf("dispatch deliveries: %v", err)
	}

	handled := make([]eventsourcing.Delivery, 0, len(deliveries))
	handler, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(func(
			_ context.Context,
			delivery eventsourcing.Delivery,
		) error {
			handled = append(handled, delivery)

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct record handler: %v", err)
	}
	groupID := "event-sourcing-compatibility-consumer"
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
		HandlerTimeout:    3 * time.Second,
		CommitTimeout:     2 * time.Second,
		DialTimeout:       10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}
	for len(handled) < len(deliveries) {
		result, err := consumer.RunOnce(ctx, handler)
		if err != nil {
			consumer.Close()
			t.Fatalf("consume deliveries: %v", err)
		}
		if result.Processed != result.Polled ||
			result.Committed != result.Processed {
			consumer.Close()
			t.Fatalf("consume result = %#v", result)
		}
		if result.Polled == 0 && ctx.Err() != nil {
			consumer.Close()
			t.Fatalf("consume deliveries: %v", ctx.Err())
		}
	}
	consumer.Close()

	if !slices.EqualFunc(
		handled,
		deliveries,
		func(left, right eventsourcing.Delivery) bool {
			return left.Mode() == right.Mode() &&
				left.Message().Equal(right.Message())
		},
	) {
		t.Fatalf("handled deliveries = %#v", handled)
	}
	assertGroupCommitted(t, ctx, brokers, topic, groupID, len(deliveries))

	deadLetterPolicy, err := gokafka.NewDeadLetterPolicy(
		producer,
		gokafka.DeadLetterPolicyConfig{
			Topic:  deadLetterTopic,
			Limits: gokafka.DefaultRecordLimits(),
		},
	)
	if err != nil {
		t.Fatalf("construct dead-letter policy: %v", err)
	}
	failingHandler, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			return errors.New("injected projection failure")
		}),
		gokafka.WithFailurePolicy(deadLetterPolicy),
	)
	if err != nil {
		t.Fatalf("construct failing record handler: %v", err)
	}
	failureGroupID := "event-sourcing-compatibility-failure"
	runIntegrationConsumer(
		t,
		ctx,
		brokers,
		topic,
		failureGroupID,
		failingHandler,
		len(deliveries),
	)
	assertGroupCommitted(
		t,
		ctx,
		brokers,
		topic,
		failureGroupID,
		len(deliveries),
	)

	deadLettered := make([]eventsourcing.Delivery, 0, len(deliveries))
	deadLetterHandler, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(func(
			_ context.Context,
			delivery eventsourcing.Delivery,
		) error {
			deadLettered = append(deadLettered, delivery)

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct dead-letter record handler: %v", err)
	}
	runIntegrationConsumer(
		t,
		ctx,
		brokers,
		deadLetterTopic,
		"event-sourcing-compatibility-dead-letter",
		kafka.HandlerFunc(func(
			handlerCtx context.Context,
			record kafka.ConsumedMessage,
		) error {
			if deadLetterSourceTopic(record.Headers) != topic {
				return errors.New("dead-letter source topic is missing")
			}

			return deadLetterHandler.Handle(handlerCtx, record)
		}),
		len(deliveries),
	)
	if !slices.EqualFunc(
		deadLettered,
		deliveries,
		func(left, right eventsourcing.Delivery) bool {
			return left.Mode() == right.Mode() &&
				left.Message().Equal(right.Message())
		},
	) {
		t.Fatalf("dead-letter deliveries = %#v", deadLettered)
	}

	proveReplaySettlementPolicy(
		t,
		ctx,
		brokers,
		producer,
		replayTopic,
		deliveries[0],
	)
}

func proveReplaySettlementPolicy(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	topic string,
	live eventsourcing.Delivery,
) {
	t.Helper()

	codec, err := gokafka.NewRecordCodec(gokafka.RecordCodecConfig{
		Resolver:      gokafka.FixedTopic(topic),
		AllowedTopics: []string{topic},
	})
	if err != nil {
		t.Fatalf("construct replay record codec: %v", err)
	}
	dispatcher, err := gokafka.NewDispatcher(
		producer,
		codec,
		gokafka.AllowReplay(),
	)
	if err != nil {
		t.Fatalf("construct replay dispatcher: %v", err)
	}
	replay, err := eventsourcing.NewDelivery(
		live.Message(),
		eventsourcing.DeliveryReplay,
	)
	if err != nil {
		t.Fatalf("construct replay delivery: %v", err)
	}
	if err := dispatcher.Dispatch(ctx, []eventsourcing.Delivery{replay}); err != nil {
		t.Fatalf("dispatch replay delivery: %v", err)
	}

	groupID := "event-sourcing-compatibility-replay"
	rejectedCalls := 0
	rejected, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(func(
			context.Context,
			eventsourcing.Delivery,
		) error {
			rejectedCalls++

			return errors.New("default handler consumed replay delivery")
		}),
	)
	if err != nil {
		t.Fatalf("construct replay-rejecting handler: %v", err)
	}
	consumer := newIntegrationConsumer(t, brokers, topic, groupID)
	for {
		result, runErr := consumer.RunOnce(ctx, rejected)
		if result.Polled == 0 && runErr == nil {
			if ctx.Err() != nil {
				consumer.Close()
				t.Fatalf("consume rejected replay: %v", ctx.Err())
			}
			continue
		}
		if !errors.Is(runErr, gokafka.ErrReplayHandlingDenied) ||
			result.Committed != 0 {
			consumer.Close()
			t.Fatalf(
				"rejected replay result/error = %#v/%v",
				result,
				runErr,
			)
		}
		break
	}
	consumer.Close()
	if rejectedCalls != 0 {
		t.Fatalf("default handler replay calls = %d", rejectedCalls)
	}

	var handled eventsourcing.Delivery
	allowed, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(func(
			_ context.Context,
			delivery eventsourcing.Delivery,
		) error {
			handled = delivery

			return nil
		}),
		gokafka.AllowReplayHandling(),
	)
	if err != nil {
		t.Fatalf("construct replay handler: %v", err)
	}
	runIntegrationConsumer(t, ctx, brokers, topic, groupID, allowed, 1)
	if handled.Mode() != eventsourcing.DeliveryReplay ||
		!handled.Message().Equal(live.Message()) {
		t.Fatalf("handled replay delivery = %#v", handled)
	}
	assertGroupCommitted(t, ctx, brokers, topic, groupID, 1)
}

func integrationDeliveries(t *testing.T) []eventsourcing.Delivery {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatalf("construct stream: %v", err)
	}
	recordedAt := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	deliveries := make([]eventsourcing.Delivery, 0, 3)
	for index := range 3 {
		event, err := eventsourcing.NewEncodedEvent(
			eventsourcing.EncodedEventInput{
				Name:        "account.sequence-recorded",
				Version:     1,
				ContentType: "application/json",
				Payload: []byte(fmt.Sprintf(
					`{"sequence":%d}`,
					index+1,
				)),
			},
		)
		if err != nil {
			t.Fatalf("construct event: %v", err)
		}
		pending, err := eventsourcing.NewPendingMessage(
			eventsourcing.PendingMessageInput{
				ID:            fmt.Sprintf("message-%d", index+1),
				Stream:        stream,
				Event:         event,
				Metadata:      map[string]string{"source": "integration"},
				RecordedAt:    recordedAt.Add(time.Duration(index) * time.Second),
				CorrelationID: "correlation-1",
				CausationID:   fmt.Sprintf("command-%d", index+1),
				Tenant:        "tenant-a",
				Partition:     "region-eu",
			},
		)
		if err != nil {
			t.Fatalf("construct pending message: %v", err)
		}
		message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
			Pending:        pending,
			StreamVersion:  uint64(index + 1),
			GlobalPosition: eventsourcing.GlobalPosition(index + 1),
		})
		if err != nil {
			t.Fatalf("construct message: %v", err)
		}
		delivery, err := eventsourcing.NewDelivery(
			message,
			eventsourcing.DeliveryLive,
		)
		if err != nil {
			t.Fatalf("construct delivery: %v", err)
		}
		deliveries = append(deliveries, delivery)
	}

	return deliveries
}

func createIntegrationTopic(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...))
	if err != nil {
		t.Fatalf("construct Kafka administrator: %v", err)
	}
	defer client.Close()
	responses, err := kadm.NewClient(client).CreateTopics(
		ctx,
		1,
		1,
		nil,
		topic,
	)
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists || response.Err != nil {
		t.Fatalf("create topic response = %#v", response)
	}
}

func assertGroupCommitted(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	count int,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "event-sourcing-compatibility-inspector",
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
		partition.CommittedOffset != int64(count) ||
		partition.EndOffset != int64(count) ||
		partition.Lag != 0 {
		t.Fatalf("committed group state = %#v", groups[0])
	}
}

func runIntegrationConsumer(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	handler kafka.Handler,
	count int,
) {
	t.Helper()

	consumer := newIntegrationConsumer(t, brokers, topic, groupID)
	defer consumer.Close()

	processed := 0
	for processed < count {
		result, err := consumer.RunOnce(ctx, handler)
		if err != nil {
			t.Fatalf("consume records: %v", err)
		}
		if result.Processed != result.Polled ||
			result.Committed != result.Processed {
			t.Fatalf("consume result = %#v", result)
		}
		processed += result.Processed
		if result.Polled == 0 && ctx.Err() != nil {
			t.Fatalf("consume records: %v", ctx.Err())
		}
	}
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
		HandlerTimeout:    3 * time.Second,
		CommitTimeout:     2 * time.Second,
		DialTimeout:       10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}

	return consumer
}

func deadLetterSourceTopic(headers []kafka.Header) string {
	for _, header := range headers {
		if header.Key == gokafka.HeaderDeadLetterSourceTopic {
			return string(header.Value)
		}
	}

	return ""
}
