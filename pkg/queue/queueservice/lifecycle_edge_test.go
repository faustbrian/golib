package queueservice

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"go.opentelemetry.io/otel/propagation"
)

type panicTracePropagator struct{}

type observeSecondDoneContext struct {
	context.Context
	calls   atomic.Int32
	entered chan struct{}
	never   chan struct{}
}

func (ctx *observeSecondDoneContext) Done() <-chan struct{} {
	if ctx.calls.Add(1) == 2 {
		close(ctx.entered)
	}

	return ctx.never
}

func (*observeSecondDoneContext) Err() error { return nil }

type cancelOnSecondDoneContext struct {
	context.Context
	calls atomic.Int32
	done  chan struct{}
}

func (ctx *cancelOnSecondDoneContext) Done() <-chan struct{} {
	if ctx.calls.Add(1) == 2 {
		close(ctx.done)
	}

	return ctx.done
}

func (ctx *cancelOnSecondDoneContext) Err() error {
	select {
	case <-ctx.done:
		return context.Canceled
	default:
		return nil
	}
}

func (panicTracePropagator) Inject(context.Context, propagation.TextMapCarrier) {
	panic("trace credential")
}

func (panicTracePropagator) Extract(
	context.Context,
	propagation.TextMapCarrier,
) context.Context {
	panic("trace credential")
}

func (panicTracePropagator) Fields() []string { return []string{"traceparent"} }

func TestProducerOptionalReadinessAndLifecycleCallbacks(t *testing.T) {
	producerWithoutReadiness, err := NewProducer(ProducerOptions[int]{
		Name: "shared", Resource: 1, Correlation: mustFactory(t),
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if readiness, ok := producerWithoutReadiness.Readiness(); ok || readiness.Run != nil {
		t.Fatalf("Readiness() = (%#v, %v), want absent", readiness, ok)
	}

	readinessErr := errors.New("dependency unavailable")
	readinessResult := error(nil)
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "owned", Resource: 1, Correlation: mustFactory(t),
		Startup: func(context.Context, int) error { return nil },
		Readiness: func(context.Context, int) error {
			if errors.Is(readinessResult, ErrCallbackPanic) {
				panic("readiness endpoint")
			}

			return readinessResult
		},
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			return nil
		},
		Shutdown: func(context.Context, int) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("repeated Start() error = %v", err)
	}
	readiness, ok := producer.Readiness()
	if !ok {
		t.Fatal("Readiness() was absent")
	}
	readinessResult = readinessErr
	if err = readiness.Run(context.Background()); !errors.Is(err, readinessErr) {
		t.Fatalf("Readiness() error = %v", err)
	}
	readinessResult = ErrCallbackPanic
	if err = readiness.Run(context.Background()); !errors.Is(err, ErrCallbackPanic) ||
		strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("panicking Readiness() error = %v", err)
	}
	if err = stopWithin(component); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err = readiness.Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Readiness() after Stop error = %v", err)
	}
}

func TestProducerConcurrentStartAndStopSerializeOwnership(t *testing.T) {
	startupEntered := make(chan struct{})
	releaseStartup := make(chan struct{})
	shutdownCalls := atomic.Int32{}
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		Startup: func(context.Context, int) error {
			close(startupEntered)
			<-releaseStartup

			return nil
		},
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			return nil
		},
		Shutdown: func(context.Context, int) error {
			shutdownCalls.Add(1)

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	started := make(chan error, 1)
	go func() { started <- component.Start(context.Background()) }()
	awaitValue(t, startupEntered)
	if err = component.Start(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("concurrent Start() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err = component.Stop(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Stop() error = %v", err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- stopWithin(component) }()
	awaitProducerStopping(t, producer)
	close(releaseStartup)
	if err = awaitValue(t, started); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Start() raced with Stop error = %v", err)
	}
	if err = awaitValue(t, stopped); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if shutdownCalls.Load() != 1 {
		t.Fatalf("shutdown calls = %d, want 1", shutdownCalls.Load())
	}
}

