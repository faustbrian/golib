package kafkaservice_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

func TestSupportedIdleAdaptersStopWithoutWaitingForWork(t *testing.T) {
	t.Run("producer", func(t *testing.T) {
		var shutdownCalls atomic.Int32
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[struct{}]{
				Name: "producer", Resource: struct{}{}, Correlation: mustFactory(t, "producer"),
				Publish: func(
					context.Context,
					struct{},
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					return kafka.DeliveryResult{}, nil
				},
				Shutdown: func(context.Context, struct{}) error {
					shutdownCalls.Add(1)

					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		component := producer.Component()
		if err = component.Start(context.Background()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err = component.Stop(ctx); err != nil {
			panic("supported producer stop did not complete within its bounded context")
		}
		if calls := shutdownCalls.Load(); calls != 1 {
			t.Fatalf("shutdown calls = %d, want 1", calls)
		}
	})

	t.Run("consumer", func(t *testing.T) {
		var shutdownCalls atomic.Int32
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[struct{}]{
				Name: "consumer", Resource: struct{}{}, Correlation: mustFactory(t, "consumer"),
				Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
					return nil
				}),
				Run: func(context.Context, struct{}, kafka.Handler) error { return nil },
				Shutdown: func(context.Context, struct{}) error {
					shutdownCalls.Add(1)

					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewConsumer() error = %v", err)
		}
		component := consumer.Plan().Components[0]
		if err = component.Start(context.Background()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err = component.Stop(ctx); err != nil {
			panic("supported consumer stop did not complete within its bounded context")
		}
		if calls := shutdownCalls.Load(); calls != 1 {
			t.Fatalf("shutdown calls = %d, want 1", calls)
		}
	})
}

func TestProducerStopCompletesAfterAdmittedPublicationReturns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name: "producer", Resource: struct{}{}, Correlation: mustFactory(t, "child"),
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				close(started)
				<-release

				return kafka.DeliveryResult{}, nil
			},
			Shutdown: func(context.Context, struct{}) error { return nil },
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	parent, startErr := mustFactory(t, "parent", "request").Start()
	if startErr != nil {
		t.Fatalf("Start() correlation error = %v", startErr)
	}
	publishResult := make(chan error, 1)
	go func() {
		_, _, publishErr := producer.Publish(
			correlation.WithValues(context.Background(), parent),
			kafka.ProducerRecord{Topic: "orders"},
		)
		publishResult <- publishErr
	}()
	select {
	case <-started:
	case publishErr := <-publishResult:
		t.Fatalf("Publish() returned before invoking the admitted callback: %v", publishErr)
	case <-time.After(time.Second):
		t.Fatal("Publish() did not invoke or return from the admitted callback")
	}
	if err = component.CloseAdmission(); err != nil {
		t.Fatalf("CloseAdmission() error = %v", err)
	}
	canceledCtx, cancelCanceled := context.WithCancel(context.Background())
	cancelCanceled()
	if err = component.Stop(canceledCtx); !errors.Is(err, context.Canceled) {
		panic("producer canceled stop did not preserve its bounded context")
	}
	close(release)
	if err = <-publishResult; err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err = component.Stop(stopCtx); err != nil {
		panic("producer stop did not complete after admitted publication returned")
	}
}

func TestConsumerStopCompletesAfterAdmittedRunReturns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	defer closeIfOpen(release)
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[struct{}]{
			Name: "consumer", Resource: struct{}{}, Correlation: mustFactory(t, "delivery"),
			Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
				return nil
			}),
			Run: func(context.Context, struct{}, kafka.Handler) error {
				close(started)
				<-release

				return nil
			},
			Shutdown: func(context.Context, struct{}) error { return nil },
		},
	)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	plan := consumer.Plan()
	component := plan.Components[0]
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- plan.Tasks[0].Run(context.Background()) }()
	select {
	case <-started:
	case runErr := <-runResult:
		t.Fatalf("Run() returned before invoking the admitted callback: %v", runErr)
	case <-time.After(time.Second):
		t.Fatal("Run() did not invoke or return from the admitted callback")
	}
	if err = component.CloseAdmission(); err != nil {
		t.Fatalf("CloseAdmission() error = %v", err)
	}
	canceledCtx, cancelCanceled := context.WithCancel(context.Background())
	cancelCanceled()
	if err = component.Stop(canceledCtx); !errors.Is(err, context.Canceled) {
		panic("consumer canceled stop did not preserve its bounded context")
	}
	close(release)
	if err = <-runResult; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	defer cancelStop()
	if err = component.Stop(stopCtx); err != nil {
		panic("consumer stop did not complete after admitted run returned")
	}
}
