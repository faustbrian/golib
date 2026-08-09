package queueservice

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/service"
)

type concretePanickingWorker struct {
	shutdownCalls atomic.Int32
}

func (*concretePanickingWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (*concretePanickingWorker) Queue(core.TaskMessage) error                { return nil }
func (*concretePanickingWorker) Request() (core.TaskMessage, error) {
	return nil, queue.ErrNoTaskInQueue
}
func (worker *concretePanickingWorker) Shutdown() error {
	worker.shutdownCalls.Add(1)
	panic("sensitive concrete shutdown value")
}

func TestConcreteWorkerServiceDrainClosesQueueAdmissionBeforeShutdown(t *testing.T) {
	handlerEntered := make(chan struct{})
	releaseHandler := make(chan struct{})
	coordinator := queue.NewPool(1, queue.WithFn(func(
		context.Context,
		core.TaskMessage,
	) error {
		close(handlerEntered)
		<-releaseHandler

		return nil
	}))
	adapter, err := NewWorker(WorkerOptions{Name: "worker", Queue: coordinator})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	runtime, err := service.New(service.Config{
		Components: []service.Component{adapter.Component()},
	})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = coordinator.Queue(plainTask("work")); err != nil {
		t.Fatalf("Queue() before drain error = %v", err)
	}
	awaitValue(t, handlerEntered)
	if err = runtime.Drain(); err != nil {
		t.Fatalf("Drain() error = %v", err)
	}
	if err = coordinator.Queue(plainTask("late")); !errors.Is(err, queue.ErrQueueShutdown) {
		t.Fatalf("Queue() after drain error = %v, want ErrQueueShutdown", err)
	}
	close(releaseHandler)
	if err = shutdownWithin(runtime); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestConcreteWorkerReleasePanicIsSecretSafeAndTerminal(t *testing.T) {
	backend := &concretePanickingWorker{}
	coordinator, err := queue.NewQueue(
		queue.WithWorker(backend),
		queue.WithWorkerCount(0),
	)
	if err != nil {
		t.Fatalf("queue.NewQueue() error = %v", err)
	}
	adapter, err := NewWorker(WorkerOptions{Name: "worker", Queue: coordinator})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	component := adapter.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for call := 0; call < 2; call++ {
		err = stopWithin(component)
		if !errors.Is(err, ErrCallbackPanic) ||
			!errors.Is(err, queue.ErrWorkerShutdownPanic) {
			t.Fatalf("Stop(%d) error = %v, want shutdown panic", call, err)
		}
		if strings.Contains(err.Error(), "sensitive") {
			t.Fatal("Stop() disclosed the concrete shutdown panic value")
		}
	}
	if backend.shutdownCalls.Load() != 1 {
		t.Fatalf("backend shutdown calls = %d, want 1", backend.shutdownCalls.Load())
	}
}

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