func TestLifecycleWorkerConcurrentStartAndStopSerializeOwnership(t *testing.T) {
	startupEntered := make(chan struct{})
	releaseStartup := make(chan struct{})
	shutdownCalls := atomic.Int32{}
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Startup: func(context.Context, int) error {
			close(startupEntered)
			<-releaseStartup

			return nil
		},
		Run: func(context.Context, int, Handler) error { return nil },
		Shutdown: func(context.Context, int) error {
			shutdownCalls.Add(1)

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	component := worker.Plan().Components[0]
	started := make(chan error, 1)
	go func() { started <- component.Start(context.Background()) }()
	awaitValue(t, startupEntered)
	if err = component.Start(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("concurrent Start() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err = component.Stop(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Stop() error = %v", err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- stopWithin(component) }()
	awaitLifecycleWorkerStopping(t, worker)
	close(releaseStartup)
	if err = awaitValue(t, started); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Start() raced with Stop error = %v", err)
	}
	if err = awaitValue(t, stopped); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err = component.Start(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Start() after Stop error = %v", err)
	}
	if shutdownCalls.Load() != 1 {
		t.Fatalf("shutdown calls = %d, want 1", shutdownCalls.Load())
	}
}

func TestLifecycleWorkerShutdownIsSingleFlightAndCachesFailure(t *testing.T) {
	shutdownEntered := make(chan struct{})
	releaseShutdown := make(chan struct{})
	shutdownErr := errors.New("shutdown failed")
	shutdownCalls := atomic.Int32{}
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Run:     func(context.Context, int, Handler) error { return nil },
		Shutdown: func(context.Context, int) error {
			if shutdownCalls.Add(1) == 1 {
				close(shutdownEntered)
				<-releaseShutdown

				return shutdownErr
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	component := worker.Plan().Components[0]
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first := make(chan error, 1)
	go func() { first <- stopWithin(component) }()
	awaitValue(t, shutdownEntered)
	waiterContext := &observeSecondDoneContext{
		Context: context.Background(), entered: make(chan struct{}), never: make(chan struct{}),
	}
	waiter := make(chan error, 1)
	go func() { waiter <- component.Stop(waiterContext) }()
	awaitValue(t, waiterContext.entered)
	canceled := &cancelOnSecondDoneContext{
		Context: context.Background(), done: make(chan struct{}),
	}
	if err = component.Stop(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled concurrent Stop() error = %v", err)
	}
	close(releaseShutdown)
	if err = awaitValue(t, first); !errors.Is(err, shutdownErr) {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err = awaitValue(t, waiter); !errors.Is(err, shutdownErr) {
		t.Fatalf("waiting Stop() error = %v", err)
	}
	if err = stopWithin(component); !errors.Is(err, shutdownErr) {
		t.Fatalf("repeated Stop() error = %v, want original shutdown failure", err)
	}
	if err = stopWithin(component); !errors.Is(err, shutdownErr) {
		t.Fatalf("second repeated Stop() error = %v, want original shutdown failure", err)
	}
	if shutdownCalls.Load() != 1 {
		t.Fatalf("shutdown calls = %d, want exactly one attempt", shutdownCalls.Load())
	}
}

func TestPublishAcceptanceRejectsContradictoryAndInvalidResults(t *testing.T) {
	backendErr := errors.New("backend failure")
	acceptance := PublishAccepted
	callbackErr := backendErr
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		PublishWithAcceptance: func(
			context.Context,
			int,
			core.QueuedMessage,
			...job.AllowOption,
		) (PublishAcceptance, error) {
			return acceptance, callbackErr
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, got, err := producer.PublishWithAcceptance(producerContext(), queuedPayload("work"))
	if got != PublishAccepted || !errors.Is(err, backendErr) ||
		errors.Is(err, ErrPublishOutcomeUnknown) {
		t.Fatalf("accepted failure = (%v, %v)", got, err)
	}
	acceptance, callbackErr = PublishNotAccepted, nil
	_, got, err = producer.PublishWithAcceptance(producerContext(), queuedPayload("work"))
	if got != PublishNotAccepted || !errors.Is(err, ErrInvalidPublishAcceptance) {
		t.Fatalf("empty rejection = (%v, %v)", got, err)
	}
	acceptance, callbackErr = PublishUnknown, nil
	_, got, err = producer.PublishWithAcceptance(producerContext(), queuedPayload("work"))
	if got != PublishUnknown || !errors.Is(err, ErrPublishOutcomeUnknown) {
		t.Fatalf("empty unknown result = (%v, %v)", got, err)
	}
	acceptance, callbackErr = PublishAcceptance(255), backendErr
	_, got, err = producer.PublishWithAcceptance(producerContext(), queuedPayload("work"))
	if got != PublishUnknown || !errors.Is(err, ErrInvalidPublishAcceptance) ||
		!errors.Is(err, ErrPublishOutcomeUnknown) || !errors.Is(err, backendErr) {
		t.Fatalf("invalid result = (%v, %v)", got, err)
	}
}

func TestPublishValidatesContextAndContainsTracePropagationPanic(t *testing.T) {
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		TracePropagator: panicTracePropagator{},
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			t.Fatal("publish callback ran after trace propagation panic")

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	var nilContext context.Context
	if _, acceptance, err := producer.PublishWithAcceptance(
		nilContext,
		queuedPayload("work"),
	); acceptance != PublishNotAccepted || !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("nil-context publish = (%v, %v)", acceptance, err)
	}
	_, acceptance, err := producer.PublishWithAcceptance(
		producerContext(),
		queuedPayload("work"),
	)
	if acceptance != PublishNotAccepted || !errors.Is(err, ErrCallbackPanic) ||
		errors.Is(err, ErrPublishOutcomeUnknown) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("trace-panic publish = (%v, %v)", acceptance, err)
	}
}

func TestStopBeforeStartClosesOwnedResourcesOnce(t *testing.T) {
	producerShutdown := 0
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			return nil
		},
		Shutdown: func(context.Context, int) error {
			producerShutdown++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = stopWithin(component); err != nil {
		t.Fatalf("producer Stop() before Start error = %v", err)
	}
	if err = stopWithin(component); err != nil {
		t.Fatalf("producer repeated Stop() error = %v", err)
	}
	if err = component.Start(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("producer Start() after Stop error = %v", err)
	}
	if producerShutdown != 1 {
		t.Fatalf("producer shutdown calls = %d, want 1", producerShutdown)
	}

	workerShutdown := 0
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Run:     func(context.Context, int, Handler) error { return nil },
		Shutdown: func(context.Context, int) error {
			workerShutdown++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	component = worker.Plan().Components[0]
	if err = stopWithin(component); err != nil {
		t.Fatalf("worker Stop() before Start error = %v", err)
	}
	if err = stopWithin(component); err != nil {
		t.Fatalf("worker repeated Stop() error = %v", err)
	}
	if err = component.Start(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("worker Start() after Stop error = %v", err)
	}
	if workerShutdown != 1 {
		t.Fatalf("worker shutdown calls = %d, want 1", workerShutdown)
	}
}

func TestShutdownPanicIsSecretSafeAndCached(t *testing.T) {
	shutdownCalls := 0
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			return nil
		},
		Shutdown: func(context.Context, int) error {
			shutdownCalls++
			if shutdownCalls == 1 {
				panic("shutdown credential")
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = stopWithin(producer.Component()); !errors.Is(err, ErrCallbackPanic) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("panicking Stop() error = %v", err)
	}
	if err = stopWithin(producer.Component()); !errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("repeated Stop() error = %v, want original panic classification", err)
	}
	if shutdownCalls != 1 {
		t.Fatalf("shutdown calls = %d, want exactly one attempt", shutdownCalls)
	}
}

func TestTraceExtractionPanicDoesNotReachApplicationHandler(t *testing.T) {
	handlerCalls := 0
	handler, err := NewHandler(HandlerOptions{
		Correlation:     mustFactory(t),
		TracePropagator: panicTracePropagator{},
		Handler: func(context.Context, core.TaskMessage) error {
			handlerCalls++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	err = handler(context.Background(), plainTask("work"))
	if !errors.Is(err, ErrCallbackPanic) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("trace extraction error = %v", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("handler calls = %d, want 0", handlerCalls)
	}
}

func TestLifecycleWorkerRejectsInvalidOwnership(t *testing.T) {
	valid := LifecycleWorkerOptions[*lifecycleResource]{
		Name: "worker", Resource: &lifecycleResource{}, Correlation: mustFactory(t),
		Handler:  func(context.Context, core.TaskMessage) error { return nil },
		Run:      func(context.Context, *lifecycleResource, Handler) error { return nil },
		Shutdown: func(context.Context, *lifecycleResource) error { return nil },
	}
	var nilResource *lifecycleResource
	tests := []LifecycleWorkerOptions[*lifecycleResource]{
		{},
		{
			Name: "worker", Resource: nilResource, Correlation: valid.Correlation,
			Handler: valid.Handler, Run: valid.Run, Shutdown: valid.Shutdown,
		},
		{
			Name: "worker", Resource: valid.Resource, Correlation: valid.Correlation,
			Handler: valid.Handler, Shutdown: valid.Shutdown,
		},
		{
			Name: "worker", Resource: valid.Resource, Correlation: valid.Correlation,
			Handler: valid.Handler, Run: valid.Run,
		},
		{
			Name: "worker", Resource: valid.Resource,
			Handler: valid.Handler, Run: valid.Run, Shutdown: valid.Shutdown,
		},
	}
	for index, options := range tests {
		if _, err := NewLifecycleWorker(options); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("NewLifecycleWorker(case %d) error = %v", index, err)
		}
	}
}

func TestLifecycleWorkerRejectsRunAndReadinessBeforeStartup(t *testing.T) {
	readinessCalls := 0
	runCalls := 0
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Readiness: func(context.Context, int) error {
			readinessCalls++

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
	if err = plan.Readiness[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Readiness() before Start error = %v, want ErrUnavailable", err)
	}
	if err = plan.Tasks[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Run() before Start error = %v, want ErrUnavailable", err)
	}
	if readinessCalls != 0 || runCalls != 0 {
		t.Fatalf("readiness calls = %d, run calls = %d, want 0", readinessCalls, runCalls)
	}
	if err = stopWithin(plan.Components[0]); err != nil {
		t.Fatalf("Stop() before Start error = %v", err)
	}
}

func TestLifecycleWorkerStartupFailureAndReadinessPanicAreSafe(t *testing.T) {
	startupErr := errors.New("startup endpoint")
	cleanupErr := errors.New("cleanup credential")
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Startup: func(context.Context, int) error { return startupErr },
		Readiness: func(context.Context, int) error {
			panic("readiness endpoint")
		},
		Run:      func(context.Context, int, Handler) error { return nil },
		Shutdown: func(context.Context, int) error { return cleanupErr },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	plan := worker.Plan()
	startErr := plan.Components[0].Start(context.Background())
	if !errors.Is(startErr, startupErr) || !errors.Is(startErr, cleanupErr) ||
		strings.Contains(startErr.Error(), "endpoint") ||
		strings.Contains(startErr.Error(), "credential") {
		t.Fatalf("Start() error = %v", startErr)
	}
	if err = plan.Readiness[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Readiness() after failed startup error = %v", err)
	}
	if err = plan.Tasks[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Run() after failed startup error = %v", err)
	}

	worker, err = NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler:   func(context.Context, core.TaskMessage) error { return nil },
		Readiness: func(context.Context, int) error { panic("readiness endpoint") },
		Run:       func(context.Context, int, Handler) error { return nil },
		Shutdown:  func(context.Context, int) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	plan = worker.Plan()
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("repeated Start() error = %v", err)
	}
	if err = plan.Readiness[0].Run(context.Background()); !errors.Is(err, ErrCallbackPanic) ||
		strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("panicking Readiness() error = %v", err)
	}
}

func TestCallbackErrorNamesRemainStableAndSecretSafe(t *testing.T) {
	operations := []CallbackOperation{
		CallbackStartup,
		CallbackReadiness,
		CallbackPublish,
		CallbackHandler,
		CallbackRun,
		CallbackShutdown,
		CallbackOperation(255),
	}
	for _, operation := range operations {
		err := (&CallbackPanicError{Operation: operation}).Error()
		if err == "" || strings.Contains(err, "secret") {
			t.Fatalf("CallbackPanicError(%d) = %q", operation, err)
		}
	}
	for _, acceptance := range []PublishAcceptance{
		PublishNotAccepted,
		PublishAccepted,
		PublishUnknown,
		PublishAcceptance(255),
	} {
		err := (&PublishError{Acceptance: acceptance, Err: errors.New("secret")}).Error()
		if err == "" || strings.Contains(err, "secret") {
			t.Fatalf("PublishError(%d) = %q", acceptance, err)
		}
	}
}

func awaitProducerStopping[R any](t *testing.T, producer *Producer[R]) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		producer.mu.Lock()
		stopping := producer.stopping
		producer.mu.Unlock()
		if stopping {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("producer did not begin stopping")
		}
		runtime.Gosched()
	}
}

func awaitLifecycleWorkerStopping[R any](
	t *testing.T,
	worker *LifecycleWorker[R],
) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		worker.mu.Lock()
		stopping := worker.stopping
		worker.mu.Unlock()
		if stopping {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not begin stopping")
		}
		runtime.Gosched()
	}
}
