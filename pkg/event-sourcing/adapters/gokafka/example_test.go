package gokafka_test

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka"
	"github.com/faustbrian/golib/pkg/kafka"
)

func ExampleNewDispatcher() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	codec, err := gokafka.NewRecordCodec(gokafka.RecordCodecConfig{
		Resolver:      gokafka.FixedTopic("accounts.events.v1"),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		log.Fatal(err)
	}
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{"kafka.internal:9093"},
		ClientID:      "accounts-event-dispatcher",
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		log.Fatal(err)
	}
	dispatcher, err := gokafka.NewDispatcher(producer, codec)
	if err != nil {
		log.Fatal(errors.Join(err, producer.Close()))
	}
	delivery, err := liveAccountDelivery()
	if err != nil {
		log.Fatal(errors.Join(err, producer.Close()))
	}

	dispatchErr := dispatcher.Dispatch(
		ctx,
		[]eventsourcing.Delivery{delivery},
	)
	if err := errors.Join(dispatchErr, producer.Close()); err != nil {
		// A failed acknowledgement can be ambiguous. Reconcile the message ID
		// before deciding whether to dispatch it again.
		log.Fatal(err)
	}
}

func ExampleNewRecordHandler() {
	codec, err := gokafka.NewRecordCodec(gokafka.RecordCodecConfig{
		Resolver:      gokafka.FixedTopic("accounts.events.v1"),
		AllowedTopics: []string{"accounts.events.v1"},
	})
	if err != nil {
		log.Fatal(err)
	}
	handler, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(persistProjection),
	)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:       []string{"kafka.internal:9093"},
		ClientID:      "account-projection",
		GroupID:       "account-projection-v1",
		Topics:        []string{"accounts.events.v1"},
		ResetOffset:   kafka.OffsetEarliest,
		BalancePolicy: kafka.BalanceCooperativeSticky,
	})
	if err != nil {
		log.Fatal(err)
	}

	runErr := consumer.Run(ctx, handler)
	if err := errors.Join(runErr, consumer.Close()); err != nil {
		log.Fatal(err)
	}
}

func persistProjection(context.Context, eventsourcing.Delivery) error {
	// Application code must atomically persist an idempotent side effect and
	// the delivery message ID before returning nil. Only then may Kafka commit
	// the source offset.
	return nil
}

func liveAccountDelivery() (eventsourcing.Delivery, error) {
	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	event, err := eventsourcing.NewEncodedEvent(
		eventsourcing.EncodedEventInput{
			Name:        "account.opened",
			Version:     1,
			ContentType: "application/json",
			Payload:     []byte(`{"account_id":"account-42"}`),
		},
	)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	pending, err := eventsourcing.NewPendingMessage(
		eventsourcing.PendingMessageInput{
			ID:         "message-01J7Y7ZP4B6JJ6ZXXK0Q8AKM2F",
			Stream:     stream,
			Event:      event,
			RecordedAt: time.Date(2026, time.August, 10, 9, 0, 0, 0, time.UTC),
		},
	)
	if err != nil {
		return eventsourcing.Delivery{}, err
	}
	message, err := eventsourcing.NewMessage(eventsourcing.MessageInput{
		Pending:       pending,
		StreamVersion: 1,
	})
	if err != nil {
		return eventsourcing.Delivery{}, err
	}

	return eventsourcing.NewDelivery(message, eventsourcing.DeliveryLive)
}
