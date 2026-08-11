package goqueue

import (
	"context"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func BenchmarkCodecEncode(b *testing.B) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		b.Fatal(err)
	}
	delivery := minimalQueueDelivery(b)

	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Encode(delivery); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCodecDecode(b *testing.B) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		b.Fatal(err)
	}
	delivery := minimalQueueDelivery(b)
	encoded, err := codec.Encode(delivery)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := codec.Decode(encoded); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDispatcherOverhead(b *testing.B) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		b.Fatal(err)
	}
	dispatcher, err := NewDispatcher(DispatcherConfig{
		Queue: benchmarkQueue{},
		Codec: codec,
	})
	if err != nil {
		b.Fatal(err)
	}
	delivery := minimalQueueDelivery(b)
	deliveries := []eventsourcing.Delivery{delivery}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if err := dispatcher.Dispatch(ctx, deliveries); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTaskHandlerOverhead(b *testing.B) {
	codec, err := NewCodec(CodecConfig{})
	if err != nil {
		b.Fatal(err)
	}
	encoded, err := codec.Encode(minimalQueueDelivery(b))
	if err != nil {
		b.Fatal(err)
	}
	handler, err := NewTaskHandler(
		codec,
		func(context.Context, eventsourcing.Delivery) error { return nil },
	)
	if err != nil {
		b.Fatal(err)
	}
	task := &settlementTask{body: encoded}
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		if err := handler.Handle(ctx, task); err != nil {
			b.Fatal(err)
		}
	}
}

type benchmarkQueue struct{}

func (benchmarkQueue) Queue(
	message core.QueuedMessage,
	_ ...job.AllowOption,
) error {
	benchmarkPayloadBytes = len(message.Bytes())
	return nil
}

var benchmarkPayloadBytes int
