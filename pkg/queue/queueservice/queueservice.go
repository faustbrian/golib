// Package queueservice composes explicit queue producers and workers with the
// service lifecycle and the existing correlation queue semantics.
//
// Producer resources remain concrete and caller-visible. A non-nil Shutdown
// transfers close ownership; shared resources omit it. Service shutdown first
// rejects new publishes, waits for active publishes within the service
// context, and only then closes an owned transport. Publish and shutdown retry
// policy remains owned by their callbacks and concrete clients.
//
// Worker adapters retain an explicit *queue.Queue. Startup starts that queue;
// shutdown stops intake, joins admitted work within the service context, and
// then releases the concrete worker transport. NewHandler must wrap the
// backend's application handler to create a new request ID for every delivery.
// TrustedMetadata must be enabled only after authenticating the immediate
// queue boundary.
package queueservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/correlation"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/service"
	"go.opentelemetry.io/otel/propagation"
)

var (
	// ErrInvalidOptions identifies invalid adapter construction.
	ErrInvalidOptions = errors.New("invalid queue service options")
	// ErrUnavailable reports an inactive or draining producer.
	ErrUnavailable = errors.New("queue service producer unavailable")
	// ErrMissingCorrelation reports a publish without an explicit parent
	// workflow. Callers beginning new work must start it with their factory.
	ErrMissingCorrelation = errors.New("queue service producer correlation missing")
)

// OptionsError identifies one rejected option.
type OptionsError struct {
	// Field identifies the rejected option.
	Field string
	// Reason describes the safe failure category.
	Reason string
}

// Error returns a secret-safe construction diagnostic.
func (err *OptionsError) Error() string {
	return fmt.Sprintf("%s: %s: %v", err.Field, err.Reason, ErrInvalidOptions)
}

// Unwrap exposes the stable option classification.
func (err *OptionsError) Unwrap() error { return ErrInvalidOptions }

// Publish appends one correlation-aware message through a concrete resource.
type Publish[R any] func(
	context.Context,
	R,
	core.QueuedMessage,
	...job.AllowOption,
) error

// Shutdown releases an explicitly transferred producer resource.
type Shutdown[R any] func(context.Context, R) error

// ProducerOptions configure one producer lifecycle adapter.
type ProducerOptions[R any] struct {
	// Name is the secret-safe component name.
	Name string
	// Resource is the caller-constructed concrete producer.
	Resource R
	// Correlation creates message-hop identifiers.
	Correlation *correlation.Factory
	// CorrelationOptions configure the existing queue propagation adapter.
	CorrelationOptions queuecorrelation.Options
	// TracePropagator explicitly injects caller-owned telemetry context when
	// configured. Nil disables trace propagation.
	TracePropagator propagation.TextMapPropagator
	// Publish performs one concrete, caller-bounded append.
	Publish Publish[R]
	// Shutdown transfers transport close ownership when non-nil.
	Shutdown Shutdown[R]
}

// Producer retains a concrete producer and coordinates its in-flight calls.
type Producer[R any] struct {
	name        string
	resource    R
	propagation *queuecorrelation.Adapter
	trace       propagation.TextMapPropagator
	publish     Publish[R]
	shutdown    Shutdown[R]

	mu              sync.Mutex
	active          bool
	stopping        bool
	inflight        int
	drained         chan struct{}
	shutdownRunning bool
	shutdownDone    chan struct{}
	shutdownErr     error
}

// NewProducer validates and constructs an inert producer adapter.
func NewProducer[R any](options ProducerOptions[R]) (*Producer[R], error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, &OptionsError{Field: "Name", Reason: "must not be blank"}
	}
	if nilValue(options.Resource) {
		return nil, &OptionsError{Field: "Resource", Reason: "must not be nil"}
	}
	if options.Correlation == nil {
		return nil, &OptionsError{Field: "Correlation", Reason: "must not be nil"}
	}
	if options.TracePropagator != nil && nilValue(options.TracePropagator) {
		return nil, &OptionsError{Field: "TracePropagator", Reason: "must not be nil"}
	}
	if options.Publish == nil {
		return nil, &OptionsError{Field: "Publish", Reason: "must not be nil"}
	}
	correlationAdapter, err := queuecorrelation.New(
		options.Correlation,
		options.CorrelationOptions,
	)
	if err != nil {
		return nil, err
	}
	shutdown := options.Shutdown
	if shutdown == nil {
		shutdown = func(context.Context, R) error { return nil }
	}

	return &Producer[R]{
		name: options.Name, resource: options.Resource,
		propagation: correlationAdapter, trace: options.TracePropagator,
		publish:  options.Publish,
		shutdown: shutdown,
	}, nil
}

