package kafkaservice_test

import (
	"context"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

func compileConcreteProducer(
	resource *kafka.Producer,
	factory *correlation.Factory,
) (*kafkaservice.Producer[*kafka.Producer], error) {
	return kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*kafka.Producer]{
			Name: "events-producer", Resource: resource, Correlation: factory,
			Startup: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Readiness: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Publish: func(
				ctx context.Context,
				resource *kafka.Producer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				result := resource.PublishRecord(ctx, record)

				return result, result.Err
			},
			Shutdown: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
}

func compileConcreteConsumer(
	resource *kafka.Consumer,
	factory *correlation.Factory,
	handler kafka.Handler,
) (*kafkaservice.Consumer[*kafka.Consumer], error) {
	return kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*kafka.Consumer]{
			Name: "events-consumer", Resource: resource, Correlation: factory,
			Handler: handler,
			Run: func(
				ctx context.Context,
				resource *kafka.Consumer,
				handler kafka.Handler,
			) error {
				return resource.Run(ctx, handler)
			},
			Shutdown: func(ctx context.Context, resource *kafka.Consumer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
}

var (
	_               = compileConcreteProducer
	_               = compileConcreteConsumer
	_ kafka.Handler = kafka.HandlerFunc(func(
		context.Context,
		kafka.ConsumedMessage,
	) error {
		return nil
	})
)
