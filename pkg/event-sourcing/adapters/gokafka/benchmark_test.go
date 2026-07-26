package gokafka

import (
	"context"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/kafka"
)

func BenchmarkRecordCodecRoundTrip(b *testing.B) {
	codec := testRecordCodec(b)
	record := testEncodedRecord(b, codec)
	consumed := consumedRecord(record)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		delivery, err := codec.Decode(consumed)
		if err != nil {
			b.Fatalf("decode record: %v", err)
		}
		if _, err := codec.Encode(delivery); err != nil {
			b.Fatalf("encode delivery: %v", err)
		}
	}
}

func BenchmarkDispatcher(b *testing.B) {
	codec := testRecordCodec(b)
	dispatcher, err := NewDispatcher(discardPublisher{}, codec)
	if err != nil {
		b.Fatalf("construct dispatcher: %v", err)
	}
	delivery, err := eventsourcing.NewDelivery(
		testMessage(b),
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		b.Fatalf("construct delivery: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := dispatcher.Dispatch(
			context.Background(),
			[]eventsourcing.Delivery{delivery},
		); err != nil {
			b.Fatalf("dispatch delivery: %v", err)
		}
	}
}

func BenchmarkRecordHandler(b *testing.B) {
	codec := testRecordCodec(b)
	handler, err := NewRecordHandler(codec, discardDeliveryConsumer{})
	if err != nil {
		b.Fatalf("construct record handler: %v", err)
	}
	record := consumedRecord(
		encodedLiveRecord(b, codec, testMessage(b)),
	)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := handler.Handle(context.Background(), record); err != nil {
			b.Fatalf("handle record: %v", err)
		}
	}
}

type discardPublisher struct{}

func (discardPublisher) Publish(context.Context, kafka.Message) error {
	return nil
}

type discardDeliveryConsumer struct{}

func (discardDeliveryConsumer) Consume(
	context.Context,
	eventsourcing.Delivery,
) error {
	return nil
}
