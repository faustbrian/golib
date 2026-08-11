package kafkaservice_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
	"github.com/faustbrian/golib/pkg/service"
)

func TestServiceDrainClosesKafkaAdmissionBeforeCancellation(t *testing.T) {
	t.Run("producer", func(t *testing.T) {
		shutdownCalls := 0
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[struct{}]{
				Name:        "producer",
				Resource:    struct{}{},
				Correlation: mustFactory(t, "child"),
				Publish: func(
					context.Context,
					struct{},
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					return kafka.DeliveryResult{}, nil
				},
				Shutdown: func(context.Context, struct{}) error {
					shutdownCalls++

					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		runtime := mustService(t, producer.Component())
		if err = runtime.Start(context.Background()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if err = runtime.Drain(); err != nil {
			t.Fatalf("Drain() error = %v", err)
		}
		parent, startErr := mustFactory(t, "parent", "request").Start()
		if startErr != nil {
			t.Fatalf("Start() correlation error = %v", startErr)
		}
		_, _, err = producer.Publish(
			correlation.WithValues(context.Background(), parent),
			kafka.ProducerRecord{Topic: "orders"},
		)
		if !errors.Is(err, kafkaservice.ErrUnavailable) {
			t.Fatalf("Publish() after Drain error = %v", err)
		}
		if shutdownCalls != 0 {
			t.Fatalf("shutdown calls during Drain = %d, want 0", shutdownCalls)
		}
		if err = runtime.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
		if shutdownCalls != 1 {
			t.Fatalf("shutdown calls = %d, want 1", shutdownCalls)
		}
	})

	t.Run("consumer", func(t *testing.T) {
		runCalls := 0
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[struct{}]{
				Name:        "consumer",
				Resource:    struct{}{},
				Correlation: mustFactory(t, "delivery"),
				Handler: kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					return nil
				}),
				Run: func(context.Context, struct{}, kafka.Handler) error {
					runCalls++

					return nil
				},
				Shutdown: func(context.Context, struct{}) error { return nil },
			},
		)
		if err != nil {
			t.Fatalf("NewConsumer() error = %v", err)
		}
		plan := consumer.Plan()
		runtime := mustService(t, plan.Components[0])
		if err = runtime.Start(context.Background()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		if err = runtime.Drain(); err != nil {
			t.Fatalf("Drain() error = %v", err)
		}
		if err = plan.Tasks[0].Run(context.Background()); !errors.Is(
			err,
			kafkaservice.ErrUnavailable,
		) {
			t.Fatalf("Run() after Drain error = %v", err)
		}
		if runCalls != 0 {
			t.Fatalf("run calls after Drain = %d, want 0", runCalls)
		}
		if err = runtime.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
	})
}

func TestCallbackFailuresRemainClassifiableWithoutExposingCauses(t *testing.T) {
	callbackCause := errors.New(
		"record=customer-profile broker=kafka.internal:9093 password=correct-horse",
	)
	startupProducer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name:        "startup-producer",
			Resource:    struct{}{},
			Correlation: mustFactory(t, "unused"),
			Startup: func(context.Context, struct{}) error {
				return callbackCause
			},
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() startup error = %v", err)
	}
	startupErr := startupProducer.Component().Start(context.Background())
	var classifiedStartup *kafkaservice.StartupError
	if !errors.As(startupErr, &classifiedStartup) {
		t.Fatalf("Start() error = %v, want StartupError", startupErr)
	}
	assertRedactedCallbackCause(
		t,
		classifiedStartup.Validation,
		callbackCause,
		kafkaservice.CallbackStartup,
	)
	assertNoCallbackCause(t, startupErr)

	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name:        "producer",
			Resource:    struct{}{},
			Correlation: mustFactory(t, "child"),
			Readiness: func(context.Context, struct{}) error {
				return callbackCause
			},
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{Offset: 42}, callbackCause
			},
			Shutdown: func(context.Context, struct{}) error {
				return callbackCause
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
	readiness, ok := producer.Readiness()
	if !ok {
		t.Fatal("Readiness() missing configured check")
	}
	assertRedactedCallbackCause(
		t,
		readiness.Run(context.Background()),
		callbackCause,
		kafkaservice.CallbackReadiness,
	)
	parent, startErr := mustFactory(t, "parent", "request").Start()
	if startErr != nil {
		t.Fatalf("Start() correlation error = %v", startErr)
	}
	_, delivery, publishErr := producer.Publish(
		correlation.WithValues(context.Background(), parent),
		kafka.ProducerRecord{Topic: "orders"},
	)
	if delivery.Offset != 42 {
		t.Fatalf("Publish() delivery = %#v", delivery)
	}
	assertRedactedCallbackCause(
		t,
		publishErr,
		callbackCause,
		kafkaservice.CallbackPublish,
	)
	assertRedactedCallbackCause(
		t,
		component.Stop(context.Background()),
		callbackCause,
		kafkaservice.CallbackShutdown,
	)

	handler, err := kafkaservice.NewHandler(kafkaservice.HandlerOptions{
		Correlation: mustFactory(t, "workflow", "delivery"),
		Handler: kafka.HandlerFunc(func(
			context.Context,
			kafka.ConsumedMessage,
		) error {
			return callbackCause
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	assertRedactedCallbackCause(
		t,
		handler.Handle(context.Background(), kafka.ConsumedMessage{}),
		callbackCause,
		kafkaservice.CallbackHandler,
	)

	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[struct{}]{
			Name:        "consumer",
			Resource:    struct{}{},
			Correlation: mustFactory(t, "delivery"),
			Handler: kafka.HandlerFunc(func(
				context.Context,
				kafka.ConsumedMessage,
			) error {
				return nil
			}),
			Readiness: func(context.Context, struct{}) error {
				return callbackCause
			},
			Run: func(context.Context, struct{}, kafka.Handler) error {
				return callbackCause
			},
			Shutdown: func(context.Context, struct{}) error {
				return callbackCause
			},
		},
	)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	plan := consumer.Plan()
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	assertRedactedCallbackCause(
		t,
		plan.Readiness[0].Run(context.Background()),
		callbackCause,
		kafkaservice.CallbackReadiness,
	)
	assertRedactedCallbackCause(
		t,
		plan.Tasks[0].Run(context.Background()),
		callbackCause,
		kafkaservice.CallbackRun,
	)
	assertRedactedCallbackCause(
		t,
		plan.Components[0].Stop(context.Background()),
		callbackCause,
		kafkaservice.CallbackShutdown,
	)
}

func TestPartialStartupRollbackIsReverseOrderedAndSecretSafe(t *testing.T) {
	startupCause := errors.New("startup broker=kafka.internal:9093")
	consumerCleanupCause := errors.New("consumer password=correct-horse")
	producerCleanupCause := errors.New("producer record=customer-profile")
	order := make([]string, 0, 2)
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name:        "producer",
			Resource:    struct{}{},
			Correlation: mustFactory(t, "unused"),
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
			Shutdown: func(context.Context, struct{}) error {
				order = append(order, "producer")

				return producerCleanupCause
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[struct{}]{
			Name:        "consumer",
			Resource:    struct{}{},
			Correlation: mustFactory(t, "unused"),
			Handler: kafka.HandlerFunc(func(
				context.Context,
				kafka.ConsumedMessage,
			) error {
				return nil
			}),
			Startup: func(context.Context, struct{}) error { return startupCause },
			Run: func(context.Context, struct{}, kafka.Handler) error {
				return nil
			},
			Shutdown: func(context.Context, struct{}) error {
				order = append(order, "consumer")

				return consumerCleanupCause
			},
		},
	)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	runtime, err := service.New(service.Config{Components: []service.Component{
		producer.Component(),
		consumer.Plan().Components[0],
	}})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	err = runtime.Start(context.Background())
	var startupErr *service.StartupError
	if !errors.As(err, &startupErr) || !errors.Is(err, startupCause) ||
		!errors.Is(err, consumerCleanupCause) || !errors.Is(err, producerCleanupCause) {
		t.Fatalf("Start() error = %#v, want all startup and cleanup causes", err)
	}
	if got := strings.Join(order, ","); got != "consumer,producer" {
		t.Fatalf("cleanup order = %q, want consumer,producer", got)
	}
	assertNoCallbackCause(t, err)
}

func TestStopPropagatesOneCallerOwnedShutdownBudget(t *testing.T) {
	type contextKey struct{}
	publishStarted := make(chan struct{})
	releasePublish := make(chan struct{})
	shutdownContext := make(chan context.Context, 1)
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name:        "producer",
			Resource:    struct{}{},
			Correlation: mustFactory(t, "child"),
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				close(publishStarted)
				<-releasePublish

				return kafka.DeliveryResult{}, nil
			},
			Shutdown: func(ctx context.Context, _ struct{}) error {
				shutdownContext <- ctx

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
	parent, err := mustFactory(t, "parent", "request").Start()
	if err != nil {
		t.Fatalf("Start() correlation error = %v", err)
	}
	publishResult := make(chan error, 1)
	go func() {
		_, _, publishErr := producer.Publish(
			correlation.WithValues(context.Background(), parent),
			kafka.ProducerRecord{Topic: "orders"},
		)
		publishResult <- publishErr
	}()
	<-publishStarted
	deadlineContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stopCtx := context.WithValue(deadlineContext, contextKey{}, "shutdown-budget")
	wantDeadline, _ := stopCtx.Deadline()
	if err = component.CloseAdmission(); err != nil {
		t.Fatalf("CloseAdmission() error = %v", err)
	}
	stopResult := make(chan error, 1)
	go func() { stopResult <- component.Stop(stopCtx) }()
	if _, _, publishErr := producer.Publish(
		correlation.WithValues(context.Background(), parent),
		kafka.ProducerRecord{Topic: "late"},
	); !errors.Is(publishErr, kafkaservice.ErrUnavailable) {
		t.Fatalf("Publish() during Stop error = %v", publishErr)
	}
	close(releasePublish)
	if err = <-publishResult; err != nil {
		t.Fatalf("admitted Publish() error = %v", err)
	}
	if err = <-stopResult; err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	gotContext := <-shutdownContext
	gotDeadline, ok := gotContext.Deadline()
	if gotContext != stopCtx || !ok || !gotDeadline.Equal(wantDeadline) ||
		gotContext.Value(contextKey{}) != "shutdown-budget" {
		t.Fatalf("shutdown context did not preserve the caller-owned budget")
	}
}

func TestPublicLifecycleAcceptsTheExactNameLimit(t *testing.T) {
	name := strings.Repeat("n", kafkaservice.MaxNameBytes)
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name: name, Resource: struct{}{}, Correlation: mustFactory(t, "producer"),
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
			Readiness: func(context.Context, struct{}) error { return nil },
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	producerReadiness, ok := producer.Readiness()
	if !ok || producer.Component().Name != name || producerReadiness.Name != name {
		t.Fatalf("producer public names do not preserve the exact limit")
	}

	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[struct{}]{
			Name: name, Resource: struct{}{}, Correlation: mustFactory(t, "consumer"),
			Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
				return nil
			}),
			Run:       func(context.Context, struct{}, kafka.Handler) error { return nil },
			Readiness: func(context.Context, struct{}) error { return nil },
			Shutdown:  func(context.Context, struct{}) error { return nil },
		},
	)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	plan := consumer.Plan()
	if plan.Components[0].Name != name || plan.Tasks[0].Name != name ||
		plan.Readiness[0].Name != name {
		t.Fatalf("consumer public names do not preserve the exact limit")
	}
}

