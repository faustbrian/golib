package kafkaservice_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type sequenceGenerator struct {
	values []string
}

func (generator *sequenceGenerator) New() (string, error) {
	value := generator.values[0]
	generator.values = generator.values[1:]

	return value, nil
}

type producerResource struct {
	context correlation.Values
	record  kafka.ProducerRecord
}

type consumerResource struct {
	runDone       chan struct{}
	shutdownCalls int
}

type nilTracePropagator struct{}

func (*nilTracePropagator) Inject(context.Context, propagation.TextMapCarrier) {}
func (*nilTracePropagator) Extract(
	ctx context.Context,
	_ propagation.TextMapCarrier,
) context.Context {
	return ctx
}
func (*nilTracePropagator) Fields() []string { return nil }

type inspectingTracePropagator struct {
	keys []string
	got  string
}

func (propagator *inspectingTracePropagator) Inject(
	_ context.Context,
	carrier propagation.TextMapCarrier,
) {
	propagator.keys = carrier.Keys()
	propagator.got = carrier.Get("application")
	carrier.Set("trace-custom", "injected")
}

func (*inspectingTracePropagator) Extract(
	ctx context.Context,
	carrier propagation.TextMapCarrier,
) context.Context {
	if carrier.Get("trace-custom") == "remote" {
		carrier.Set("trace-custom", "mutated")

		return context.WithValue(ctx, traceContextKey{}, "extracted")
	}

	return ctx
}

func (*inspectingTracePropagator) Fields() []string { return []string{"trace-custom"} }

type traceContextKey struct{}

type observingContext struct {
	context.Context
	observed  chan struct{}
	wantCalls int
	mu        sync.Mutex
	calls     int
}

func (ctx *observingContext) Done() <-chan struct{} {
	ctx.mu.Lock()
	ctx.calls++
	if ctx.calls == ctx.wantCalls {
		close(ctx.observed)
	}
	ctx.mu.Unlock()

	return ctx.Context.Done()
}

type producerShutdownCanceledContext struct {
	context.Context
	done  <-chan struct{}
	mu    sync.Mutex
	calls int
}

func (ctx *producerShutdownCanceledContext) Done() <-chan struct{} {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.calls++
	if ctx.calls == 1 {
		return nil
	}

	return ctx.done
}

func (*producerShutdownCanceledContext) Err() error {
	return context.Canceled
}

