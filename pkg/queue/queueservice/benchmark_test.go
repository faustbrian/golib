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

func BenchmarkHandlerDelivery(b *testing.B) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		b.Fatal(err)
	}
	handler, err := NewHandler(HandlerOptions{
		Correlation: factory,
		Handler:     func(context.Context, core.TaskMessage) error { return nil },
	})
	if err != nil {
		b.Fatal(err)
	}
	message := plainTask("benchmark-delivery")

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err = handler(context.Background(), message); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLifecycleWorkerDrain(b *testing.B) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		runEntered := make(chan struct{})
		worker, workerErr := NewLifecycleWorker(LifecycleWorkerOptions[int]{
			Name: "benchmark-worker", Resource: 1, Correlation: factory,
			Handler: func(context.Context, core.TaskMessage) error { return nil },
			Run: func(ctx context.Context, _ int, _ Handler) error {
				close(runEntered)
				<-ctx.Done()

				return nil
			},
			Shutdown: func(context.Context, int) error { return nil },
		})
		if workerErr != nil {
			b.Fatal(workerErr)
		}
		plan := worker.Plan()
		if workerErr = plan.Components[0].Start(context.Background()); workerErr != nil {
			b.Fatal(workerErr)
		}
		runContext, cancelRun := context.WithCancel(context.Background())
		runResult := make(chan error, 1)
		go func() { runResult <- plan.Tasks[0].Run(runContext) }()
		<-runEntered

		b.StartTimer()
		cancelRun()
		if workerErr = <-runResult; workerErr != nil {
			b.Fatal(workerErr)
		}
		if workerErr = plan.Components[0].Stop(context.Background()); workerErr != nil {
			b.Fatal(workerErr)
		}
	}
}