func TestProducerStopWaitsForEveryAdmittedOperation(t *testing.T) {
	publishStarted := make(chan struct{})
	readinessStarted := make(chan struct{})
	releasePublish := make(chan struct{})
	releaseReadiness := make(chan struct{})
	defer closeIfOpen(releasePublish)
	defer closeIfOpen(releaseReadiness)
	var shutdownCalls atomic.Int32
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name: "producer", Resource: struct{}{}, Correlation: mustFactory(t, "child"),
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				close(publishStarted)
				<-releasePublish

				return kafka.DeliveryResult{}, nil
			},
			Readiness: func(context.Context, struct{}) error {
				close(readinessStarted)
				<-releaseReadiness

				return nil
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
	readiness, ok := producer.Readiness()
	if !ok {
		t.Fatal("Readiness() missing configured check")
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
	readinessResult := make(chan error, 1)
	go func() { readinessResult <- readiness.Run(context.Background()) }()
	<-publishStarted
	<-readinessStarted
	if err = component.CloseAdmission(); err != nil {
		t.Fatalf("CloseAdmission() error = %v", err)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelStop()
	stopResult := make(chan error, 1)
	go func() { stopResult <- component.Stop(stopCtx) }()
	close(releasePublish)
	if err = <-publishResult; err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err = <-stopResult; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() with readiness admitted error = %v", err)
	}
	if calls := shutdownCalls.Load(); calls != 0 {
		t.Fatalf("shutdown calls with readiness admitted = %d, want 0", calls)
	}
	close(releaseReadiness)
	if err = <-readinessResult; err != nil {
		t.Fatalf("Readiness() error = %v", err)
	}
	retryCtx, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err = component.Stop(retryCtx); err != nil {
		t.Fatalf("retry Stop() error = %v", err)
	}
	if calls := shutdownCalls.Load(); calls != 1 {
		t.Fatalf("shutdown calls = %d, want 1", calls)
	}
}

func TestConsumerStopWaitsForEveryAdmittedOperation(t *testing.T) {
	runStarted := make(chan struct{})
	readinessStarted := make(chan struct{})
	releaseRun := make(chan struct{})
	releaseReadiness := make(chan struct{})
	defer closeIfOpen(releaseRun)
	defer closeIfOpen(releaseReadiness)
	var shutdownCalls atomic.Int32
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[struct{}]{
			Name: "consumer", Resource: struct{}{}, Correlation: mustFactory(t, "delivery"),
			Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
				return nil
			}),
			Run: func(context.Context, struct{}, kafka.Handler) error {
				close(runStarted)
				<-releaseRun

				return nil
			},
			Readiness: func(context.Context, struct{}) error {
				close(readinessStarted)
				<-releaseReadiness

				return nil
			},
			Shutdown: func(context.Context, struct{}) error {
				shutdownCalls.Add(1)

				return nil
			},
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
	readinessResult := make(chan error, 1)
	go func() { readinessResult <- plan.Readiness[0].Run(context.Background()) }()
	<-runStarted
	<-readinessStarted
	if err = component.CloseAdmission(); err != nil {
		t.Fatalf("CloseAdmission() error = %v", err)
	}
	stopCtx, cancelStop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelStop()
	stopResult := make(chan error, 1)
	go func() { stopResult <- component.Stop(stopCtx) }()
	close(releaseRun)
	if err = <-runResult; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err = <-stopResult; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop() with readiness admitted error = %v", err)
	}
	if calls := shutdownCalls.Load(); calls != 0 {
		t.Fatalf("shutdown calls with readiness admitted = %d, want 0", calls)
	}
	close(releaseReadiness)
	if err = <-readinessResult; err != nil {
		t.Fatalf("Readiness() error = %v", err)
	}
	retryCtx, cancelRetry := context.WithTimeout(context.Background(), time.Second)
	defer cancelRetry()
	if err = component.Stop(retryCtx); err != nil {
		t.Fatalf("retry Stop() error = %v", err)
	}
	if calls := shutdownCalls.Load(); calls != 1 {
		t.Fatalf("shutdown calls = %d, want 1", calls)
	}
}

