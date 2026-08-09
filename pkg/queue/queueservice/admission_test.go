package queueservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/service"
)

func TestServiceDrainClosesProducerAdmissionBeforeShutdown(t *testing.T) {
	publishCalls := 0
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		Readiness: func(context.Context, int) error { return nil },
		Publish: func(
			context.Context,
			int,
			core.QueuedMessage,
			...job.AllowOption,
		) error {
			publishCalls++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	readiness, ok := producer.Readiness()
	if !ok {
		t.Fatal("Readiness() was absent")
	}
	runtime, err := service.New(service.Config{
		Components: []service.Component{producer.Component()},
	})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err = producer.Publish(
		producerContext(),
		queuedPayload("before-drain"),
	); err != nil {
		t.Fatalf("Publish() before drain error = %v", err)
	}
	if err = runtime.Drain(); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if err = readiness.Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Readiness() after drain error = %v, want ErrUnavailable", err)
	}
	if _, err = producer.Publish(
		producerContext(),
		queuedPayload("after-drain"),
	); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Publish() after drain error = %v, want ErrUnavailable", err)
	}
	if publishCalls != 1 {
		t.Fatalf("publish calls = %d, want only the pre-drain call", publishCalls)
	}
	if err = shutdownWithin(runtime); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestServiceDrainClosesWorkerReadinessAndIntake(t *testing.T) {
	runCalls := 0
	admissionCalls := 0
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler:   func(context.Context, core.TaskMessage) error { return nil },
		Readiness: func(context.Context, int) error { return nil },
		CloseAdmission: func(int) error {
			admissionCalls++

			return nil
		},
		Run: func(context.Context, int, Handler) error {
			runCalls++

			return nil
		},
		Shutdown: func(context.Context, int) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	plan := worker.Plan()
	runtime, err := service.New(service.Config{Components: plan.Components})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = runtime.Drain(); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if err = runtime.Drain(); err != nil {
		t.Fatalf("repeated Drain() error = %v", err)
	}
	if admissionCalls != 1 {
		t.Fatalf("close admission calls = %d, want 1", admissionCalls)
	}
	if err = plan.Readiness[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Readiness() after drain error = %v, want ErrUnavailable", err)
	}
	if err = plan.Tasks[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Run() after drain error = %v, want ErrUnavailable", err)
	}
	if runCalls != 0 {
		t.Fatalf("run calls = %d after drain, want 0", runCalls)
	}
	if err = shutdownWithin(runtime); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestProducerAdmissionClosureLetsAnAcceptedPublishFinishBeforeStop(t *testing.T) {
	publishEntered := make(chan struct{})
	releasePublish := make(chan struct{})
	type result struct {
		err       error
		recovered any
	}
	published := make(chan result, 1)
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			close(publishEntered)
			<-releasePublish

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	go func() {
		var publishErr error
		deferred := result{}
		defer func() {
			deferred.err = publishErr
			deferred.recovered = recover()
			published <- deferred
		}()
		_, publishErr = producer.Publish(producerContext(), queuedPayload("work"))
	}()
	awaitValue(t, publishEntered)
	if err = component.CloseAdmission(); err != nil {
		t.Fatalf("CloseAdmission() error = %v", err)
	}
	close(releasePublish)
	publishResult := awaitValue(t, published)
	if publishResult.recovered != nil || publishResult.err != nil {
		t.Fatalf(
			"Publish() after admission closure = (panic %v, error %v)",
			publishResult.recovered,
			publishResult.err,
		)
	}
	if err = stopWithin(component); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestWorkerAdmissionClosureLetsAnAcceptedHandlerFinishBeforeStop(t *testing.T) {
	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error {
			close(handlerEntered)
			<-releaseHandler

			return nil
		},
		Run: func(ctx context.Context, _ int, handler Handler) error {
			return handler(ctx, plainTask("work"))
		},
		Shutdown: func(context.Context, int) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	plan := worker.Plan()
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- plan.Tasks[0].Run(context.Background()) }()
	awaitValue(t, handlerEntered)
	if err = plan.Components[0].CloseAdmission(); err != nil {
		t.Fatalf("CloseAdmission() error = %v", err)
	}
	close(releaseHandler)
	if err = awaitValue(t, runResult); !errors.Is(err, ErrWorkerExited) {
		t.Fatalf("Run() error = %v, want ErrWorkerExited", err)
	}
	if err = stopWithin(plan.Components[0]); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func shutdownWithin(runtime *service.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	return runtime.Shutdown(ctx)
}
