package queueservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

type lifecycleResource struct{}

func TestProducerStartupReadinessAndShutdownLifecycle(t *testing.T) {
	startupErr := errors.New("startup endpoint secret")
	cleanupErr := errors.New("shutdown credential secret")
	shutdownCalls := 0
	producer, err := NewProducer(ProducerOptions[*lifecycleResource]{
		Name:        "orders-producer",
		Resource:    &lifecycleResource{},
		Correlation: mustFactory(t),
		Startup: func(context.Context, *lifecycleResource) error {
			return startupErr
		},
		Readiness: func(context.Context, *lifecycleResource) error {
			t.Fatal("readiness ran after failed startup")

			return nil
		},
		Publish: func(
			context.Context,
			*lifecycleResource,
			core.QueuedMessage,
			...job.AllowOption,
		) error {
			return nil
		},
		Shutdown: func(context.Context, *lifecycleResource) error {
			shutdownCalls++
			if shutdownCalls == 1 {
				return cleanupErr
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	readiness, ok := producer.Readiness()
	if !ok || readiness.Name != "orders-producer" {
		t.Fatalf("Readiness() = (%#v, %v)", readiness, ok)
	}
	startErr := producer.Component().Start(context.Background())
	if !errors.Is(startErr, startupErr) || !errors.Is(startErr, cleanupErr) {
		t.Fatalf("Start() error = %v, want startup and cleanup causes", startErr)
	}
	if strings.Contains(startErr.Error(), "endpoint") ||
		strings.Contains(startErr.Error(), "credential") {
		t.Fatalf("Start() disclosed callback error text: %q", startErr)
	}
	if err = readiness.Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("readiness after failed startup error = %v", err)
	}
	if err = stopWithin(producer.Component()); !errors.Is(err, cleanupErr) {
		t.Fatalf("Stop() error = %v, want original cleanup cause", err)
	}
	if err = stopWithin(producer.Component()); !errors.Is(err, cleanupErr) {
		t.Fatalf("repeated Stop() error = %v, want original cleanup cause", err)
	}
	if shutdownCalls != 1 {
		t.Fatalf("shutdown calls = %d, want exactly one cleanup attempt", shutdownCalls)
	}
}

func TestSharedProducerCanRetryAfterStartupValidationFailure(t *testing.T) {
	startupErr := errors.New("dependency unavailable")
	startupCalls := 0
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "shared-producer", Resource: 1, Correlation: mustFactory(t),
		Startup: func(context.Context, int) error {
			startupCalls++
			if startupCalls == 1 {
				return startupErr
			}

			return nil
		},
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); !errors.Is(err, startupErr) {
		t.Fatalf("first Start() error = %v, want startup failure", err)
	}
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("retried Start() error = %v", err)
	}
	if err = stopWithin(component); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if startupCalls != 2 {
		t.Fatalf("startup calls = %d, want 2", startupCalls)
	}
}

func TestProducerPublishAcceptanceAndCallbackPanicsAreExplicit(t *testing.T) {
	backendErr := errors.New("backend endpoint secret")
	callbackCalls := 0
	producer, err := NewProducer(ProducerOptions[*lifecycleResource]{
		Name:        "orders-producer",
		Resource:    &lifecycleResource{},
		Correlation: mustFactory(t),
		PublishWithAcceptance: func(
			_ context.Context,
			_ *lifecycleResource,
			message core.QueuedMessage,
			_ ...job.AllowOption,
		) (PublishAcceptance, error) {
			callbackCalls++
			switch string(message.Bytes()) {
			case "rejected":
				return PublishNotAccepted, backendErr
			case "unknown":
				return PublishUnknown, backendErr
			case "panic":
				panic("payload secret")
			default:
				return PublishAccepted, nil
			}
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	canceled, cancel := context.WithCancel(producerContext())
	cancel()
	_, acceptance, err := producer.PublishWithAcceptance(
		canceled,
		queuedPayload("accepted"),
	)
	if acceptance != PublishNotAccepted || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled publish = (%v, %v), want not accepted cancellation", acceptance, err)
	}
	if callbackCalls != 0 {
		t.Fatalf("canceled publish callback calls = %d, want 0", callbackCalls)
	}

	_, acceptance, err = producer.PublishWithAcceptance(
		producerContext(),
		queuedPayload("rejected"),
	)
	if acceptance != PublishNotAccepted || !errors.Is(err, backendErr) ||
		errors.Is(err, ErrPublishOutcomeUnknown) {
		t.Fatalf("rejected publish = (%v, %v)", acceptance, err)
	}

	_, acceptance, err = producer.PublishWithAcceptance(
		producerContext(),
		queuedPayload("unknown"),
	)
	if acceptance != PublishUnknown || !errors.Is(err, backendErr) ||
		!errors.Is(err, ErrPublishOutcomeUnknown) {
		t.Fatalf("unknown publish = (%v, %v)", acceptance, err)
	}
	if strings.Contains(err.Error(), "endpoint") {
		t.Fatalf("unknown publish disclosed callback error text: %q", err)
	}

	_, acceptance, err = producer.PublishWithAcceptance(
		producerContext(),
		queuedPayload("panic"),
	)
	var panicErr *CallbackPanicError
	if acceptance != PublishUnknown || !errors.As(err, &panicErr) ||
		panicErr.Operation != CallbackPublish || strings.Contains(err.Error(), "payload") {
		t.Fatalf("panicking publish = (%v, %v)", acceptance, err)
	}

	_, acceptance, err = producer.PublishWithAcceptance(
		producerContext(),
		queuedPayload("accepted"),
	)
	if acceptance != PublishAccepted || err != nil {
		t.Fatalf("accepted publish = (%v, %v)", acceptance, err)
	}
}

func TestLifecycleWorkerPlanSupervisesRunReadinessAndDrain(t *testing.T) {
	resource := &lifecycleResource{}
	runEntered := make(chan struct{})
	releaseRun := make(chan struct{})
	shutdownCalls := 0
	handled := 0
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[*lifecycleResource]{
		Name:        "orders-worker",
		Resource:    resource,
		Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error {
			handled++

			return nil
		},
		Readiness: func(context.Context, *lifecycleResource) error { return nil },
		Run: func(
			_ context.Context,
			_ *lifecycleResource,
			handler Handler,
		) error {
			close(runEntered)
			if err := handler(context.Background(), plainTask("work")); err != nil {
				return err
			}
			<-releaseRun

			return nil
		},
		Shutdown: func(context.Context, *lifecycleResource) error {
			shutdownCalls++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	if worker.Resource() != resource {
		t.Fatal("Resource() did not preserve the concrete worker")
	}
	plan := worker.Plan()
	if len(plan.Components) != 1 || len(plan.Tasks) != 1 ||
		len(plan.Readiness) != 1 || plan.Components[0].Name != "orders-worker" ||
		plan.Tasks[0].Name != "orders-worker" ||
		plan.Readiness[0].Name != "orders-worker" {
		t.Fatalf("Plan() = %#v", plan)
	}
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = plan.Readiness[0].Run(context.Background()); err != nil {
		t.Fatalf("Readiness() error = %v", err)
	}
	runResult := make(chan error, 1)
	go func() { runResult <- plan.Tasks[0].Run(context.Background()) }()
	awaitValue(t, runEntered)

	stopContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err = plan.Components[0].Stop(stopContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() while run active error = %v", err)
	}
	if shutdownCalls != 0 {
		t.Fatalf("shutdown calls while run active = %d, want 0", shutdownCalls)
	}
	if err = plan.Readiness[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("readiness during drain error = %v", err)
	}
	close(releaseRun)
	if err = awaitValue(t, runResult); !errors.Is(err, ErrWorkerExited) {
		t.Fatalf("Run() error = %v, want ErrWorkerExited", err)
	}
	if err = stopWithin(plan.Components[0]); err != nil {
		t.Fatalf("resumed Stop() error = %v", err)
	}
	if err = stopWithin(plan.Components[0]); err != nil {
		t.Fatalf("repeated Stop() error = %v", err)
	}
	if handled != 1 || shutdownCalls != 1 {
		t.Fatalf("handled = %d, shutdown calls = %d", handled, shutdownCalls)
	}
}

func TestLifecycleWorkerDrainWaitsForEveryAdmittedHandler(t *testing.T) {
	handlerEntered := make(chan string, 2)
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	handlerDone := make(chan string, 2)
	allHandlersDone := make(chan struct{})
	var admittedHandler Handler
	shutdownCalls := 0
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(_ context.Context, message core.TaskMessage) error {
			payload := string(message.Payload())
			handlerEntered <- payload
			switch payload {
			case "first":
				<-firstRelease
			case "second":
				<-secondRelease
			}

			return nil
		},
		Run: func(_ context.Context, _ int, handler Handler) error {
			admittedHandler = handler
			for _, payload := range []string{"first", "second"} {
				go func() {
					_ = handler(context.Background(), plainTask(payload))
					if payload == "second" {
						close(allHandlersDone)
					}
					handlerDone <- payload
				}()
			}
			seen := map[string]bool{
				awaitValue(t, handlerEntered): true,
				awaitValue(t, handlerEntered): true,
			}
			if !seen["first"] || !seen["second"] {
				t.Fatalf("entered handlers = %v", seen)
			}

			return nil
		},
		Shutdown: func(context.Context, int) error {
			shutdownCalls++
			select {
			case <-allHandlersDone:
				return nil
			default:
				return errors.New("resource closed while handler was active")
			}
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	plan := worker.Plan()
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = plan.Tasks[0].Run(context.Background()); !errors.Is(err, ErrWorkerExited) {
		t.Fatalf("Run() error = %v, want ErrWorkerExited", err)
	}
	stopContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err = plan.Components[0].Stop(stopContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() with active handler error = %v, want context cancellation", err)
	}
	if shutdownCalls != 0 {
		t.Fatalf("shutdown calls with active handler = %d, want 0", shutdownCalls)
	}

	close(firstRelease)
	if completed := awaitValue(t, handlerDone); completed != "first" {
		t.Fatalf("first completed handler = %q", completed)
	}
	stopContext, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	if err = plan.Components[0].Stop(stopContext); !errors.Is(err, context.DeadlineExceeded) {
		cancel()
		t.Fatalf("Stop() with second handler active error = %v, want deadline", err)
	}
	cancel()
	if shutdownCalls != 0 {
		t.Fatalf("shutdown calls with second handler active = %d, want 0", shutdownCalls)
	}

	close(secondRelease)
	if completed := awaitValue(t, handlerDone); completed != "second" {
		t.Fatalf("second completed handler = %q", completed)
	}
	if err = stopWithin(plan.Components[0]); err != nil {
		t.Fatalf("Stop() after handler completion error = %v", err)
	}
	if shutdownCalls != 1 {
		t.Fatalf("shutdown calls = %d, want 1", shutdownCalls)
	}
	if err = admittedHandler(context.Background(), plainTask("late-work")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("handler after worker exit error = %v, want ErrUnavailable", err)
	}
}

func TestLifecycleCallbacksRecoverPanicsWithoutRetainingValues(t *testing.T) {
	producer, err := NewProducer(ProducerOptions[*lifecycleResource]{
		Name:        "panic-producer",
		Resource:    &lifecycleResource{},
		Correlation: mustFactory(t),
		Startup: func(context.Context, *lifecycleResource) error {
			panic("startup credential")
		},
		Publish: func(
			context.Context,
			*lifecycleResource,
			core.QueuedMessage,
			...job.AllowOption,
		) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	startErr := producer.Component().Start(context.Background())
	var panicErr *CallbackPanicError
	if !errors.As(startErr, &panicErr) || panicErr.Operation != CallbackStartup ||
		strings.Contains(startErr.Error(), "credential") {
		t.Fatalf("panicking startup error = %v", startErr)
	}

	handler, err := NewHandler(HandlerOptions{
		Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error {
			panic("task payload")
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	handlerErr := handler(context.Background(), plainTask("work"))
	if !errors.As(handlerErr, &panicErr) || panicErr.Operation != CallbackHandler ||
		strings.Contains(handlerErr.Error(), "payload") {
		t.Fatalf("panicking handler error = %v", handlerErr)
	}
}

func TestLifecycleNamesAreStableAndBounded(t *testing.T) {
	valid := strings.Repeat("a", MaxNameBytes)
	worker, err := NewWorker(WorkerOptions{Name: valid, Queue: queue.NewPool(0)})
	if err != nil || worker.Component().Name != valid {
		t.Fatalf("exact-limit name worker = (%#v, %v)", worker, err)
	}
	for _, name := range []string{strings.Repeat("a", MaxNameBytes+1), string([]byte{0xff})} {
		if _, err := NewProducer(ProducerOptions[int]{
			Name: name, Resource: 1, Correlation: mustFactory(t),
			Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
				return nil
			},
		}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatalf("NewProducer(%q) error = %v", name, err)
		}
	}
}

func TestLifecycleWorkerRunPanicAndBackendFailureRemainObservable(t *testing.T) {
	backendErr := errors.New("backend failure")
	for _, test := range []struct {
		name string
		run  Run[*lifecycleResource]
		want error
	}{
		{
			name: "backend error",
			run: func(context.Context, *lifecycleResource, Handler) error {
				return backendErr
			},
			want: backendErr,
		},
		{
			name: "panic",
			run: func(context.Context, *lifecycleResource, Handler) error {
				panic("endpoint secret")
			},
			want: ErrCallbackPanic,
		},
		{
			name: "unexpected clean exit",
			run: func(context.Context, *lifecycleResource, Handler) error {
				return nil
			},
			want: ErrWorkerExited,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker, err := NewLifecycleWorker(LifecycleWorkerOptions[*lifecycleResource]{
				Name: "worker", Resource: &lifecycleResource{},
				Correlation: mustFactory(t),
				Handler:     func(context.Context, core.TaskMessage) error { return nil },
				Run:         test.run,
				Shutdown:    func(context.Context, *lifecycleResource) error { return nil },
			})
			if err != nil {
				t.Fatalf("NewLifecycleWorker() error = %v", err)
			}
			plan := worker.Plan()
			if err = plan.Components[0].Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			err = plan.Tasks[0].Run(context.Background())
			if !errors.Is(err, test.want) {
				t.Fatalf("Run() error = %v, want %v", err, test.want)
			}
			if strings.Contains(err.Error(), "endpoint") {
				t.Fatalf("Run() disclosed panic value: %q", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err = plan.Components[0].Stop(ctx); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
		})
	}
}

func TestLifecycleWorkerCanceledRunExitIsGraceful(t *testing.T) {
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name:        "worker",
		Resource:    1,
		Correlation: mustFactory(t),
		Handler:     func(context.Context, core.TaskMessage) error { return nil },
		Run: func(ctx context.Context, _ int, _ Handler) error {
			<-ctx.Done()

			return nil
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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = plan.Tasks[0].Run(ctx); err != nil {
		t.Fatalf("Run() after cancellation error = %v", err)
	}
	if err = stopWithin(plan.Components[0]); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