func TestSIGTERMWithdrawsReadinessAndFencesAdmissionOnce(t *testing.T) {
	started := make(chan struct{})
	shutdownStarted := make(chan struct{})
	releaseShutdown := make(chan struct{})
	var shutdownCalls atomic.Int32
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[struct{}]{
			Name:        "producer",
			Resource:    struct{}{},
			Correlation: mustFactory(t, "child"),
			Startup: func(context.Context, struct{}) error {
				close(started)

				return nil
			},
			Publish: func(
				context.Context,
				struct{},
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
			Shutdown: func(context.Context, struct{}) error {
				shutdownCalls.Add(1)
				close(shutdownStarted)
				<-releaseShutdown

				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	runtime := mustService(t, producer.Component())
	signals := make(chan os.Signal, 3)
	runResult := make(chan error, 1)
	go func() {
		runResult <- service.RunWithSignals(
			context.Background(),
			runtime,
			time.Second,
			signals,
		)
	}()
	<-started
	signals <- syscall.SIGTERM
	<-shutdownStarted
	if runtime.Ready() {
		t.Fatal("service remained ready after SIGTERM")
	}
	parent, startErr := mustFactory(t, "parent", "request").Start()
	if startErr != nil {
		t.Fatalf("Start() correlation error = %v", startErr)
	}
	if _, _, publishErr := producer.Publish(
		correlation.WithValues(context.Background(), parent),
		kafka.ProducerRecord{Topic: "orders"},
	); !errors.Is(publishErr, kafkaservice.ErrUnavailable) {
		t.Fatalf("Publish() after SIGTERM error = %v", publishErr)
	}
	signals <- syscall.SIGTERM
	signals <- syscall.SIGTERM
	close(releaseShutdown)
	if err = <-runResult; err != nil {
		t.Fatalf("RunWithSignals() error = %v", err)
	}
	if got := shutdownCalls.Load(); got != 1 {
		t.Fatalf("shutdown calls = %d, want 1", got)
	}
}

func assertRedactedCallbackCause(
	t *testing.T,
	err error,
	cause error,
	operation kafkaservice.CallbackOperation,
) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want retained callback cause", err)
	}
	var callbackErr *kafkaservice.CallbackError
	if !errors.As(err, &callbackErr) || callbackErr.Operation != operation ||
		callbackErr.Err != cause {
		t.Fatalf("error = %#v, want callback operation %d", err, operation)
	}
	assertNoCallbackCause(t, err)
}

func assertNoCallbackCause(t *testing.T, err error) {
	t.Helper()
	for _, format := range []string{"%v", "%+v", "%#v"} {
		formatted := fmt.Sprintf(format, err)
		for _, secret := range []string{"customer-profile", "kafka.internal", "correct-horse"} {
			if strings.Contains(formatted, secret) {
				t.Fatalf("format %q exposed %q: %q", format, secret, formatted)
			}
		}
	}
}

func mustService(t *testing.T, component service.Component) *service.Service {
	t.Helper()
	runtime, err := service.New(service.Config{Components: []service.Component{component}})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}

	return runtime
}

func closeIfOpen(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}