func TestProducerPropagatesCorrelationAndTraceWithoutMutatingCallerRecord(
	t *testing.T,
) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{"kafka-request"}},
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	resource := &producerResource{}
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*producerResource]{
			Name: "events-producer", Resource: resource, Correlation: factory,
			TracePropagator: propagation.TraceContext{},
			Publish: func(
				ctx context.Context,
				resource *producerResource,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				resource.context, _ = correlation.FromContext(ctx)
				resource.record = record

				return kafka.DeliveryResult{Topic: record.Topic, Partition: 2}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	parent := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("workflow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("http-request", correlation.Policy{}),
	}
	traceID := trace.TraceID{1}
	spanID := trace.SpanID{2}
	span := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(
		correlation.WithValues(context.Background(), parent),
		span,
	)
	record := kafka.ProducerRecord{
		Topic: "events",
		Key:   []byte("key"),
		Value: []byte("value"),
		Headers: []kafka.Header{
			{Key: "application", Value: []byte("metadata")},
			{Key: "traceparent", Value: []byte("stale")},
		},
	}
	values, delivery, err := producer.Publish(ctx, record)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if values.CorrelationID != parent.CorrelationID ||
		values.RequestID != "kafka-request" ||
		values.CausationID != "http-request" {
		t.Fatalf("Publish() values = %#v", values)
	}
	if resource.context != values {
		t.Fatalf("publish callback context = %#v, want %#v", resource.context, values)
	}
	if delivery.Topic != record.Topic || delivery.Partition != 2 {
		t.Fatalf("Publish() delivery = %#v", delivery)
	}
	assertHeader(t, resource.record.Headers, "correlation_id", "workflow")
	assertHeader(t, resource.record.Headers, "request_id", "kafka-request")
	assertHeader(t, resource.record.Headers, "causation_id", "http-request")
	assertHeader(
		t,
		resource.record.Headers,
		"traceparent",
		"00-01000000000000000000000000000000-0200000000000000-01",
	)
	assertHeader(t, resource.record.Headers, "application", "metadata")
	assertHeader(t, record.Headers, "traceparent", "stale")
	if len(record.Headers) != 2 {
		t.Fatalf("caller record headers mutated = %#v", record.Headers)
	}
}

func TestHandlerCreatesFreshDeliveryAttemptFromTrustedRecord(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{"delivery-request"}},
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	var (
		handledValues correlation.Values
		handledTrace  trace.SpanContext
	)
	handler, err := kafkaservice.NewHandler(kafkaservice.HandlerOptions{
		Correlation:     factory,
		TrustedMetadata: true,
		TracePropagator: propagation.TraceContext{},
		Handler: kafka.HandlerFunc(func(
			ctx context.Context,
			_ kafka.ConsumedMessage,
		) error {
			handledValues, _ = correlation.FromContext(ctx)
			handledTrace = trace.SpanContextFromContext(ctx)

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	record := kafka.ConsumedMessage{
		Topic: "events",
		Headers: []kafka.Header{
			{Key: "correlation_id", Value: []byte("workflow")},
			{Key: "request_id", Value: []byte("producer-request")},
			{
				Key: "traceparent",
				Value: []byte(
					"00-01000000000000000000000000000000-0200000000000000-01",
				),
			},
		},
	}
	if err = handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if handledValues.CorrelationID != "workflow" ||
		handledValues.RequestID != "delivery-request" ||
		handledValues.CausationID != "producer-request" {
		t.Fatalf("handler values = %#v", handledValues)
	}
	if !handledTrace.IsRemote() ||
		handledTrace.TraceID() != (trace.TraceID{1}) ||
		handledTrace.SpanID() != (trace.SpanID{2}) ||
		!handledTrace.IsSampled() {
		t.Fatalf("handler span context = %v", handledTrace)
	}
}

func TestConsumerPlanRunsCorrelatedHandlerThenShutsDownResource(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{"delivery-request"}},
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	resource := &consumerResource{runDone: make(chan struct{})}
	var handled correlation.Values
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*consumerResource]{
			Name: "events-consumer", Resource: resource, Correlation: factory,
			TrustedMetadata: true,
			Handler: kafka.HandlerFunc(func(
				ctx context.Context,
				_ kafka.ConsumedMessage,
			) error {
				handled, _ = correlation.FromContext(ctx)

				return nil
			}),
			Run: func(
				ctx context.Context,
				_ *consumerResource,
				handler kafka.Handler,
			) error {
				err := handler.Handle(ctx, kafka.ConsumedMessage{
					Topic: "events",
					Headers: []kafka.Header{
						{Key: "correlation_id", Value: []byte("workflow")},
						{Key: "request_id", Value: []byte("producer-request")},
					},
				})
				if err != nil {
					return err
				}
				<-ctx.Done()
				close(resource.runDone)

				return ctx.Err()
			},
			Shutdown: func(context.Context, *consumerResource) error {
				select {
				case <-resource.runDone:
				default:
					t.Fatal("Shutdown ran before the consumer task joined")
				}
				resource.shutdownCalls++

				return nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	if consumer.Resource() != resource {
		t.Fatal("Resource() did not preserve the concrete consumer")
	}
	plan := consumer.Plan()
	if len(plan.Components) != 1 || plan.Components[0].Name != "events-consumer" ||
		len(plan.Tasks) != 1 || plan.Tasks[0].Name != "events-consumer" {
		t.Fatalf("Plan() = %#v", plan)
	}
	if err = plan.Components[0].Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runContext, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- plan.Tasks[0].Run(runContext) }()
	cancel()
	if err = <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if handled.CorrelationID != "workflow" ||
		handled.RequestID != "delivery-request" ||
		handled.CausationID != "producer-request" {
		t.Fatalf("handler values = %#v", handled)
	}
	if err = plan.Components[0].Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if resource.shutdownCalls != 1 {
		t.Fatalf("Shutdown calls = %d, want 1", resource.shutdownCalls)
	}
}

func TestAdapterConstructionRejectsInvalidOptions(t *testing.T) {
	factory := mustFactory(t, "unused")
	resource := &producerResource{}
	validProducer := kafkaservice.ProducerOptions[*producerResource]{
		Name: "producer", Resource: resource, Correlation: factory,
		Publish: func(
			context.Context,
			*producerResource,
			kafka.ProducerRecord,
		) (kafka.DeliveryResult, error) {
			return kafka.DeliveryResult{}, nil
		},
	}
	var typedNilTrace *nilTracePropagator
	producerCases := []struct {
		name   string
		mutate func(*kafkaservice.ProducerOptions[*producerResource])
		field  string
	}{
		{"blank name", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.Name = " "
		}, "Name"},
		{"invalid UTF-8 name", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.Name = string([]byte{0xff})
		}, "Name"},
		{"oversized name", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.Name = strings.Repeat("n", kafkaservice.MaxNameBytes+1)
		}, "Name"},
		{"nil resource", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.Resource = nil
		}, "Resource"},
		{"nil correlation", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.Correlation = nil
		}, "Correlation"},
		{"typed nil trace", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.TracePropagator = typedNilTrace
		}, "TracePropagator"},
		{"nil publish", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.Publish = nil
		}, "Publish"},
		{"invalid message limits", func(options *kafkaservice.ProducerOptions[*producerResource]) {
			options.MessageLimits = kafka.MessageLimits{MaxTopicBytes: 1}
		}, "MessageLimits"},
	}
	for _, test := range producerCases {
		t.Run("producer/"+test.name, func(t *testing.T) {
			options := validProducer
			test.mutate(&options)
			_, err := kafkaservice.NewProducer(options)
			assertOptionsError(t, err, test.field)
		})
	}
	invalidCodec := validProducer
	invalidCodec.CorrelationCodec.Policy.MaxLength = -1
	if _, err := kafkaservice.NewProducer(invalidCodec); !errors.Is(
		err,
		correlation.ErrInvalidCarrier,
	) {
		t.Fatalf("NewProducer(invalid codec) error = %v", err)
	}

	validHandler := kafkaservice.HandlerOptions{
		Correlation: factory,
		Handler:     kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error { return nil }),
	}
	handlerCases := []struct {
		name   string
		mutate func(*kafkaservice.HandlerOptions)
		field  string
	}{
		{"nil correlation", func(options *kafkaservice.HandlerOptions) {
			options.Correlation = nil
		}, "Correlation"},
		{"typed nil trace", func(options *kafkaservice.HandlerOptions) {
			options.TracePropagator = typedNilTrace
		}, "TracePropagator"},
		{"nil handler", func(options *kafkaservice.HandlerOptions) {
			options.Handler = nil
		}, "Handler"},
	}
	for _, test := range handlerCases {
		t.Run("handler/"+test.name, func(t *testing.T) {
			options := validHandler
			test.mutate(&options)
			_, err := kafkaservice.NewHandler(options)
			assertOptionsError(t, err, test.field)
		})
	}
	invalidHandlerCodec := validHandler
	invalidHandlerCodec.CorrelationCodec.Policy.MaxLength = -1
	if _, err := kafkaservice.NewHandler(invalidHandlerCodec); !errors.Is(
		err,
		correlation.ErrInvalidCarrier,
	) {
		t.Fatalf("NewHandler(invalid codec) error = %v", err)
	}

	validConsumer := kafkaservice.ConsumerOptions[*consumerResource]{
		Name: "consumer", Resource: &consumerResource{}, Correlation: factory,
		Handler: validHandler.Handler,
		Run: func(context.Context, *consumerResource, kafka.Handler) error {
			return nil
		},
		Shutdown: func(context.Context, *consumerResource) error { return nil },
	}
	consumerCases := []struct {
		name   string
		mutate func(*kafkaservice.ConsumerOptions[*consumerResource])
		field  string
	}{
		{"blank name", func(options *kafkaservice.ConsumerOptions[*consumerResource]) {
			options.Name = ""
		}, "Name"},
		{"invalid UTF-8 name", func(options *kafkaservice.ConsumerOptions[*consumerResource]) {
			options.Name = string([]byte{0xff})
		}, "Name"},
		{"oversized name", func(options *kafkaservice.ConsumerOptions[*consumerResource]) {
			options.Name = strings.Repeat("n", kafkaservice.MaxNameBytes+1)
		}, "Name"},
		{"nil resource", func(options *kafkaservice.ConsumerOptions[*consumerResource]) {
			options.Resource = nil
		}, "Resource"},
		{"nil run", func(options *kafkaservice.ConsumerOptions[*consumerResource]) {
			options.Run = nil
		}, "Run"},
		{"nil shutdown", func(options *kafkaservice.ConsumerOptions[*consumerResource]) {
			options.Shutdown = nil
		}, "Shutdown"},
		{"invalid handler", func(options *kafkaservice.ConsumerOptions[*consumerResource]) {
			options.Handler = nil
		}, "Handler"},
	}
	for _, test := range consumerCases {
		t.Run("consumer/"+test.name, func(t *testing.T) {
			options := validConsumer
			test.mutate(&options)
			_, err := kafkaservice.NewConsumer(options)
			assertOptionsError(t, err, test.field)
		})
	}
}