// Resource returns the exact caller-provided producer.
func (producer *Producer[R]) Resource() R { return producer.resource }

// Component returns the producer's ordered service lifecycle component.
func (producer *Producer[R]) Component() service.Component {
	return service.Component{
		Name: producer.name,
		Start: func(context.Context) error {
			producer.mu.Lock()
			defer producer.mu.Unlock()
			if producer.stopping {
				return ErrUnavailable
			}
			producer.active = true

			return nil
		},
		Stop: producer.stop,
	}
}

// Publish creates a message hop, attaches its carrier to cloned job metadata,
// and invokes the concrete producer with that child correlation in its
// context. Correlation values are also returned for caller-owned logging and
// telemetry.
func (producer *Producer[R]) Publish(
	ctx context.Context,
	message core.QueuedMessage,
	options ...job.AllowOption,
) (correlation.Values, error) {
	producer.mu.Lock()
	if !producer.active || producer.stopping {
		producer.mu.Unlock()
		return correlation.Values{}, ErrUnavailable
	}
	producer.inflight++
	producer.mu.Unlock()
	defer producer.finishPublish()

	parent, ok := correlation.FromContext(ctx)
	if !ok {
		return correlation.Values{}, ErrMissingCorrelation
	}
	carrier := correlationCarrier(options)
	values, err := producer.propagation.Send(carrier, parent)
	if err != nil {
		return correlation.Values{}, err
	}
	traceCarrier := propagation.MapCarrier{}
	if producer.trace != nil {
		producer.trace.Inject(ctx, traceCarrier)
		removeEmptyFields(traceCarrier)
	}
	transportOptions := withTransportMetadata(options, carrier, traceCarrier)
	if err = transportOptions[0].Metadata.Validate(); err != nil {
		return values, err
	}

	return values, producer.publish(
		correlation.WithValues(ctx, values),
		producer.resource,
		message,
		transportOptions...,
	)
}

func correlationCarrier(options []job.AllowOption) map[string]string {
	carrier := make(map[string]string, 3)
	if len(options) == 0 || options[0].Metadata == nil {
		return carrier
	}
	for key, value := range options[0].Metadata.Correlation {
		carrier[key] = value
	}

	return carrier
}

func (producer *Producer[R]) finishPublish() {
	producer.mu.Lock()
	defer producer.mu.Unlock()
	producer.inflight--
	if producer.stopping && producer.inflight == 0 && producer.drained != nil {
		close(producer.drained)
		producer.drained = nil
	}
}

