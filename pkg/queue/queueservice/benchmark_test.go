package queueservice

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func BenchmarkProducerPublish(b *testing.B) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		b.Fatal(err)
	}
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name:        "benchmark-producer",
		Resource:    &producerResource{},
		Correlation: factory,
		Publish: func(
			context.Context,
			*producerResource,
			core.QueuedMessage,
			...job.AllowOption,
		) error {
			return nil
		},
	})
	if err != nil {
		b.Fatal(err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		b.Fatal(err)
	}
	ctx := producerContext()
	message := queuedPayload("benchmark-payload")
	options := []job.AllowOption{{Metadata: &job.Metadata{
		JobType: "benchmark",
		Tags:    map[string]string{"source": "benchmark"},
	}}}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err = producer.Publish(ctx, message, options...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProducerDrain(b *testing.B) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		producer, producerErr := NewProducer(ProducerOptions[int]{
			Name:        "benchmark-producer",
			Resource:    1,
			Correlation: factory,
			Publish: func(
				context.Context,
				int,
				core.QueuedMessage,
				...job.AllowOption,
			) error {
				return nil
			},
			Shutdown: func(context.Context, int) error { return nil },
		})
		if producerErr != nil {
			b.Fatal(producerErr)
		}
		component := producer.Component()
		if producerErr = component.Start(context.Background()); producerErr != nil {
			b.Fatal(producerErr)
		}
		b.StartTimer()
		if producerErr = component.Stop(context.Background()); producerErr != nil {
			b.Fatal(producerErr)
		}
	}
}