func TestProducerBoundsRecordBeforeCopyAndAfterPropagation(t *testing.T) {
	parent, err := mustFactory(t, "workflow", "request").Start()
	if err != nil {
		t.Fatalf("Start() correlation error = %v", err)
	}
	publishCalls := 0
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*producerResource]{
			Name: "producer", Resource: &producerResource{},
			Correlation: mustFactory(
				t,
				"oversized-child",
				"header-child",
			),
			Publish: func(
				context.Context,
				*producerResource,
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				publishCalls++

				return kafka.DeliveryResult{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx := correlation.WithValues(context.Background(), parent)
	_, _, err = producer.Publish(ctx, kafka.ProducerRecord{
		Topic: "orders",
		Value: make([]byte, kafka.DefaultMessageLimits().MaxValueBytes+1),
	})
	if !errors.Is(err, kafka.ErrValueTooLarge) {
		t.Fatalf("oversized Publish() error = %v", err)
	}
	headers := make(
		[]kafka.Header,
		kafka.DefaultMessageLimits().MaxHeaders,
	)
	for index := range headers {
		headers[index] = kafka.Header{Key: "application"}
	}
	values, _, err := producer.Publish(ctx, kafka.ProducerRecord{
		Topic:   "orders",
		Headers: headers,
	})
	if !errors.Is(err, kafka.ErrTooManyHeaders) ||
		values.RequestID.String() != "oversized-child" {
		t.Fatalf("propagated-header Publish() = %#v, %v", values, err)
	}
	if publishCalls != 0 {
		t.Fatalf("publish calls = %d, want 0", publishCalls)
	}
}

func TestProducerLifecycleValidatesReadinessDrainsAndRetriesShutdown(t *testing.T) {
	readinessErr := errors.New("readiness")
	shutdownErr := errors.New("shutdown")
	type resourceState struct {
		readinessErr   error
		shutdownErr    error
		startupCalls   int
		shutdownCalls  int
		publishStarted chan struct{}
		releasePublish chan struct{}
	}
	resource := &resourceState{
		readinessErr: readinessErr, shutdownErr: shutdownErr,
		publishStarted: make(chan struct{}), releasePublish: make(chan struct{}),
	}
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*resourceState]{
			Name: "producer", Resource: resource, Correlation: mustFactory(t, "child"),
			Startup: func(_ context.Context, resource *resourceState) error {
				resource.startupCalls++

				return nil
			},
			Readiness: func(_ context.Context, resource *resourceState) error {
				return resource.readinessErr
			},
			Publish: func(
				_ context.Context,
				resource *resourceState,
				_ kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				close(resource.publishStarted)
				<-resource.releasePublish

				return kafka.DeliveryResult{Offset: 9}, nil
			},
			Shutdown: func(_ context.Context, resource *resourceState) error {
				resource.shutdownCalls++

				return resource.shutdownErr
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if producer.Resource() != resource {
		t.Fatal("Resource() did not preserve the concrete producer")
	}
	readiness, ok := producer.Readiness()
	if !ok || readiness.Name != "producer" {
		t.Fatalf("Readiness() = %#v, %v", readiness, ok)
	}
	if err = readiness.Run(context.Background()); !errors.Is(err, kafkaservice.ErrUnavailable) {
		t.Fatalf("readiness before start = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if resource.startupCalls != 1 {
		t.Fatalf("startup calls = %d, want 1", resource.startupCalls)
	}
	if err = readiness.Run(context.Background()); !errors.Is(err, readinessErr) {
		t.Fatalf("readiness after start = %v", err)
	}
	parent, _ := mustFactory(t, "workflow", "parent").Start()
	publishResult := make(chan error, 1)
	go func() {
		_, _, publishErr := producer.Publish(
			correlation.WithValues(context.Background(), parent),
			kafka.ProducerRecord{Topic: "events"},
		)
		publishResult <- publishErr
	}()
	<-resource.publishStarted
	stopContext, cancelStop := context.WithCancel(context.Background())
	cancelStop()
	if err = component.Stop(stopContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("first Stop() error = %v, want context.Canceled", err)
	}
	if _, _, err = producer.Publish(
		correlation.WithValues(context.Background(), parent),
		kafka.ProducerRecord{Topic: "events"},
	); !errors.Is(err, kafkaservice.ErrUnavailable) {
		t.Fatalf("Publish() while draining error = %v", err)
	}
	close(resource.releasePublish)
	if err = <-publishResult; err != nil {
		t.Fatalf("in-flight Publish() error = %v", err)
	}
	if err = component.Stop(context.Background()); !errors.Is(err, shutdownErr) {
		t.Fatalf("second Stop() error = %v", err)
	}
	if err = component.Stop(context.Background()); !errors.Is(err, shutdownErr) {
		t.Fatalf("repeated Stop() error = %v", err)
	}
	if resource.shutdownCalls != 2 {
		t.Fatalf("shutdown calls = %d, want 2", resource.shutdownCalls)
	}
	if err = component.Start(context.Background()); !errors.Is(err, kafkaservice.ErrUnavailable) {
		t.Fatalf("Start() after stop error = %v", err)
	}
	if err = readiness.Run(context.Background()); !errors.Is(err, kafkaservice.ErrUnavailable) {
		t.Fatalf("readiness after stop = %v", err)
	}
}

func TestProducerStartupFailureBeginsCleanupAndPreservesFailures(t *testing.T) {
	validationErr := errors.New("secret validation detail")
	cleanupErr := errors.New("secret cleanup detail")
	resource := &consumerResource{}
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*consumerResource]{
			Name: "producer", Resource: resource, Correlation: mustFactory(t, "unused"),
			Startup: func(context.Context, *consumerResource) error {
				return validationErr
			},
			Publish: func(
				context.Context,
				*consumerResource,
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
			Shutdown: func(context.Context, *consumerResource) error {
				resource.shutdownCalls++

				return cleanupErr
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	err = component.Start(context.Background())
	if !errors.Is(err, validationErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Start() error = %v", err)
	}
	var startupErr *kafkaservice.StartupError
	var validationCallback *kafkaservice.CallbackError
	var cleanupCallback *kafkaservice.CallbackError
	if !errors.As(err, &startupErr) ||
		!errors.As(startupErr.Validation, &validationCallback) ||
		validationCallback.Operation != kafkaservice.CallbackStartup ||
		validationCallback.Err != validationErr ||
		!errors.As(startupErr.Cleanup, &cleanupCallback) ||
		cleanupCallback.Operation != kafkaservice.CallbackShutdown ||
		cleanupCallback.Err != cleanupErr ||
		err.Error() != "kafka service startup validation and cleanup failed" ||
		strings.Contains(err.Error(), validationErr.Error()) ||
		strings.Contains(err.Error(), cleanupErr.Error()) {
		t.Fatalf("Start() classification = %#v, %v", startupErr, err)
	}
	if stopErr := component.Stop(context.Background()); !errors.Is(stopErr, cleanupErr) {
		t.Fatalf("Stop() after failed start error = %v", stopErr)
	}
	if resource.shutdownCalls != 2 {
		t.Fatalf("shutdown calls = %d, want 2", resource.shutdownCalls)
	}
}

func TestProducerStartupFailureWithoutOwnershipIsSecretSafe(t *testing.T) {
	validationErr := errors.New("secret broker detail")
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*producerResource]{
			Name: "producer", Resource: &producerResource{},
			Correlation: mustFactory(t, "unused"),
			Startup: func(context.Context, *producerResource) error {
				return validationErr
			},
			Publish: func(
				context.Context,
				*producerResource,
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	err = producer.Component().Start(context.Background())
	var startupErr *kafkaservice.StartupError
	if !errors.Is(err, validationErr) || !errors.As(err, &startupErr) ||
		startupErr.Cleanup != nil ||
		err.Error() != "kafka service startup validation failed" ||
		strings.Contains(err.Error(), validationErr.Error()) {
		t.Fatalf("Start() error = %#v, %v", startupErr, err)
	}
}

func TestProducerPublishErrorsPreserveChildValuesAndReservedMetadata(t *testing.T) {
	publishErr := errors.New("publish")
	calls := 0
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*producerResource]{
			Name: "producer", Resource: &producerResource{},
			Correlation: mustFactory(t, "child", "next-child"),
			Publish: func(
				context.Context,
				*producerResource,
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				calls++

				return kafka.DeliveryResult{Offset: 4}, publishErr
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if _, ok := producer.Readiness(); ok {
		t.Fatal("Readiness() reported an unconfigured check")
	}
	if _, _, err = producer.Publish(context.Background(), kafka.ProducerRecord{}); !errors.Is(
		err,
		kafkaservice.ErrUnavailable,
	) {
		t.Fatalf("Publish() before start error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, _, err = producer.Publish(context.Background(), kafka.ProducerRecord{}); !errors.Is(
		err,
		kafkaservice.ErrMissingCorrelation,
	) {
		t.Fatalf("Publish() without parent error = %v", err)
	}
	parent, _ := mustFactory(t, "workflow", "parent").Start()
	values, delivery, err := producer.Publish(
		correlation.WithValues(context.Background(), parent),
		kafka.ProducerRecord{Topic: "orders", Headers: []kafka.Header{{
			Key: "correlation_id", Value: []byte("application"),
		}}},
	)
	if !errors.Is(err, correlation.ErrCarrierOverwrite) ||
		values != (correlation.Values{}) || delivery != (kafka.DeliveryResult{}) {
		t.Fatalf("Publish(reserved header) = %#v, %#v, %v", values, delivery, err)
	}
	values, delivery, err = producer.Publish(
		correlation.WithValues(context.Background(), parent),
		kafka.ProducerRecord{Topic: "orders"},
	)
	if !errors.Is(err, publishErr) || values.RequestID != "next-child" ||
		delivery.Offset != 4 || calls != 1 {
		t.Fatalf("Publish(callback error) = %#v, %#v, %v, calls=%d", values, delivery, err, calls)
	}
	if err = component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop(shared producer) error = %v", err)
	}
}

func TestHandlerReplacesInvalidOrUntrustedMetadataAndCanReject(t *testing.T) {
	var untrusted correlation.Values
	handler, err := kafkaservice.NewHandler(kafkaservice.HandlerOptions{
		Correlation: mustFactory(t, "local-workflow", "local-request"),
		Handler: kafka.HandlerFunc(func(
			ctx context.Context,
			_ kafka.ConsumedMessage,
		) error {
			untrusted, _ = correlation.FromContext(ctx)

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	record := kafka.ConsumedMessage{Headers: []kafka.Header{
		{Key: "correlation_id", Value: []byte("remote-workflow")},
		{Key: "request_id", Value: []byte("remote-request")},
	}}
	if err = handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle(untrusted) error = %v", err)
	}
	if untrusted.CorrelationID != "local-workflow" ||
		untrusted.RequestID != "local-request" || untrusted.CausationID != "" {
		t.Fatalf("untrusted values = %#v", untrusted)
	}

	var replaced correlation.Values
	handler, err = kafkaservice.NewHandler(kafkaservice.HandlerOptions{
		Correlation:     mustFactory(t, "replacement-workflow", "replacement-request"),
		TrustedMetadata: true,
		Handler: kafka.HandlerFunc(func(
			ctx context.Context,
			_ kafka.ConsumedMessage,
		) error {
			replaced, _ = correlation.FromContext(ctx)

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler(replace) error = %v", err)
	}
	invalid := kafka.ConsumedMessage{Headers: []kafka.Header{{
		Key: "correlation_id", Value: []byte("contains spaces"),
	}}}
	if err = handler.Handle(context.Background(), invalid); err != nil {
		t.Fatalf("Handle(invalid replacement) error = %v", err)
	}
	if replaced.CorrelationID != "replacement-workflow" ||
		replaced.RequestID != "replacement-request" {
		t.Fatalf("replacement values = %#v", replaced)
	}

	called := false
	handler, err = kafkaservice.NewHandler(kafkaservice.HandlerOptions{
		Correlation: mustFactory(t, "unused"), TrustedMetadata: true,
		RejectInvalidMetadata: true,
		Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
			called = true

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler(reject) error = %v", err)
	}
	if err = handler.Handle(context.Background(), invalid); !errors.Is(
		err,
		correlation.ErrInvalidCarrier,
	) {
		t.Fatalf("Handle(invalid rejection) error = %v", err)
	}
	if called {
		t.Fatal("invalid metadata reached application handler")
	}
}

func TestCustomCorrelationAndTraceCarriersRemainExplicit(t *testing.T) {
	tracePropagator := &inspectingTracePropagator{}
	resource := &producerResource{}
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*producerResource]{
			Name: "producer", Resource: resource, Correlation: mustFactory(t, "child"),
			CorrelationCodec: correlation.CodecOptions{
				CorrelationField: "workflow",
				RequestField:     "attempt",
				CausationField:   "parent",
			},
			TracePropagator: tracePropagator,
			Publish: func(
				_ context.Context,
				resource *producerResource,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				resource.record = record

				return kafka.DeliveryResult{}, nil
			},
		},
	)
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	parent, _ := mustFactory(t, "workflow-id", "parent-id").Start()
	record := kafka.ProducerRecord{Topic: "orders", Headers: []kafka.Header{
		{Key: "application", Value: []byte("present")},
		{Key: "TRACE-CUSTOM", Value: []byte("stale")},
	}}
	if _, _, err = producer.Publish(
		correlation.WithValues(context.Background(), parent),
		record,
	); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	assertHeader(t, resource.record.Headers, "workflow", "workflow-id")
	assertHeader(t, resource.record.Headers, "attempt", "child")
	assertHeader(t, resource.record.Headers, "parent", "parent-id")
	assertHeader(t, resource.record.Headers, "trace-custom", "injected")
	if tracePropagator.got != "present" ||
		!contains(tracePropagator.keys, "application") {
		t.Fatalf("trace carrier observed keys=%v application=%q", tracePropagator.keys, tracePropagator.got)
	}

	var (
		extracted       string
		receivedHeaders []kafka.Header
	)
	handler, err := kafkaservice.NewHandler(kafkaservice.HandlerOptions{
		Correlation: mustFactory(t, "delivery"), TrustedMetadata: true,
		CorrelationCodec: correlation.CodecOptions{
			CorrelationField: "workflow",
			RequestField:     "attempt",
			CausationField:   "parent",
		},
		TracePropagator: tracePropagator,
		Handler: kafka.HandlerFunc(func(
			ctx context.Context,
			record kafka.ConsumedMessage,
		) error {
			extracted, _ = ctx.Value(traceContextKey{}).(string)
			receivedHeaders = record.Headers

			return nil
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if err = handler.Handle(context.Background(), kafka.ConsumedMessage{
		Headers: []kafka.Header{
			{Key: "workflow", Value: []byte("workflow-id")},
			{Key: "attempt", Value: []byte("child")},
			{Key: "trace-custom", Value: []byte("remote")},
		},
	}); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if extracted != "extracted" {
		t.Fatalf("trace extraction = %q", extracted)
	}
	assertHeader(t, receivedHeaders, "trace-custom", "remote")
}

func TestConsumerStartupReadinessAndFailureCleanup(t *testing.T) {
	validationErr := errors.New("validation")
	cleanupErr := errors.New("cleanup")
	resource := &consumerResource{}
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*consumerResource]{
			Name: "consumer", Resource: resource, Correlation: mustFactory(t, "unused"),
			Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
				return nil
			}),
			Startup: func(context.Context, *consumerResource) error {
				return validationErr
			},
			Readiness: func(context.Context, *consumerResource) error { return nil },
			Run: func(context.Context, *consumerResource, kafka.Handler) error {
				return nil
			},
			Shutdown: func(context.Context, *consumerResource) error {
				resource.shutdownCalls++

				return cleanupErr
			},
		},
	)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	plan := consumer.Plan()
	if len(plan.Readiness) != 1 || plan.Readiness[0].Name != "consumer" {
		t.Fatalf("Plan().Readiness = %#v", plan.Readiness)
	}
	if err = plan.Readiness[0].Run(context.Background()); !errors.Is(
		err,
		kafkaservice.ErrUnavailable,
	) {
		t.Fatalf("readiness before start error = %v", err)
	}
	if err = plan.Tasks[0].Run(context.Background()); !errors.Is(
		err,
		kafkaservice.ErrUnavailable,
	) {
		t.Fatalf("task before start error = %v", err)
	}
	err = plan.Components[0].Start(context.Background())
	if !errors.Is(err, validationErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("Start() error = %v", err)
	}
	if err = plan.Components[0].Stop(context.Background()); !errors.Is(err, cleanupErr) {
		t.Fatalf("Stop() error = %v", err)
	}
	if resource.shutdownCalls != 2 {
		t.Fatalf("shutdown calls = %d, want 2", resource.shutdownCalls)
	}
	if err = plan.Components[0].Start(context.Background()); !errors.Is(
		err,
		kafkaservice.ErrUnavailable,
	) {
		t.Fatalf("Start() after cleanup error = %v", err)
	}
}

func TestConsumerReadinessRunAndShutdownErrorsRemainClassifiable(t *testing.T) {
	readinessErr := errors.New("readiness")
	runErr := errors.New("run")
	shutdownErr := errors.New("shutdown")
	resource := &consumerResource{}
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*consumerResource]{
			Name: "consumer", Resource: resource, Correlation: mustFactory(t, "unused"),
			Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
				return nil
			}),
			Readiness: func(context.Context, *consumerResource) error {
				return readinessErr
			},
			Run: func(context.Context, *consumerResource, kafka.Handler) error {
				return runErr
			},
			Shutdown: func(context.Context, *consumerResource) error {
				resource.shutdownCalls++

				return shutdownErr
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
	if err = plan.Readiness[0].Run(context.Background()); !errors.Is(err, readinessErr) {
		t.Fatalf("Readiness error = %v", err)
	}
	if err = plan.Tasks[0].Run(context.Background()); !errors.Is(err, runErr) {
		t.Fatalf("Run error = %v", err)
	}
	if err = plan.Components[0].Stop(context.Background()); !errors.Is(err, shutdownErr) {
		t.Fatalf("Stop error = %v", err)
	}
	if err = plan.Components[0].Stop(context.Background()); !errors.Is(err, shutdownErr) {
		t.Fatalf("repeated Stop error = %v", err)
	}
	if resource.shutdownCalls != 2 {
		t.Fatalf("shutdown calls = %d, want 2", resource.shutdownCalls)
	}
	if err = plan.Tasks[0].Run(context.Background()); !errors.Is(
		err,
		kafkaservice.ErrUnavailable,
	) {
		t.Fatalf("Run after stop error = %v", err)
	}
}

func TestHandlerPreservesApplicationFailure(t *testing.T) {
	handlerErr := errors.New("handler")
	handler, err := kafkaservice.NewHandler(kafkaservice.HandlerOptions{
		Correlation: mustFactory(t, "workflow", "request"),
		Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
			return handlerErr
		}),
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if err = handler.Handle(context.Background(), kafka.ConsumedMessage{}); !errors.Is(
		err,
		handlerErr,
	) {
		t.Fatalf("Handle() error = %v", err)
	}
}

func TestConcurrentProducerStopsObserveOneShutdownResult(t *testing.T) {
	shutdownErr := errors.New("shutdown")
	started := make(chan struct{})
	release := make(chan struct{})
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*producerResource]{
			Name: "producer", Resource: &producerResource{},
			Correlation: mustFactory(t, "unused"),
			Publish: func(
				context.Context,
				*producerResource,
				kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				return kafka.DeliveryResult{}, nil
			},
			Shutdown: func(context.Context, *producerResource) error {
				close(started)
				<-release

				return shutdownErr
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
	first := make(chan error, 1)
	go func() { first <- component.Stop(context.Background()) }()
	<-started
	observed := make(chan struct{})
	waiterContext := &observingContext{
		Context:   context.Background(),
		observed:  observed,
		wantCalls: 2,
	}
	waiter := make(chan error, 1)
	go func() { waiter <- component.Stop(waiterContext) }()
	<-observed
	canceled := make(chan struct{})
	close(canceled)
	cancelContext := &producerShutdownCanceledContext{
		Context: context.Background(),
		done:    canceled,
	}
	if err = component.Stop(cancelContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("concurrent Stop(canceled) error = %v", err)
	}
	close(release)
	if err = <-first; !errors.Is(err, shutdownErr) {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err = <-waiter; !errors.Is(err, shutdownErr) {
		t.Fatalf("waiting Stop() error = %v", err)
	}
}

func TestConcurrentConsumerStopsObserveOneShutdownResult(t *testing.T) {
	shutdownErr := errors.New("shutdown")
	started := make(chan struct{})
	release := make(chan struct{})
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*consumerResource]{
			Name: "consumer", Resource: &consumerResource{},
			Correlation: mustFactory(t, "unused"),
			Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
				return nil
			}),
			Run: func(context.Context, *consumerResource, kafka.Handler) error {
				return nil
			},
			Shutdown: func(context.Context, *consumerResource) error {
				close(started)
				<-release

				return shutdownErr
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
	first := make(chan error, 1)
	go func() { first <- component.Stop(context.Background()) }()
	<-started
	observed := make(chan struct{})
	waiterContext := &observingContext{
		Context:   context.Background(),
		observed:  observed,
		wantCalls: 1,
	}
	waiter := make(chan error, 1)
	go func() { waiter <- component.Stop(waiterContext) }()
	<-observed
	canceled := make(chan struct{})
	close(canceled)
	cancelContext := &producerShutdownCanceledContext{
		Context: context.Background(),
		done:    canceled,
	}
	if err = component.Stop(cancelContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("concurrent Stop(canceled) error = %v", err)
	}
	close(release)
	if err = <-first; !errors.Is(err, shutdownErr) {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err = <-waiter; !errors.Is(err, shutdownErr) {
		t.Fatalf("waiting Stop() error = %v", err)
	}
}

func TestFailedShutdownCanBeRetried(t *testing.T) {
	t.Run("producer", func(t *testing.T) {
		shutdownCalls := 0
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[*producerResource]{
				Name: "producer", Resource: &producerResource{},
				Correlation: mustFactory(t, "unused"),
				Publish: func(
					context.Context,
					*producerResource,
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					return kafka.DeliveryResult{}, nil
				},
				Shutdown: func(context.Context, *producerResource) error {
					shutdownCalls++
					if shutdownCalls == 1 {
						return context.DeadlineExceeded
					}

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
		if err = component.Stop(context.Background()); !errors.Is(
			err,
			context.DeadlineExceeded,
		) {
			t.Fatalf("first Stop() error = %v", err)
		}
		if err = component.Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
		if err = component.Stop(context.Background()); err != nil {
			t.Fatalf("completed Stop() error = %v", err)
		}
		if shutdownCalls != 2 {
			t.Fatalf("shutdown calls = %d, want 2", shutdownCalls)
		}
	})

	t.Run("consumer", func(t *testing.T) {
		shutdownCalls := 0
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[*consumerResource]{
				Name: "consumer", Resource: &consumerResource{},
				Correlation: mustFactory(t, "unused"),
				Handler: kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					return nil
				}),
				Run: func(
					context.Context,
					*consumerResource,
					kafka.Handler,
				) error {
					return nil
				},
				Shutdown: func(context.Context, *consumerResource) error {
					shutdownCalls++
					if shutdownCalls == 1 {
						return context.DeadlineExceeded
					}

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
		if err = component.Stop(context.Background()); !errors.Is(
			err,
			context.DeadlineExceeded,
		) {
			t.Fatalf("first Stop() error = %v", err)
		}
		if err = component.Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
		if err = component.Stop(context.Background()); err != nil {
			t.Fatalf("completed Stop() error = %v", err)
		}
		if shutdownCalls != 2 {
			t.Fatalf("shutdown calls = %d, want 2", shutdownCalls)
		}
	})
}

func TestConcurrentStartAndStopLeaveAdaptersUnavailable(t *testing.T) {
	t.Run("producer", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		released := false
		defer func() {
			if !released {
				close(release)
			}
		}()
		shutdownStarted := make(chan struct{})
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[*producerResource]{
				Name: "producer", Resource: &producerResource{},
				Correlation: mustFactory(t, "unused"),
				Startup: func(context.Context, *producerResource) error {
					close(started)
					<-release

					return nil
				},
				Publish: func(
					context.Context,
					*producerResource,
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					return kafka.DeliveryResult{}, nil
				},
				Shutdown: func(context.Context, *producerResource) error {
					close(shutdownStarted)

					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		component := producer.Component()
		result := make(chan error, 1)
		go func() { result <- component.Start(context.Background()) }()
		<-started
		stopContext, cancel := context.WithCancel(context.Background())
		observed := make(chan struct{})
		stopResult := make(chan error, 1)
		go func() {
			stopResult <- component.Stop(&observingContext{
				Context: stopContext, observed: observed, wantCalls: 1,
			})
		}()
		<-observed
		cancel()
		if err = <-stopResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop() during startup error = %v", err)
		}
		select {
		case <-shutdownStarted:
			t.Fatal("Shutdown ran before Startup returned")
		default:
		}
		close(release)
		released = true
		if err = <-result; !errors.Is(err, kafkaservice.ErrUnavailable) {
			t.Fatalf("concurrent Start() error = %v", err)
		}
		if err = component.Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
	})

	t.Run("consumer", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		released := false
		defer func() {
			if !released {
				close(release)
			}
		}()
		shutdownStarted := make(chan struct{})
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[*consumerResource]{
				Name: "consumer", Resource: &consumerResource{},
				Correlation: mustFactory(t, "unused"),
				Handler: kafka.HandlerFunc(func(context.Context, kafka.ConsumedMessage) error {
					return nil
				}),
				Startup: func(context.Context, *consumerResource) error {
					close(started)
					<-release

					return nil
				},
				Run: func(context.Context, *consumerResource, kafka.Handler) error {
					return nil
				},
				Shutdown: func(context.Context, *consumerResource) error {
					close(shutdownStarted)

					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewConsumer() error = %v", err)
		}
		plan := consumer.Plan()
		result := make(chan error, 1)
		go func() { result <- plan.Components[0].Start(context.Background()) }()
		<-started
		stopContext, cancel := context.WithCancel(context.Background())
		observed := make(chan struct{})
		stopResult := make(chan error, 1)
		go func() {
			stopResult <- plan.Components[0].Stop(&observingContext{
				Context: stopContext, observed: observed, wantCalls: 1,
			})
		}()
		select {
		case <-observed:
		case <-shutdownStarted:
			t.Fatal("Shutdown ran before Startup returned")
		}
		cancel()
		if err = <-stopResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop() during startup error = %v", err)
		}
		close(release)
		released = true
		if err = <-result; !errors.Is(err, kafkaservice.ErrUnavailable) {
			t.Fatalf("concurrent Start() error = %v", err)
		}
		if err = plan.Components[0].Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
	})
}

func TestConcurrentAndRepeatedStartsDoNotReenterStartup(t *testing.T) {
	t.Run("producer", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[*producerResource]{
				Name: "producer", Resource: &producerResource{},
				Correlation: mustFactory(t, "unused"),
				Startup: func(context.Context, *producerResource) error {
					close(started)
					<-release

					return nil
				},
				Publish: func(
					context.Context,
					*producerResource,
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					return kafka.DeliveryResult{}, nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		component := producer.Component()
		first := make(chan error, 1)
		go func() { first <- component.Start(context.Background()) }()
		<-started
		if err = component.Start(context.Background()); !errors.Is(
			err,
			kafkaservice.ErrUnavailable,
		) {
			t.Fatalf("concurrent Start() error = %v", err)
		}
		close(release)
		if err = <-first; err != nil {
			t.Fatalf("first Start() error = %v", err)
		}
		if err = component.Start(context.Background()); err != nil {
			t.Fatalf("repeated Start() error = %v", err)
		}
	})

	t.Run("consumer", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[*consumerResource]{
				Name: "consumer", Resource: &consumerResource{},
				Correlation: mustFactory(t, "unused"),
				Handler: kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					return nil
				}),
				Startup: func(context.Context, *consumerResource) error {
					close(started)
					<-release

					return nil
				},
				Run: func(
					context.Context,
					*consumerResource,
					kafka.Handler,
				) error {
					return nil
				},
				Shutdown: func(context.Context, *consumerResource) error {
					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewConsumer() error = %v", err)
		}
		component := consumer.Plan().Components[0]
		first := make(chan error, 1)
		go func() { first <- component.Start(context.Background()) }()
		<-started
		if err = component.Start(context.Background()); !errors.Is(
			err,
			kafkaservice.ErrUnavailable,
		) {
			t.Fatalf("concurrent Start() error = %v", err)
		}
		close(release)
		if err = <-first; err != nil {
			t.Fatalf("first Start() error = %v", err)
		}
		if err = component.Start(context.Background()); err != nil {
			t.Fatalf("repeated Start() error = %v", err)
		}
	})
}

func TestStopDoesNotShutdownResourcesUsedByActiveCallbacks(t *testing.T) {
	t.Run("producer readiness", func(t *testing.T) {
		callbackStarted := make(chan struct{})
		releaseCallback := make(chan struct{})
		released := false
		defer func() {
			if !released {
				close(releaseCallback)
			}
		}()
		shutdownStarted := make(chan struct{})
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[*producerResource]{
				Name: "producer", Resource: &producerResource{},
				Correlation: mustFactory(t, "unused"),
				Readiness: func(context.Context, *producerResource) error {
					close(callbackStarted)
					<-releaseCallback

					return nil
				},
				Publish: func(
					context.Context,
					*producerResource,
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					return kafka.DeliveryResult{}, nil
				},
				Shutdown: func(context.Context, *producerResource) error {
					close(shutdownStarted)

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
		callbackResult := make(chan error, 1)
		go func() { callbackResult <- readiness.Run(context.Background()) }()
		<-callbackStarted
		stopContext, cancel := context.WithCancel(context.Background())
		observed := make(chan struct{})
		stopResult := make(chan error, 1)
		go func() {
			stopResult <- component.Stop(&observingContext{
				Context: stopContext, observed: observed, wantCalls: 1,
			})
		}()
		<-observed
		cancel()
		if err = <-stopResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop() during readiness error = %v", err)
		}
		select {
		case <-shutdownStarted:
			t.Fatal("Shutdown ran before Readiness returned")
		default:
		}
		close(releaseCallback)
		released = true
		if err = <-callbackResult; err != nil {
			t.Fatalf("Readiness() error = %v", err)
		}
		if err = component.Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
	})

	t.Run("consumer run", func(t *testing.T) {
		callbackStarted := make(chan struct{})
		releaseCallback := make(chan struct{})
		released := false
		defer func() {
			if !released {
				close(releaseCallback)
			}
		}()
		shutdownStarted := make(chan struct{})
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[*consumerResource]{
				Name: "consumer", Resource: &consumerResource{},
				Correlation: mustFactory(t, "unused"),
				Handler: kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					return nil
				}),
				Run: func(
					context.Context,
					*consumerResource,
					kafka.Handler,
				) error {
					close(callbackStarted)
					<-releaseCallback

					return nil
				},
				Shutdown: func(context.Context, *consumerResource) error {
					close(shutdownStarted)

					return nil
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
		callbackResult := make(chan error, 1)
		go func() { callbackResult <- plan.Tasks[0].Run(context.Background()) }()
		<-callbackStarted
		stopContext, cancel := context.WithCancel(context.Background())
		observed := make(chan struct{})
		stopResult := make(chan error, 1)
		go func() {
			stopResult <- plan.Components[0].Stop(&observingContext{
				Context: stopContext, observed: observed, wantCalls: 1,
			})
		}()
		select {
		case <-observed:
		case <-shutdownStarted:
			t.Fatal("Shutdown ran before Run returned")
		}
		cancel()
		if err = <-stopResult; !errors.Is(err, context.Canceled) {
			t.Fatalf("Stop() during run error = %v", err)
		}
		close(releaseCallback)
		released = true
		if err = <-callbackResult; err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if err = plan.Components[0].Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
	})
}

func TestCallbackPanicsAreContainedAndShutdownRemainsRetryable(t *testing.T) {
	if got := (&kafkaservice.CallbackPanicError{}).Error(); !strings.Contains(
		got,
		"unknown",
	) {
		t.Fatalf("zero CallbackPanicError = %q", got)
	}

	t.Run("producer callbacks", func(t *testing.T) {
		shutdownCalls := 0
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[*producerResource]{
				Name: "producer", Resource: &producerResource{},
				Correlation: mustFactory(t, "child"),
				Readiness: func(context.Context, *producerResource) error {
					panic("readiness secret")
				},
				Publish: func(
					context.Context,
					*producerResource,
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					panic("publish secret")
				},
				Shutdown: func(context.Context, *producerResource) error {
					shutdownCalls++
					if shutdownCalls == 1 {
						panic("shutdown secret")
					}

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
		assertCallbackPanic(
			t,
			readiness.Run(context.Background()),
			kafkaservice.CallbackReadiness,
		)
		parent, startErr := mustFactory(t, "parent", "parent-request").Start()
		if startErr != nil {
			t.Fatalf("Start() correlation error = %v", startErr)
		}
		_, _, err = producer.Publish(
			correlation.WithValues(context.Background(), parent),
			kafka.ProducerRecord{Topic: "orders"},
		)
		assertCallbackPanic(t, err, kafkaservice.CallbackPublish)
		assertCallbackPanic(
			t,
			component.Stop(context.Background()),
			kafkaservice.CallbackShutdown,
		)
		if err = component.Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
		if shutdownCalls != 2 {
			t.Fatalf("shutdown calls = %d, want 2", shutdownCalls)
		}
	})

	t.Run("producer startup", func(t *testing.T) {
		producer, err := kafkaservice.NewProducer(
			kafkaservice.ProducerOptions[*producerResource]{
				Name: "producer", Resource: &producerResource{},
				Correlation: mustFactory(t, "unused"),
				Startup: func(context.Context, *producerResource) error {
					panic("startup secret")
				},
				Publish: func(
					context.Context,
					*producerResource,
					kafka.ProducerRecord,
				) (kafka.DeliveryResult, error) {
					return kafka.DeliveryResult{}, nil
				},
				Shutdown: func(context.Context, *producerResource) error {
					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		err = producer.Component().Start(context.Background())
		var startupErr *kafkaservice.StartupError
		if !errors.As(err, &startupErr) {
			t.Fatalf("Start() error = %v, want StartupError", err)
		}
		assertCallbackPanic(t, startupErr.Validation, kafkaservice.CallbackStartup)
	})

	t.Run("consumer callbacks", func(t *testing.T) {
		shutdownCalls := 0
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[*consumerResource]{
				Name: "consumer", Resource: &consumerResource{},
				Correlation: mustFactory(t, "unused"),
				Readiness: func(context.Context, *consumerResource) error {
					panic("readiness secret")
				},
				Handler: kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					return nil
				}),
				Run: func(context.Context, *consumerResource, kafka.Handler) error {
					panic("run secret")
				},
				Shutdown: func(context.Context, *consumerResource) error {
					shutdownCalls++
					if shutdownCalls == 1 {
						panic("shutdown secret")
					}

					return nil
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
		assertCallbackPanic(
			t,
			plan.Readiness[0].Run(context.Background()),
			kafkaservice.CallbackReadiness,
		)
		assertCallbackPanic(
			t,
			plan.Tasks[0].Run(context.Background()),
			kafkaservice.CallbackRun,
		)
		assertCallbackPanic(
			t,
			plan.Components[0].Stop(context.Background()),
			kafkaservice.CallbackShutdown,
		)
		if err = plan.Components[0].Stop(context.Background()); err != nil {
			t.Fatalf("retry Stop() error = %v", err)
		}
	})

	t.Run("consumer startup and handler", func(t *testing.T) {
		consumer, err := kafkaservice.NewConsumer(
			kafkaservice.ConsumerOptions[*consumerResource]{
				Name: "consumer", Resource: &consumerResource{},
				Correlation: mustFactory(t, "unused"),
				Handler: kafka.HandlerFunc(func(
					context.Context,
					kafka.ConsumedMessage,
				) error {
					panic("handler secret")
				}),
				Startup: func(context.Context, *consumerResource) error {
					panic("startup secret")
				},
				Run: func(context.Context, *consumerResource, kafka.Handler) error {
					return nil
				},
				Shutdown: func(context.Context, *consumerResource) error {
					return nil
				},
			},
		)
		if err != nil {
			t.Fatalf("NewConsumer() error = %v", err)
		}
		err = consumer.Plan().Components[0].Start(context.Background())
		var startupErr *kafkaservice.StartupError
		if !errors.As(err, &startupErr) {
			t.Fatalf("Start() error = %v, want StartupError", err)
		}
		assertCallbackPanic(t, startupErr.Validation, kafkaservice.CallbackStartup)

		handler, handlerErr := kafkaservice.NewHandler(kafkaservice.HandlerOptions{
			Correlation: mustFactory(t, "workflow", "request"),
			Handler: kafka.HandlerFunc(func(
				context.Context,
				kafka.ConsumedMessage,
			) error {
				panic("handler secret")
			}),
		})
		if handlerErr != nil {
			t.Fatalf("NewHandler() error = %v", handlerErr)
		}
		assertCallbackPanic(
			t,
			handler.Handle(context.Background(), kafka.ConsumedMessage{}),
			kafkaservice.CallbackHandler,
		)
	})
}

func TestStartupErrorFormattingDoesNotExposeCauses(t *testing.T) {
	startupErr := &kafkaservice.StartupError{
		Validation: errors.New("validation secret"),
		Cleanup:    errors.New("cleanup secret"),
	}
	for _, format := range []string{"%v", "%+v", "%#v"} {
		formatted := fmt.Sprintf(format, startupErr)
		if strings.Contains(formatted, "secret") {
			t.Fatalf("format %q exposed cause: %q", format, formatted)
		}
	}
}

func mustFactory(t *testing.T, values ...string) *correlation.Factory {
	t.Helper()
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: values},
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}

	return factory
}

func assertCallbackPanic(
	t *testing.T,
	err error,
	operation kafkaservice.CallbackOperation,
) {
	t.Helper()
	var panicErr *kafkaservice.CallbackPanicError
	if !errors.Is(err, kafkaservice.ErrCallbackPanic) ||
		!errors.As(err, &panicErr) ||
		panicErr.Operation != operation ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("error = %v, want redacted %d CallbackPanicError", err, operation)
	}
}

func assertOptionsError(t *testing.T, err error, field string) {
	t.Helper()
	var optionsErr *kafkaservice.OptionsError
	if !errors.Is(err, kafkaservice.ErrInvalidOptions) ||
		!errors.As(err, &optionsErr) || optionsErr.Field != field ||
		!strings.Contains(err.Error(), field) {
		t.Fatalf("error = %v, want OptionsError for %s", err, field)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func assertHeader(t *testing.T, headers []kafka.Header, key, want string) {
	t.Helper()
	var values []string
	for _, header := range headers {
		if header.Key == key {
			values = append(values, string(header.Value))
		}
	}
	if len(values) != 1 || values[0] != want {
		t.Fatalf("header %q = %v, want [%q]", key, values, want)
	}
}
