package queueservice

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/correlation"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type testGenerator struct {
	mu     sync.Mutex
	values []string
}

func (generator *testGenerator) New() (string, error) {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	value := generator.values[0]
	generator.values = generator.values[1:]

	return value, nil
}

type queuedPayload string

func (payload queuedPayload) Bytes() []byte { return []byte(payload) }

type plainTask string

func (task plainTask) Bytes() []byte   { return []byte(task) }
func (task plainTask) Payload() []byte { return []byte(task) }

type producerResource struct{}

type testTracePropagator struct {
	inject map[string]string
}

func (propagator *testTracePropagator) Inject(
	_ context.Context,
	carrier propagation.TextMapCarrier,
) {
	for key, value := range propagator.inject {
		carrier.Set(key, value)
	}
}

func (*testTracePropagator) Extract(
	ctx context.Context,
	_ propagation.TextMapCarrier,
) context.Context {
	return ctx
}

func (propagator *testTracePropagator) Fields() []string {
	fields := make([]string, 0, len(propagator.inject))
	for key := range propagator.inject {
		fields = append(fields, key)
	}

	return fields
}

func TestProducerPropagatesCorrelationAndHandlerCreatesDeliveryAttempt(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &testGenerator{values: []string{"message-request", "delivery-request"}},
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	var captured *job.Message
	var publishedContextValues correlation.Values
	enqueuedAt := time.Unix(10, 0).UTC()
	originalEnqueuedAt := enqueuedAt
	metadata := &job.Metadata{
		JobType: "delivery", EnqueuedAt: &enqueuedAt,
		Tags: map[string]string{"source": "test"},
	}
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name:        "jobs-producer",
		Resource:    &producerResource{},
		Correlation: factory,
		Publish: func(
			ctx context.Context,
			_ *producerResource,
			message core.QueuedMessage,
			options ...job.AllowOption,
		) error {
			queued := job.NewMessage(message, options...)
			captured = &queued
			publishedContextValues, _ = correlation.FromContext(ctx)

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
	parent := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("workflow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("http-request", correlation.Policy{}),
	}
	messageValues, err := producer.Publish(
		correlation.WithValues(context.Background(), parent),
		queuedPayload("payload"),
		job.AllowOption{Metadata: metadata},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if messageValues.CorrelationID != parent.CorrelationID ||
		messageValues.RequestID != "message-request" ||
		messageValues.CausationID != "http-request" {
		t.Fatalf("Publish() values = %#v", messageValues)
	}
	if publishedContextValues != messageValues {
		t.Fatalf(
			"publish callback context values = %#v, want %#v",
			publishedContextValues,
			messageValues,
		)
	}
	if captured == nil || captured.Metadata.JobType != "delivery" {
		t.Fatal("Publish() did not preserve application job metadata")
	}
	metadata.Tags["source"] = "mutated"
	*metadata.EnqueuedAt = time.Unix(20, 0).UTC()
	if captured.Metadata.Tags["source"] != "test" ||
		!captured.Metadata.EnqueuedAt.Equal(originalEnqueuedAt) {
		t.Fatal("Publish() retained caller-owned metadata aliases")
	}

	var deliveryValues correlation.Values
	handler, err := NewHandler(HandlerOptions{
		Correlation:     factory,
		TrustedMetadata: true,
		Handler: func(ctx context.Context, _ core.TaskMessage) error {
			var ok bool
			deliveryValues, ok = correlation.FromContext(ctx)
			if !ok {
				t.Fatal("handler context did not contain correlation values")
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if err = handler(context.Background(), captured); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if deliveryValues.CorrelationID != parent.CorrelationID ||
		deliveryValues.RequestID != "delivery-request" ||
		deliveryValues.CausationID != "message-request" {
		t.Fatalf("delivery values = %#v", deliveryValues)
	}
}

func TestProducerAndHandlerPropagateCallerOwnedTraceContext(t *testing.T) {
	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	const spanID = "00f067aa0ba902b7"
	traceIdentifier, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		t.Fatalf("TraceIDFromHex() error = %v", err)
	}
	spanIdentifier, err := trace.SpanIDFromHex(spanID)
	if err != nil {
		t.Fatalf("SpanIDFromHex() error = %v", err)
	}
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceIdentifier, SpanID: spanIdentifier,
		TraceFlags: trace.FlagsSampled,
	})
	propagator := propagation.TraceContext{}
	var captured *job.Message
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name: "jobs-producer", Resource: &producerResource{},
		Correlation: mustFactory(t), TracePropagator: propagator,
		Publish: func(
			_ context.Context,
			_ *producerResource,
			message core.QueuedMessage,
			options ...job.AllowOption,
		) error {
			queued := job.NewMessage(message, options...)
			captured = &queued

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	ctx := trace.ContextWithSpanContext(producerContext(), parent)
	if _, err = producer.Publish(ctx, queuedPayload("payload")); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if captured == nil || captured.Metadata == nil ||
		captured.Metadata.TraceContext["traceparent"] == "" {
		t.Fatal("Publish() did not inject trace context")
	}

	var extracted trace.SpanContext
	handler, err := NewHandler(HandlerOptions{
		Correlation: mustFactory(t), TracePropagator: propagator,
		Handler: func(ctx context.Context, _ core.TaskMessage) error {
			extracted = trace.SpanContextFromContext(ctx)

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if err = handler(context.Background(), captured); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if !extracted.IsRemote() || extracted.TraceID() != traceIdentifier ||
		extracted.SpanID() != spanIdentifier || !extracted.IsSampled() {
		t.Fatalf("extracted span context = %v", extracted)
	}
}

func TestProducerReplacesPrepopulatedTraceContext(t *testing.T) {
	var captured *job.Message
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name: "jobs-producer", Resource: &producerResource{},
		Correlation: mustFactory(t), TracePropagator: propagation.TraceContext{},
		Publish: func(
			_ context.Context,
			_ *producerResource,
			message core.QueuedMessage,
			options ...job.AllowOption,
		) error {
			queued := job.NewMessage(message, options...)
			captured = &queued

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	metadata := &job.Metadata{TraceContext: map[string]string{
		"traceparent": "application-supplied",
	}}
	if _, err = producer.Publish(
		producerContext(),
		queuedPayload("payload"),
		job.AllowOption{Metadata: metadata},
	); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if captured == nil || captured.Metadata == nil {
		t.Fatal("Publish() did not capture job metadata")
	}
	if captured.Metadata.TraceContext != nil {
		t.Fatalf("Publish() retained stale trace context = %v", captured.Metadata.TraceContext)
	}
	if metadata.TraceContext["traceparent"] != "application-supplied" {
		t.Fatal("Publish() mutated caller-owned trace metadata")
	}
}

func TestTracePropagationRejectsUnboundedMetadataBeforeApplicationCode(t *testing.T) {
	publishCalls := 0
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name: "jobs-producer", Resource: &producerResource{},
		Correlation: mustFactory(t),
		TracePropagator: &testTracePropagator{inject: map[string]string{
			"traceparent": string(make([]byte, job.MaxTraceContextValueBytes+1)),
		}},
		Publish: func(
			context.Context,
			*producerResource,
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
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err = producer.Publish(
		producerContext(),
		queuedPayload("payload"),
	); !errors.Is(err, job.ErrInvalidMessage) {
		t.Fatalf("Publish() error = %v, want invalid message", err)
	}
	if publishCalls != 0 {
		t.Fatalf("Publish callback calls = %d, want 0", publishCalls)
	}

	handlerCalls := 0
	handler, err := NewHandler(HandlerOptions{
		Correlation:     mustFactory(t),
		TracePropagator: &testTracePropagator{},
		Handler: func(context.Context, core.TaskMessage) error {
			handlerCalls++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	message := &job.Message{Metadata: &job.Metadata{TraceContext: map[string]string{
		"traceparent": string(make([]byte, job.MaxTraceContextValueBytes+1)),
	}}}
	if err = handler(context.Background(), message); !errors.Is(err, job.ErrInvalidMessage) {
		t.Fatalf("handler() error = %v, want invalid message", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("application handler calls = %d, want 0", handlerCalls)
	}
}

func TestProducerDropsEmptyTraceCarrierFields(t *testing.T) {
	var captured *job.Message
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name: "jobs-producer", Resource: &producerResource{},
		Correlation: mustFactory(t),
		TracePropagator: &testTracePropagator{inject: map[string]string{
			"traceparent": " ",
		}},
		Publish: func(
			_ context.Context,
			_ *producerResource,
			message core.QueuedMessage,
			options ...job.AllowOption,
		) error {
			queued := job.NewMessage(message, options...)
			captured = &queued

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if err = producer.Component().Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err = producer.Publish(producerContext(), queuedPayload("payload")); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if captured == nil || captured.Metadata == nil {
		t.Fatal("Publish() did not capture job metadata")
	}
	if captured.Metadata.TraceContext != nil {
		t.Fatalf("Publish() retained empty trace fields = %v", captured.Metadata.TraceContext)
	}
}

func TestHandlerDoesNotTrustMetadataByDefault(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &testGenerator{values: []string{"fresh-workflow", "fresh-request"}},
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	message := job.NewMessage(
		queuedPayload("payload"),
		job.AllowOption{Metadata: &job.Metadata{Correlation: map[string]string{
			"correlation_id": "untrusted-workflow",
			"request_id":     "untrusted-request",
		}}},
	)
	var values correlation.Values
	handler, err := NewHandler(HandlerOptions{
		Correlation: factory,
		Handler: func(ctx context.Context, _ core.TaskMessage) error {
			values, _ = correlation.FromContext(ctx)

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if err = handler(context.Background(), &message); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if values.CorrelationID != "fresh-workflow" ||
		values.RequestID != "fresh-request" ||
		values.CausationID != "" {
		t.Fatalf("untrusted delivery values = %#v", values)
	}
}

func TestHandlerSupportsAbsentCarriersAndRejectsMalformedMetadata(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &testGenerator{values: []string{"workflow", "request"}},
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	calls := 0
	handler, err := NewHandler(HandlerOptions{
		Correlation: factory,
		Handler: func(ctx context.Context, _ core.TaskMessage) error {
			calls++
			if values, ok := correlation.FromContext(ctx); !ok ||
				values.CorrelationID == "" || values.RequestID == "" {
				t.Fatal("handler did not receive generated correlation")
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if err = handler(context.Background(), plainTask("payload")); err != nil {
		t.Fatalf("handler() error = %v", err)
	}

	malformed := &job.Message{Metadata: &job.Metadata{
		Correlation: map[string]string{"correlation_id": "bad value"},
	}}
	if err = handler(context.Background(), malformed); err == nil {
		t.Fatal("handler accepted malformed correlation metadata")
	}
	if calls != 1 {
		t.Fatalf("application handler calls = %d, want 1", calls)
	}
}

func TestProducerDrainsPublishersBeforeOwnedShutdown(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	shutdown := make(chan struct{})
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name:        "jobs-producer",
		Resource:    &producerResource{},
		Correlation: mustFactory(t),
		Publish: func(
			_ context.Context,
			_ *producerResource,
			message core.QueuedMessage,
			_ ...job.AllowOption,
		) error {
			if string(message.Bytes()) == "work" {
				close(entered)
				<-release
			}

			return nil
		},
		Shutdown: func(context.Context, *producerResource) error {
			close(shutdown)

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
	published := make(chan error, 1)
	go func() {
		_, publishErr := producer.Publish(producerContext(), queuedPayload("work"))
		published <- publishErr
	}()
	<-entered

	stopped := make(chan error, 1)
	go func() { stopped <- component.Stop(context.Background()) }()
	for {
		_, err = producer.Publish(producerContext(), queuedPayload("late"))
		if errors.Is(err, ErrUnavailable) {
			break
		}
		if err != nil {
			t.Fatalf("Publish() during drain error = %v", err)
		}
	}
	select {
	case <-shutdown:
		t.Fatal("resource shut down before active publisher drained")
	default:
	}

	close(release)
	if err = <-published; err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err = <-stopped; err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-shutdown:
	default:
		t.Fatal("owned producer was not shut down after drain")
	}
}

func TestProducerRejectsPrepopulatedCorrelationMetadata(t *testing.T) {
	publishCalls := 0
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name:        "jobs-producer",
		Resource:    &producerResource{},
		Correlation: mustFactory(t),
		Publish: func(
			context.Context,
			*producerResource,
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
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err = producer.Publish(
		producerContext(),
		queuedPayload("work"),
		job.AllowOption{Metadata: &job.Metadata{Correlation: map[string]string{
			"correlation_id": "application-value",
		}}},
	)
	if !errors.Is(err, correlation.ErrCarrierOverwrite) {
		t.Fatalf("Publish() error = %v, want carrier overwrite", err)
	}
	if publishCalls != 0 {
		t.Fatalf("Publish callback calls = %d, want 0", publishCalls)
	}
}

func TestCanceledProducerDrainCanBeResumed(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	shutdownCalls := 0
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name:        "jobs-producer",
		Resource:    &producerResource{},
		Correlation: mustFactory(t),
		Publish: func(
			context.Context,
			*producerResource,
			core.QueuedMessage,
			...job.AllowOption,
		) error {
			close(entered)
			<-release

			return nil
		},
		Shutdown: func(context.Context, *producerResource) error {
			shutdownCalls++

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
	published := make(chan struct{})
	go func() {
		_, _ = producer.Publish(producerContext(), queuedPayload("work"))
		close(published)
	}()
	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = component.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop() error = %v, want context cancellation", err)
	}
	if shutdownCalls != 0 {
		t.Fatalf("Shutdown() calls = %d before drain, want 0", shutdownCalls)
	}
	close(release)
	<-published
	if err = component.Stop(context.Background()); err != nil {
		t.Fatalf("resumed Stop() error = %v", err)
	}
	if shutdownCalls != 1 {
		t.Fatalf("Shutdown() calls = %d, want 1", shutdownCalls)
	}
}

func TestProducerShutdownIsSingleFlightAndRetainsFailure(t *testing.T) {
	shutdownStarted := make(chan struct{})
	releaseShutdown := make(chan struct{})
	shutdownErr := errors.New("shutdown failed")
	resource := &producerResource{}
	producer, err := NewProducer(ProducerOptions[*producerResource]{
		Name:        "jobs-producer",
		Resource:    resource,
		Correlation: mustFactory(t),
		Publish:     noPublish,
		Shutdown: func(context.Context, *producerResource) error {
			close(shutdownStarted)
			<-releaseShutdown

			return shutdownErr
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	if producer.Resource() != resource {
		t.Fatal("Resource() did not preserve the concrete producer")
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	first := make(chan error, 1)
	go func() { first <- component.Stop(context.Background()) }()
	<-shutdownStarted

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = component.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("concurrent Stop() error = %v, want context cancellation", err)
	}
	close(releaseShutdown)
	if err = <-first; !errors.Is(err, shutdownErr) {
		t.Fatalf("first Stop() error = %v, want shutdown failure", err)
	}
	if err = component.Stop(context.Background()); !errors.Is(err, shutdownErr) {
		t.Fatalf("repeated Stop() error = %v, want shutdown failure", err)
	}
	if err = component.Start(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("restart error = %v, want ErrUnavailable", err)
	}
}

func TestSharedProducerAndPublishFailuresRemainExplicit(t *testing.T) {
	publishErr := errors.New("publish failed")
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "shared-producer", Resource: 1, Correlation: mustFactory(t),
		Publish: func(
			context.Context,
			int,
			core.QueuedMessage,
			...job.AllowOption,
		) error {
			return publishErr
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	values, err := producer.Publish(producerContext(), queuedPayload("work"))
	if !errors.Is(err, publishErr) || values.CorrelationID == "" ||
		values.RequestID == "" {
		t.Fatalf("Publish() = (%#v, %v), want values and publish failure", values, err)
	}
	if _, err = producer.Publish(context.Background(), queuedPayload("work")); !errors.Is(err, ErrMissingCorrelation) {
		t.Fatalf("Publish() without parent error = %v", err)
	}
	if err = component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err = component.Stop(context.Background()); err != nil {
		t.Fatalf("repeated Stop() error = %v", err)
	}
}

func TestWorkerComponentUsesConcreteQueueLifecycle(t *testing.T) {
	coordinator := queue.NewPool(1)
	worker, err := NewWorker(WorkerOptions{Name: "jobs-worker", Queue: coordinator})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if worker.Queue() != coordinator {
		t.Fatal("Queue() did not expose the concrete queue")
	}
	component := worker.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = component.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestAdaptersRejectInvalidOptions(t *testing.T) {
	var nilResource *producerResource
	var nilTracePropagator *testTracePropagator
	producerTests := []ProducerOptions[*producerResource]{
		{},
		{Name: "producer", Resource: nilResource, Correlation: mustFactory(t), Publish: noPublish},
		{Name: "producer", Resource: &producerResource{}, Publish: noPublish},
		{Name: "producer", Resource: &producerResource{}, Correlation: mustFactory(t)},
		{
			Name: "producer", Resource: &producerResource{},
			Correlation: mustFactory(t), Publish: noPublish,
			TracePropagator: nilTracePropagator,
		},
		{
			Name: "producer", Resource: &producerResource{},
			Correlation: mustFactory(t), Publish: noPublish,
			CorrelationOptions: queuecorrelation.Options{
				Codec: correlation.CodecOptions{Policy: correlation.Policy{MaxLength: -1}},
			},
		},
	}
	for _, options := range producerTests {
		if _, err := NewProducer(options); !errors.Is(err, ErrInvalidOptions) &&
			!errors.Is(err, correlation.ErrInvalidCarrier) {
			t.Fatalf("NewProducer() error = %v", err)
		} else if errors.Is(err, ErrInvalidOptions) && err.Error() == "" {
			t.Fatal("option error did not provide a safe diagnostic")
		}
	}
	if _, err := NewProducer(ProducerOptions[any]{
		Name: "producer", Resource: nil,
		Correlation: mustFactory(t),
		Publish: func(
			context.Context,
			any,
			core.QueuedMessage,
			...job.AllowOption,
		) error {
			return nil
		},
	}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("NewProducer(nil interface) error = %v", err)
	}

	if _, err := NewWorker(WorkerOptions{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("NewWorker() error = %v", err)
	}
	if _, err := NewWorker(WorkerOptions{Name: "worker"}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("NewWorker(nil queue) error = %v", err)
	}
	if _, err := NewHandler(HandlerOptions{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("NewHandler() error = %v", err)
	}
	if _, err := NewHandler(HandlerOptions{
		Correlation: mustFactory(t),
	}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("NewHandler(nil handler) error = %v", err)
	}
	if _, err := NewHandler(HandlerOptions{
		Correlation:     mustFactory(t),
		TracePropagator: nilTracePropagator,
		Handler:         func(context.Context, core.TaskMessage) error { return nil },
	}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("NewHandler(nil trace propagator) error = %v", err)
	}
	if _, err := NewHandler(HandlerOptions{
		Correlation: mustFactory(t),
		CorrelationOptions: queuecorrelation.Options{
			Codec: correlation.CodecOptions{Policy: correlation.Policy{MaxLength: -1}},
		},
		Handler: func(context.Context, core.TaskMessage) error { return nil },
	}); !errors.Is(err, correlation.ErrInvalidCarrier) {
		t.Fatalf("NewHandler(invalid codec) error = %v", err)
	}
}

func mustFactory(t *testing.T) *correlation.Factory {
	t.Helper()
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}

	return factory
}

func noPublish(
	context.Context,
	*producerResource,
	core.QueuedMessage,
	...job.AllowOption,
) error {
	return nil
}

func producerContext() context.Context {
	return correlation.WithValues(context.Background(), correlation.Values{
		CorrelationID: correlation.MustCorrelationID("workflow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("parent-request", correlation.Policy{}),
	})
}
