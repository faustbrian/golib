package queueservice_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/correlation"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/queueservice"
	"go.opentelemetry.io/otel/propagation"
)

type payload string

func (value payload) Bytes() []byte { return []byte(value) }

func ExampleNewWorker() {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	handled := make(chan string, 1)
	handler, err := queueservice.NewHandler(queueservice.HandlerOptions{
		Correlation:     factory,
		TrustedMetadata: true,
		TracePropagator: propagation.TraceContext{},
		Handler: func(_ context.Context, task core.TaskMessage) error {
			handled <- string(task.Payload())

			return nil
		},
	})
	if err != nil {
		return
	}
	ring := queue.NewRing(queue.WithFn(handler))
	concrete, err := queue.NewQueue(
		queue.WithWorker(ring),
		queue.WithWorkerCount(1),
	)
	if err != nil {
		return
	}
	worker, err := queueservice.NewWorker(queueservice.WorkerOptions{
		Name: "jobs-worker", Queue: concrete,
	})
	if err != nil {
		return
	}
	producer, err := queueservice.NewProducer(
		queueservice.ProducerOptions[*queue.Queue]{
			Name: "jobs-producer", Resource: concrete, Correlation: factory,
			TracePropagator: propagation.TraceContext{},
			Publish: func(
				_ context.Context,
				resource *queue.Queue,
				message core.QueuedMessage,
				options ...job.AllowOption,
			) error {
				return resource.Queue(message, options...)
			},
		},
	)
	if err != nil {
		return
	}

	workerComponent := worker.Component()
	producerComponent := producer.Component()
	if err = workerComponent.Start(context.Background()); err != nil {
		return
	}
	if err = producerComponent.Start(context.Background()); err != nil {
		return
	}
	parent, _ := factory.Start()
	if _, err = producer.Publish(
		correlation.WithValues(context.Background(), parent),
		payload("delivery"),
	); err != nil {
		return
	}
	fmt.Println(<-handled)
	if err = producerComponent.Stop(context.Background()); err != nil {
		return
	}
	if err = workerComponent.Stop(context.Background()); err != nil {
		return
	}

	// Output: delivery
}