func (producer *Producer[R]) stop(ctx context.Context) error {
	producer.mu.Lock()
	producer.active = false
	producer.stopping = true
	if producer.drained == nil {
		producer.drained = make(chan struct{})
		if producer.inflight == 0 {
			close(producer.drained)
		}
	}
	drained := producer.drained
	producer.mu.Unlock()

	select {
	case <-drained:
	default:
		select {
		case <-drained:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return producer.shutdownResource(ctx)
}

func (producer *Producer[R]) shutdownResource(ctx context.Context) error {
	producer.mu.Lock()
	if producer.shutdownDone == nil {
		producer.shutdownDone = make(chan struct{})
	}
	done := producer.shutdownDone
	switch {
	case producer.shutdownRunning:
		producer.mu.Unlock()
		select {
		case <-done:
			producer.mu.Lock()
			err := producer.shutdownErr
			producer.mu.Unlock()

			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		producer.shutdownRunning = true
		producer.mu.Unlock()
	}

	err := producer.shutdown(ctx, producer.resource)
	producer.mu.Lock()
	producer.shutdownErr = err
	close(done)
	producer.mu.Unlock()

	return err
}

// Handler is the queue worker handler signature.
type Handler func(context.Context, core.TaskMessage) error

// HandlerOptions configure a correlation-aware delivery boundary.
type HandlerOptions struct {
	// Correlation creates a new request ID for every delivery attempt.
	Correlation *correlation.Factory
	// CorrelationOptions configure the existing queue propagation adapter.
	CorrelationOptions queuecorrelation.Options
	// TrustedMetadata preserves inbound correlation only when explicitly true.
	TrustedMetadata bool
	// TracePropagator explicitly extracts caller-owned telemetry context when
	// configured. Nil disables trace propagation.
	TracePropagator propagation.TextMapPropagator
	// Handler performs application-owned work.
	Handler Handler
}

// NewHandler wraps application work with the existing queue receive boundary.
func NewHandler(options HandlerOptions) (Handler, error) {
	if options.Correlation == nil {
		return nil, &OptionsError{Field: "Correlation", Reason: "must not be nil"}
	}
	if options.TracePropagator != nil && nilValue(options.TracePropagator) {
		return nil, &OptionsError{Field: "TracePropagator", Reason: "must not be nil"}
	}
	if options.Handler == nil {
		return nil, &OptionsError{Field: "Handler", Reason: "must not be nil"}
	}
	correlationAdapter, err := queuecorrelation.New(
		options.Correlation,
		options.CorrelationOptions,
	)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, message core.TaskMessage) error {
		var metadata map[string]string
		if carrier, ok := message.(interface {
			CorrelationMetadata() map[string]string
		}); ok {
			metadata = carrier.CorrelationMetadata()
		}
		values, receiveErr := correlationAdapter.Receive(
			metadata,
			options.TrustedMetadata,
		)
		if receiveErr != nil {
			return receiveErr
		}
		if options.TracePropagator != nil {
			var traceMetadata map[string]string
			if carrier, ok := message.(interface {
				TraceContextMetadata() map[string]string
			}); ok {
				traceMetadata = carrier.TraceContextMetadata()
			}
			if err := (job.Metadata{TraceContext: traceMetadata}).Validate(); err != nil {
				return err
			}
			ctx = options.TracePropagator.Extract(
				ctx,
				propagation.MapCarrier(traceMetadata),
			)
		}

		return options.Handler(correlation.WithValues(ctx, values), message)
	}, nil
}

// WorkerOptions configure one concrete queue worker lifecycle.
type WorkerOptions struct {
	// Name is the secret-safe component name.
	Name string
	// Queue is the caller-constructed concrete queue.
	Queue *queue.Queue
}

// Worker retains one concrete queue.
type Worker struct {
	name  string
	queue *queue.Queue
}

// NewWorker validates and constructs an inert worker lifecycle adapter.
func NewWorker(options WorkerOptions) (*Worker, error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, &OptionsError{Field: "Name", Reason: "must not be blank"}
	}
	if options.Queue == nil {
		return nil, &OptionsError{Field: "Queue", Reason: "must not be nil"}
	}

	return &Worker{name: options.Name, queue: options.Queue}, nil
}

// Queue returns the exact caller-provided queue.
func (worker *Worker) Queue() *queue.Queue { return worker.queue }

// Component starts the queue and gracefully releases it during service stop.
func (worker *Worker) Component() service.Component {
	return service.Component{
		Name: worker.name,
		Start: func(context.Context) error {
			worker.queue.Start()

			return nil
		},
		Stop: worker.queue.ReleaseContext,
	}
}

func withTransportMetadata(
	options []job.AllowOption,
	correlationCarrier map[string]string,
	traceCarrier propagation.MapCarrier,
) []job.AllowOption {
	copied := append([]job.AllowOption(nil), options...)
	if len(copied) == 0 {
		copied = []job.AllowOption{{}}
	}
	metadata := cloneMetadata(copied[0].Metadata)
	if metadata == nil {
		metadata = &job.Metadata{}
	}
	metadata.Correlation = make(map[string]string, len(correlationCarrier))
	for key, value := range correlationCarrier {
		metadata.Correlation[key] = value
	}
	if len(traceCarrier) == 0 {
		metadata.TraceContext = nil
	} else {
		metadata.TraceContext = make(map[string]string, len(traceCarrier))
		for key, value := range traceCarrier {
			metadata.TraceContext[key] = value
		}
	}
	copied[0].Metadata = metadata

	return copied
}

func cloneMetadata(metadata *job.Metadata) *job.Metadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	if metadata.EnqueuedAt != nil {
		value := *metadata.EnqueuedAt
		clone.EnqueuedAt = &value
	}
	if metadata.Tags != nil {
		clone.Tags = make(map[string]string, len(metadata.Tags))
		for key, value := range metadata.Tags {
			clone.Tags[key] = value
		}
	}
	if metadata.TraceContext != nil {
		clone.TraceContext = make(map[string]string, len(metadata.TraceContext))
		for key, value := range metadata.TraceContext {
			clone.TraceContext[key] = value
		}
	}
	return &clone
}

func removeEmptyFields(carrier propagation.MapCarrier) {
	for key, value := range carrier {
		if strings.TrimSpace(value) == "" {
			delete(carrier, key)
		}
	}
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	//exhaustive:ignore only nil-capable kinds need special handling
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
